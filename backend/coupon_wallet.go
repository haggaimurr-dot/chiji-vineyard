package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

type WalletCoupon struct {
	Coupon
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func walletStatus(c Coupon, used, reserved bool, now time.Time) (string, string) {
	switch {
	case used:
		return "USED", "已使用"
	case reserved:
		return "RESERVED", "待付款订单占用中"
	case !c.Active:
		return "DISABLED", "优惠券已停用或下架"
	case chinaDay(now) > c.EndDate:
		return "EXPIRED", "已过期"
	case chinaDay(now) < c.StartDate:
		return "UPCOMING", "尚未生效"
	default:
		return "AVAILABLE", "可使用"
	}
}

func (s *Server) couponWallet(ctx context.Context, openid string) ([]WalletCoupon, error) {
	// One statement gives a consistent view of ownership, current rules and order occupancy.
	rows, err := s.store.db.QueryContext(ctx, `SELECT w.code,w.payload,c.payload,
 EXISTS(SELECT 1 FROM orders o WHERE o.openid=w.openid AND JSON_UNQUOTE(JSON_EXTRACT(o.payload,'$.quote.couponCode'))=w.code AND o.payment_status<>'UNPAID'),
 EXISTS(SELECT 1 FROM orders o WHERE o.openid=w.openid AND JSON_UNQUOTE(JSON_EXTRACT(o.payload,'$.quote.couponCode'))=w.code AND o.payment_status='UNPAID' AND o.order_status NOT IN ('CANCELLED','CLOSED'))
 FROM user_coupons w LEFT JOIN configs c ON c.config_key='coupons' WHERE w.openid=? ORDER BY w.created_at DESC,w.code`, openid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []WalletCoupon{}
	now := time.Now()
	for rows.Next() {
		var code string
		var snapshot []byte
		var current sql.NullString
		var used, reserved bool
		if err = rows.Scan(&code, &snapshot, &current, &used, &reserved); err != nil {
			return nil, err
		}
		var c Coupon
		if err = json.Unmarshal(snapshot, &c); err != nil {
			return nil, err
		}
		c.Active = false
		if current.Valid {
			var config CouponConfig
			if err = json.Unmarshal([]byte(current.String), &config); err != nil {
				return nil, err
			}
			for _, item := range config.Items {
				if item.Code == code {
					c = item
					break
				}
			}
		}
		status, reason := walletStatus(c, used, reserved, now)
		result = append(result, WalletCoupon{Coupon: c, Status: status, Reason: reason})
	}
	return result, rows.Err()
}

func (s *Server) redeemCoupon(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		Code string `json:"code"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	d.Code = strings.ToUpper(strings.TrimSpace(d.Code))
	if !couponRE.MatchString(d.Code) {
		return nil, errors.New("请输入有效优惠券兑换码")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var payload []byte
	if err = tx.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key='coupons' FOR UPDATE").Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("优惠券不存在")
		}
		return nil, err
	}
	var config CouponConfig
	if err = json.Unmarshal(payload, &config); err != nil {
		return nil, err
	}
	for _, c := range config.Items {
		if c.Code != d.Code {
			continue
		}
		if !c.Active || chinaDay(time.Now()) > c.EndDate {
			return nil, errors.New("优惠券已停用或过期")
		}
		snapshot, err := json.Marshal(c)
		if err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO user_coupons(openid,code,payload) VALUES(?,?,?) ON DUPLICATE KEY UPDATE code=VALUES(code)", openid, c.Code, snapshot)
		if err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return map[string]any{"code": c.Code}, nil
	}
	return nil, errors.New("优惠券不存在")
}

func bestWalletCoupon(wallet []WalletCoupon, base Quote, now time.Time) string {
	// Actual saving first; ties consume the earliest expiring coupon, then sort by code.
	candidates := append([]WalletCoupon(nil), wallet...)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].EndDate != candidates[j].EndDate {
			return candidates[i].EndDate < candidates[j].EndDate
		}
		return candidates[i].Code < candidates[j].Code
	})
	best, saving := "", 0
	for _, c := range candidates {
		if c.Status != "AVAILABLE" {
			continue
		}
		q := base
		if discountCoupon(c.Coupon, &q, now) == nil && q.Discount > saving {
			best, saving = c.Code, q.Discount
		}
	}
	return best
}

// 发券效果统计：按模板聚合领取/核销/过期数。
type couponStat struct {
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	Minimum    int     `json:"minimum"`
	Amount     int     `json:"amount"`
	Type       string  `json:"type"`
	Percent    int     `json:"percent"`
	MaxDiscount int    `json:"maxDiscount"`
	NewUserOnly bool   `json:"newUserOnly"`
	Issued     int     `json:"issued"`
	Used       int     `json:"used"`
	Expired    int     `json:"expired"`
	Active     int     `json:"active"`
	RedeemRate float64 `json:"redeemRate"`
}

func (s *Server) couponStats(ctx context.Context) (any, error) {
	var raw []byte
	if err := s.store.db.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key='coupons'").Scan(&raw); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var config CouponConfig
	if raw != nil {
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, err
		}
	}
	now := time.Now()
	stats := []couponStat{}
	total := couponStat{}
	for _, item := range config.Items {
		st := couponStat{Code: item.Code, Name: item.Name, Minimum: item.Minimum, Amount: item.Amount, Type: couponType(item), Percent: item.Percent, MaxDiscount: item.MaxDiscount, NewUserOnly: item.NewUserOnly}
		if err := s.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_coupons WHERE code=?", item.Code).Scan(&st.Issued); err != nil {
			return nil, err
		}
		if err := s.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_coupons w WHERE w.code=? AND EXISTS(
			SELECT 1 FROM orders o WHERE o.openid=w.openid AND JSON_UNQUOTE(JSON_EXTRACT(o.payload,'$.quote.couponCode'))=w.code AND o.payment_status<>'UNPAID')`, item.Code).Scan(&st.Used); err != nil {
			return nil, err
		}
		if item.EndDate != "" {
			if end, err := time.Parse("2006-01-02", item.EndDate); err == nil && now.After(end.AddDate(0, 0, 1)) {
				st.Expired = st.Issued - st.Used
			}
		}
		if st.Expired < 0 {
			st.Expired = 0
		}
		st.Active = st.Issued - st.Used - st.Expired
		if st.Issued > 0 {
			st.RedeemRate = float64(int(float64(st.Used) / float64(st.Issued) * 10000)) / 100
		}
		stats = append(stats, st)
		total.Issued += st.Issued
		total.Used += st.Used
		total.Expired += st.Expired
		total.Active += st.Active
	}
	if total.Issued > 0 {
		total.RedeemRate = float64(int(float64(total.Used) / float64(total.Issued) * 10000)) / 100
	}
	return M{"items": stats, "total": total}, nil
}
