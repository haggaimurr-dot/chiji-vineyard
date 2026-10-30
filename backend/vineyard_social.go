package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// 小游戏排行榜与好友助力（新增）。
// 排行榜按果园繁荣度（累计收益 + 金币）排名；助力为好友果园加金币，带防刷限制。

// gameProsperity 繁荣度分数。
func gameProsperity(f vineyardFarm) int {
	return f.TotalRevenue + f.Coins
}

// defaultGameNickname 默认昵称（匿名）。
func defaultGameNickname(owner string) string {
	sum := sha256.Sum256([]byte(owner))
	return "果园" + hex.EncodeToString(sum[:])[:4]
}

// updateGameScore 同步后更新排行榜分数（best-effort）。
func (s *Store) updateGameScore(ctx context.Context, owner string, f vineyardFarm) {
	nickname := defaultGameNickname(owner)
	// 保留用户设置的昵称。
	var existing string
	_ = s.db.QueryRowContext(ctx, "SELECT nickname FROM game_scores WHERE owner=?", owner).Scan(&existing)
	if existing != "" {
		nickname = existing
	}
	_, _ = s.db.ExecContext(ctx, "INSERT INTO game_scores(owner,nickname,score) VALUES(?,?,?) ON DUPLICATE KEY UPDATE nickname=VALUES(nickname),score=VALUES(score)", owner, nickname, gameProsperity(f))
}

func cleanNickname(s string) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) > 16 {
		s = string([]rune(s)[:16])
	}
	return s
}

// gameSocialDispatch 排行榜/助力/昵称相关动作（在 gameDispatch 的事务外处理，
// 助力单独开事务）。
func (s *Server) gameSocialDispatch(ctx context.Context, actorSubject, action string, raw []byte) (any, error) {
	switch action {
	case "game.leaderboard":
		return s.gameLeaderboard(ctx, actorSubject)
	case "game.profile":
		var d struct {
			Nickname string `json:"nickname"`
		}
		if err := decodeGameData(raw, &d); err != nil {
			return nil, err
		}
		nickname := cleanNickname(d.Nickname)
		if nickname == "" {
			return nil, gameError("INVALID_NICKNAME", "昵称不能为空")
		}
		if _, err := s.store.db.ExecContext(ctx, "INSERT INTO game_scores(owner,nickname,score) VALUES(?,?,0) ON DUPLICATE KEY UPDATE nickname=VALUES(nickname)", actorSubject, nickname); err != nil {
			return nil, err
		}
		return M{"nickname": nickname}, nil
	case "game.help":
		var d struct {
			Target string `json:"target"`
		}
		if err := decodeGameData(raw, &d); err != nil {
			return nil, err
		}
		return s.gameHelp(ctx, actorSubject, strings.TrimSpace(d.Target))
	}
	return nil, gameError("UNKNOWN_ACTION", "不支持的小游戏请求")
}

func (s *Server) gameLeaderboard(ctx context.Context, owner string) (any, error) {
	rows, err := s.store.db.QueryContext(ctx, "SELECT nickname,score FROM game_scores ORDER BY score DESC,owner ASC LIMIT 50")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type entry struct {
		Nickname string `json:"nickname"`
		Score    int    `json:"score"`
		Mine     bool   `json:"mine,omitempty"`
	}
	list := []entry{}
	for rows.Next() {
		var e entry
		var nick string
		if err = rows.Scan(&nick, &e.Score); err != nil {
			return nil, err
		}
		e.Nickname = nick
		list = append(list, e)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rank := 0
	var myScore int
	err = s.store.db.QueryRowContext(ctx, "SELECT COUNT(*)+1 FROM game_scores WHERE score>(SELECT score FROM game_scores WHERE owner=?)", owner).Scan(&rank)
	if err != nil {
		rank = 0
	}
	_ = s.store.db.QueryRowContext(ctx, "SELECT score FROM game_scores WHERE owner=?", owner).Scan(&myScore)
	return M{"items": list, "myRank": rank, "myScore": myScore}, nil
}

// gameHelp 好友助力：为目标果园 +30 金币。防刷：每人每天对同一好友限 1 次，
// 每人每天最多助力 5 次，每人每天最多被助力 10 次。
func (s *Server) gameHelp(ctx context.Context, helper, target string) (any, error) {
	if target == "" || len(target) > 80 {
		return nil, gameError("INVALID_TARGET", "助力目标无效")
	}
	if target == helper {
		return nil, gameError("CANNOT_HELP_SELF", "不能给自己助力")
	}
	day := time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var dup int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM game_helps WHERE helper=? AND target=? AND day=?", helper, target, day).Scan(&dup); err != nil {
		return nil, err
	}
	if dup > 0 {
		return nil, gameError("ALREADY_HELPED", "今天已经助力过这位好友了")
	}
	var given int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM game_helps WHERE helper=? AND day=?", helper, day).Scan(&given); err != nil {
		return nil, err
	}
	if given >= 5 {
		return nil, gameError("HELP_LIMIT", "今天助力次数已用完（5 次）")
	}
	var received int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM game_helps WHERE target=? AND day=?", target, day).Scan(&received); err != nil {
		return nil, err
	}
	if received >= 10 {
		return nil, gameError("TARGET_LIMIT", "该好友今天被助力已达上限")
	}
	// 目标果园必须存在。
	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM game_farms WHERE owner=?", target).Scan(&exists); err != nil || exists == 0 {
		return nil, gameError("TARGET_NOT_FOUND", "好友果园不存在")
	}
	now := time.Now().UnixMilli()
	farm, err := loadLockedVineyardFarm(ctx, tx, target, now)
	if err != nil {
		return nil, err
	}
	farm.Coins += 30
	farm.Version++
	if _, err = tx.ExecContext(ctx, "UPDATE game_farms SET state=?,version=? WHERE owner=?", jsonValue(farm), farm.Version, target); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO game_helps(helper,target,day) VALUES(?,?,?)", helper, target, day); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	// 刷新目标分数（best-effort）。
	s.store.updateGameScore(ctx, target, farm)
	return M{"helped": true, "coins": 30}, nil
}
