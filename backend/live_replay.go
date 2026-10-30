package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

// 直播回放（新增）：按机位维护回放视频列表，小程序直播页可回看。

type LiveReplay struct {
	ID          string `json:"id"`
	Channel     string `json:"channel"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	CoverURL    string `json:"coverUrl"`
	DurationSec int    `json:"durationSec"`
	PlayedAt    string `json:"playedAt"`
	CreatedAt   string `json:"createdAt"`
}

func validReplayChannel(c string) bool { return c == "orchard" || c == "stall" }

func validateReplayURL(u string) error {
	u = strings.TrimSpace(u)
	if u == "" || len(u) > 2000 {
		return errors.New("回放地址不能为空且最多2000字符")
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || strings.ContainsAny(u, " \t\r\n") {
		return errors.New("回放地址须为有效的 HTTPS 地址")
	}
	lower := strings.ToLower(parsed.Path)
	if !strings.HasSuffix(lower, ".mp4") && !strings.HasSuffix(lower, ".m3u8") {
		return errors.New("回放地址须为 .mp4 或 .m3u8")
	}
	return nil
}

func (s *Store) listLiveReplays(ctx context.Context, channel string) ([]LiveReplay, error) {
	query := "SELECT id,channel,title,url,cover_url,duration_sec,played_at,created_at FROM live_replays WHERE 1=1"
	args := []any{}
	if channel != "" {
		if !validReplayChannel(channel) {
			return nil, errors.New("机位无效")
		}
		query += " AND channel=?"
		args = append(args, channel)
	}
	query += " ORDER BY created_at DESC,id DESC LIMIT 100"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LiveReplay{}
	for rows.Next() {
		var r LiveReplay
		var created string
		if err = rows.Scan(&r.ID, &r.Channel, &r.Title, &r.URL, &r.CoverURL, &r.DurationSec, &r.PlayedAt, &created); err != nil {
			return nil, err
		}
		r.CreatedAt = created
		out = append(out, r)
	}
	return out, rows.Err()
}

// liveReplayList：用户端回放列表。
func (s *Server) liveReplayList(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		Channel string `json:"channel"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	return s.store.listLiveReplays(ctx, d.Channel)
}

// adminLiveReplaySave：新增/编辑回放（SUPER_ADMIN）。
func (s *Server) adminLiveReplaySave(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		ID          string `json:"id"`
		Channel     string `json:"channel"`
		Title       string `json:"title"`
		URL         string `json:"url"`
		CoverURL    string `json:"coverUrl"`
		DurationSec int    `json:"durationSec"`
		PlayedAt    string `json:"playedAt"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if !validReplayChannel(d.Channel) {
		return nil, errors.New("机位无效")
	}
	d.Title = strings.TrimSpace(d.Title)
	if d.Title == "" || len([]rune(d.Title)) > 120 {
		return nil, errors.New("回放标题不能为空且最多120字")
	}
	if err := validateReplayURL(d.URL); err != nil {
		return nil, err
	}
	if d.CoverURL != "" {
		d.CoverURL = strings.TrimSpace(d.CoverURL)
		parsed, err := url.Parse(d.CoverURL)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
			return nil, errors.New("封面须为有效的 HTTPS 地址")
		}
	}
	if d.DurationSec < 0 || d.DurationSec > 86400 {
		return nil, errors.New("时长无效")
	}
	if len([]rune(d.PlayedAt)) > 32 {
		return nil, errors.New("直播日期无效")
	}
	id := d.ID
	if id == "" {
		id = randomID("REPLAY")
		if _, err := s.store.db.ExecContext(ctx, "INSERT INTO live_replays(id,channel,title,url,cover_url,duration_sec,played_at) VALUES(?,?,?,?,?,?,?)", id, d.Channel, d.Title, strings.TrimSpace(d.URL), d.CoverURL, d.DurationSec, strings.TrimSpace(d.PlayedAt)); err != nil {
			return nil, err
		}
	} else {
		res, err := s.store.db.ExecContext(ctx, "UPDATE live_replays SET channel=?,title=?,url=?,cover_url=?,duration_sec=?,played_at=? WHERE id=?", d.Channel, d.Title, strings.TrimSpace(d.URL), d.CoverURL, d.DurationSec, strings.TrimSpace(d.PlayedAt), id)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, errors.New("回放不存在")
		}
	}
	if _, err := s.store.db.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "LIVE_REPLAY_SAVE", id, jsonValue(M{"channel": d.Channel, "title": d.Title})); err != nil {
		return nil, err
	}
	return M{"id": id}, nil
}

// adminLiveReplayDelete：删除回放（SUPER_ADMIN）。
func (s *Server) adminLiveReplayDelete(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	id := mustID(raw, "id")
	res, err := s.store.db.ExecContext(ctx, "DELETE FROM live_replays WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errors.New("回放不存在")
	}
	_, _ = s.store.db.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "LIVE_REPLAY_DELETE", id, jsonValue(M{}))
	return M{"deleted": true}, nil
}

// liveProducts：直播间挂品列表（用户端）。
func (s *Server) liveProducts(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		Channel string `json:"channel"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if !validReplayChannel(d.Channel) {
		return nil, errors.New("机位无效")
	}
	// 复用 homeFor：取当前生效的首页配置。
	h, err := s.store.homeFor(ctx, "")
	if err != nil {
		return nil, err
	}
	var ids []string
	if h.Live != nil {
		if d.Channel == "orchard" {
			ids = h.Live.Orchard.ProductIDs
		} else {
			ids = h.Live.Stall.ProductIDs
		}
	}
	out := []M{}
	for _, id := range ids {
		p, err := s.store.product(ctx, id, false)
		if err != nil || p == nil || p.Status != "PUBLISHED" {
			continue
		}
		price := 0
		if len(p.SKUs) > 0 {
			price = p.SKUs[0].Price
			for _, sk := range p.SKUs {
				if sk.Price < price {
					price = sk.Price
				}
			}
		}
		out = append(out, M{"id": p.ID, "name": p.Name, "cover": p.Cover, "price": price})
	}
	return out, nil
}

// liveSourceStats：直播来源订单统计（管理端）。
func (s *Server) liveSourceStats(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		Days int `json:"days"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.Days <= 0 {
		d.Days = 30
	}
	if d.Days > 365 {
		d.Days = 365
	}
	rows, err := s.store.db.QueryContext(ctx, "SELECT JSON_UNQUOTE(JSON_EXTRACT(payload,'$.sourceLive')),COUNT(*),COALESCE(SUM(total),0) FROM orders WHERE JSON_UNQUOTE(JSON_EXTRACT(payload,'$.sourceLive')) IN ('orchard','stall') AND payment_status<>'UNPAID' AND created_at>=DATE_SUB(NOW(),INTERVAL ? DAY) GROUP BY JSON_UNQUOTE(JSON_EXTRACT(payload,'$.sourceLive'))", d.Days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []M{}
	for rows.Next() {
		var ch string
		var n, sum int
		if err = rows.Scan(&ch, &n, &sum); err != nil {
			return nil, err
		}
		items = append(items, M{"channel": ch, "orders": n, "amount": sum})
	}
	return M{"days": d.Days, "items": items}, nil
}
