package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func cancelProviderWaybill(ctx context.Context, c waybillSettings, w waybillRecord, providerOrderID, reason string) (string, string) {
	carrier, ok := carrierForWaybill(w.CarrierCode)
	if !ok {
		return "READY", "承运商无效"
	}
	a := c.Accounts[w.CarrierCode]
	param := jsonValue(M{"partnerId": a.PartnerID, "partnerKey": a.PartnerKey, "partnerSecret": a.PartnerSecret, "partnerName": a.PartnerName, "net": a.Net, "code": a.Code, "checkMan": a.CheckMan, "expType": a.ExpType, "kuaidicom": carrier.ProviderCode, "kuaidinum": w.TrackingNo, "orderId": providerOrderID, "reason": reason})
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	sum := md5.Sum([]byte(param + timestamp + c.Key + c.Secret))
	form := url.Values{"method": {"cancel"}, "key": {c.Key}, "t": {timestamp}, "sign": {strings.ToUpper(hex.EncodeToString(sum[:]))}, "param": {param}}
	r, err := http.NewRequestWithContext(ctx, "POST", "https://api.kuaidi100.com/label/order", strings.NewReader(form.Encode()))
	unknown := "取消结果待核实，请在快递100核实后由管理员登记结果；暂不可打印、发货或重新取号"
	if err != nil {
		return "CANCEL_UNCERTAIN", unknown
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := waybillHTTP.Do(r)
	if err != nil {
		return "CANCEL_UNCERTAIN", unknown
	}
	defer response.Body.Close()
	var data struct {
		Success bool            `json:"success"`
		Code    json.RawMessage `json:"code"`
	}
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&data) != nil {
		return "CANCEL_UNCERTAIN", unknown
	}
	code := strings.Trim(string(data.Code), `"`)
	if data.Success && code == "200" {
		return "CANCELLED", ""
	}
	if !data.Success && (code == "30001" || code == "30002" || code == "30003" || code == "30005" || code == "30006") {
		return "READY", "服务商拒绝取消（" + code + "），原面单仍保留；请核实承运商及账号配置"
	}
	return "CANCEL_UNCERTAIN", unknown
}

func (s *Server) cancelWaybill(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		OrderID   string `json:"orderId"`
		WaybillID string `json:"waybillId"`
		Reason    string `json:"reason"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	d.Reason = strings.TrimSpace(d.Reason)
	if d.Reason == "" || len([]rune(d.Reason)) > 100 {
		return nil, errors.New("请填写取消原因，最多100字")
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
	w, err := readWaybill(ctx, tx, d.OrderID, true, false)
	if err != nil {
		return nil, err
	}
	if w == nil || w.ID != d.WaybillID {
		return nil, errors.New("面单已变化，请刷新")
	}
	if w.Status == "CANCELLED" {
		return w.Waybill, nil
	}
	if o.Shipment != nil || (o.FulfillmentStatus != "UNFULFILLED" && o.FulfillmentStatus != "PREPARING") {
		return nil, errors.New("已发货订单不能取消面单，请联系承运商拦截")
	}
	if w.Status != "READY" {
		return nil, errors.New("面单结果尚未确认，不能发起取消")
	}
	c, _, err := loadWaybillSettings(ctx, tx)
	if err != nil {
		return nil, err
	}
	var providerOrderID string
	if err = tx.QueryRowContext(ctx, "SELECT provider_order_id FROM order_waybills WHERE id=?", w.ID).Scan(&providerOrderID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE order_waybills SET status='CANCELLING',error='' WHERE id=?", w.ID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "WAYBILL_CANCEL_REQUEST", o.ID, jsonValue(M{"waybillId": w.ID, "reason": d.Reason})); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	status, message := cancelProviderWaybill(ctx, c, *w, providerOrderID, d.Reason)
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tx2, err := s.store.db.BeginTx(persist, nil)
	if err != nil {
		return nil, err
	}
	defer tx2.Rollback()
	w.Status, w.Error, w.LabelURL, w.UpdatedAt = status, message, "", nowString()
	if _, err = tx2.ExecContext(persist, "UPDATE order_waybills SET status=?,error=? WHERE id=? AND status='CANCELLING'", status, message, w.ID); err != nil {
		return nil, err
	}
	entry := M{"waybill": w.Waybill, "reason": d.Reason, "actor": actor.Username, "at": nowString()}
	if _, err = tx2.ExecContext(persist, "INSERT INTO waybill_history(id,order_id,payload) VALUES(?,?,?)", randomID("WBH"), o.ID, jsonValue(entry)); err != nil {
		return nil, err
	}
	if err = tx2.Commit(); err != nil {
		return nil, err
	}
	return w.Waybill, nil
}

func (s *Server) waybillHistory(ctx context.Context, orderID string) (any, error) {
	rows, err := s.store.db.QueryContext(ctx, "SELECT payload FROM waybill_history WHERE order_id=? ORDER BY created_at DESC LIMIT 200", orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []M{}
	for rows.Next() {
		var raw []byte
		var item M
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
