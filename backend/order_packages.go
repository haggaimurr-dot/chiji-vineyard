package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type PackageItem struct {
	ItemIndex int `json:"itemIndex"`
	Quantity  int `json:"quantity"`
}
type OrderPackage struct {
	ID         string        `json:"id"`
	Number     string        `json:"number"`
	Items      []PackageItem `json:"items"`
	Shipment   *Shipment     `json:"shipment,omitempty"`
	ReceivedAt string        `json:"receivedAt,omitempty"`
}
type OrderEvent struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Detail    string `json:"detail"`
	Actor     string `json:"actor"`
	PackageID string `json:"packageId,omitempty"`
	CreatedAt string `json:"createdAt"`
}

func appendOrderEvent(o *Order, kind, detail, actor, packageID string) {
	o.Events = append(o.Events, OrderEvent{ID: randomID("OE"), Kind: kind, Detail: detail, Actor: actor, PackageID: packageID, CreatedAt: nowString()})
}

// Use immutable order-line indexes: even repeated SKUs can be allocated precisely.
func validatePackagePlan(o Order, plan [][]PackageItem) error {
	if len(plan) < 2 || len(plan) > 10 {
		return errors.New("请拆成2至10个包裹")
	}
	totals := make([]int, len(o.Items))
	for _, items := range plan {
		if len(items) == 0 {
			return errors.New("包裹不能为空")
		}
		seen := map[int]bool{}
		for _, item := range items {
			if item.ItemIndex < 0 || item.ItemIndex >= len(o.Items) || item.Quantity <= 0 || seen[item.ItemIndex] {
				return errors.New("包裹商品或数量无效、同一包裹不能重复商品行")
			}
			seen[item.ItemIndex] = true
			// Check before adding to avoid integer overflow from untrusted quantities.
			if item.Quantity > o.Items[item.ItemIndex].Quantity-totals[item.ItemIndex] {
				return errors.New("包裹商品总数量超过订单数量")
			}
			totals[item.ItemIndex] += item.Quantity
		}
	}
	for i, item := range o.Items {
		if totals[i] != item.Quantity {
			return errors.New("每件商品必须完整分配到包裹，不能遗漏")
		}
	}
	return nil
}

func packagesShipped(o Order) bool {
	if len(o.Packages) == 0 {
		return o.Shipment != nil
	}
	for _, p := range o.Packages {
		if p.Shipment == nil {
			return false
		}
	}
	return true
}

