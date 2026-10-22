package main

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
)

type ShipmentInput struct {
	Carrier     string `json:"carrier"`
	CarrierCode string `json:"carrierCode"`
	TrackingNo  string `json:"trackingNo"`
}

var carrierCodeRE = regexp.MustCompile(`^[A-Z0-9_-]{1,20}$`)
var trackingNoRE = regexp.MustCompile(`^[A-Za-z0-9_-]{4,50}$`)

func validateShipment(d ShipmentInput) (ShipmentInput, error) {
	d.Carrier = strings.TrimSpace(d.Carrier)
	d.CarrierCode = strings.ToUpper(strings.TrimSpace(d.CarrierCode))
	d.TrackingNo = strings.TrimSpace(d.TrackingNo)
	if d.Carrier == "" || len([]rune(d.Carrier)) > 40 || strings.ContainsAny(d.Carrier, "<>") || strings.IndexFunc(d.Carrier, unicode.IsControl) >= 0 {
		return d, errors.New("请填写物流公司名称，最多40字且不能包含特殊控制字符")
	}
	if !carrierCodeRE.MatchString(d.CarrierCode) {
		return d, errors.New("物流公司编码须为1–20位字母、数字、下划线或短横线")
	}
	if !trackingNoRE.MatchString(d.TrackingNo) {
		return d, errors.New("物流单号须为4–50位字母、数字、下划线或短横线")
	}
	return d, nil
}

func shipmentInput(s Shipment) ShipmentInput {
	return ShipmentInput{Carrier: s.Carrier, CarrierCode: s.CarrierCode, TrackingNo: s.TrackingNo}
}

func shipmentSyncActive(s *Shipment, now time.Time) bool {
	if s == nil || s.SyncStatus != "PENDING" {
		return false
	}
	started, err := time.Parse(time.RFC3339Nano, s.SyncStartedAt)
	return err == nil && now.Sub(started) < 2*time.Minute
}

func canUpdateShipment(o Order) bool {
	return o.Shipment != nil && o.OrderStatus == "CONFIRMED" && o.PaymentStatus == "PAID" && o.FulfillmentStatus == "SHIPPED" && (o.RefundStatus == "NONE" || o.RefundStatus == "REJECTED" || o.RefundStatus == "PARTIAL")
}

// Compatibility entry point for the existing batch-shipping workflow.
func (s *Server) shipOrderInput(ctx context.Context, actor Claims, d ShippingInput, resync, idempotent bool) (M, error) {
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	mode := "ship"
	if resync {
		mode = "resync"
	} else if idempotent {
		mode = "batchShip"
	}
	value, err := s.changeShipment(ctx, actor, raw, mode)
	if err != nil {
		return nil, err
	}
	return value.(M), nil
}

