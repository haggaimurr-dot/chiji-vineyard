package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type ShippingInput struct {
	OrderID     string `json:"orderId"`
	Carrier     string `json:"carrier"`
	CarrierCode string `json:"carrierCode"`
	TrackingNo  string `json:"trackingNo"`
}

var shippingOrderIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)
var csvNumberTextRE = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

func validateShippingInput(d ShippingInput) (ShippingInput, error) {
	d.OrderID = strings.TrimSpace(d.OrderID)
	if !shippingOrderIDRE.MatchString(d.OrderID) {
		return d, errors.New("订单 ID 无效")
	}
	shipment, err := validateShipment(ShipmentInput{Carrier: d.Carrier, CarrierCode: d.CarrierCode, TrackingNo: d.TrackingNo})
	d.Carrier, d.CarrierCode, d.TrackingNo = shipment.Carrier, shipment.CarrierCode, shipment.TrackingNo
	return d, err
}

func sameShipment(a Shipment, d ShippingInput) bool {
	return strings.TrimSpace(a.Carrier) == d.Carrier && strings.EqualFold(a.CarrierCode, d.CarrierCode) && strings.EqualFold(a.TrackingNo, d.TrackingNo)
}

// Both manual shipping and electronic waybills lock the order before claiming
// a parcel. The unique key serializes claims across orders and server instances.
func reserveShipmentNumber(ctx context.Context, tx *sql.Tx, orderID string, shipment Shipment) error {
	code := strings.ToUpper(strings.TrimSpace(shipment.CarrierCode))
	number := strings.ToUpper(strings.TrimSpace(shipment.TrackingNo))
	// Check legacy parcels before inserting a claim so callers can retain an
	// uncertain provider result without committing a claim for a conflicting
	// parcel. Concurrent new writes still serialize on the unique key below.
	var legacyID string
	err := tx.QueryRowContext(ctx, "SELECT id FROM orders WHERE id<>? AND UPPER(JSON_UNQUOTE(JSON_EXTRACT(payload,'$.shipment.carrierCode')))=? AND UPPER(JSON_UNQUOTE(JSON_EXTRACT(payload,'$.shipment.trackingNo')))=? LIMIT 1", orderID, code, number).Scan(&legacyID)
	if err == nil {
		return errors.New("该运单号已被其他订单使用")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT IGNORE INTO shipment_numbers(carrier_code,tracking_no,order_id) VALUES(?,?,?)", code, number, orderID); err != nil {
		return err
	}
	var owner string
	if err := tx.QueryRowContext(ctx, "SELECT order_id FROM shipment_numbers WHERE carrier_code=? AND tracking_no=? FOR UPDATE", code, number).Scan(&owner); err != nil {
		return err
	}
	if owner != orderID {
		return errors.New("该运单号已被其他订单使用")
	}
	return nil
}

type BatchShippingResult struct {
	OrderID    string    `json:"orderId"`
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`
	Shipment   *Shipment `json:"shipment,omitempty"`
	SyncStatus string    `json:"syncStatus,omitempty"`
}

func (s *Server) batchShipOrders(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		Items []ShippingInput `json:"items"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if len(d.Items) == 0 || len(d.Items) > 50 {
		return nil, errors.New("每次批量发货请选择 1 至 50 个订单")
	}
	ids, parcels := map[string]bool{}, map[string]bool{}
	validationErrors := make([]error, len(d.Items))
	// Structural duplicates invalidate the request before any order is changed.
	// A malformed parcel remains a per-order error so valid rows can still ship.
	for i, item := range d.Items {
		item.OrderID = strings.TrimSpace(item.OrderID)
		if !shippingOrderIDRE.MatchString(item.OrderID) {
			return nil, errors.New("订单 ID 无效")
		}
		if ids[item.OrderID] {
			return nil, errors.New("批量发货包含重复订单")
		}
		ids[item.OrderID] = true
		item, validationErrors[i] = validateShippingInput(item)
		d.Items[i] = item
		if item.CarrierCode != "" && item.TrackingNo != "" {
			key := item.CarrierCode + ":" + strings.ToUpper(item.TrackingNo)
			if parcels[key] {
				return nil, errors.New("批量发货包含重复运单号")
			}
			parcels[key] = true
		}
	}
	results := make([]BatchShippingResult, 0, len(d.Items))
	succeeded := 0
	for i, item := range d.Items {
		row := BatchShippingResult{OrderID: item.OrderID}
		if validationErrors[i] != nil {
			row.Error = validationErrors[i].Error()
		} else {
			value, err := s.changeShipment(ctx, actor, json.RawMessage(jsonValue(item)), "batchShip")
			if err != nil {
				row.Error = err.Error()
			} else {
				shipment := value.(M)["shipment"].(Shipment)
				row.Success, row.Shipment, row.SyncStatus = true, &shipment, shipment.SyncStatus
				succeeded++
			}
		}
		results = append(results, row)
	}
	s.store.audit(ctx, actor, "ORDER_BATCH_SHIP", "", M{"count": len(results), "succeeded": succeeded, "failed": len(results) - succeeded})
	return M{"results": results, "succeeded": succeeded, "failed": len(results) - succeeded}, nil
}

