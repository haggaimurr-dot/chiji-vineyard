package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type accountSecurity struct {
	Enabled      bool
	Secret       string
	Pending      string
	PendingUntil int64
	LastStep     int64
	Recovery     []string
}

// Domain-separated AES-GCM key; account ID authenticates ownership of ciphertext.
func sealMFA(value, id string, decrypt bool) (string, error) {
	key, err := secret()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(append([]byte("chiji-mfa-encryption-v1\x00"), key...))
	block, err := aes.NewCipher(digest[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if decrypt {
		data, e := base64.RawStdEncoding.DecodeString(value)
		if e != nil || len(data) < gcm.NonceSize() {
			return "", errors.New("验证器密钥不可用")
		}
		plain, e := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], []byte(id))
		return string(plain), e
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(value), []byte(id))), nil
}

func totpCode(secret string, step int64) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return ""
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 15
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[offset:offset+4])&0x7fffffff)%1000000)
}
func matchTOTP(secret, code string, now, last int64) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	for _, step := range []int64{now / 30, now/30 - 1, now/30 + 1} {
		if step > last && subtle.ConstantTimeCompare([]byte(totpCode(secret, step)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}
func recoveryHash(id, code string) string {
	sum := sha256.Sum256([]byte(id + "\x00" + strings.ToLower(strings.TrimSpace(code))))
	return hex.EncodeToString(sum[:])
}
func verifySecondFactor(state *accountSecurity, id, code string) error {
	plain, err := sealMFA(state.Secret, id, true)
	if err != nil {
		return err
	}
	if step, ok := matchTOTP(plain, code, time.Now().Unix(), state.LastStep); ok {
		state.LastStep = step
		return nil
	}
	hash := recoveryHash(id, code)
	for i, saved := range state.Recovery {
		if subtle.ConstantTimeCompare([]byte(hash), []byte(saved)) == 1 {
			state.Recovery = append(state.Recovery[:i], state.Recovery[i+1:]...)
			return nil
		}
	}
	return errors.New("动态码或恢复码无效，请重试；已使用的动态码不能重复使用")
}
func readSecurity(ctx context.Context, tx *sql.Tx, id string) (accountSecurity, error) {
	state := accountSecurity{LastStep: -1, Recovery: []string{}}
	var raw string
	err := tx.QueryRowContext(ctx, "SELECT payload FROM admin_security WHERE admin_id=? FOR UPDATE", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	err = json.Unmarshal([]byte(raw), &state)
	return state, err
}
func writeSecurity(ctx context.Context, tx *sql.Tx, id string, state accountSecurity) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO admin_security(admin_id,payload) VALUES(?,?) ON DUPLICATE KEY UPDATE payload=VALUES(payload)", id, jsonValue(state))
	return err
}
func lockPersonalAccount(ctx context.Context, tx *sql.Tx, id string) (Admin, error) {
	var a Admin
	err := tx.QueryRowContext(ctx, "SELECT id,username,password_hash,role,active FROM admins WHERE id=? FOR UPDATE", id).Scan(&a.ID, &a.Username, &a.PasswordHash, &a.Role, &a.Active)
	if err != nil || !a.Active {
		return a, errors.New("管理员登录已失效")
	}
	return a, nil
}

func (s *Server) personalSecurity(r *http.Request, actor Claims, req Request) (any, error) {
	ctx := r.Context()
	if req.Action != "admin.security.status" {
		if err := s.store.limitLogin(r, "personal-security", actor.Subject, 10, 900); err != nil {
			return nil, err
		}
	}
	var d struct {
		CurrentPassword string `json:"currentPassword"`
		Password        string `json:"password"`
		Code            string `json:"code"`
	}
	if err := decode(req.Data, &d); err != nil {
		return nil, err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	a, err := lockPersonalAccount(ctx, tx, actor.Subject)
	if err != nil {
		return nil, err
	}
	if actor.Credential != adminCredential(a.PasswordHash) {
		return nil, errors.New("管理员登录已失效")
	}
	state, err := readSecurity(ctx, tx, a.ID)
	if err != nil {
		return nil, err
	}
	if req.Action == "admin.security.status" {
		return M{"enabled": state.Enabled, "recoveryRemaining": len(state.Recovery)}, nil
	}
	if !verifyPassword(d.CurrentPassword, a.PasswordHash) {
		return nil, errors.New("原密码错误")
	}
	result := M{}
	revoke := false
	switch req.Action {
	case "admin.security.setup":
		if state.Enabled {
			return nil, errors.New("多因素认证已启用")
		}
		bytes := make([]byte, 20)
		if _, err = rand.Read(bytes); err != nil {
			return nil, err
		}
		plain := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(bytes)
		state.Pending, err = sealMFA(plain, a.ID, false)
		if err != nil {
			return nil, err
		}
		state.PendingUntil = time.Now().Add(10 * time.Minute).Unix()
		result = M{"secret": plain, "uri": "otpauth://totp/" + url.PathEscape("ChijiGrape:"+a.Username) + "?secret=" + plain + "&issuer=ChijiGrape&algorithm=SHA1&digits=6&period=30", "expiresAt": state.PendingUntil}
	case "admin.security.enable":
		if state.Enabled || state.Pending == "" || state.PendingUntil <= time.Now().Unix() {
			return nil, errors.New("绑定已过期，请重新开始")
		}
		plain, e := sealMFA(state.Pending, a.ID, true)
		if e != nil {
			return nil, e
		}
		step, ok := matchTOTP(plain, d.Code, time.Now().Unix(), -1)
		if !ok {
			return nil, errors.New("动态码无效")
		}
		state.Secret, state.Pending, state.PendingUntil, state.LastStep, state.Enabled = state.Pending, "", 0, step, true
		codes := []string{}
		state.Recovery = []string{}
		for i := 0; i < 8; i++ {
			b := make([]byte, 10)
			if _, err = rand.Read(b); err != nil {
				return nil, err
			}
			code := hex.EncodeToString(b)
			codes = append(codes, code)
			state.Recovery = append(state.Recovery, recoveryHash(a.ID, code))
		}
		result["recoveryCodes"] = codes
		revoke = true
	case "admin.security.disable", "admin.account.changePassword":
		if state.Enabled {
			if err = verifySecondFactor(&state, a.ID, d.Code); err != nil {
				return nil, err
			}
		}
		if req.Action == "admin.security.disable" {
			state = accountSecurity{LastStep: -1, Recovery: []string{}}
		} else {
			if d.Password == d.CurrentPassword {
				return nil, errors.New("新密码不能与原密码相同")
			}
			a.PasswordHash, err = hashPassword(d.Password)
			if err != nil {
				return nil, err
			}
			state.Pending = ""
			state.PendingUntil = 0
		}
		revoke = true
	default:
		return nil, errPermission
	}
	if revoke {
		if req.Action != "admin.account.changePassword" {
			a.PasswordHash, err = hashPassword(d.CurrentPassword)
			if err != nil {
				return nil, err
			}
		}
		if _, err = tx.ExecContext(ctx, "UPDATE admins SET password_hash=? WHERE id=?", a.PasswordHash, a.ID); err != nil {
			return nil, err
		}
		result["sessionRevoked"] = true
	}
	if err = writeSecurity(ctx, tx, a.ID, state); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", a.ID, a.Username, req.Action, a.ID, jsonValue(M{"sessionsRevoked": revoke})); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// Lock the account before security state so concurrent password/MFA changes cannot
// issue a token from obsolete credentials. Recovery codes are consumed atomically.
func (s *Server) loginSecondFactor(ctx context.Context, a Admin, code string) (bool, error) {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	current, err := lockPersonalAccount(ctx, tx, a.ID)
	if err != nil {
		return false, err
	}
	if current.PasswordHash != a.PasswordHash {
		return false, errors.New("凭据已变更，请重试")
	}
	state, err := readSecurity(ctx, tx, a.ID)
	if err != nil {
		return false, err
	}
	if !state.Enabled {
		return false, nil
	}
	if code == "" {
		return true, nil
	}
	if err = verifySecondFactor(&state, a.ID, code); err != nil {
		return false, err
	}
	if err = writeSecurity(ctx, tx, a.ID, state); err != nil {
		return false, err
	}
	return false, tx.Commit()
}