func (s *Server) splitOrder(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		OrderID         string          `json:"orderId"`
		ExpectedVersion int             `json:"expectedVersion"`
		Packages        [][]PackageItem `json:"packages"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
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
	if o == nil || o.OrderStatus != "CONFIRMED" || !canShip(*o) || o.Shipment != nil {
		return nil, errors.New("仅已付款且未发货、无进行中退款的订单可以拆单")
	}
	if o.PackageVersion != d.ExpectedVersion {
		return nil, errors.New("包裹方案已变更，请刷新重试")
	}
	if err = validatePackagePlan(*o, d.Packages); err != nil {
		return nil, err
	}
	// Electronic labels are order-scoped; never orphan an existing provider label.
	var labels int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM order_waybills WHERE order_id=?", o.ID).Scan(&labels); err != nil {
		return nil, err
	}
	if labels > 0 {
		return nil, errors.New("订单已有电子面单，不能拆单")
	}
	o.Packages = make([]OrderPackage, len(d.Packages))
	for i, items := range d.Packages {
		o.Packages[i] = OrderPackage{ID: randomID("PKG"), Number: fmt.Sprintf("%s-%02d", o.OrderNo, i+1), Items: items}
	}
	o.PackageVersion++
	appendOrderEvent(o, "ORDER_SPLIT", fmt.Sprintf("拆分为%d个履约子单（包裹）", len(o.Packages)), actor.Username, "")
	if _, err = tx.ExecContext(ctx, "UPDATE orders SET payload=? WHERE id=?", jsonValue(o), o.ID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "ORDER_SPLIT", o.ID, jsonValue(M{"packages": o.Packages, "version": o.PackageVersion})); err != nil {
		return nil, err
	}
	return o, tx.Commit()
}

func (s *Server) shipPackage(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		OrderID         string `json:"orderId"`
		PackageID       string `json:"packageId"`
		ExpectedVersion int    `json:"expectedVersion"`
		ShipmentInput
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	input, err := validateShipment(d.ShipmentInput)
	if err != nil {
		return nil, err
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
	index := -1
	for i, p := range o.Packages {
		if p.ID == d.PackageID {
			index = i
		}
	}
	if index < 0 {
		return nil, errors.New("包裹不存在")
	}
	p := &o.Packages[index]
	if p.Shipment != nil {
		if shipmentInput(*p.Shipment) == input {
			return o, nil
		}
		return nil, errors.New("包裹已发货，不能覆盖原运单")
	}
	if o.OrderStatus != "CONFIRMED" || !canShip(*o) {
		return nil, errors.New("当前订单不能发货")
	}
	if o.PackageVersion != d.ExpectedVersion {
		return nil, errors.New("包裹已变更，请刷新重试")
	}
	for _, other := range o.Packages {
		if other.Shipment != nil && sameShipment(*other.Shipment, ShippingInput{Carrier: input.Carrier, CarrierCode: input.CarrierCode, TrackingNo: input.TrackingNo}) {
			return nil, errors.New("包裹运单号不能重复")
		}
	}
	shipment := Shipment{Carrier: input.Carrier, CarrierCode: input.CarrierCode, TrackingNo: input.TrackingNo, ShippedAt: nowString(), SyncStatus: "PENDING"}
	if err = reserveShipmentNumber(ctx, tx, o.ID, shipment); err != nil {
		return nil, err
	}
	p.Shipment = &shipment
	if o.Shipment == nil {
		o.Shipment = &shipment
	}
	if packagesShipped(*o) {
		o.FulfillmentStatus = "SHIPPED"
	}
	o.PackageVersion++
	appendOrderEvent(o, "PACKAGE_SHIPPED", p.Number+" 已发货："+shipment.Carrier+" "+shipment.TrackingNo, actor.Username, p.ID)
	if _, err = tx.ExecContext(ctx, "UPDATE orders SET fulfillment_status=?,payload=? WHERE id=?", o.FulfillmentStatus, jsonValue(o), o.ID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "ORDER_PACKAGE_SHIP", o.ID, jsonValue(M{"packageId": p.ID, "shipment": shipment})); err != nil {
		return nil, err
	}
	if packagesShipped(*o) {
		if _, err = tx.ExecContext(ctx, "INSERT INTO shipping_notifications(order_id,openid,notified_at) VALUES(?,?,NOW(3)) ON DUPLICATE KEY UPDATE notified_at=COALESCE(notified_at,VALUES(notified_at))", o.ID, o.OpenID); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	// Multi-parcel synchronization has its own retry; shipping stays committed.
	return o, nil
}

func (s *Server) orderEvents(ctx context.Context, id, openid string) (any, error) {
	o, err := s.store.order(ctx, s.store.db, id, false)
	if err != nil {
		return nil, err
	}
	if o == nil || openid != "" && o.OpenID != openid {
		return nil, errors.New("订单不存在")
	}
	events := o.Events
	if events == nil {
		events = []OrderEvent{}
	}
	if openid != "" {
		for i := range events {
			events[i].Actor = "商家/系统"
		}
	}
	return events, nil
}

func (s *Server) syncPackages(ctx context.Context, actor Claims, id string) (any, error) {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	o, err := s.store.order(ctx, tx, id, true)
	if err != nil {
		return nil, err
	}
	if o == nil || len(o.Packages) == 0 || o.Shipment == nil {
		return nil, errors.New("没有可同步的包裹")
	}
	if o.PaymentStatus != "PAID" || (o.OrderStatus != "CONFIRMED" && o.OrderStatus != "COMPLETED") || (o.RefundStatus != "NONE" && o.RefundStatus != "REJECTED" && o.RefundStatus != "PARTIAL") {
		return nil, errors.New("退款或终止状态不能同步发货信息")
	}
	// Serialize uploads with package shipping and other syncs under the order lock.
	err = s.wechat.uploadShipping(*o, *o.Shipment)
	status, message := "SUCCESS", ""
	if err != nil {
		status, message = "FAILED", clean(err.Error(), 240)
	}
	for i := range o.Packages {
		if p := o.Packages[i].Shipment; p != nil {
			p.SyncStatus, p.SyncError = status, message
		}
	}
	o.Shipment.SyncStatus, o.Shipment.SyncError = status, message
	appendOrderEvent(o, "PACKAGES_SYNC", "微信发货同步："+status, actor.Username, "")
	if _, err = tx.ExecContext(ctx, "UPDATE orders SET payload=? WHERE id=?", jsonValue(o), o.ID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if packagesShipped(*o) {
		_, _ = s.sendShippingNotice(ctx, o.ID)
	}
	return o, nil
}