type orderExportRequest struct {
	OrderSearch
	OrderIDs *[]string `json:"orderIds"`
}

func orderExportQuery(d orderExportRequest) (string, []any, error) {
	const columns = "SELECT id,order_no,order_status,payment_status,fulfillment_status,refund_status,payload,created_at,(SELECT payload FROM invoice_requests WHERE order_id=orders.id) AS invoice_payload FROM orders WHERE "
	if d.OrderIDs != nil {
		ids := *d.OrderIDs
		if len(ids) == 0 || len(ids) > 500 {
			return "", nil, errors.New("每次请选择 1 至 500 个订单导出")
		}
		args := make([]any, 0, len(ids))
		seen := map[string]bool{}
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if !shippingOrderIDRE.MatchString(id) || seen[id] {
				return "", nil, errors.New("导出订单 ID 无效或重复")
			}
			seen[id] = true
			args = append(args, id)
		}
		return columns + "id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ") ORDER BY created_at DESC,id DESC", args, nil
	}
	search, err := normalizeOrderSearch(d.OrderSearch, false)
	if err != nil {
		return "", nil, err
	}
	where, args := orderSearchWhere("", search)
	args = append(args, 10001)
	return columns + where + " ORDER BY created_at DESC,id DESC LIMIT ?", args, nil
}

func (s *Server) exportOrders(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d orderExportRequest
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	query, args, err := orderExportQuery(d)
	if err != nil {
		return nil, err
	}
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	orders := []Order{}
	for rows.Next() {
		var id, no, orderStatus, paymentStatus, fulfillmentStatus, refundStatus string
		var payload, invoicePayload []byte
		var created time.Time
		if err = rows.Scan(&id, &no, &orderStatus, &paymentStatus, &fulfillmentStatus, &refundStatus, &payload, &created, &invoicePayload); err != nil {
			rows.Close()
			return nil, err
		}
		var o Order
		if err = json.Unmarshal(payload, &o); err != nil {
			rows.Close()
			return nil, err
		}
		o.ID, o.OrderNo, o.CreatedAt = id, no, created.UTC().Format(time.RFC3339Nano)
		o.OrderStatus, o.PaymentStatus, o.FulfillmentStatus, o.RefundStatus = orderStatus, paymentStatus, fulfillmentStatus, refundStatus
		if len(invoicePayload) != 0 {
			var invoice Invoice
			if err = json.Unmarshal(invoicePayload, &invoice); err != nil {
				rows.Close()
				return nil, err
			}
			o.Invoice = &invoice
		}
		orders = append(orders, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(orders) > 10000 {
		return nil, errors.New("筛选结果超过 10000 个订单，请缩小筛选范围后导出")
	}
	if d.OrderIDs != nil && len(orders) != len(*d.OrderIDs) {
		return nil, errors.New("部分选中订单不存在，请刷新列表后重试")
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	content, err := ordersCSV(orders)
	if err != nil {
		return nil, err
	}
	mode := "filtered"
	if d.OrderIDs != nil {
		mode = "selected"
	}
	// Do not log keywords, customer details, order payloads, or generated CSV.
	detail := M{"count": len(orders), "mode": mode, "searchApplied": strings.TrimSpace(d.Keyword) != ""}
	if _, err = s.store.db.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "ORDER_EXPORT", "", jsonValue(detail)); err != nil {
		return nil, errors.New("导出日志保存失败，请稍后重试")
	}
	filename := "订单导出_" + time.Now().In(time.FixedZone("CST", 8*3600)).Format("20060102_150405") + ".csv"
	return M{"filename": filename, "content": content, "count": len(orders)}, nil
}

// CSV quoting alone does not prevent spreadsheet formulas. Prefix unsafe text
// and text that resembles a number; numeric amount columns remain numeric.
func safeCSVText(value string) string {
	value = strings.ReplaceAll(value, "\x00", "")
	trimmed := strings.TrimLeftFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || r == '\ufeff' })
	if strings.ContainsAny(value, "\t\r\n") || (trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0]))) || csvNumberTextRE.MatchString(trimmed) {
		return "'" + value
	}
	return value
}

