package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// The game uses its own AppID, subject namespace and session kind. A mall token
// cannot read or mutate a game farm, and game tokens cannot access mall APIs.
type vineyardGameError struct {
	Code    string
	Message string
	Status  int
}

func (e *vineyardGameError) Error() string { return e.Message }
func gameError(code, message string) error {
	return &vineyardGameError{Code: code, Message: message, Status: http.StatusBadRequest}
}

var gameAppIDPattern = regexp.MustCompile(`^wx[0-9a-fA-F]{16}$`)
var gameOpenIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,54}$`)
var gameRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,80}$`)
var gameWechatHTTPClient = &http.Client{Timeout: 8 * time.Second}

func gameAppConfiguration() (string, string, error) {
	appID, appSecret := strings.TrimSpace(os.Getenv("WECHAT_GAME_APP_ID")), strings.TrimSpace(os.Getenv("WECHAT_GAME_APP_SECRET"))
	if !gameAppIDPattern.MatchString(appID) || appSecret == "" || appID == strings.TrimSpace(os.Getenv("WECHAT_APP_ID")) {
		return "", "", &vineyardGameError{Code: "GAME_NOT_CONFIGURED", Message: "小游戏微信身份配置尚未完成", Status: http.StatusServiceUnavailable}
	}
	return appID, appSecret, nil
}

