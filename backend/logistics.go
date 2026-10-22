package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// Primary shipment remains compatible with older clients; parcels are additional packages.
func shipmentParcels(shipment Shipment) []Shipment {
	primary := shipment
	primary.Parcels = nil
	return append([]Shipment{primary}, shipment.Parcels...)
}

func selectParcel(o *Order, number, code string) error {
	if o.Shipment == nil {
		return errors.New("订单尚未发货")
	}
	if number == "" {
		return nil
	}
	for _, parcel := range shipmentParcels(*o.Shipment) {
		if strings.EqualFold(parcel.TrackingNo, number) && (code == "" || strings.EqualFold(parcel.CarrierCode, code)) {
			o.Shipment = &parcel
			return nil
		}
	}
	return errors.New("运单不属于该订单")
}

type LogisticsTask struct {
	ID          string `json:"id"`
	OrderID     string `json:"orderId"`
	OrderNo     string `json:"orderNo"`
	CarrierCode string `json:"carrierCode"`
	TrackingNo  string `json:"trackingNo"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	Owner       string `json:"owner"`
	Note        string `json:"note"`
	Version     int    `json:"version"`
	UpdatedAt   string `json:"updatedAt"`
	History     []M    `json:"history"`
}

func (s *Server) logisticsTasks(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		Status  string `json:"status"`
		OrderID string `json:"orderId"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	rows, err := s.store.db.QueryContext(ctx, "SELECT payload FROM logistics_tasks WHERE (?='' OR status=?) AND (?='' OR order_id=?) ORDER BY updated_at DESC LIMIT 200", d.Status, d.Status, d.OrderID, d.OrderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []LogisticsTask{}
	for rows.Next() {
		var b []byte
		var task LogisticsTask
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &task); err != nil {
			return nil, err
		}
		items = append(items, task)
	}
	return items, rows.Err()
}

func (s *Server) saveLogisticsTask(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d LogisticsTask
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if !shippingOrderIDRE.MatchString(d.OrderID) || len([]rune(d.Owner)) > 60 || len([]rune(d.Note)) > 1000 || strings.TrimSpace(d.Note) == "" {
		return nil, errors.New("请填写订单、处理说明（最多1000字）及有效负责人")
	}
	if d.Status != "OPEN" && d.Status != "PROCESSING" && d.Status != "RESOLVED" {
		return nil, errors.New("任务状态无效")
	}
	if d.Kind != "DELAY" && d.Kind != "EXCEPTION" && d.Kind != "RETURN" && d.Kind != "SYNC" && d.Kind != "NOTICE" {
		return nil, errors.New("异常类型无效")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	o, err := s.store.order(ctx, tx, d.OrderID, true)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, errors.New("订单不存在")
	}
	if d.ID == "" {
		if err = selectParcel(o, d.TrackingNo, d.CarrierCode); err != nil {
			return nil, err
		}
		d.CarrierCode, d.TrackingNo = o.Shipment.CarrierCode, o.Shipment.TrackingNo
		var id string
		err = tx.QueryRowContext(ctx, "SELECT id FROM logistics_tasks WHERE order_id=? AND carrier_code=? AND tracking_no=? AND kind=? AND status<>'RESOLVED' LIMIT 1", d.OrderID, d.CarrierCode, d.TrackingNo, d.Kind).Scan(&id)
		if err == nil {
			return nil, errors.New("该运单已有相同类型的未关闭任务，请继续跟进")
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		d.ID = randomID("LOG")
		d.OrderNo = o.OrderNo
		d.Version = 1
		d.History = []M{}
	} else {
		var b []byte
		if err = tx.QueryRowContext(ctx, "SELECT payload FROM logistics_tasks WHERE id=? AND order_id=? FOR UPDATE", d.ID, d.OrderID).Scan(&b); err != nil {
			return nil, err
		}
		var old LogisticsTask
		if err = json.Unmarshal(b, &old); err != nil {
			return nil, err
		}
		if d.Version != old.Version {
			return nil, errors.New("任务已被修改，请刷新后重试")
		}
		old.Owner, old.Note, old.Status = d.Owner, d.Note, d.Status
		old.Version++
		d = old
	}
	d.UpdatedAt = nowString()
	d.History = append(d.History, M{"actor": actor.Username, "status": d.Status, "owner": d.Owner, "note": d.Note, "at": d.UpdatedAt})
	if _, err = tx.ExecContext(ctx, "INSERT INTO logistics_tasks(id,order_id,carrier_code,tracking_no,kind,status,payload) VALUES(?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE status=VALUES(status),payload=VALUES(payload)", d.ID, d.OrderID, d.CarrierCode, d.TrackingNo, d.Kind, d.Status, jsonValue(d)); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "LOGISTICS_TASK_UPDATE", d.ID, jsonValue(d)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return d, nil
}
