package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Wechat struct {
	client     *http.Client
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
}

type wechatAPIError struct {
	Code   string
	Status int
}

func (e *wechatAPIError) Error() string {
	return fmt.Sprintf("微信支付返回 %d (%s)", e.Status, e.Code)
}

func (w *Wechat) sendShippingMessage(ctx context.Context, o Order, c shippingMessageConfig) error {
	for _, source := range c.Fields {
		if (source == "orderNo" && len([]rune(o.OrderNo)) > 32) || (source == "trackingNo" && len([]rune(o.Shipment.TrackingNo)) > 32) {
			return &messageRejection{Code: -1, Message: "订单号或运单号超过模板32字符限制，尚未发送"}
		}
		if source == "shippedAt" {
			if _, err := time.Parse(time.RFC3339Nano, o.Shipment.ShippedAt); err != nil {
				return &messageRejection{Code: -1, Message: "发货时间格式无效，尚未发送"}
			}
		}
	}
	return w.sendSubscriptionMessage(ctx, shippingMessage(o, c))
}

func (w *Wechat) sendSubscriptionMessage(ctx context.Context, body M) error {
	token, err := w.miniAccessToken(ctx)
	if err != nil {
		return &messageRejection{Code: -1, Message: "获取微信凭据失败，尚未发送"}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.weixin.qq.com/cgi-bin/message/subscribe/send?access_token="+url.QueryEscape(token), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var result struct {
		ErrCode *int   `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK || result.ErrCode == nil {
		return errors.New("微信消息响应无效")
	}
	if *result.ErrCode != 0 {
		return &messageRejection{Code: *result.ErrCode, Message: result.ErrMsg}
	}
	return nil
}

func (w *Wechat) miniAccessToken(ctx context.Context) (string, error) {
	appid, secret := os.Getenv("WECHAT_APP_ID"), os.Getenv("WECHAT_APP_SECRET")
	if appid == "" || secret == "" {
		return "", errors.New("微信小程序接口配置未完成")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.weixin.qq.com/cgi-bin/token?grant_type=client_credential&appid="+url.QueryEscape(appid)+"&secret="+url.QueryEscape(secret), nil)
	if err != nil {
		return "", err
	}
	response, err := w.client.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var result struct {
		AccessToken string `json:"access_token"`
		ErrMsg      string `json:"errmsg"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK || result.AccessToken == "" {
		return "", errors.New("获取微信凭据失败")
	}
	return result.AccessToken, nil
}

func newWechat() (*Wechat, error) {
	w := &Wechat{client: &http.Client{Timeout: 12 * time.Second}}
	private := strings.ReplaceAll(os.Getenv("WECHAT_MCH_PRIVATE_KEY"), `\n`, "\n")
	public := strings.ReplaceAll(os.Getenv("WECHAT_PAY_PUBLIC_KEY"), `\n`, "\n")
	if private == "" || public == "" {
		return w, nil
	}
	block, _ := pem.Decode([]byte(private))
	if block == nil {
		return nil, errors.New("商户私钥格式错误")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	}
	if err != nil {
		return nil, err
	}
	w.privateKey = key.(*rsa.PrivateKey)
	block, _ = pem.Decode([]byte(public))
	if block == nil {
		return nil, errors.New("微信支付公钥格式错误")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		if cert, e := x509.ParseCertificate(block.Bytes); e == nil {
			parsed = cert.PublicKey
		} else {
			return nil, err
		}
	}
	w.publicKey = parsed.(*rsa.PublicKey)
	return w, nil
}
func (w *Wechat) sign(message string) (string, error) {
	if w.privateKey == nil {
		return "", errors.New("微信支付私钥未配置")
	}
	digest := sha256.Sum256([]byte(message))
	signature, err := rsa.SignPKCS1v15(rand.Reader, w.privateKey, crypto.SHA256, digest[:])
	return base64.StdEncoding.EncodeToString(signature), err
}
func (w *Wechat) verify(message, encoded string) bool {
	if w.publicKey == nil {
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return false
	}
	digest := sha256.Sum256([]byte(message))
	return rsa.VerifyPKCS1v15(w.publicKey, crypto.SHA256, digest[:], sig) == nil
}
func (w *Wechat) request(method, path string, body any) (map[string]any, error) {
	mchid := os.Getenv("WECHAT_MCH_ID")
	serial := os.Getenv("WECHAT_MCH_SERIAL_NO")
	if mchid == "" || serial == "" {
		return nil, errors.New("微信支付配置未完成")
	}
	raw := []byte{}
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := randomNonce()
	signature, err := w.sign(method + "\n" + path + "\n" + timestamp + "\n" + nonce + "\n" + string(raw) + "\n")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, "https://api.mch.weixin.qq.com"+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", fmt.Sprintf(`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",timestamp="%s",serial_no="%s",signature="%s"`, mchid, nonce, timestamp, serial, signature))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	response, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	_ = json.Unmarshal(response, &result)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		code, _ := result["code"].(string)
		return nil, &wechatAPIError{Code: code, Status: resp.StatusCode}
	}
	ts := resp.Header.Get("Wechatpay-Timestamp")
	n := resp.Header.Get("Wechatpay-Nonce")
	sig := resp.Header.Get("Wechatpay-Signature")
	if ts == "" || !w.verify(ts+"\n"+n+"\n"+string(response)+"\n", sig) {
		return nil, errors.New("微信支付响应验签失败")
	}
	return result, nil
}
func (w *Wechat) prepay(o Order) (M, error) {
	appid := os.Getenv("WECHAT_APP_ID")
	mchid := os.Getenv("WECHAT_MCH_ID")
	notify := os.Getenv("WECHAT_PAY_NOTIFY_URL")
	if appid == "" || notify == "" {
		return nil, errors.New("微信支付配置未完成")
	}
	description := "迟记葡萄牙"
	if len(o.Items) > 0 {
		description += "-" + o.Items[0].Name
	}
	result, err := w.request("POST", "/v3/pay/transactions/jsapi", M{"appid": appid, "mchid": mchid, "description": truncate(description, 80), "out_trade_no": o.OrderNo, "notify_url": notify, "amount": M{"total": o.Quote.Total, "currency": "CNY"}, "payer": M{"openid": o.OpenID}})
	if err != nil {
		return nil, err
	}
	prepayID, _ := result["prepay_id"].(string)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := randomNonce()
	pkg := "prepay_id=" + prepayID
	paySign, err := w.sign(appid + "\n" + timestamp + "\n" + nonce + "\n" + pkg + "\n")
	if err != nil {
		return nil, err
	}
	return M{"timeStamp": timestamp, "nonceStr": nonce, "package": pkg, "signType": "RSA", "paySign": paySign}, nil
}
func (w *Wechat) query(no string) (M, error) {
	path := "/v3/pay/transactions/out-trade-no/" + url.PathEscape(no) + "?mchid=" + url.QueryEscape(os.Getenv("WECHAT_MCH_ID"))
	return w.request("GET", path, nil)
}
func (w *Wechat) close(no string) error {
	_, err := w.request("POST", "/v3/pay/transactions/out-trade-no/"+url.PathEscape(no)+"/close", M{"mchid": os.Getenv("WECHAT_MCH_ID")})
	return err
}
func (w *Wechat) refund(o Order, r Refund) (M, error) {
	return w.request("POST", "/v3/refund/domestic/refunds", M{"out_trade_no": o.OrderNo, "out_refund_no": r.ID, "reason": truncate(r.Reason, 80), "notify_url": os.Getenv("WECHAT_REFUND_NOTIFY_URL"), "amount": M{"refund": r.Amount, "total": o.Quote.Total, "currency": "CNY"}})
}
func (w *Wechat) verifyNotice(r *http.Request, raw []byte) (M, error) {
	ts := r.Header.Get("Wechatpay-Timestamp")
	nonce := r.Header.Get("Wechatpay-Nonce")
	sig := r.Header.Get("Wechatpay-Signature")
	serial := r.Header.Get("Wechatpay-Serial")
	if ts == "" || nonce == "" || sig == "" || serial != os.Getenv("WECHAT_PAY_PUBLIC_KEY_ID") {
		return nil, errors.New("支付通知头无效")
	}
	seconds, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || abs(time.Now().Unix()-seconds) > 300 {
		return nil, errors.New("支付通知已过期")
	}
	if !w.verify(ts+"\n"+nonce+"\n"+string(raw)+"\n", sig) {
		return nil, errors.New("支付通知验签失败")
	}
	var notice struct {
		EventType string `json:"event_type"`
		Resource  struct {
			Nonce          string `json:"nonce"`
			Ciphertext     string `json:"ciphertext"`
			AssociatedData string `json:"associated_data"`
		} `json:"resource"`
	}
	if err = json.Unmarshal(raw, &notice); err != nil {
		return nil, err
	}
	key := []byte(os.Getenv("WECHAT_PAY_API_V3_KEY"))
	if len(key) != 32 {
		return nil, errors.New("API v3 密钥长度错误")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(notice.Resource.Ciphertext)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, []byte(notice.Resource.Nonce), ciphertext, []byte(notice.Resource.AssociatedData))
	if err != nil {
		return nil, err
	}
	result := M{"event_type": notice.EventType}
	var resource M
	if err = json.Unmarshal(plaintext, &resource); err != nil {
		return nil, err
	}
	result["resource"] = resource
	return result, nil
}
func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
func (w *Wechat) uploadShipping(o Order, shipment Shipment) error {
	appid := os.Getenv("WECHAT_APP_ID")
	secret := os.Getenv("WECHAT_APP_SECRET")
	if appid == "" || secret == "" {
		return errors.New("微信小程序接口配置未完成")
	}
	res, err := w.client.Get("https://api.weixin.qq.com/cgi-bin/token?grant_type=client_credential&appid=" + url.QueryEscape(appid) + "&secret=" + url.QueryEscape(secret))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var token struct {
		AccessToken string `json:"access_token"`
		ErrMsg      string `json:"errmsg"`
	}
	if err = json.NewDecoder(res.Body).Decode(&token); err != nil {
		return err
	}
	if token.AccessToken == "" {
		return errors.New(token.ErrMsg)
	}
	description := ""
	for _, item := range o.Items {
		description += item.Name + "、"
	}
	shippingList := []M{}
	for _, parcel := range shipmentParcels(shipment) {
		shippingList = append(shippingList, M{"tracking_no": parcel.TrackingNo, "express_company": parcel.CarrierCode, "item_desc": truncate(description, 120)})
	}
	body := M{"order_key": M{"order_number_type": 1, "mchid": os.Getenv("WECHAT_MCH_ID"), "out_trade_no": o.OrderNo}, "logistics_type": 1, "delivery_mode": 1, "shipping_list": shippingList, "upload_time": time.Now().UTC().Format(time.RFC3339), "payer": M{"openid": o.OpenID}}
	if len(o.Packages) > 0 {
		shippingList = []M{}
		for _, p := range o.Packages {
			if p.Shipment != nil {
				shippingList = append(shippingList, M{"tracking_no": p.Shipment.TrackingNo, "express_company": p.Shipment.CarrierCode, "item_desc": truncate(description, 120)})
			}
		}
		body["shipping_list"] = shippingList
		body["delivery_mode"] = 2
		body["is_all_delivered"] = packagesShipped(o)
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", "https://api.weixin.qq.com/wxa/sec/order/upload_shipping_info?access_token="+url.QueryEscape(token.AccessToken), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		ErrCode *int   `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if result.ErrCode == nil {
		return errors.New("微信发货同步返回无效结果")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || *result.ErrCode != 0 {
		return fmt.Errorf("微信发货同步失败: %s (%d)", result.ErrMsg, *result.ErrCode)
	}
	return nil
}
