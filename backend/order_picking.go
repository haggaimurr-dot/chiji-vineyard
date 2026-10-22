package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

func (s *Server) pickingOrders(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		OrderIDs []string `json:"orderIds"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if len(d.OrderIDs) == 0 || len(d.OrderIDs) > 50 {
		return nil, errors.New("拣货单请选择1至50笔订单")
	}
	seen := map[string]bool{}
	for i, id := range d.OrderIDs {
		id = strings.TrimSpace(id)
		if !shippingOrderIDRE.MatchString(id) || seen[id] {
			return nil, errors.New("订单ID无效或重复")
		}
		seen[id] = true
		d.OrderIDs[i] = id
	}
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	orders := []Order{}
	for _, id := range d.OrderIDs {
		o, err := s.store.order(ctx, tx, id, false)
		if err != nil {
			return nil, err
		}
		if o == nil || o.OrderStatus != "CONFIRMED" || !canShip(*o) {
			return nil, errors.New("部分订单已不可拣货，请刷新后只选择可发货订单")
		}
		orders = append(orders, *o)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return orders, nil
}
