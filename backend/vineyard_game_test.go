package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type vineyardTestCommand struct {
	Hash   string
	Result []byte
}
type vineyardTestSession struct {
	Subject, Kind string
	Expires       int64
	Revoked       bool
}
type vineyardTestDB struct {
	farms, pendingFarms                                map[string]vineyardFarm
	commands, pendingCommands                          map[string]vineyardTestCommand
	sessions, pendingSessions                          map[string]vineyardTestSession
	inTx, farmLocked, sessionLocked, failCommandInsert bool
	owner                                              string
	commits, rollbacks                                 int
}
type vineyardTestConnector struct{ fixture *vineyardTestDB }
type vineyardTestDriver struct{ fixture *vineyardTestDB }

func (c vineyardTestConnector) Connect(context.Context) (driver.Conn, error) { return c.fixture, nil }
func (c vineyardTestConnector) Driver() driver.Driver                        { return vineyardTestDriver(c) }
func (d vineyardTestDriver) Open(string) (driver.Conn, error)                { return d.fixture, nil }
func (f *vineyardTestDB) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}
func (f *vineyardTestDB) Close() error { return nil }
func (f *vineyardTestDB) BeginTx(_ context.Context, _ driver.TxOptions) (driver.Tx, error) {
	return f.Begin()
}
func (f *vineyardTestDB) Begin() (driver.Tx, error) {
	if f.inTx {
		return nil, errors.New("nested transaction")
	}
	f.inTx, f.farmLocked, f.sessionLocked = true, false, false
	f.pendingFarms = map[string]vineyardFarm{}
	for k, v := range f.farms {
		var copied vineyardFarm
		_ = json.Unmarshal([]byte(jsonValue(v)), &copied)
		f.pendingFarms[k] = copied
	}
	f.pendingCommands = map[string]vineyardTestCommand{}
	for k, v := range f.commands {
		f.pendingCommands[k] = v
	}
	f.pendingSessions = map[string]vineyardTestSession{}
	for k, v := range f.sessions {
		f.pendingSessions[k] = v
	}
	return f, nil
}
func (f *vineyardTestDB) Commit() error {
	f.farms, f.commands, f.sessions = f.pendingFarms, f.pendingCommands, f.pendingSessions
	f.inTx = false
	f.commits++
	return nil
}
func (f *vineyardTestDB) Rollback() error { f.inTx = false; f.rollbacks++; return nil }
func (f *vineyardTestDB) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if strings.Contains(query, "login_limits") {
		return driver.RowsAffected(1), nil
	}
	if !f.inTx {
		return nil, errors.New("game write outside transaction")
	}
	switch {
	case strings.HasPrefix(query, "INSERT INTO auth_sessions"):
		f.pendingSessions[args[0].Value.(string)] = vineyardTestSession{Subject: args[1].Value.(string), Kind: args[2].Value.(string), Expires: args[7].Value.(int64)}
	case strings.HasPrefix(query, "UPDATE auth_sessions"):
		if !f.sessionLocked {
			return nil, errors.New("session update without lock")
		}
		id := args[1].Value.(string)
		v := f.pendingSessions[id]
		v.Revoked = true
		f.pendingSessions[id] = v
	case strings.HasPrefix(query, "INSERT INTO game_farms"):
		if !f.sessionLocked {
			return nil, errors.New("farm init without session lock")
		}
		owner := args[0].Value.(string)
		if _, ok := f.pendingFarms[owner]; !ok {
			var farm vineyardFarm
			if err := json.Unmarshal([]byte(args[1].Value.(string)), &farm); err != nil {
				return nil, err
			}
			f.pendingFarms[owner] = farm
		}
	case strings.HasPrefix(query, "UPDATE game_farms"):
		if !f.farmLocked || f.owner != args[2].Value {
			return nil, errors.New("farm mutation without owner row lock")
		}
		var farm vineyardFarm
		if err := json.Unmarshal([]byte(args[0].Value.(string)), &farm); err != nil {
			return nil, err
		}
		f.pendingFarms[f.owner] = farm
	case strings.HasPrefix(query, "DELETE FROM game_commands"):
		if !f.farmLocked || f.owner != args[0].Value {
			return nil, errors.New("reset without owner row lock")
		}
		for key := range f.pendingCommands {
			if strings.HasPrefix(key, f.owner+"\x00") {
				delete(f.pendingCommands, key)
			}
		}
	case strings.HasPrefix(query, "INSERT INTO game_commands"):
		if f.failCommandInsert {
			return nil, errors.New("ledger unavailable")
		}
		if !f.farmLocked || f.owner != args[0].Value {
			return nil, errors.New("command without owner row lock")
		}
		key := f.owner + "\x00" + args[1].Value.(string)
		if _, ok := f.pendingCommands[key]; ok {
			return nil, errors.New("duplicate request")
		}
		f.pendingCommands[key] = vineyardTestCommand{Hash: args[2].Value.(string), Result: []byte(args[3].Value.(string))}
	default:
		return nil, errors.New("unexpected game exec: " + query)
	}
	return driver.RowsAffected(1), nil
}
func (f *vineyardTestDB) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "FROM login_limits") {
		return &vineyardTestRows{columns: []string{"attempts", "retry"}, rows: [][]driver.Value{{int64(1), int64(1)}}}, nil
	}
	if !f.inTx {
		return nil, errors.New("game read outside transaction")
	}
	switch {
	case strings.HasPrefix(query, "SELECT id FROM auth_sessions"):
		if !strings.Contains(query, "kind='game'") || !strings.HasSuffix(query, "FOR UPDATE") {
			return nil, errors.New("incorrect game session lock")
		}
		result := &vineyardTestRows{columns: []string{"id"}}
		id := args[0].Value.(string)
		session, ok := f.pendingSessions[id]
		if ok && session.Subject == args[1].Value && session.Kind == "game" && !session.Revoked && session.Expires > args[2].Value.(int64) {
			f.sessionLocked = true
			result.rows = [][]driver.Value{{id}}
		}
		return result, nil
	case strings.HasPrefix(query, "SELECT state,version FROM game_farms"):
		if !f.sessionLocked || !strings.HasSuffix(query, "FOR UPDATE") {
			return nil, errors.New("farm read without lock")
		}
		f.owner = args[0].Value.(string)
		f.farmLocked = true
		farm, ok := f.pendingFarms[f.owner]
		result := &vineyardTestRows{columns: []string{"state", "version"}}
		if ok {
			result.rows = [][]driver.Value{{[]byte(jsonValue(farm)), farm.Version}}
		}
		return result, nil
	case strings.HasPrefix(query, "SELECT request_hash,result FROM game_commands"):
		if !f.farmLocked {
			return nil, errors.New("idempotency read without owner lock")
		}
		command, ok := f.pendingCommands[args[0].Value.(string)+"\x00"+args[1].Value.(string)]
		result := &vineyardTestRows{columns: []string{"hash", "result"}}
		if ok {
			result.rows = [][]driver.Value{{command.Hash, command.Result}}
		}
		return result, nil
	default:
		return nil, errors.New("unexpected game query: " + query)
	}
}

type vineyardTestRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *vineyardTestRows) Columns() []string { return r.columns }
func (r *vineyardTestRows) Close() error      { return nil }
func (r *vineyardTestRows) Next(values []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(values, r.rows[r.index])
	r.index++
	return nil
}

func setupVineyardGameTest(t *testing.T) (*Server, *vineyardTestDB, string, string) {
	t.Helper()
	t.Setenv("TOKEN_SECRET", strings.Repeat("g", 40))
	t.Setenv("WECHAT_GAME_APP_ID", "wx0123456789abcdef")
	t.Setenv("WECHAT_GAME_APP_SECRET", "server-only-secret")
	t.Setenv("WECHAT_APP_ID", "wx1111111111111111")
	owner := "game:wx0123456789abcdef:test-openid"
	f := &vineyardTestDB{farms: map[string]vineyardFarm{owner: createVineyardFarm(time.Now().UnixMilli(), 12345)}, commands: map[string]vineyardTestCommand{}, sessions: map[string]vineyardTestSession{"game-session": {Subject: owner, Kind: "game", Expires: time.Now().Add(time.Hour).Unix()}}}
	db := sql.OpenDB(vineyardTestConnector{f})
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	token, err := signToken(Claims{SessionID: "game-session", Subject: owner, Kind: "game", Exp: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return &Server{store: &Store{db: db}}, f, owner, token
}
func vineyardCall(s *Server, token, action string, data any) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/game-api", strings.NewReader(jsonValue(M{"action": action, "data": data})))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	return w
}

func TestVineyardGameAuthIsolation(t *testing.T) {
	s, f, owner, token := setupVineyardGameTest(t)
	mini, _ := signToken(Claims{SessionID: "game-session", Subject: owner, Kind: "mini", Exp: time.Now().Add(time.Hour).Unix()})
	for _, bad := range []string{"", mini, token + "corrupted"} {
		result := vineyardCall(s, bad, "game.bootstrap", M{})
		if result.Code != http.StatusUnauthorized {
			t.Fatalf("bad token accepted: %s", result.Body)
		}
	}
	result := vineyardCall(s, token, "game.bootstrap", M{})
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), `"demo":false`) {
		t.Fatalf("bootstrap: %s", result.Body)
	}
	if _, err := verifyToken(token, "mini"); err == nil {
		t.Fatal("game token accepted as mall token")
	}
	result = vineyardCall(s, token, "game.logout", M{})
	if result.Code != 200 {
		t.Fatal(result.Body)
	}
	if !f.sessions["game-session"].Revoked {
		t.Fatal("logout did not revoke own session")
	}
	result = vineyardCall(s, token, "game.sync", M{})
	if result.Code != 401 {
		t.Fatal("revoked token accepted")
	}
}

