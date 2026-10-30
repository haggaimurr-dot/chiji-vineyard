package main

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Waybill contains admin-visible metadata only. Provider credentials, temporary
// image links and image bytes never enter an order's public payload.
type Waybill struct {
	ID          string `json:"id"`
	OrderID     string `json:"orderId"`
	OrderNo     string `json:"orderNo"`
	Carrier     string `json:"carrier"`
	CarrierCode string `json:"carrierCode"`
	TrackingNo  string `json:"trackingNo"`
	LabelURL    string `json:"labelUrl"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type waybillRecord struct {
	Waybill
	ProviderTaskID string
	ProviderURL    string
	MIME           string
	Content        []byte
}

type waybillCarrier struct {
	Code         string `json:"code"`
	Name         string `json:"name"`
	ProviderCode string `json:"-"`
}

var waybillCarriers = []waybillCarrier{
	{"SF", "顺丰速运", "shunfeng"}, {"YTO", "圆通速递", "yuantong"},
	{"ZTO", "中通快递", "zhongtong"}, {"STO", "申通快递", "shentong"},
	{"YD", "韵达快递", "yunda"}, {"EMS", "EMS", "ems"},
	{"JD", "京东物流", "jd"}, {"JT", "极兔速递", "jtexpress"},
}

type waybillAccount struct {
	Disabled      bool   `json:"disabled"`
	PartnerID     string `json:"partnerId"`
	PartnerKey    string `json:"partnerKey"`
	PartnerSecret string `json:"partnerSecret"`
	PartnerName   string `json:"partnerName"`
	Net           string `json:"net"`
	Code          string `json:"code"`
	CheckMan      string `json:"checkMan"`
	TempID        string `json:"tempId"`
	ExpType       string `json:"expType"`
	PayType       string `json:"payType"`
}

type waybillSettings struct {
	Key, Secret, SenderName, SenderPhone, SenderAddress string
	Accounts                                            map[string]waybillAccount
}

func waybillSettingsFromEnv() (waybillSettings, error) {
	c := waybillSettings{Key: strings.TrimSpace(os.Getenv("KUAIDI100_KEY")), Secret: strings.TrimSpace(os.Getenv("KUAIDI100_SECRET")), SenderName: clean(os.Getenv("KUAIDI100_SENDER_NAME"), 30), SenderPhone: strings.TrimSpace(os.Getenv("KUAIDI100_SENDER_PHONE")), SenderAddress: clean(os.Getenv("KUAIDI100_SENDER_ADDRESS"), 240)}
	if c.Key == "" || c.Secret == "" || c.SenderName == "" || !phoneRE.MatchString(c.SenderPhone) || c.SenderAddress == "" {
		return c, errors.New("快递100电子面单未配置完整，请在服务端配置企业密钥和寄件人信息")
	}
	if json.Unmarshal([]byte(os.Getenv("KUAIDI100_WAYBILL_ACCOUNTS")), &c.Accounts) != nil || len(c.Accounts) == 0 {
		return c, errors.New("请在服务端配置快递100网点/月结账号和 V2 面单模板")
	}
	return c, nil
}

func waybillAccountReady(code string, a waybillAccount) bool {
	if a.Disabled {
		return false
	}
	if strings.TrimSpace(a.PartnerID) == "" || strings.TrimSpace(a.TempID) == "" || strings.TrimSpace(a.ExpType) == "" {
		return false
	}
	// This integration uses native branch accounts. Platform-authorized labels
	// may return PDF regardless of IMAGE and need a separate platform workflow.
	switch strings.ToLower(a.Net) {
	case "taobao", "cainiao", "jdalpha", "pinduoduowx", "douyin", "kuaishou", "weipinhui", "wechatchannels", "xiaohongshu":
		return false
	}
	if a.PayType != "" && a.PayType != "SHIPPER" && a.PayType != "MONTHLY" {
		return false
	}
	if code != "JD" && strings.TrimSpace(a.PartnerKey) == "" {
		return false
	}
	if code == "SF" && strings.TrimSpace(a.PartnerSecret) == "" {
		return false
	}
	return (code != "ZTO" && code != "STO") || strings.TrimSpace(a.Net) != ""
}

func waybillConfig() M {
	carriers := []waybillCarrier{}
	c, err := waybillSettingsFromEnv()
	if err == nil {
		for _, carrier := range waybillCarriers {
			if waybillAccountReady(carrier.Code, c.Accounts[carrier.Code]) {
				carriers = append(carriers, carrier)
			}
		}
		if len(carriers) == 0 {
			err = errors.New("网点/月结账号或 V2 模板配置不完整，请联系管理员")
		}
	}
	result := M{"configured": err == nil, "carriers": carriers}
	if err != nil {
		result["message"] = err.Error()
	}
	return result
}

func carrierForWaybill(code string) (waybillCarrier, bool) {
	for _, c := range waybillCarriers {
		if c.Code == code {
			return c, true
		}
	}
	return waybillCarrier{}, false
}

type waybillQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readWaybill(ctx context.Context, q waybillQueryer, orderID string, lock bool, content bool) (*waybillRecord, error) {
	query := "SELECT id,order_id,order_no,carrier,carrier_code,tracking_no,status,error,provider_task_id,provider_label_url,mime,created_at,updated_at"
	if content {
		query += ",content"
	}
	query += " FROM order_waybills WHERE order_id=?"
	if lock {
		query += " FOR UPDATE"
	}
	var w waybillRecord
	var created, updated time.Time
	args := []any{&w.ID, &w.OrderID, &w.OrderNo, &w.Carrier, &w.CarrierCode, &w.TrackingNo, &w.Status, &w.Error, &w.ProviderTaskID, &w.ProviderURL, &w.MIME, &created, &updated}
	if content {
		args = append(args, &w.Content)
	}
	err := q.QueryRowContext(ctx, query, orderID).Scan(args...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	w.CreatedAt, w.UpdatedAt = created.UTC().Format(time.RFC3339Nano), updated.UTC().Format(time.RFC3339Nano)
	if w.Status == "READY" {
		w.LabelURL = "/admin/waybills/" + url.PathEscape(w.ID) + "/label"
	}
	return &w, nil
}

// WaybillPrintItem 是批量打印面单的前端拼模板数据：只读，不改变订单状态。
type WaybillPrintItem struct {
	OrderID    string      `json:"orderId"`
	OrderNo    string      `json:"orderNo"`
	Recipient  Address      `json:"recipient"`
	Items      []OrderItem  `json:"items"`
	Shipment   *Shipment    `json:"shipment,omitempty"`
	WaybillURL string       `json:"waybillUrl,omitempty"`
}

func (s *Server) batchWaybillPrint(ctx context.Context, raw json.RawMessage) (any, error) {
	var d struct {
		OrderIDs []string `json:"orderIds"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if len(d.OrderIDs) == 0 || len(d.OrderIDs) > 100 {
		return nil, errors.New("请选择 1–100 个订单")
	}
	items := make([]WaybillPrintItem, 0, len(d.OrderIDs))
	for _, id := range d.OrderIDs {
		o, err := s.store.order(ctx, s.store.db, id, false)
		if err != nil {
			return nil, err
		}
		if o == nil {
			return nil, errors.New("订单不存在：" + id)
		}
		w, err := readWaybill(ctx, s.store.db, id, false, false)
		if err != nil {
			return nil, err
		}
		item := WaybillPrintItem{OrderID: o.ID, OrderNo: o.OrderNo, Recipient: o.Address, Items: o.Items, Shipment: o.Shipment}
		if w != nil && w.Status == "READY" {
			item.WaybillURL = w.LabelURL
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Server) getWaybill(ctx context.Context, orderID string) (any, error) {
	// A process may have stopped after claiming a request. Never silently issue
	// another bill for that order, even after the provider's 48-hour cache expires.
	_, err := s.store.db.ExecContext(ctx, "UPDATE order_waybills SET status='UNCERTAIN',error='创建过程已中断，请先向快递100核实，勿重复下单' WHERE order_id=? AND status='CREATING' AND updated_at<UTC_TIMESTAMP(3)-INTERVAL 2 MINUTE", orderID)
	if err != nil {
		return nil, err
	}
	w, err := readWaybill(ctx, s.store.db, orderID, false, false)
	if err != nil || w == nil {
		return nil, err
	}
	return w.Waybill, nil
}

func validateWaybillShipment(ctx context.Context, tx *sql.Tx, orderID string, shipment Shipment) error {
	w, err := readWaybill(ctx, tx, orderID, true, false)
	if err != nil || w == nil {
		return err
	}
	switch w.Status {
	case "CREATING", "UNCERTAIN", "CANCELLING", "CANCEL_UNCERTAIN":
		return errors.New("电子面单尚未确认，请先核实面单结果后发货")
	case "READY":
		if w.CarrierCode != shipment.CarrierCode || w.TrackingNo != shipment.TrackingNo {
			return errors.New("发货信息必须与已生成的电子面单一致")
		}
	}
	return nil
}

var waybillHTTP = &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
	return errors.New("电子面单下单接口不接受重定向")
}}
var waybillImageHTTP = &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("面单重定向过多")
	}
	_, err := verifiedWaybillURL(r.URL.String())
	if err == nil {
		r.URL.Scheme = "https"
	}
	return err
}}

