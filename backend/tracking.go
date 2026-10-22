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
	"os"
	"strings"
	"time"
)

type TrackingEvent struct {
	Time    string `json:"time"`
	Context string `json:"context"`
}
type Tracking struct {
	State     string          `json:"state,omitempty"`
	Status    string          `json:"status"`
	Message   string          `json:"message"`
	UpdatedAt string          `json:"updatedAt"`
	Events    []TrackingEvent `json:"events"`
}

var trackingHTTP = &http.Client{Timeout: 12 * time.Second}

func queryTracking(ctx context.Context, o Order) (Tracking, error) {
	result := Tracking{Status: "UNAVAILABLE", Events: []TrackingEvent{}, UpdatedAt: nowString()}
	customer, key := os.Getenv("KUAIDI100_CUSTOMER"), os.Getenv("KUAIDI100_KEY")
	if customer == "" || key == "" {
		result.Message = "物流查询暂未开通，可复制运单号向承运商查询"
		return result, nil
	}
	codes := map[string]string{"SF": "shunfeng", "YTO": "yuantong", "ZTO": "zhongtong", "STO": "shentong", "YD": "yunda", "EMS": "ems", "JD": "jd", "JT": "jtexpress"}
	code := codes[o.Shipment.CarrierCode]
	if code == "" {
		result.Message = "该承运商暂不支持轨迹查询，请联系商家"
		return result, nil
	}
	phone := o.Address.Phone
	if len(phone) > 4 {
		phone = phone[len(phone)-4:]
	}
	param := jsonValue(M{"com": code, "num": o.Shipment.TrackingNo, "phone": phone, "order": "desc"})
	sum := md5.Sum([]byte(param + key + customer))
	form := url.Values{"customer": {customer}, "sign": {strings.ToUpper(hex.EncodeToString(sum[:]))}, "param": {param}}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://poll.kuaidi100.com/poll/query.do", strings.NewReader(form.Encode()))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := trackingHTTP.Do(req)
	if err != nil {
		result.Message = "物流服务暂时不可用，请稍后重试"
		return result, nil
	}
	defer response.Body.Close()
	var data struct {
		Status string          `json:"status"`
		State  string          `json:"state"`
		Data   []TrackingEvent `json:"data"`
	}
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&data) != nil {
		result.Message = "物流服务暂时不可用，请稍后重试"
		return result, nil
	}
	if data.Status != "200" {
		result.Status = "PENDING"
		result.Message = "暂未获取轨迹，请稍后重试或联系商家"
		return result, nil
	}
	labels := map[string]string{"0": "运输中", "1": "已揽收", "2": "运输异常", "3": "已签收", "4": "退签", "5": "派送中", "6": "退回", "7": "转投"}
	result.Status = "SUCCESS"
	result.State = data.State
	result.Message = labels[data.State]
	if result.Message == "" {
		result.Message = "物流更新"
	}
	result.Events = data.Data
	if result.Events == nil {
		result.Events = []TrackingEvent{}
	}
	return result, nil
}
func (s *Server) tracking(ctx context.Context, id, openid string, parcel ...string) (any, error) {
	o, err := s.store.order(ctx, s.store.db, id, false)
	if err != nil {
		return nil, err
	}
	if o == nil || openid != "" && o.OpenID != openid {
		return nil, errors.New("订单不存在")
	}
	if len(parcel) > 2 && parcel[2] != "" {
		found := false
		for _, p := range o.Packages {
			if p.ID == parcel[2] {
				o.Shipment = p.Shipment
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("包裹不存在")
		}
	}
	if o.Shipment == nil {
		return nil, errors.New("订单尚未发货")
	}
	if len(parcel) > 0 {
		code := ""
		if len(parcel) > 1 {
			code = parcel[1]
		}
		if err = selectParcel(o, parcel[0], code); err != nil {
			return nil, err
		}
	}
	// Cache lock serializes queries for the same parcel across server instances.
	cacheKey := o.Shipment.CarrierCode + ":" + o.Shipment.TrackingNo
	_, err = s.store.db.ExecContext(ctx, "INSERT IGNORE INTO shipment_tracking(cache_key,payload,checked_at) VALUES(?, '{}', '2000-01-01')", cacheKey)
	if err != nil {
		return nil, err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var raw []byte
	var checked time.Time
	if err = tx.QueryRowContext(ctx, "SELECT payload,checked_at FROM shipment_tracking WHERE cache_key=? FOR UPDATE", cacheKey).Scan(&raw, &checked); err != nil {
		return nil, err
	}
	var result Tracking
	if time.Since(checked) < 30*time.Minute {
		if err = json.Unmarshal(raw, &result); err != nil {
			return nil, err
		}
		return result, nil
	}
	result, err = queryTracking(ctx, *o)
	if err != nil {
		return nil, err
	}
	if result.Status != "SUCCESS" {
		var previous Tracking
		if json.Unmarshal(raw, &previous) == nil && len(previous.Events) > 0 {
			previous.Status = "STALE"
			previous.Message = "物流暂未更新，以下为上次查询轨迹"
			result = previous
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE shipment_tracking SET payload=?,checked_at=UTC_TIMESTAMP(3) WHERE cache_key=?", jsonValue(result), cacheKey); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