func (s *Server) changeShipment(ctx context.Context, actor Claims, raw json.RawMessage, mode string) (any, error) {
	var d struct {
		OrderID string `json:"orderId"`
		ShipmentInput
		ExpectedShipment *ShipmentInput `json:"expectedShipment"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	d.OrderID = strings.TrimSpace(d.OrderID)
	if !shippingOrderIDRE.MatchString(d.OrderID) {
		return nil, errors.New("订单 ID 无效")
	}
	if mode != "resync" {
		validated, err := validateShipment(d.ShipmentInput)
		if err != nil {
			return nil, err
		}
		d.ShipmentInput = validated
	}
	if mode == "update" && d.ExpectedShipment == nil {
		return nil, errors.New("缺少原发货信息，请刷新订单后重试")
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
	if len(o.Packages) > 0 {
		return nil, errors.New("拆单订单请按包裹发货或同步")
	}
	if mode == "batchShip" && (o.FulfillmentStatus == "SHIPPED" || o.FulfillmentStatus == "RECEIVED") && o.Shipment != nil {
		input := ShippingInput{OrderID: d.OrderID, Carrier: d.Carrier, CarrierCode: d.CarrierCode, TrackingNo: d.TrackingNo}
		if !sameShipment(*o.Shipment, input) {
			return nil, errors.New("订单已发货，不能更换快递或运单号")
		}
		// A retry confirms the committed shipment without sending it to
		// providers or notifying the customer again, even while sync is pending.
		return M{"shipment": *o.Shipment, "syncStatus": o.Shipment.SyncStatus}, nil
	}
	if shipmentSyncActive(o.Shipment, time.Now()) {
		return nil, errors.New("物流信息正在同步微信，请稍后刷新再操作")
	}
	var shipment Shipment
	auditAction := "ORDER_SHIP"
	switch mode {
	case "append":
		if !canUpdateShipment(*o) {
			return nil, errors.New("仅未收货且无进行中退款的已发货订单可增加包裹")
		}
		for _, parcel := range shipmentParcels(*o.Shipment) {
			if strings.EqualFold(parcel.CarrierCode, d.CarrierCode) && strings.EqualFold(parcel.TrackingNo, d.TrackingNo) {
				return M{"shipment": *o.Shipment, "syncStatus": o.Shipment.SyncStatus}, nil
			}
		}
		if len(o.Shipment.Parcels) >= 9 {
			return nil, errors.New("每笔订单最多10个包裹")
		}
		shipment = *o.Shipment
		auditAction = "ORDER_PARCEL_ADD"
	case "ship", "batchShip":
		if !canShip(*o) || o.OrderStatus != "CONFIRMED" {
			return nil, errors.New("当前订单不能发货")
		}
		if err = requireOrderBatches(ctx, tx, *o); err != nil {
			return nil, err
		}
		shipment = Shipment{ShippedAt: nowString()}
	case "update":
		if !canUpdateShipment(*o) {
			return nil, errors.New("当前订单不能更正发货信息；请确认尚未收货且没有进行中退款")
		}
		if *d.ExpectedShipment != shipmentInput(*o.Shipment) {
			return nil, errors.New("发货信息已被其他管理员修改，请刷新订单后重试")
		}
		shipment = *o.Shipment
		if d.ShipmentInput == shipmentInput(shipment) {
			return M{"shipment": shipment, "syncStatus": shipment.SyncStatus, "shippingNotice": M{"status": o.ShippingNoticeStatus, "error": o.ShippingNoticeError}}, nil
		}
		auditAction = "ORDER_SHIPMENT_UPDATE"
	case "resync":
		if o.Shipment == nil || (o.FulfillmentStatus != "SHIPPED" && o.FulfillmentStatus != "RECEIVED") {
			return nil, errors.New("无可重试的发货信息")
		}
		if o.PaymentStatus != "PAID" || (o.RefundStatus != "NONE" && o.RefundStatus != "REJECTED" && o.RefundStatus != "PARTIAL") || (o.OrderStatus != "CONFIRMED" && o.OrderStatus != "COMPLETED") {
			return nil, errors.New("当前订单处于退款或终止状态，不能同步发货信息")
		}
		shipment = *o.Shipment
		d.ShipmentInput, err = validateShipment(shipmentInput(shipment))
		if err != nil {
			return nil, err
		}
		auditAction = "ORDER_SHIPMENT_RESYNC"
	default:
		return nil, errors.New("发货操作无效")
	}
	if mode == "append" {
		parcel := Shipment{Carrier: d.Carrier, CarrierCode: d.CarrierCode, TrackingNo: d.TrackingNo, ShippedAt: nowString()}
		if err = reserveShipmentNumber(ctx, tx, o.ID, parcel); err != nil {
			return nil, err
		}
		shipment.Parcels = append(append([]Shipment{}, shipment.Parcels...), parcel)
	} else {
		shipment.Carrier, shipment.CarrierCode, shipment.TrackingNo = d.Carrier, d.CarrierCode, d.TrackingNo
	}
	if mode != "resync" && mode != "append" {
		if err = validateWaybillShipment(ctx, tx, o.ID, shipment); err != nil {
			return nil, err
		}
		if err = reserveShipmentNumber(ctx, tx, o.ID, shipment); err != nil {
			return nil, err
		}
	}
	shipment.SyncStatus, shipment.SyncError = "PENDING", ""
	shipment.SyncAttempt, shipment.SyncStartedAt = randomID("SHIP"), nowString()
	// Claim the provider upload under the order lock. Corrections retain the
	// original shipment time and never reset the original notice/read state.
	event := OrderEvent{ID: randomID("OE"), Kind: auditAction, Detail: shipment.Carrier + " " + shipment.TrackingNo, Actor: actor.Username, CreatedAt: nowString()}
	if mode == "ship" || mode == "batchShip" {
		_, err = tx.ExecContext(ctx, "UPDATE orders SET fulfillment_status='SHIPPED',payload=JSON_SET(payload,'$.fulfillmentStatus','SHIPPED','$.shipment',CAST(? AS JSON),'$.events',JSON_ARRAY_APPEND(COALESCE(JSON_EXTRACT(payload,'$.events'),JSON_ARRAY()),'$',CAST(? AS JSON))) WHERE id=?", jsonValue(shipment), jsonValue(event), o.ID)
		if err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO shipping_notifications(order_id,openid,notified_at) VALUES(?,?,NOW(3)) ON DUPLICATE KEY UPDATE notified_at=COALESCE(notified_at,VALUES(notified_at))", o.ID, o.OpenID)
		}
		o.FulfillmentStatus = "SHIPPED"
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE orders SET payload=JSON_SET(payload,'$.shipment',CAST(? AS JSON),'$.events',JSON_ARRAY_APPEND(COALESCE(JSON_EXTRACT(payload,'$.events'),JSON_ARRAY()),'$',CAST(? AS JSON))) WHERE id=?", jsonValue(shipment), jsonValue(event), o.ID)
	}
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, auditAction, o.ID, jsonValue(M{"before": o.Shipment, "shipment": shipment})); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	o.Shipment = &shipment
	committedShipment := shipment
	if err = s.wechat.uploadShipping(*o, shipment); err != nil {
		shipment.SyncStatus, shipment.SyncError = "FAILED", clean(err.Error(), 240)
	} else {
		shipment.SyncStatus, shipment.SyncError = "SUCCESS", ""
	}
	// The edit was committed already. Finish recording the upload even when
	// the browser closes or its request deadline expires during the provider call.
	resultCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	// An older upload must never overwrite a later correction or retry.
	result, err := s.store.db.ExecContext(resultCtx, "UPDATE orders SET payload=JSON_SET(payload,'$.shipment',CAST(? AS JSON)) WHERE id=? AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.shipment.syncAttempt'))=?", jsonValue(shipment), o.ID, shipment.SyncAttempt)
	if err != nil {
		// Shipping has committed already; retain its success and pending sync
		// state when only saving the provider response fails.
		shipment = committedShipment
	} else {
		n, resultErr := result.RowsAffected()
		if resultErr != nil {
			shipment = committedShipment
		} else if n == 0 {
			if mode != "ship" && mode != "batchShip" {
				return nil, errors.New("发货信息已更新，请刷新订单查看最新同步状态")
			}
			shipment = committedShipment
		}
	}
	push := M{"status": o.ShippingNoticeStatus, "error": o.ShippingNoticeError}
	if mode == "ship" || mode == "batchShip" {
		var pushErr error
		push, pushErr = s.sendShippingNotice(ctx, o.ID)
		if pushErr != nil {
			push = M{"status": "UNAVAILABLE"}
		}
	}
	// Persisted data remains a success even when WeChat rejects the upload;
	// clients show syncStatus/syncError and offer a separate retry.
	return M{"shipment": shipment, "syncStatus": shipment.SyncStatus, "shippingNotice": push}, nil
}
