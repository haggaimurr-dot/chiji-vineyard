package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Opt-in acceptance uses a temporary database created specifically for this
// test. It never reads local credentials or mutates the configured business DB.
func TestVineyardGameMySQLConcurrency(t *testing.T) {
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("set MYSQL_TEST_DSN to run real game MySQL concurrency acceptance")
	}
	configuration, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	configuration.DBName = ""
	configuration.MultiStatements = true
	configuration.ParseTime = true
	configuration.Loc = time.UTC
	root, err := sql.Open("mysql", configuration.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	name := fmt.Sprintf("chiji_game_test_%d", time.Now().UnixNano())
	if _, err = root.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, dropErr := root.Exec("DROP DATABASE `" + name + "`"); dropErr != nil {
			t.Error(dropErr)
		}
	}()
	configuration.DBName = name
	db, err := sql.Open("mysql", configuration.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(24)
	for _, filename := range []string{"migrations/20261008_auth_sessions.sql", "migrations/20261012_vineyard_game.sql"} {
		migration, readErr := os.ReadFile(filename)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, err = db.Exec(string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("TOKEN_SECRET", strings.Repeat("g", 40))
	t.Setenv("WECHAT_GAME_APP_ID", "wx0123456789abcdef")
	t.Setenv("WECHAT_GAME_APP_SECRET", "test-only-game-secret")
	t.Setenv("WECHAT_APP_ID", "wx1111111111111111")
	s := &Server{store: &Store{db: db}}
	owner := "game:wx0123456789abcdef:mysql-acceptance"
	const count = 12
	tokens := make([]string, count)
	for index := range tokens {
		tokens[index], err = s.issueSession(httptest.NewRequest("POST", "/game-api", nil), Claims{Subject: owner, Kind: "game", Exp: time.Now().Add(time.Hour).Unix()})
		if err != nil {
			t.Fatal(err)
		}
	}
	parallel := func(action string, data func(int) any) []int {
		statuses := make([]int, count)
		gate := make(chan struct{})
		var group sync.WaitGroup
		for index := range statuses {
			group.Add(1)
			go func(index int) {
				defer group.Done()
				<-gate
				statuses[index] = vineyardCall(s, tokens[index], action, data(index)).Code
			}(index)
		}
		close(gate)
		group.Wait()
		return statuses
	}
	for _, status := range parallel("game.bootstrap", func(int) any { return M{} }) {
		if status != 200 {
			t.Fatalf("concurrent first entry: %d", status)
		}
	}
	var farms int
	if err = db.QueryRow("SELECT COUNT(*) FROM game_farms WHERE owner=?", owner).Scan(&farms); err != nil || farms != 1 {
		t.Fatalf("farm initialization count=%d err=%v", farms, err)
	}
	for _, status := range parallel("game.action", func(int) any {
		return M{"requestId": "same_command_001", "expectedVersion": 0, "command": M{"type": "water", "plotId": 1}}
	}) {
		if status != 200 {
			t.Fatalf("idempotent concurrent request: %d", status)
		}
	}
	var state vineyardFarm
	var raw []byte
	var version int64
	if err = db.QueryRow("SELECT state,version FROM game_farms WHERE owner=?", owner).Scan(&raw, &version); err != nil {
		t.Fatal(err)
	}
	if err = decode(raw, &state); err != nil {
		t.Fatal(err)
	}
	if version != 1 || state.Coins != 18000-70 {
		t.Fatal("duplicate concurrent command charged more than once")
	}
	statuses := parallel("game.action", func(index int) any {
		return M{"requestId": fmt.Sprintf("parallel_%03d", index), "expectedVersion": 1, "command": M{"type": "water", "plotId": 2}}
	})
	success, conflicts := 0, 0
	for _, status := range statuses {
		if status == 200 {
			success++
		} else if status == 409 {
			conflicts++
		} else {
			t.Fatalf("parallel mutation error=%d", status)
		}
	}
	if success != 1 || conflicts != count-1 {
		t.Fatalf("version race success=%d conflicts=%d", success, conflicts)
	}
	gate := make(chan struct{})
	var group sync.WaitGroup
	racing := make([]int, 2)
	group.Add(2)
	go func() {
		defer group.Done()
		<-gate
		racing[0] = vineyardCall(s, tokens[0], "game.reset", M{"requestId": "concurrent_reset", "expectedVersion": 2, "confirm": "清空游戏进度"}).Code
	}()
	go func() {
		defer group.Done()
		<-gate
		racing[1] = vineyardCall(s, tokens[1], "game.action", M{"requestId": "concurrent_action", "expectedVersion": 2, "command": M{"type": "water", "plotId": 3}}).Code
	}()
	close(gate)
	group.Wait()
	if !((racing[0] == 200 && racing[1] == 409) || (racing[0] == 409 && racing[1] == 200)) {
		t.Fatalf("reset/action race statuses=%v", racing)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = db.QueryRowContext(ctx, "SELECT version FROM game_farms WHERE owner=?", owner).Scan(&version); err != nil || version != 3 {
		t.Fatalf("reset version reused/row lost: version=%d err=%v", version, err)
	}
}
