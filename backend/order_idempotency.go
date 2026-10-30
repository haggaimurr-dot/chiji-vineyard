package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
)

type CreateOrderInput struct {
	IdempotencyKey string     `json:"idempotencyKey"`
	CouponCode     string     `json:"couponCode"`
	DeliveryDate   string     `json:"deliveryDate"`
	Items          []CartItem `json:"items"`
	Address        Address    `json:"address"`
	Note           string     `json:"note"`
	SourceArticleID string    `json:"sourceArticleId"`
	SourceLive      string    `json:"sourceLive"`
}

var orderKeyRE = regexp.MustCompile(`^[A-Za-z0-9_-]{16,80}$`)

func (s *Server) recoverOrder(ctx context.Context, openid, key string) (any, error) {
	if !orderKeyRE.MatchString(key) {
		return nil, errors.New("下单幂等键无效")
	}
	var id string
	err := s.store.db.QueryRowContext(ctx, "SELECT order_id FROM order_idempotency WHERE openid=? AND idempotency_key=?", openid, key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if id == "" {
		return nil, nil
	}
	return s.store.order(ctx, s.store.db, id, false)
}

// Hash only purchase intent, never client product/price snapshots.
func orderFingerprint(d CreateOrderInput) string {
	d.IdempotencyKey = ""
	d.CouponCode = strings.ToUpper(strings.TrimSpace(d.CouponCode))
	d.Note = clean(d.Note, 100)
	d.Address.ID = ""
	d.Address.IsDefault = false
	items := make([]CartItem, len(d.Items))
	for i, x := range d.Items {
		items[i] = CartItem{ProductID: x.ProductID, SKUID: x.SKUID, Quantity: x.Quantity}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].SKUID == items[j].SKUID {
			return items[i].ProductID < items[j].ProductID
		}
		return items[i].SKUID < items[j].SKUID
	})
	d.Items = items
	raw, _ := json.Marshal(d)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func claimOrderKey(ctx context.Context, tx *sql.Tx, openid, key, fingerprint string) (string, error) {
	if !orderKeyRE.MatchString(key) {
		return "", errors.New("下单幂等键须为16至80位字母、数字、下划线或连字符")
	}
	// The unique row serializes concurrent requests before inventory/coupon locks.
	_, err := tx.ExecContext(ctx, "INSERT INTO order_idempotency(openid,idempotency_key,request_hash,order_id) VALUES(?,?,?,'') ON DUPLICATE KEY UPDATE idempotency_key=idempotency_key", openid, key, fingerprint)
	if err != nil {
		return "", err
	}
	var stored, orderID string
	err = tx.QueryRowContext(ctx, "SELECT request_hash,order_id FROM order_idempotency WHERE openid=? AND idempotency_key=? FOR UPDATE", openid, key).Scan(&stored, &orderID)
	if err != nil {
		return "", err
	}
	if stored != fingerprint {
		return "", errors.New("下单幂等键已用于其他结算内容，请重新发起结算")
	}
	return orderID, nil
}