func exchangeGameCode(ctx context.Context, code string) (string, error) {
	appID, appSecret, err := gameAppConfiguration()
	if err != nil {
		return "", err
	}
	if code == "" || len(code) > 256 || strings.TrimSpace(code) != code {
		return "", gameError("INVALID_CODE", "请通过微信小游戏登录获取有效凭据")
	}
	query := url.Values{"appid": {appID}, "secret": {appSecret}, "js_code": {code}, "grant_type": {"authorization_code"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.weixin.qq.com/sns/jscode2session?"+query.Encode(), nil)
	if err != nil {
		return "", err
	}
	response, err := gameWechatHTTPClient.Do(req)
	if err != nil {
		return "", &vineyardGameError{Code: "WECHAT_UNAVAILABLE", Message: "微信登录暂不可用，请稍后重试", Status: http.StatusServiceUnavailable}
	}
	defer response.Body.Close()
	var result struct {
		OpenID  string `json:"openid"`
		ErrCode int    `json:"errcode"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result) != nil {
		return "", &vineyardGameError{Code: "WECHAT_UNAVAILABLE", Message: "微信登录返回无效，请稍后重试", Status: http.StatusServiceUnavailable}
	}
	if result.ErrCode != 0 || !gameOpenIDPattern.MatchString(result.OpenID) {
		return "", &vineyardGameError{Code: "GAME_LOGIN_FAILED", Message: "微信小游戏登录失败，请重新登录", Status: http.StatusUnauthorized}
	}
	subject := "game:" + appID + ":" + result.OpenID
	if len(subject) > 80 {
		return "", gameError("GAME_LOGIN_FAILED", "微信小游戏身份无效")
	}
	return subject, nil
}

func decodeGameData(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return gameError("INVALID_REQUEST", "小游戏请求参数无效")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return gameError("INVALID_REQUEST", "小游戏请求参数无效")
	}
	return nil
}

func (s *Server) gameAPI(w http.ResponseWriter, r *http.Request) {
	adminCORS(w, r)
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var request Request
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&request)
	if err == nil {
		var trailing any
		if decoder.Decode(&trailing) != io.EOF {
			err = errors.New("extra payload")
		}
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, M{"ok": false, "error": APIError{Code: "INVALID_REQUEST", Message: "小游戏请求格式无效"}})
		return
	}
	result, err := s.gameDispatch(r, request)
	if err != nil {
		if writeAccessError(w, err) {
			return
		}
		var gameErr *vineyardGameError
		if errors.As(err, &gameErr) {
			status := gameErr.Status
			if status == 0 {
				status = http.StatusBadRequest
			}
			writeJSON(w, status, M{"ok": false, "error": APIError{Code: gameErr.Code, Message: gameErr.Message}})
		} else {
			log.Printf("game api action=%s error=%v", request.Action, err)
			writeJSON(w, http.StatusServiceUnavailable, M{"ok": false, "error": APIError{Code: "GAME_UNAVAILABLE", Message: "小游戏服务暂不可用，请稍后重试"}})
		}
		return
	}
	writeJSON(w, http.StatusOK, M{"ok": true, "data": result, "requestId": randomID("GAME")})
}

type vineyardMutation struct {
	RequestID       string          `json:"requestId"`
	ExpectedVersion *int64          `json:"expectedVersion"`
	Command         vineyardCommand `json:"command"`
	Confirm         string          `json:"confirm,omitempty"`
}

func decodeGameMutation(action string, raw json.RawMessage) (vineyardMutation, string, error) {
	var data vineyardMutation
	if err := decodeGameData(raw, &data); err != nil {
		return data, "", err
	}
	if !gameRequestIDPattern.MatchString(data.RequestID) || data.ExpectedVersion == nil || *data.ExpectedVersion < 0 {
		return data, "", gameError("INVALID_REQUEST", "操作需要有效请求编号与存档版本")
	}
	if action == "game.reset" {
		if data.Confirm != "清空游戏进度" || data.Command != (vineyardCommand{}) {
			return data, "", gameError("RESET_CONFIRMATION_REQUIRED", "请确认清空游戏进度，商城账号和数据不受影响")
		}
	} else if data.Confirm != "" {
		return data, "", gameError("INVALID_REQUEST", "操作请求参数无效")
	} else if err := validateVineyardCommand(data.Command); err != nil {
		return data, "", err
	}
	// Hash the parsed, canonical command, not raw JSON field ordering. Persisted
	// results survive retries; reusing a request ID for a different action fails.
	payload, err := json.Marshal(struct {
		Action  string
		Version int64
		Command vineyardCommand
		Confirm string
	}{action, *data.ExpectedVersion, data.Command, data.Confirm})
	if err != nil {
		return data, "", err
	}
	digest := sha256.Sum256(payload)
	return data, hex.EncodeToString(digest[:]), nil
}

func (s *Server) gameDispatch(r *http.Request, request Request) (any, error) {
	if request.Action == "game.login" {
		var data struct {
			Code string `json:"code"`
		}
		if err := decodeGameData(request.Data, &data); err != nil {
			return nil, err
		}
		if _, _, err := gameAppConfiguration(); err != nil {
			return nil, err
		}
		if data.Code == "" || len(data.Code) > 256 {
			return nil, gameError("INVALID_CODE", "请通过微信小游戏登录获取有效凭据")
		}
		if err := s.store.limitLogin(r, "game-source", loginPeer(r), 30, 900); err != nil {
			return nil, err
		}
		subject, err := exchangeGameCode(r.Context(), data.Code)
		if err != nil {
			return nil, err
		}
		expires := time.Now().Add(7 * 24 * time.Hour)
		token, err := s.issueSession(r, Claims{Subject: subject, Kind: "game", Exp: expires.Unix()})
		if err != nil {
			return nil, err
		}
		return M{"token": token, "expiresAt": expires.UnixMilli(), "demo": false}, nil
	}
	switch request.Action {
	case "game.bootstrap", "game.sync", "game.action", "game.history", "game.report", "game.logout", "game.reset":
	default:
		return nil, gameError("UNKNOWN_ACTION", "不支持的小游戏请求")
	}
	actor, err := verifyToken(r.Header.Get("Authorization"), "game")
	if err != nil {
		return nil, &vineyardGameError{Code: "GAME_UNAUTHORIZED", Message: "小游戏登录已失效，请重新登录", Status: http.StatusUnauthorized}
	}
	appID, _, err := gameAppConfiguration()
	if err != nil {
		return nil, err
	}
	if actor.SessionID == "" || !strings.HasPrefix(actor.Subject, "game:"+appID+":") || len(actor.Subject) > 80 {
		return nil, &vineyardGameError{Code: "GAME_UNAUTHORIZED", Message: "小游戏登录身份无效", Status: http.StatusUnauthorized}
	}
	var mutation vineyardMutation
	var requestHash string
	if request.Action == "game.action" || request.Action == "game.reset" {
		mutation, requestHash, err = decodeGameMutation(request.Action, request.Data)
	} else {
		err = decodeGameData(request.Data, &struct{}{})
	}
	if err != nil {
		return nil, err
	}
	tx, err := s.store.db.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Check and lock the active game session in the same transaction. Logout and
	// writes cannot race between an external session check and farm settlement.
	var sessionID string
	err = tx.QueryRowContext(r.Context(), "SELECT id FROM auth_sessions WHERE id=? AND subject=? AND kind='game' AND revoked_at IS NULL AND expires_at>? FOR UPDATE", actor.SessionID, actor.Subject, time.Now().Unix()).Scan(&sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &vineyardGameError{Code: "GAME_UNAUTHORIZED", Message: "小游戏登录已失效，请重新登录", Status: http.StatusUnauthorized}
	}
	if err != nil {
		return nil, err
	}
	if request.Action == "game.logout" {
		if _, err = tx.ExecContext(r.Context(), "UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,?) WHERE id=? AND subject=? AND kind='game'", time.Now().Unix(), actor.SessionID, actor.Subject); err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return M{"loggedOut": true, "demo": false}, nil
	}
	now := time.Now().UnixMilli()
	state, err := loadLockedVineyardFarm(r.Context(), tx, actor.Subject, now)
	if err != nil {
		return nil, err
	}
	if mutation.RequestID != "" {
		var previousHash string
		var previousResult []byte
		err = tx.QueryRowContext(r.Context(), "SELECT request_hash,result FROM game_commands WHERE owner=? AND request_id=?", actor.Subject, mutation.RequestID).Scan(&previousHash, &previousResult)
		if err == nil {
			if previousHash != requestHash {
				return nil, &vineyardGameError{Code: "REQUEST_ID_REUSED", Message: "请求编号已用于其他操作，请刷新后重试", Status: http.StatusConflict}
			}
			var result M
			if err = json.Unmarshal(previousResult, &result); err != nil {
				return nil, err
			}
			if err = tx.Commit(); err != nil {
				return nil, err
			}
			return result, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if state.Version != *mutation.ExpectedVersion {
			return nil, &vineyardGameError{Code: "VERSION_CONFLICT", Message: "存档已更新，请同步葡萄园后重试", Status: http.StatusConflict}
		}
	}
	previousVersion := state.Version
	switch request.Action {
	case "game.bootstrap", "game.sync":
		state, err = settleVineyardFarm(state, now)
	case "game.action":
		state, err = applyVineyardAction(state, mutation.Command, now)
	case "game.reset":
		seed, seedErr := vineyardSecureSeed()
		if seedErr != nil {
			return nil, seedErr
		}
		state = createVineyardFarm(now, seed)
		state.Version = previousVersion + 1
		if _, err = tx.ExecContext(r.Context(), "DELETE FROM game_commands WHERE owner=?", actor.Subject); err != nil {
			return nil, err
		}
	}
	if err != nil {
		return nil, err
	}
	if state.Version != previousVersion {
		if _, err = tx.ExecContext(r.Context(), "UPDATE game_farms SET state=?,version=? WHERE owner=?", jsonValue(state), state.Version, actor.Subject); err != nil {
			return nil, err
		}
	}
	result := M{"state": state, "demo": false}
	if request.Action == "game.history" {
		result["items"] = state.History
	}
	if request.Action == "game.report" {
		result["report"] = getVineyardReport(state)
	}
	if request.Action == "game.reset" {
		result["reset"] = true
	}
	if mutation.RequestID != "" {
		if _, err = tx.ExecContext(r.Context(), "INSERT INTO game_commands(owner,request_id,request_hash,result) VALUES(?,?,?,?)", actor.Subject, mutation.RequestID, requestHash, jsonValue(result)); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func vineyardSecureSeed() (uint32, error) {
	var data [4]byte
	if _, err := rand.Read(data[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(data[:]), nil
}

func loadLockedVineyardFarm(ctx context.Context, tx *sql.Tx, owner string, now int64) (vineyardFarm, error) {
	seed, err := vineyardSecureSeed()
	if err != nil {
		return vineyardFarm{}, err
	}
	initial := createVineyardFarm(now, seed)
	// The owner row is never deleted by reset. A no-op duplicate update acquires
	// an exclusive lock immediately; INSERT IGNORE followed by FOR UPDATE can
	// deadlock when concurrent sessions upgrade their duplicate-key shared locks.
	// Existing farm state and version are never replaced during initialization.
	_, err = tx.ExecContext(ctx, "INSERT INTO game_farms(owner,state,version) VALUES(?,?,?) ON DUPLICATE KEY UPDATE owner=owner", owner, jsonValue(initial), initial.Version)
	if err != nil {
		return vineyardFarm{}, err
	}
	var raw []byte
	var version int64
	err = tx.QueryRowContext(ctx, "SELECT state,version FROM game_farms WHERE owner=? FOR UPDATE", owner).Scan(&raw, &version)
	if err != nil {
		return vineyardFarm{}, err
	}
	var state vineyardFarm
	if err = json.Unmarshal(raw, &state); err != nil {
		return state, fmt.Errorf("decode farm: %w", err)
	}
	if !validateVineyardSave(state) || state.Version != version {
		return state, errors.New("game farm state or rule version invalid")
	}
	return state, nil
}
