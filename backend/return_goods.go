package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

type ReturnItem struct {
	SKUID    string `json:"skuId"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	Restock  int    `json:"restock"`
}
type TicketReturn struct {
	Status       string         `json:"status"`
	Address      Address        `json:"address"`
	Items        []ReturnItem   `json:"items"`
	Shipment     *ShipmentInput `json:"shipment,omitempty"`
	AuthorizedAt string         `json:"authorizedAt"`
	ShippedAt    string         `json:"shippedAt,omitempty"`
	ReceivedAt   string         `json:"receivedAt,omitempty"`
	Inspection   string         `json:"inspection,omitempty"`
	ReceivedBy   string         `json:"receivedBy,omitempty"`
}
type ReturnAction struct {
	Action     string        `json:"action"`
	Address    Address       `json:"address"`
	Items      []ReturnItem  `json:"items"`
	Shipment   ShipmentInput `json:"shipment"`
	Inspection string        `json:"inspection"`
}

func validateReturnItems(items []ReturnItem, available map[string]int, receiving bool) error {
	if len(items) == 0 || len(items) > 100 {
		return errors.New("请选择退货明细")
	}
	seen := map[string]bool{}
	for _, i := range items {
		if seen[i.SKUID] || i.Quantity <= 0 || i.Quantity > available[i.SKUID] || i.Restock < 0 || i.Restock > i.Quantity || (!receiving && i.Restock != 0) {
			return errors.New("退货规格、数量或可售入库数量无效")
		}
		seen[i.SKUID] = true
	}
	if receiving && len(seen) != len(available) {
		return errors.New("请验收全部退货明细")
	}
	return nil
}

// Order lock serializes return authorizations across all tickets for this order.
// Stock, receipt, ticket version and audit commit in one transaction.
func (s *Server) applyTicketReturn(ctx context.Context, tx *sql.Tx, t *AfterSalesTicket, actor *Claims, d ReturnAction) (string, error) {
	if d.Action == "SHIP" {
		if t.Return == nil || (t.Return.Status != "AWAITING_SHIPMENT" && t.Return.Status != "SHIPPED") {
			return "", errors.New("当前退货不能登记寄件")
		}
		shipment, err := validateShipment(d.Shipment)
		if err != nil {
			return "", err
		}
		t.Return.Shipment = &shipment
		t.Return.Status = "SHIPPED"
		t.Return.ShippedAt = nowString()
		return "退货已寄出：" + shipment.Carrier + " / " + shipment.TrackingNo, nil
	}
	if actor == nil {
		return "", errors.New("仅商家可确认退货地址或验收入库")
	}
	o, err := s.store.order(ctx, tx, t.OrderID, true)
	if err != nil {
		return "", err
	}
	if o == nil {
		return "", errors.New("订单不存在")
	}
	if o.FulfillmentStatus != "SHIPPED" && o.FulfillmentStatus != "RECEIVED" {
		return "", errors.New("仅已发货订单可办理实物退货")
	}
	switch d.Action {
	case "AUTHORIZE":
		if t.Return != nil {
			return "", errors.New("此工单已登记退货，地址与明细不可覆盖")
		}
		address, e := validateAddress(d.Address)
		if e != nil {
			return "", e
		}
		address.ID = ""
		address.IsDefault = false
		quantities := map[string]int{}
		names := map[string]string{}
		for _, item := range o.Items {
			quantities[item.SKUID] += item.Quantity
			names[item.SKUID] = item.Name + " / " + item.SKUName
		}
		rows, e := tx.QueryContext(ctx, "SELECT payload FROM after_sales_tickets WHERE order_id=? AND id<>?", o.ID, t.ID)
		if e != nil {
			return "", e
		}
		for rows.Next() {
			var raw []byte
			var other AfterSalesTicket
			if e = rows.Scan(&raw); e == nil {
				e = json.Unmarshal(raw, &other)
			}
			if e != nil {
				rows.Close()
				return "", e
			}
			if other.Return != nil {
				for _, item := range other.Return.Items {
					quantities[item.SKUID] -= item.Quantity
				}
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return "", e
		}
		if e = validateReturnItems(d.Items, quantities, false); e != nil {
			return "", e
		}
		for i := range d.Items {
			d.Items[i].Name = names[d.Items[i].SKUID]
		}
		t.Return = &TicketReturn{Status: "AWAITING_SHIPMENT", Address: address, Items: d.Items, AuthorizedAt: nowString()}
		t.Status = "PROCESSING"
		return "商家已确认退货地址与明细，请按此地址寄回并登记运单", nil
	case "RECEIVE":
		if t.Return == nil || t.Return.Status != "SHIPPED" {
			return "", errors.New("请先登记退货寄件，已验收的退货不能重复入库")
		}
		note := strings.TrimSpace(d.Inspection)
		if note == "" || len([]rune(note)) > 1000 {
			return "", errors.New("请填写验收结果，最多1000字")
		}
		quantities := map[string]int{}
		for _, i := range t.Return.Items {
			quantities[i.SKUID] = i.Quantity
		}
		if e := validateReturnItems(d.Items, quantities, true); e != nil {
			return "", e
		}
		for _, i := range d.Items {
			if i.Quantity != quantities[i.SKUID] {
				return "", errors.New("验收数量须与退货明细一致；缺件请先在工单核实")
			}
		}
		sort.Slice(d.Items, func(i, j int) bool { return d.Items[i].SKUID < d.Items[j].SKUID })
		for _, i := range d.Items {
			if i.Restock > 0 {
				result, e := applyInventory(ctx, tx, i.SKUID, "RETURN_RECEIPT", t.ID, actor.Username, note, "UPDATE skus SET stock=stock+? WHERE id=? AND stock<=2147483647-?", i.Restock, i.SKUID, i.Restock)
				if e != nil {
					return "", e
				}
				count, e := result.RowsAffected()
				if e != nil {
					return "", e
				}
				if count != 1 {
					return "", errors.New("库存超出上限，入库未完成")
				}
			}
			for n := range t.Return.Items {
				if t.Return.Items[n].SKUID == i.SKUID {
					t.Return.Items[n].Restock = i.Restock
				}
			}
		}
		t.Return.Status = "RECEIVED"
		t.Return.ReceivedAt = nowString()
		t.Return.ReceivedBy = actor.Username
		t.Return.Inspection = note
		return "退货已验收：" + note + "；仅验收可售数量计入库存，其余隔离处理", nil
	default:
		return "", errors.New("退货操作无效")
	}
}

func (s *Server) returnTracking(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct{ ID string }
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	var user string
	var stored []byte
	if err := s.store.db.QueryRowContext(ctx, "SELECT openid,payload FROM after_sales_tickets WHERE id=?", d.ID).Scan(&user, &stored); err != nil {
		return nil, errors.New("工单不存在")
	}
	if openid != "" && user != openid {
		return nil, errors.New("工单不存在")
	}
	var t AfterSalesTicket
	if err := json.Unmarshal(stored, &t); err != nil {
		return nil, err
	}
	if t.Return == nil || t.Return.Shipment == nil {
		return nil, errors.New("退货尚未寄出")
	}
	p := t.Return.Shipment
	return queryTracking(ctx, Order{Address: t.Return.Address, Shipment: &Shipment{Carrier: p.Carrier, CarrierCode: p.CarrierCode, TrackingNo: p.TrackingNo}})
}
