package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"os"
	"sort"
	"strings"
)

// This config is private: it is never exposed by public config endpoints or
// operational config history. Enterprise API keys stay in environment variables.
type waybillAdminSettings struct {
	Revision      int                       `json:"revision"`
	SenderName    string                    `json:"senderName"`
	SenderPhone   string                    `json:"senderPhone"`
	SenderAddress string                    `json:"senderAddress"`
	Accounts      map[string]waybillAccount `json:"accounts"`
	Rates         []waybillRate             `json:"rates"`
}
type waybillRate struct {
	CarrierCode      string  `json:"carrierCode"`
	Province         string  `json:"province"` // empty = explicitly nationwide
	FirstWeight      float64 `json:"firstWeight"`
	FirstFee         int     `json:"firstFee"` // cents
	AdditionalWeight float64 `json:"additionalWeight"`
	AdditionalFee    int     `json:"additionalFee"`
	Surcharge        int     `json:"surcharge"`
	Delivery         string  `json:"delivery"`
}

func envWaybillAdminSettings() waybillAdminSettings {
	c, _ := waybillSettingsFromEnv()
	a := waybillAdminSettings{SenderName: c.SenderName, SenderPhone: c.SenderPhone, SenderAddress: c.SenderAddress, Accounts: map[string]waybillAccount{}, Rates: []waybillRate{}}
	_ = json.Unmarshal([]byte(os.Getenv("KUAIDI100_WAYBILL_ACCOUNTS")), &a.Accounts)
	if a.Accounts == nil {
		a.Accounts = map[string]waybillAccount{}
	}
	return a
}
func readWaybillSettings(ctx context.Context, q waybillQueryer, lock bool) (waybillAdminSettings, error) {
	a := envWaybillAdminSettings()
	query := "SELECT payload FROM configs WHERE config_key='waybill'"
	if lock {
		query += " FOR UPDATE"
	}
	var raw []byte
	err := q.QueryRowContext(ctx, query).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return a, nil
	}
	if err != nil {
		return a, err
	}
	a = waybillAdminSettings{Accounts: map[string]waybillAccount{}, Rates: []waybillRate{}}
	err = json.Unmarshal(raw, &a)
	return a, err
}
func loadWaybillSettings(ctx context.Context, q waybillQueryer) (waybillSettings, waybillAdminSettings, error) {
	a, err := readWaybillSettings(ctx, q, false)
	c := waybillSettings{Key: strings.TrimSpace(os.Getenv("KUAIDI100_KEY")), Secret: strings.TrimSpace(os.Getenv("KUAIDI100_SECRET")), SenderName: a.SenderName, SenderPhone: a.SenderPhone, SenderAddress: a.SenderAddress, Accounts: a.Accounts}
	if err == nil && (c.Key == "" || c.Secret == "" || c.SenderName == "" || !phoneRE.MatchString(c.SenderPhone) || c.SenderAddress == "") {
		err = errors.New("请配置服务端企业密钥、寄件人及网点/月结账号")
	}
	return c, a, err
}
func waybillConfigFor(c waybillSettings, err error) M {
	carriers := []waybillCarrier{}
	if err == nil {
		for _, carrier := range waybillCarriers {
			if waybillAccountReady(carrier.Code, c.Accounts[carrier.Code]) {
				carriers = append(carriers, carrier)
			}
		}
	}
	if err == nil && len(carriers) == 0 {
		err = errors.New("尚未配置完整的网点/月结账号及 V2 图片模板")
	}
	result := M{"configured": err == nil, "carriers": carriers}
	if err != nil {
		result["message"] = err.Error()
	}
	return result
}
func redactedWaybillSettings(a waybillAdminSettings) M {
	accounts := map[string]M{}
	for code, account := range a.Accounts {
		var row M
		_ = json.Unmarshal([]byte(jsonValue(account)), &row)
		for _, key := range []string{"partnerId", "partnerKey", "partnerSecret"} {
			row[key+"Set"] = row[key] != ""
			row[key] = ""
		}
		accounts[code] = row
	}
	return M{"revision": a.Revision, "senderName": a.SenderName, "senderPhone": a.SenderPhone, "senderAddress": a.SenderAddress, "accounts": accounts, "rates": a.Rates, "carriers": waybillCarriers, "enterpriseConfigured": os.Getenv("KUAIDI100_KEY") != "" && os.Getenv("KUAIDI100_SECRET") != ""}
}
func validateWaybillAdminSettings(a *waybillAdminSettings, previous waybillAdminSettings) error {
	a.SenderName = strings.TrimSpace(a.SenderName)
	a.SenderPhone = strings.TrimSpace(a.SenderPhone)
	a.SenderAddress = strings.TrimSpace(a.SenderAddress)
	if a.SenderName == "" || len([]rune(a.SenderName)) > 30 || !phoneRE.MatchString(a.SenderPhone) || a.SenderAddress == "" || len([]rune(a.SenderAddress)) > 240 {
		return errors.New("请填写有效的寄件人、手机号及完整寄件地址")
	}
	if len(a.Accounts) > len(waybillCarriers) || len(a.Rates) > 200 {
		return errors.New("承运商或价目表数量超出限制")
	}
	for code, account := range a.Accounts {
		if _, ok := carrierForWaybill(code); !ok {
			return errors.New("承运商编码无效")
		}
		old := previous.Accounts[code]
		account.PartnerID = strings.TrimSpace(account.PartnerID)
		account.PartnerKey = strings.TrimSpace(account.PartnerKey)
		account.PartnerSecret = strings.TrimSpace(account.PartnerSecret)
		if account.PartnerID == "" {
			account.PartnerID = old.PartnerID
		}
		if account.PartnerKey == "" {
			account.PartnerKey = old.PartnerKey
		}
		if account.PartnerSecret == "" {
			account.PartnerSecret = old.PartnerSecret
		}
		for _, value := range []string{account.PartnerID, account.PartnerKey, account.PartnerSecret, account.PartnerName, account.Net, account.Code, account.CheckMan, account.TempID, account.ExpType, account.PayType} {
			if len(value) > 500 {
				return errors.New("账号字段过长")
			}
		}
		if !account.Disabled && !waybillAccountReady(code, account) {
			return errors.New("承运商 " + code + " 的账号、网点、产品或模板未配置完整")
		}
		a.Accounts[code] = account
	}
	seen := map[string]bool{}
	for i := range a.Rates {
		r := &a.Rates[i]
		r.Province = strings.TrimSpace(r.Province)
		r.Delivery = strings.TrimSpace(r.Delivery)
		if _, ok := a.Accounts[r.CarrierCode]; !ok {
			return errors.New("价目表必须关联已配置的承运商")
		}
		key := r.CarrierCode + ":" + r.Province
		if seen[key] {
			return errors.New("同一承运商的同一省份只能配置一条协议价")
		}
		seen[key] = true
		if math.Abs(r.FirstWeight*1000-math.Round(r.FirstWeight*1000)) > 1e-7 || math.Abs(r.AdditionalWeight*1000-math.Round(r.AdditionalWeight*1000)) > 1e-7 {
			return errors.New("协议价重量最多保留三位小数（精确到克）")
		}
		if !validWaybillWeight(r.FirstWeight) || !validWaybillWeight(r.AdditionalWeight) || r.FirstFee < 0 || r.FirstFee > 10000000 || r.AdditionalFee < 0 || r.AdditionalFee > 10000000 || r.Surcharge < 0 || r.Surcharge > 10000000 || len([]rune(r.Province)) > 30 || len([]rune(r.Delivery)) > 80 {
			return errors.New("协议价重量、金额或地区无效")
		}
	}
	return nil
}
func validWaybillWeight(w float64) bool {
	return !math.IsNaN(w) && !math.IsInf(w, 0) && w >= 0.01 && w <= 1000
}
func (s *Server) saveWaybillSettings(ctx context.Context, actor Claims, data json.RawMessage) (any, error) {
	var a waybillAdminSettings
	if err := decode(data, &a); err != nil {
		return nil, err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	initial := envWaybillAdminSettings()
	if _, err = tx.ExecContext(ctx, "INSERT IGNORE INTO configs(config_key,payload) VALUES('waybill',?)", jsonValue(initial)); err != nil {
		return nil, err
	}
	previous, err := readWaybillSettings(ctx, tx, true)
	if err != nil {
		return nil, err
	}
	if a.Revision != previous.Revision {
		return nil, errors.New("电子面单配置已被修改，请重新读取后保存")
	}
	if err = validateWaybillAdminSettings(&a, previous); err != nil {
		return nil, err
	}
	a.Revision++
	if _, err = tx.ExecContext(ctx, "UPDATE configs SET payload=? WHERE config_key='waybill'", jsonValue(a)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.store.audit(ctx, actor, "WAYBILL_CONFIG_SAVE", "waybill", M{"revision": a.Revision, "carrierCount": len(a.Accounts), "rateCount": len(a.Rates)})
	return redactedWaybillSettings(a), nil
}
func compareWaybillRates(a waybillAdminSettings, province string, weight float64) []M {
	result := []M{}
	for _, carrier := range waybillCarriers {
		if !waybillAccountReady(carrier.Code, a.Accounts[carrier.Code]) {
			continue
		}
		var chosen *waybillRate
		for i := range a.Rates {
			r := &a.Rates[i]
			if r.CarrierCode == carrier.Code && (r.Province == "" || r.Province == province) && (chosen == nil || r.Province != "") {
				chosen = r
			}
		}
		row := M{"carrierCode": carrier.Code, "carrier": carrier.Name, "available": chosen != nil}
		if chosen == nil {
			row["message"] = "未配置目的省份的协议价"
		} else {
			// Integer grams avoid charging an extra step from floating-point error.
			grams := int64(math.Ceil(weight*1000 - 1e-7))
			first := int64(math.Round(chosen.FirstWeight * 1000))
			step := int64(math.Round(chosen.AdditionalWeight * 1000))
			extra := max(int64(0), grams-first)
			units := (extra + step - 1) / step
			row["fee"] = int64(chosen.FirstFee) + units*int64(chosen.AdditionalFee) + int64(chosen.Surcharge)
			row["delivery"] = chosen.Delivery
			row["province"] = chosen.Province
			row["billableWeight"] = float64(first+units*step) / 1000
		}
		result = append(result, row)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i]["available"] != result[j]["available"] {
			return result[i]["available"] == true
		}
		if result[i]["available"] == true {
			return result[i]["fee"].(int64) < result[j]["fee"].(int64)
		}
		return false
	})
	return result
}
func (s *Server) quoteWaybillRates(ctx context.Context, data json.RawMessage) (any, error) {
	var d struct {
		OrderID string  `json:"orderId"`
		Weight  float64 `json:"weight"`
	}
	if err := decode(data, &d); err != nil {
		return nil, err
	}
	if !validWaybillWeight(d.Weight) || d.Weight > 100 {
		return nil, errors.New("包裹重量须为0.01至100公斤")
	}
	o, err := s.store.order(ctx, s.store.db, d.OrderID, false)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, errors.New("订单不存在")
	}
	_, a, err := loadWaybillSettings(ctx, s.store.db)
	if err != nil {
		return nil, err
	}
	return M{"items": compareWaybillRates(a, o.Address.Province, d.Weight), "weight": d.Weight, "province": o.Address.Province, "revision": a.Revision, "estimated": true, "message": "按网点/月结协议价预估，未含体积重及未配置的特殊附加费；最终费用以承运商账单为准"}, nil
}
