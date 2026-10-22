package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

func randomID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixMilli(), hex.EncodeToString(b))
}
func randomNonce() string { b := make([]byte, 16); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func secret() ([]byte, error) {
	s := os.Getenv("TOKEN_SECRET")
	if len(s) < 32 {
		return nil, errors.New("TOKEN_SECRET 至少需要 32 字节")
	}
	return []byte(s), nil
}
func signToken(c Claims) (string, error) {
	key, err := secret()
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(c)
	payload := base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func verifyToken(token, kind string) (Claims, error) {
	var c Claims
	key, err := secret()
	if err != nil {
		return c, err
	}
	parts := strings.Split(strings.TrimPrefix(token, "Bearer "), ".")
	if len(parts) != 2 {
		return c, errors.New("登录已失效")
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return c, errors.New("登录已失效")
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(parts[0]))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return c, errors.New("登录已失效")
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return c, errors.New("登录已失效")
	}
	if err = json.Unmarshal(b, &c); err != nil {
		return c, errors.New("登录已失效")
	}
	if c.Kind != kind || c.Exp <= time.Now().Unix() {
		return c, errors.New("登录已过期")
	}
	return c, nil
}
func miniToken(openid string) (string, error) {
	return signToken(Claims{SessionID: randomNonce() + randomNonce(), Subject: openid, Kind: "mini", Exp: time.Now().Add(7 * 24 * time.Hour).Unix()})
}
func adminToken(a Admin) (string, error) {
	return signToken(Claims{SessionID: randomNonce() + randomNonce(), Subject: a.ID, Kind: "admin", Username: a.Username, Role: a.Role, Credential: adminCredential(a.PasswordHash), Exp: time.Now().Add(8 * time.Hour).Unix()})
}

// Bind sessions to the current password hash without exposing that hash. A reset
// creates a new salt even for the same password, invalidating all old sessions.
func adminCredential(passwordHash string) string {
	digest := sha256.Sum256([]byte(passwordHash))
	return hex.EncodeToString(digest[:])
}

func (s *Store) requireAdmin(r *http.Request) (Claims, error) {
	c, err := verifyToken(r.Header.Get("Authorization"), "admin")
	if err != nil {
		return c, err
	}
	var active bool
	var role, username, hash string
	err = s.db.QueryRowContext(r.Context(), "SELECT active,role,username,password_hash FROM admins WHERE id=?", c.Subject).Scan(&active, &role, &username, &hash)
	if err != nil || !active || subtle.ConstantTimeCompare([]byte(c.Credential), []byte(adminCredential(hash))) != 1 {
		return c, errors.New("管理员登录已失效")
	}
	if err = s.checkSession(r, c); err != nil {
		return c, err
	}
	c.Role = role
	c.Username = username
	return c, nil
}
func hashPassword(password string) (string, error) {
	if len([]rune(password)) < 10 {
		return "", errors.New("管理员密码至少 10 位")
	}
	if len([]rune(password)) > 128 {
		return "", errors.New("管理员密码最多 128 位")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	digest := derivePassword([]byte(password), salt)
	return "sha256-100k$" + hex.EncodeToString(salt) + "$" + hex.EncodeToString(digest), nil
}
func derivePassword(password, salt []byte) []byte {
	sum := sha256.Sum256(append(append([]byte{}, salt...), password...))
	for i := 0; i < 100000; i++ {
		next := sha256.Sum256(append(sum[:], password...))
		sum = next
	}
	return sum[:]
}
func verifyPassword(password, encoded string) bool {
	if len([]rune(password)) > 128 {
		return false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 3 || parts[0] != "sha256-100k" {
		return false
	}
	salt, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	expected, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}
	actual := derivePassword([]byte(password), salt)
	return subtle.ConstantTimeCompare(expected, actual) == 1
}
func exchangeCode(code string) (string, error) {
	appid := os.Getenv("WECHAT_APP_ID")
	appsecret := os.Getenv("WECHAT_APP_SECRET")
	if appid == "" || appsecret == "" {
		return "", errors.New("小程序身份配置未完成")
	}
	u := "https://api.weixin.qq.com/sns/jscode2session?appid=" + url.QueryEscape(appid) + "&secret=" + url.QueryEscape(appsecret) + "&js_code=" + url.QueryEscape(code) + "&grant_type=authorization_code"
	client := http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	var result struct {
		OpenID  string `json:"openid"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err = json.Unmarshal(b, &result); err != nil {
		return "", err
	}
	if result.ErrCode != 0 || result.OpenID == "" {
		return "", fmt.Errorf("微信登录失败 %s", strconv.Itoa(result.ErrCode))
	}
	return result.OpenID, nil
}