func TestVineyardGameIdempotencyVersionAndRollback(t *testing.T) {
	s, f, owner, token := setupVineyardGameTest(t)
	request := M{"requestId": "operation_001", "expectedVersion": 0, "command": M{"type": "water", "plotId": 1}}
	result := vineyardCall(s, token, "game.action", request)
	if result.Code != 200 {
		t.Fatal(result.Body)
	}
	committed := jsonValue(f.farms[owner])
	if f.farms[owner].Version != 1 {
		t.Fatal("wrong version")
	}
	retry := vineyardCall(s, token, "game.action", request)
	if retry.Code != 200 {
		t.Fatal(retry.Body)
	}
	var first, repeated struct {
		Data json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(result.Body.Bytes(), &first)
	_ = json.Unmarshal(retry.Body.Bytes(), &repeated)
	var originalData, replayedData any
	_ = json.Unmarshal(first.Data, &originalData)
	_ = json.Unmarshal(repeated.Data, &replayedData)
	if !reflect.DeepEqual(originalData, replayedData) || jsonValue(f.farms[owner]) != committed {
		t.Fatal("duplicate retry mutated or changed response")
	}
	request["command"] = M{"type": "water", "plotId": 2}
	result = vineyardCall(s, token, "game.action", request)
	if result.Code != 409 || !strings.Contains(result.Body.String(), "REQUEST_ID_REUSED") {
		t.Fatal(result.Body)
	}
	request["requestId"] = "operation_002"
	result = vineyardCall(s, token, "game.action", request)
	if result.Code != 409 || !strings.Contains(result.Body.String(), "VERSION_CONFLICT") {
		t.Fatal(result.Body)
	}
	request["expectedVersion"] = 1
	f.failCommandInsert = true
	result = vineyardCall(s, token, "game.action", request)
	if result.Code != 503 {
		t.Fatal(result.Body)
	}
	if jsonValue(f.farms[owner]) != committed || len(f.commands) != 1 {
		t.Fatal("farm write survived failed ledger insert")
	}
}

func TestVineyardGameResetKeepsOtherAccountsAndVersion(t *testing.T) {
	s, f, owner, token := setupVineyardGameTest(t)
	other := "game:wx0123456789abcdef:other"
	f.farms[other] = createVineyardFarm(time.Now().UnixMilli(), 67890)
	f.commands[other+"\x00other-command"] = vineyardTestCommand{Hash: "other", Result: []byte(`{}`)}
	f.commands[owner+"\x00old-command"] = vineyardTestCommand{Hash: "old", Result: []byte(`{}`)}
	request := M{"requestId": "reset_0001", "expectedVersion": 0, "confirm": "清空游戏进度"}
	invalid := M{"requestId": "reset_0001", "expectedVersion": 0, "confirm": "yes"}
	result := vineyardCall(s, token, "game.reset", invalid)
	if result.Code != 400 {
		t.Fatal(result.Body)
	}
	otherBefore := jsonValue(f.farms[other])
	result = vineyardCall(s, token, "game.reset", request)
	if result.Code != 200 {
		t.Fatal(result.Body)
	}
	if f.farms[owner].Version != 1 || jsonValue(f.farms[other]) != otherBefore || len(f.commands) != 2 {
		t.Fatal("reset crossed accounts or reused initial version")
	}
	before := jsonValue(f.farms[owner])
	result = vineyardCall(s, token, "game.reset", request)
	if result.Code != 200 || jsonValue(f.farms[owner]) != before {
		t.Fatal("reset retry repeated reset")
	}
	result = vineyardCall(s, token, "game.action", M{"requestId": "stale_0001", "expectedVersion": 0, "command": M{"type": "advance"}})
	if result.Code != 409 {
		t.Fatal("stale pre-reset command accepted")
	}
}

type vineyardRoundTripper func(*http.Request) (*http.Response, error)

func (f vineyardRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestVineyardGameLoginUsesIndependentAppAndFailsClosed(t *testing.T) {
	s, _, _, _ := setupVineyardGameTest(t)
	old := gameWechatHTTPClient
	t.Cleanup(func() { gameWechatHTTPClient = old })
	gameWechatHTTPClient = &http.Client{Transport: vineyardRoundTripper(func(r *http.Request) (*http.Response, error) {
		query := r.URL.Query()
		if query.Get("appid") != "wx0123456789abcdef" || query.Get("secret") != "server-only-secret" || query.Get("js_code") != "wx-code" {
			t.Fatal("wrong game login credentials")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"openid":"test-game-player"}`)), Header: make(http.Header)}, nil
	})}
	result := vineyardCall(s, "", "game.login", M{"code": "wx-code"})
	if result.Code != 200 {
		t.Fatal(result.Body)
	}
	var response struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(result.Body.Bytes(), &response)
	claims, err := verifyToken(response.Data.Token, "game")
	if err != nil || claims.Subject != "game:wx0123456789abcdef:test-game-player" || claims.SessionID == "" {
		t.Fatalf("invalid game subject: %v", err)
	}
	t.Setenv("WECHAT_GAME_APP_SECRET", "")
	result = vineyardCall(s, "", "game.login", M{"code": "wx-code"})
	if result.Code != 503 || strings.Contains(result.Body.String(), `"token"`) {
		t.Fatal("unconfigured game login did not fail closed")
	}
	t.Setenv("WECHAT_GAME_APP_SECRET", "server-only-secret")
	result = vineyardCall(s, "", "game.login", M{"code": ""})
	if result.Code != 400 {
		t.Fatal("empty login code accepted")
	}
	t.Setenv("WECHAT_APP_ID", "wx0123456789abcdef")
	result = vineyardCall(s, "", "game.login", M{"code": "wx-code"})
	if result.Code != 503 {
		t.Fatal("game configuration accepted mall AppID")
	}
}

func TestVineyardGameRejectsClientStateAndCanonicalizesRequests(t *testing.T) {
	s, _, _, token := setupVineyardGameTest(t)
	for _, data := range []any{
		M{"requestId": "bad_00001", "expectedVersion": 0, "command": M{"type": "water", "plotId": 1}, "state": M{"coins": 999999}},
		M{"requestId": "bad_00001", "expectedVersion": 0, "command": M{"type": "advance", "days": 84}},
		M{"requestId": "bad_00001", "expectedVersion": 0, "command": M{"type": "water", "plotId": 5}},
		M{"requestId": "bad_00001", "command": M{"type": "advance"}},
	} {
		result := vineyardCall(s, token, "game.action", data)
		if result.Code != 400 {
			t.Fatal(result.Body)
		}
	}
	_, hash1, err := decodeGameMutation("game.action", []byte(`{"requestId":"same_0001","expectedVersion":0,"command":{"plotId":1,"type":"water"}}`))
	if err != nil {
		t.Fatal(err)
	}
	_, hash2, err := decodeGameMutation("game.action", []byte(`{"command":{"type":"water","plotId":1},"expectedVersion":0,"requestId":"same_0001"}`))
	if err != nil || hash1 != hash2 {
		t.Fatal("field order changes canonical request digest")
	}
}
