package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// 晒图评价激励（新增）：评价审核通过且带图时，按配置自动发一张优惠券到用户券包。
// 每条评价最多发一次（review_rewards 主键防重）；无图不发券。
// 配置键 review_photo_reward：{"enabled":true,"couponCode":"XXX"}，默认关闭。

type reviewPhotoRewardConfig struct {
	Enabled    bool   `json:"enabled"`
	CouponCode string `json:"couponCode"`
}

func (s *Store) reviewPhotoRewardConfig(ctx context.Context) (reviewPhotoRewardConfig, error) {
	var cfg reviewPhotoRewardConfig
	var raw []byte
	if err := s.db.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key='review_photo_reward'").Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return reviewPhotoRewardConfig{}, err
	}
	cfg.CouponCode = strings.ToUpper(strings.TrimSpace(cfg.CouponCode))
	return cfg, nil
}

func (s *Server) reviewRewardConfigGet(ctx context.Context) (any, error) {
	return s.store.reviewPhotoRewardConfig(ctx)
}

func (s *Server) reviewRewardConfigSave(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var cfg reviewPhotoRewardConfig
	if err := decode(raw, &cfg); err != nil {
		return nil, err
	}
	cfg.CouponCode = strings.ToUpper(strings.TrimSpace(cfg.CouponCode))
	if cfg.Enabled && !couponRE.MatchString(cfg.CouponCode) {
		return nil, errors.New("请填写有效的优惠券兑换码")
	}
	payload, _ := json.Marshal(cfg)
	if _, err := s.store.db.ExecContext(ctx, "INSERT INTO configs(config_key,payload) VALUES('review_photo_reward',?) ON DUPLICATE KEY UPDATE payload=VALUES(payload)", payload); err != nil {
		return nil, err
	}
	s.store.audit(ctx, actor, "REVIEW_REWARD_CONFIG", "", M{"enabled": cfg.Enabled, "couponCode": cfg.CouponCode})
	return cfg, nil
}

// issueReviewPhotoReward 在评价审核通过后调用：有图才发券，每条评价只发一次。
// 返回 issued=true 表示本次发出了券。
func (s *Store) issueReviewPhotoReward(ctx context.Context, reviewID, openid string, images []string) (issued bool, err error) {
	if len(images) == 0 {
		return false, nil
	}
	cfg, err := s.reviewPhotoRewardConfig(ctx)
	if err != nil || !cfg.Enabled || cfg.CouponCode == "" {
		return false, err
	}
	var payload []byte
	if err = s.db.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key='coupons'").Scan(&payload); err != nil {
		return false, err
	}
	var config CouponConfig
	if err = json.Unmarshal(payload, &config); err != nil {
		return false, err
	}
	var coupon *Coupon
	for i := range config.Items {
		if config.Items[i].Code == cfg.CouponCode {
			coupon = &config.Items[i]
			break
		}
	}
	if coupon == nil || !coupon.Active || chinaDay(time.Now()) > coupon.EndDate {
		return false, nil
	}
	snapshot, err := json.Marshal(coupon)
	if err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx, "INSERT IGNORE INTO review_rewards(review_id,openid,coupon_code) VALUES(?,?,?)", reviewID, openid, coupon.Code)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return false, nil
	}
	if _, err = s.db.ExecContext(ctx, "INSERT INTO user_coupons(openid,code,payload) VALUES(?,?,?) ON DUPLICATE KEY UPDATE code=VALUES(code)", openid, coupon.Code, snapshot); err != nil {
		return false, err
	}
	return true, nil
}