type waybillProviderResult struct {
	Status, Error, TrackingNo, TaskID, LabelURL, ProviderOrderID string
}

func createProviderWaybill(ctx context.Context, c waybillSettings, carrier waybillCarrier, order Order, weight float64) waybillProviderResult {
	a := c.Accounts[carrier.Code]
	providerCode := a.Code
	if providerCode == "" {
		switch carrier.Code {
		case "SF":
			providerCode = "sf_secret"
		case "ZTO":
			providerCode = "ztoOpen"
		case "STO":
			providerCode = "44"
		}
	}
	payType := a.PayType
	if payType == "" {
		payType = "SHIPPER"
	}
	param := jsonValue(M{"printType": "IMAGE", "partnerId": a.PartnerID, "partnerKey": a.PartnerKey, "partnerSecret": a.PartnerSecret, "partnerName": a.PartnerName, "net": a.Net, "code": providerCode, "checkMan": a.CheckMan, "kuaidicom": carrier.ProviderCode, "recMan": M{"name": order.Address.Name, "mobile": order.Address.Phone, "printAddr": order.Address.Province + order.Address.City + order.Address.District + order.Address.Detail}, "sendMan": M{"name": c.SenderName, "mobile": c.SenderPhone, "printAddr": c.SenderAddress}, "cargo": "葡萄", "count": 1, "weight": weight, "payType": payType, "expType": a.ExpType, "tempId": a.TempID, "orderId": order.OrderNo, "reorder": false, "needChild": "0", "needBack": "0", "needSubscribe": false, "needOcr": false})
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	sum := md5.Sum([]byte(param + timestamp + c.Key + c.Secret))
	form := url.Values{"method": {"order"}, "key": {c.Key}, "t": {timestamp}, "sign": {strings.ToUpper(hex.EncodeToString(sum[:]))}, "param": {param}}
	unknown := waybillProviderResult{Status: "UNCERTAIN", Error: "快递100下单结果未知，请在快递100后台核实，勿重复下单"}
	r, err := http.NewRequestWithContext(ctx, "POST", "https://api.kuaidi100.com/label/order", strings.NewReader(form.Encode()))
	if err != nil {
		return unknown
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := waybillHTTP.Do(r)
	if err != nil {
		return unknown
	}
	defer response.Body.Close()
	var data struct {
		Success bool            `json:"success"`
		Code    json.RawMessage `json:"code"`
		Data    struct {
			ProviderOrderID string `json:"kdComOrderNum"`
			TrackingNo      string `json:"kuaidinum"`
			TaskID          string `json:"taskId"`
			LabelURL        string `json:"label"`
			ChildNum        string `json:"childNum"`
			ReturnNum       string `json:"returnNum"`
		} `json:"data"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&data) != nil {
		return unknown
	}
	code := strings.Trim(string(data.Code), `"`)
	if (data.Success && code == "200") || code == "30011" {
		unknown.ProviderOrderID = data.Data.ProviderOrderID
		unknown.TrackingNo, unknown.TaskID = strings.TrimSpace(data.Data.TrackingNo), strings.TrimSpace(data.Data.TaskID)
		if !trackingNoRE.MatchString(unknown.TrackingNo) || unknown.TaskID == "" || data.Data.ChildNum != "" || data.Data.ReturnNum != "" {
			unknown.Error = "快递100返回的面单资料不完整或包含多包裹，请联系服务商核实"
			return unknown
		}
		label, err := verifiedWaybillURL(data.Data.LabelURL)
		if err != nil {
			unknown.Error = "运单已生成，快递100未返回有效图片地址，请联系服务商核实"
			return unknown
		}
		unknown.LabelURL = label
		unknown.Error = "运单已生成，面单图片尚未保存，请刷新面单图片"
		return unknown
	}
	// Only documented, explicit rejections may be retried. Do not include raw
	// provider messages: they can echo credentials or recipient information.
	rejections := map[string]string{"30001": "请求参数不完整", "30002": "签名校验失败", "30003": "企业账号配置错误", "30004": "电子面单余额不足", "30005": "快递网点账号或参数校验失败", "30006": "请求参数类型错误", "30007": "V2 面单模板配置错误", "30010": "打印设备状态异常"}
	if !data.Success && data.Data.TrackingNo == "" && data.Data.TaskID == "" {
		if message, ok := rejections[code]; ok {
			return waybillProviderResult{Status: "FAILED", Error: "快递100拒绝下单（" + code + "）：" + message}
		}
	}
	return unknown
}

func verifiedWaybillURL(value string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.User != nil || u.Fragment != "" || u.Port() != "" || strings.Contains(value, ",") {
		return "", errors.New("面单地址无效")
	}
	host := strings.ToLower(u.Hostname())
	if host != "kuaidi100.com" && !strings.HasSuffix(host, ".kuaidi100.com") {
		return "", errors.New("面单地址不属于快递100")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", errors.New("面单地址协议无效")
	}
	u.Scheme = "https"
	return u.String(), nil
}

func downloadWaybillImage(ctx context.Context, labelURL string) (string, []byte, error) {
	labelURL, err := verifiedWaybillURL(labelURL)
	if err != nil {
		return "", nil, err
	}
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(500 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return "", nil, errors.New("面单图片尚未保存，请稍后刷新")
			case <-timer.C:
			}
		}
		r, err := http.NewRequestWithContext(ctx, "GET", labelURL, nil)
		if err != nil {
			return "", nil, errors.New("面单图片地址无效")
		}
		response, err := waybillImageHTTP.Do(r)
		if err != nil {
			continue
		}
		content, readErr := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
		response.Body.Close()
		if response.StatusCode != http.StatusOK || readErr != nil || len(content) == 0 {
			continue
		}
		if len(content) > 4<<20 {
			return "", nil, errors.New("快递100面单图片超过4MB，请联系服务商核实")
		}
		mime := http.DetectContentType(content)
		if mime != "image/png" && mime != "image/jpeg" && mime != "image/webp" {
			return "", nil, errors.New("快递100返回的内容不是 PNG、JPEG 或 WebP 面单图片")
		}
		return mime, content, nil
	}
	return "", nil, errors.New("运单已生成，图片暂未就绪，请稍后刷新面单图片")
}