func orderStatusLabel(value string, labels map[string]string) string {
	if label := labels[value]; label != "" {
		return label
	}
	return value
}

func moneyCSV(cents int) string { return strconv.FormatFloat(float64(cents)/100, 'f', 2, 64) }

func csvChinaTime(value string) string {
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")
	}
	return value
}

func ordersCSV(orders []Order) (string, error) {
	var content strings.Builder
	content.WriteRune('\ufeff')
	w := csv.NewWriter(&content)
	w.UseCRLF = true
	header := []string{"订单编号", "订单ID", "下单时间", "订单状态", "支付状态", "发货状态", "退款状态", "商品金额（元）", "运费（元）", "优惠金额（元）", "实付金额（元）", "已退款金额（元）", "商品明细", "收件人", "手机号", "省", "市", "区", "详细地址", "预约配送日期", "订单备注", "发票类型", "发票抬头", "税号", "发票邮箱", "发票状态", "发票号码", "快递公司", "快递编码", "运单号", "发货时间", "微信发货同步"}
	if err := w.Write(header); err != nil {
		return "", err
	}
	for _, o := range orders {
		items := make([]string, 0, len(o.Items))
		for _, item := range o.Items {
			items = append(items, fmt.Sprintf("%s / %s × %d（单价 %s 元）", item.Name, item.SKUName, item.Quantity, moneyCSV(item.UnitPrice)))
		}
		invoice := Invoice{}
		if o.Invoice != nil {
			invoice = *o.Invoice
		}
		shipment := Shipment{}
		if o.Shipment != nil {
			shipment = *o.Shipment
		}
		row := []string{
			o.OrderNo, o.ID, csvChinaTime(o.CreatedAt),
			orderStatusLabel(o.OrderStatus, map[string]string{"PENDING_PAYMENT": "待付款", "CONFIRMED": "已确认", "COMPLETED": "已完成", "CANCELLED": "已取消", "CLOSED": "已关闭"}),
			orderStatusLabel(o.PaymentStatus, map[string]string{"UNPAID": "未付款", "PAID": "已付款", "REFUNDING": "退款中", "REFUNDED": "已退款"}),
			orderStatusLabel(o.FulfillmentStatus, map[string]string{"UNFULFILLED": "待发货", "PREPARING": "备货中", "SHIPPED": "已发货", "RECEIVED": "已收货"}),
			orderStatusLabel(o.RefundStatus, map[string]string{"NONE": "无退款", "APPLIED": "申请中", "APPROVED": "已批准", "REJECTED": "已拒绝", "FAILED": "退款失败", "PARTIAL": "部分退款", "SUCCEEDED": "退款成功"}),
			moneyCSV(o.Quote.Subtotal), moneyCSV(o.Quote.Freight), moneyCSV(o.Quote.Discount), moneyCSV(o.Quote.Total), moneyCSV(o.RefundedAmount), strings.Join(items, "；"),
			o.Address.Name, o.Address.Phone, o.Address.Province, o.Address.City, o.Address.District, o.Address.Detail, o.DeliveryDate, o.Note,
			orderStatusLabel(invoice.Kind, map[string]string{"personal": "个人", "company": "企业"}), invoice.Title, invoice.TaxID, invoice.Email,
			orderStatusLabel(invoice.Status, map[string]string{"APPLIED": "待开票", "REQUESTED": "待开票", "ISSUED": "已开票", "REJECTED": "已拒绝"}), invoice.Number,
			shipment.Carrier, shipment.CarrierCode, shipment.TrackingNo, csvChinaTime(shipment.ShippedAt),
			orderStatusLabel(shipment.SyncStatus, map[string]string{"PENDING": "待同步", "SUCCESS": "已同步", "FAILED": "同步失败"}),
		}
		for i := range row {
			if i < 7 || i > 11 {
				row[i] = safeCSVText(row[i])
			}
		}
		if err := w.Write(row); err != nil {
			return "", err
		}
	}
	w.Flush()
	return content.String(), w.Error()
}
