package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// Keep the existing per-user row; transparently read the legacy single address.
func decodeAddresses(raw []byte) ([]Address, error) {
	var list []Address
	if err := json.Unmarshal(raw, &list); err == nil {
		if list == nil {
			list = []Address{}
		}
		return list, nil
	}
	var old Address
	if err := json.Unmarshal(raw, &old); err != nil {
		return nil, err
	}
	old.ID, old.IsDefault = "legacy", true
	return []Address{old}, nil
}

func (s *Store) addresses(ctx context.Context, openid string) ([]Address, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, "SELECT payload FROM addresses WHERE openid=?", openid).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return []Address{}, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeAddresses(raw)
}

func changeAddresses(list []Address, action string, a Address, id string) ([]Address, *Address, error) {
	index := -1
	target := id
	if action == "address.save" {
		target = a.ID
	}
	for i := range list {
		if list[i].ID == target {
			index = i
			break
		}
	}
	var saved *Address
	switch action {
	case "address.save":
		var err error
		a, err = validateAddress(a)
		if err != nil {
			return nil, nil, err
		}
		if a.ID != "" && index < 0 {
			return nil, nil, errors.New("地址不存在")
		}
		if index < 0 {
			a.ID = randomID("ADR")
			a.IsDefault = a.IsDefault || len(list) == 0
			list = append(list, a)
			index = len(list) - 1
		} else {
			a.IsDefault = a.IsDefault || list[index].IsDefault
			list[index] = a
		}
		if a.IsDefault {
			for i := range list {
				list[i].IsDefault = i == index
			}
		}
	case "address.default":
		if index < 0 {
			return nil, nil, errors.New("地址不存在")
		}
		for i := range list {
			list[i].IsDefault = i == index
		}
	case "address.delete":
		if index < 0 {
			return nil, nil, errors.New("地址不存在")
		}
		wasDefault := list[index].IsDefault
		list = append(list[:index], list[index+1:]...)
		if wasDefault && len(list) > 0 {
			list[0].IsDefault = true
		}
	default:
		return nil, nil, errors.New("未知地址操作")
	}
	if action == "address.save" {
		saved = &list[index]
	}
	return list, saved, nil
}

func (s *Store) changeAddress(ctx context.Context, openid, action string, a Address, id string) (any, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Serialize changes for this user, including the first address creation.
	_, err = tx.ExecContext(ctx, "INSERT INTO addresses(openid,payload) VALUES(?,'[]') ON DUPLICATE KEY UPDATE openid=VALUES(openid)", openid)
	if err != nil {
		return nil, err
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, "SELECT payload FROM addresses WHERE openid=? FOR UPDATE", openid).Scan(&raw); err != nil {
		return nil, err
	}
	list, err := decodeAddresses(raw)
	if err != nil {
		return nil, err
	}
	list, saved, err := changeAddresses(list, action, a, id)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE addresses SET payload=? WHERE openid=?", jsonValue(list), openid); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if saved != nil {
		return saved, nil
	}
	return list, nil
}