func (s *Server) createWaybill(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		OrderID     string  `json:"orderId"`
		CarrierCode string  `json:"carrierCode"`
		Weight      float64 `json:"weight"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	d.OrderID = clean(d.OrderID, 80)
	d.CarrierCode = strings.ToUpper(strings.TrimSpace(d.CarrierCode))
	carrier, ok := carrierForWaybill(d.CarrierCode)
	if !ok || d.OrderID == "" || math.IsNaN(d.Weight) || math.IsInf(d.Weight, 0) || d.Weight <= 0 || d.Weight > 100 {
		return nil, errors.New("请选择有效承运商，包裹重量须大于0且不超过100公斤")
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
	w, err := readWaybill(ctx, tx, o.ID, true, false)
	if err != nil {
		return nil, err
	}
	recreated := w != nil && w.Status == "CANCELLED"
	if recreated {
		if _, err = tx.ExecContext(ctx, "DELETE FROM order_waybills WHERE id=? AND status='CANCELLED'", w.ID); err != nil {
			return nil, err
		}
		w = nil
	}
	if w != nil && w.CarrierCode != carrier.Code {
		return nil, errors.New("该订单已申请其他承运商的面单，请先人工核实原面单")
	}
	if w != nil && (w.Status == "READY" || w.Status == "CREATING" || w.Status == "UNCERTAIN" || w.Status == "CANCELLING" || w.Status == "CANCEL_UNCERTAIN") {
		if w.Status == "CREATING" && time.Since(normalizeDate(w.UpdatedAt)) > 2*time.Minute {
			w.Status = "UNCERTAIN"
			w.Error = "创建过程已中断，请先向快递100核实，勿重复下单"
			if _, err = tx.ExecContext(ctx, "UPDATE order_waybills SET status=?,error=? WHERE id=?", w.Status, w.Error, w.ID); err != nil {
				return nil, err
			}
			if err = tx.Commit(); err != nil {
				return nil, err
			}
		}
		return w.Waybill, nil
	}
	if len(o.Packages) > 0 {
		return nil, errors.New("拆单订单请按包裹录入运单")
	}
	if !canShip(*o) || o.OrderStatus != "CONFIRMED" || o.Shipment != nil {
		return nil, errors.New("仅已付款、未发货且无退款中的订单可创建电子面单")
	}
	if _, err = validateAddress(o.Address); err != nil {
		return nil, err
	}
	c, _, err := loadWaybillSettings(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !waybillAccountReady(carrier.Code, c.Accounts[carrier.Code]) {
		return nil, errors.New("该承运商的网点/月结账号或 V2 模板尚未配置完整")
	}
	if w == nil {
		w = &waybillRecord{Waybill: Waybill{ID: randomID("WBL"), OrderID: o.ID, OrderNo: o.OrderNo, Carrier: carrier.Name, CarrierCode: carrier.Code, Status: "CREATING", CreatedAt: nowString(), UpdatedAt: nowString()}}
		_, err = tx.ExecContext(ctx, "INSERT INTO order_waybills(id,order_id,order_no,carrier,carrier_code,status) VALUES(?,?,?,?,?,'CREATING')", w.ID, o.ID, o.OrderNo, carrier.Name, carrier.Code)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE order_waybills SET status='CREATING',error='',tracking_no='',provider_task_id='',provider_label_url='',mime='',content=NULL WHERE id=? AND status='FAILED'", w.ID)
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	// The durable claim is committed before the billable provider request.
	providerOrder := *o
	if recreated {
		providerOrder.OrderNo = w.ID
	}
	result := createProviderWaybill(ctx, c, carrier, providerOrder, d.Weight)
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	w, err = s.recordWaybillResult(persistCtx, o.ID, w.ID, result)
	cancel()
	if err != nil {
		return nil, errors.New("面单结果保存失败，请先在快递100后台核实，勿重复下单")
	}
	auditCtx, auditCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	s.store.audit(auditCtx, actor, "WAYBILL_CREATE", o.ID, M{"waybillId": w.ID, "status": w.Status, "carrierCode": w.CarrierCode})
	auditCancel()
	if w.Status == "UNCERTAIN" && w.ProviderURL != "" {
		return s.syncWaybill(ctx, actor, o.ID)
	}
	return w.Waybill, nil
}

func (s *Server) recordWaybillResult(ctx context.Context, orderID, id string, result waybillProviderResult) (*waybillRecord, error) {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Match shipping's lock order: order, waybill, shipment number reservation.
	o, err := s.store.order(ctx, tx, orderID, true)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, errors.New("订单不存在")
	}
	w, err := readWaybill(ctx, tx, orderID, true, false)
	if err != nil {
		return nil, err
	}
	if w == nil || w.ID != id {
		return nil, errors.New("面单创建记录不匹配")
	}
	if w.Status == "READY" {
		return w, nil
	}
	if trackingNoRE.MatchString(result.TrackingNo) {
		if err = reserveShipmentNumber(ctx, tx, orderID, Shipment{Carrier: w.Carrier, CarrierCode: w.CarrierCode, TrackingNo: result.TrackingNo}); err != nil {
			result.Status = "UNCERTAIN"
			result.Error = "快递100返回的运单号与其他订单冲突，请联系服务商核实"
			result.LabelURL = ""
		}
	}
	_, err = tx.ExecContext(ctx, "UPDATE order_waybills SET status=?,error=?,tracking_no=?,provider_task_id=?,provider_label_url=? WHERE id=?", result.Status, result.Error, result.TrackingNo, result.TaskID, result.LabelURL, id)
	if err != nil {
		return nil, err
	}
	if result.ProviderOrderID != "" {
		if _, err = tx.ExecContext(ctx, "UPDATE order_waybills SET provider_order_id=? WHERE id=?", result.ProviderOrderID, id); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return readWaybill(ctx, s.store.db, orderID, false, false)
}

func (s *Server) syncWaybill(ctx context.Context, actor Claims, orderID string) (any, error) {
	w, err := readWaybill(ctx, s.store.db, orderID, false, false)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, errors.New("订单尚无电子面单")
	}
	if w.Status == "READY" {
		return w.Waybill, nil
	}
	if w.Status != "UNCERTAIN" || w.ProviderURL == "" || !trackingNoRE.MatchString(w.TrackingNo) {
		return nil, errors.New("下单结果尚未确认，请先在快递100后台核实；系统不会重新下单")
	}
	imageCtx, cancel := context.WithTimeout(ctx, 7*time.Second)
	mime, content, imageErr := downloadWaybillImage(imageCtx, w.ProviderURL)
	cancel()
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if imageErr != nil {
		_, err = s.store.db.ExecContext(persistCtx, "UPDATE order_waybills SET error=? WHERE id=? AND status='UNCERTAIN'", imageErr.Error(), w.ID)
	} else {
		_, err = s.store.db.ExecContext(persistCtx, "UPDATE order_waybills SET status='READY',error='',mime=?,content=? WHERE id=? AND status='UNCERTAIN' AND tracking_no=?", mime, content, w.ID, w.TrackingNo)
		if err == nil {
			s.store.audit(persistCtx, actor, "WAYBILL_LABEL_SAVE", orderID, M{"waybillId": w.ID})
		}
	}
	if err != nil {
		return nil, errors.New("面单图片保存失败，请稍后刷新图片，勿重新下单")
	}
	w, err = readWaybill(persistCtx, s.store.db, orderID, false, false)
	if err != nil {
		return nil, err
	}
	return w.Waybill, nil
}

func (s *Server) readWaybillLabel(ctx context.Context, orderID string) (any, error) {
	w, err := readWaybill(ctx, s.store.db, orderID, false, true)
	if err != nil {
		return nil, err
	}
	if w == nil || w.Status != "READY" || len(w.Content) == 0 {
		return nil, errors.New("电子面单图片尚未就绪")
	}
	if w.MIME != "image/png" && w.MIME != "image/jpeg" && w.MIME != "image/webp" {
		return nil, errors.New("面单图片类型无效")
	}
	return M{"mime": w.MIME, "dataUrl": "data:" + w.MIME + ";base64," + base64.StdEncoding.EncodeToString(w.Content)}, nil
}

func (s *Server) waybillLabelHTTP(w http.ResponseWriter, r *http.Request) {
	adminCORS(w, r)
	actor, err := s.store.requireAdmin(r)
	if err != nil {
		fail(w, http.StatusUnauthorized, err)
		return
	}
	if err = s.store.authorizeAdmin(r, actor, "admin.waybill.label"); err != nil {
		if !writeAccessError(w, err) {
			fail(w, http.StatusServiceUnavailable, err)
		}
		return
	}
	var orderID string
	if err := s.store.db.QueryRowContext(r.Context(), "SELECT order_id FROM order_waybills WHERE id=?", r.PathValue("id")).Scan(&orderID); err != nil {
		fail(w, http.StatusNotFound, errors.New("面单不存在"))
		return
	}
	label, err := readWaybill(r.Context(), s.store.db, orderID, false, true)
	if err != nil || label == nil || label.Status != "READY" || len(label.Content) == 0 {
		fail(w, http.StatusNotFound, errors.New("面单图片尚未就绪"))
		return
	}
	if label.MIME != "image/png" && label.MIME != "image/jpeg" && label.MIME != "image/webp" {
		fail(w, http.StatusBadRequest, errors.New("面单图片类型无效"))
		return
	}
	w.Header().Set("Content-Type", label.MIME)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="waybill-%s"`, label.ID))
	_, _ = w.Write(label.Content)
}
