package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	store  *Store
	wechat *Wechat
}

func main() {
	store, err := openStore(os.Getenv("MYSQL_DSN"))
	if err != nil {
		log.Fatal(err)
	}
	defer store.db.Close()
	wechat, err := newWechat()
	if err != nil {
		log.Fatal(err)
	}
	s := &Server{store: store, wechat: wechat}
	publisherCtx, stopPublisher := context.WithCancel(context.Background())
	defer stopPublisher()
	go store.runArticlePublisher(publisherCtx)
	go s.runPaymentReconciler(publisherCtx)
	go s.runAlertThresholdScanner(publisherCtx)
	go s.runLiveNoticeScanner(publisherCtx)
	go store.runReviewAlertScanner(publisherCtx)
	go store.runOrderAlertScanner(publisherCtx)
	go store.runNotifyWorker(publisherCtx)
	go s.runAutoInvoices(publisherCtx)
	go store.runLeadExternalReminders(publisherCtx)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("chiji Go cloud hosting listening on :%s", port)
	server := &http.Server{Addr: ":" + port, Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 90 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second}
	log.Fatal(server.ListenAndServe())
}
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("POST /api", s.api)
	mux.HandleFunc("OPTIONS /api", adminPreflight)
	mux.HandleFunc("POST /game-api", s.gameAPI)
	mux.HandleFunc("OPTIONS /game-api", adminPreflight)
	mux.HandleFunc("POST /api/media/upload", s.uploadVideo)
	mux.HandleFunc("OPTIONS /api/media/upload", adminPreflight)
	mux.HandleFunc("POST /wechat-pay-notify", s.paymentNotice)
	mux.HandleFunc("GET /media/", s.media)
	mux.HandleFunc("GET /admin/waybills/{id}/label", s.waybillLabelHTTP)
	return mux
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, M{"ok": false, "error": APIError{Code: "BUSINESS_ERROR", Message: err.Error()}})
}
func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	adminCORS(w, r)
	body, err := io.ReadAll(io.LimitReader(r.Body, 6<<20))
	if err != nil {
		fail(w, 400, err)
		return
	}
	var req Request
	if err = json.Unmarshal(body, &req); err != nil {
		fail(w, 400, errors.New("请求格式无效"))
		return
	}
	timeout := 25 * time.Second
	if req.Action == "admin.payment.financeReconcile" || req.Action == "internal.paymentFinanceReconcile" || req.Action == "admin.payment.reconcile" || req.Action == "internal.paymentReconcile" {
		timeout = 110 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	r = r.WithContext(ctx)
	var result any
	if strings.HasPrefix(req.Action, "auth.") {
		w.Header().Set("Cache-Control", "no-store")
	}
	if err = s.checkLoginLimit(r, req); err != nil {
		if !writeAccessError(w, err) {
			fail(w, http.StatusServiceUnavailable, errors.New("登录安全检查暂不可用"))
		}
		return
	}
	if strings.HasPrefix(req.Action, "admin.") {
		w.Header().Set("Cache-Control", "no-store")
		result, err = s.admin(r, req)
	} else if strings.HasPrefix(req.Action, "internal.") {
		result, err = s.internal(r, req)
	} else {
		result, err = s.public(r, req)
	}
	if err != nil {
		if writeAccessError(w, err) {
			return
		}
		log.Printf("api action=%s error=%v", req.Action, err)
		status := 400
		if strings.Contains(err.Error(), "登录") || strings.Contains(err.Error(), "身份") {
			status = 401
		}
		fail(w, status, err)
		return
	}
	writeJSON(w, 200, M{"ok": true, "data": result, "requestId": randomID("REQ")})
}
func (s *Server) paymentNotice(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		fail(w, 400, err)
		return
	}
	notice, err := s.wechat.verifyNotice(r, raw)
	if err != nil {
		s.notePaymentAlert(r.Context(), "CALLBACK_INVALID", "payment-notify", "支付通知验签或解密失败")
		fail(w, 401, err)
		return
	}
	resource := notice["resource"].(M)
	event, _ := notice["event_type"].(string)
	ctx := r.Context()
	switch event {
	case "TRANSACTION.SUCCESS":
		no, _ := resource["out_trade_no"].(string)
		o, err := s.store.orderByNo(ctx, no)
		if err != nil || o == nil {
			s.notePaymentAlert(ctx, "CALLBACK_ORDER_MISSING", no, "支付回调订单不存在或读取失败，请人工核查")
			fail(w, 404, errors.New("支付订单不存在"))
			return
		}
		err = s.markVerifiedPayment(ctx, o, resource)
	case "REFUND.SUCCESS", "REFUND.CLOSED", "REFUND.ABNORMAL":
		refundID, _ := resource["out_refund_no"].(string)
		rf, e := s.store.refund(ctx, s.store.db, refundID, false)
		if e != nil {
			err = e
		} else if rf == nil {
			err = errors.New("退款申请不存在")
		} else if e = validateRefundResult(*rf, resource, true); e != nil {
			err = e
		} else {
			status, _ := resource["refund_status"].(string)
			if "REFUND."+status != event {
				err = errors.New("退款通知状态不匹配")
			} else {
				providerID, _ := resource["refund_id"].(string)
				err = s.recordRefundResult(ctx, refundID, status, providerID)
			}
		}
	}
	if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, M{"code": "SUCCESS", "message": "成功"})
}
func mustID(data json.RawMessage, key string) string {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(data, &m)
	var id string
	_ = json.Unmarshal(m[key], &id)
	return clean(id, 80)
}
func (s *Server) public(r *http.Request, req Request) (any, error) {
	ctx := r.Context()
	if req.Action == "trace.query" {
		return s.queryTraceCode(ctx, req.Data)
	}
	if req.Action == "auth.login" {
		var data struct {
			Code    string `json:"code"`
			Confirm string `json:"confirm"`
		}
		if err := decode(req.Data, &data); err != nil {
			return nil, err
		}
		openid, err := exchangeCode(data.Code)
		if err != nil {
			return nil, err
		}
		unlock, err := s.store.lockMiniAccount(ctx, openid)
		if err != nil {
			return nil, err
		}
		defer unlock()
		closed, err := s.store.closedAccount(ctx, openid)
		if err != nil {
			return nil, err
		}
		if closed {
			if data.Confirm != "重新注册" {
				return nil, errors.New("账号已注销，请在账号与个人数据页主动重新注册")
			}
			if _, err = s.store.db.ExecContext(ctx, "DELETE FROM account_closures WHERE subject_hash=?", articleReaderHash(openid)); err != nil {
				return nil, err
			}
		}
		token, err := s.issueSession(r, Claims{Subject: openid, Kind: "mini", Exp: time.Now().Add(7 * 24 * time.Hour).Unix()})
		if err != nil {
			return nil, err
		}
		return M{"token": token}, nil
	}
	if req.Action == "service.get" {
		var h HomeConfig
		if err := s.store.loadConfig(ctx, "home", &h); err != nil {
			return nil, err
		}
		if !h.CustomerService.Enabled {
			return CustomerService{}, nil
		}
		return h.CustomerService, nil
	}
	if req.Action == "privacy.get" {
		p, err := s.store.privacy(ctx)
		if err != nil {
			return nil, err
		}
		if !p.Published {
			return nil, errors.New("隐私政策正在完善，请联系商家")
		}
		return p, nil
	}
	user, err := verifyToken(r.Header.Get("Authorization"), "mini")
	if err != nil {
		return nil, err
	}
	if err = s.store.checkSession(r, user); err != nil {
		return nil, err
	}
	unlock, err := s.store.lockMiniAccount(ctx, user.Subject)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err = s.store.checkSession(r, user); err != nil {
		return nil, err
	}
	openid := user.Subject
	switch req.Action {
	case "analytics.visit":
		err := s.store.recordCommerceActivity(ctx, openid, false)
		return M{"recorded": err == nil}, err
	case "account.status", "account.data.delete", "account.close":
		return s.store.personalData(ctx, user, req.Action, req.Data)
	case "auth.session.list", "auth.session.revoke", "auth.logout":
		return s.personalSessions(r, user, req)
	case "bootstrap":
		_, err = s.store.db.ExecContext(ctx, "INSERT INTO users(openid) VALUES(?) ON DUPLICATE KEY UPDATE last_seen_at=CURRENT_TIMESTAMP(3)", openid)
		if err != nil {
			return nil, err
		}
		var nickname string
		_ = s.store.db.QueryRowContext(ctx, "SELECT nickname FROM users WHERE openid=?", openid).Scan(&nickname)
		var count int
		_ = s.store.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(quantity),0) FROM carts WHERE openid=?", openid).Scan(&count)
		return M{"用户": M{"id": openid, "nickname": nickname}, "cartCount": count}, nil
	case "home.get":
		return s.store.homeFor(ctx, openid)
	case "activity.reservation.list":
		return s.store.activityReservations(ctx, openid)
	case "activity.reservation.status":
		return s.store.activityReservationStatus(ctx, openid)
	case "activity.reservation.set":
		return s.store.setActivityReservation(ctx, openid, req.Data)
	case "activity.checkin.code":
		return s.store.activityCheckinCode(ctx, openid, req.Data)
	case "notify.config":
		return s.store.notifyConfig(ctx, openid)
	case "notify.subscribe":
		return s.store.notifySubscribe(ctx, openid, req.Data)
	case "notify.pref.set":
		return s.store.notifyPrefSet(ctx, openid, req.Data)
	case "activity.notice.list":
		return s.store.activityNotices(ctx, openid, req.Data)
	case "activity.notice.read":
		return s.store.readActivityNotice(ctx, openid, req.Data)
	case "activity.subscription.config":
		return s.activityPushSubscription(ctx, openid, mustID(req.Data, "activityId"), "", false)
	case "activity.subscription.accept":
		return s.activityPushSubscription(ctx, openid, mustID(req.Data, "activityId"), mustID(req.Data, "templateId"), true)
	case "order.events":
		return s.orderEvents(ctx, mustID(req.Data, "orderId"), openid)
	case "order.tracking":
		return s.tracking(ctx, mustID(req.Data, "orderId"), openid, mustID(req.Data, "trackingNo"), mustID(req.Data, "carrierCode"), mustID(req.Data, "packageId"))
	case "product.search":
		var d ProductSearch
		if err := decode(req.Data, &d); err != nil {
			return nil, err
		}
		return s.store.searchProducts(ctx, openid, d)
	case "product.list":
		var d struct {
			Category string `json:"category"`
		}
		_ = decode(req.Data, &d)
		products, e := s.store.products(ctx, d.Category, false)
		if e != nil {
			return nil, e
		}
		e = s.store.attachAttention(ctx, openid, products)
		return products, e
	case "product.detail":
		productID := mustID(req.Data, "id")
		p, e := s.store.product(ctx, productID, false)
		if e != nil || p == nil {
			return p, e
		}
		products := []Product{*p}
		e = s.store.attachAttention(ctx, openid, products)
		if e != nil {
			return nil, e
		}
		if products[0].LinkedArticles, e = s.store.productLinkedArticles(ctx, productID); e != nil {
			return nil, e
		}
		return products[0], nil
	case "reminder.click":
		return s.store.recordReminderClick(ctx, openid, req.Data)
	case "product.attention.set":
		return s.store.setAttention(ctx, openid, req.Data)
	case "product.attention.history":
		return s.store.attentionHistory(ctx, openid, req.Data)
	case "product.priceDropTarget.set":
		return s.store.setPriceDropTarget(ctx, openid, req.Data)
	case "product.restockSubscription.config":
		return restockSubscriptionConfig(), nil
	case "product.restockSubscription.accept":
		return s.acceptRestockSubscription(ctx, openid, mustID(req.Data, "productId"), mustID(req.Data, "templateId"))
	case "product.priceDropSubscription.config":
		return priceDropSubscriptionConfig(), nil
	case "product.priceDropSubscription.accept":
		return s.acceptPriceDropSubscription(ctx, openid, mustID(req.Data, "productId"), mustID(req.Data, "templateId"))
	case "product.reviews":
		return s.productReviews(ctx, req.Data)
	case "review.order":
		return s.orderReviews(ctx, openid, req.Data)
	case "review.append":
		return s.appendReview(ctx, openid, req.Data)
	case "review.create":
		return s.createReview(ctx, openid, req.Data)
	case "product.attention.list":
		return s.store.attentionProducts(ctx, openid, mustID(req.Data, "kind"))
	case "article.list":
		var d struct {
			Stage   string `json:"stage"`
			Variety string `json:"variety"`
		}
		_ = decode(req.Data, &d)
		return s.store.articles(ctx, d.Stage, d.Variety, false)
	case "article.detail":
		return s.store.learningArticle(ctx, openid, mustID(req.Data, "id"))
	case "article.search":
		return s.store.searchArticles(ctx, openid, req.Data)
	case "article.favorite.set":
		return s.store.setArticleFavorite(ctx, openid, req.Data)
	case "classroom.video.get":
		return s.store.videoProgress(ctx, openid, req.Data, false)
	case "classroom.video.save":
		return s.store.videoProgress(ctx, openid, req.Data, true)
	case "classroom.courses":
		return s.store.classroomCourses(ctx, openid)
	case "classroom.complete":
		return s.store.classroomComplete(ctx, openid, req.Data)
	case "classroom.questions":
		return s.store.classroomQuestions(ctx, openid, req.Data, false)
	case "classroom.ask":
		return s.store.classroomAsk(ctx, openid, req.Data)
	case "article.read":
		return s.store.recordArticleRead(ctx, openid, req.Data)
	case "address.get":
		return s.store.address(ctx, openid)
	case "address.list":
		return s.store.addresses(ctx, openid)
	case "address.save", "address.default", "address.delete":
		var d struct {
			Address Address `json:"address"`
			ID      string  `json:"id"`
		}
		if err = decode(req.Data, &d); err != nil {
			return nil, err
		}
		return s.store.changeAddress(ctx, openid, req.Action, d.Address, d.ID)
	case "cart.get":
		return s.store.cart(ctx, openid)
	case "cart.batch":
		return s.store.batchCart(ctx, openid, req.Data)
	case "cart.update":
		var d CartItem
		if err = decode(req.Data, &d); err != nil {
			return nil, err
		}
		var selected struct {
			Selected  *bool `json:"selected"`
			AddToCart bool  `json:"addToCart"`
		}
		_ = decode(req.Data, &selected)
		if d.Quantity < 0 || d.SKUID == "" {
			return nil, errors.New("购物袋参数无效")
		}
		if d.Quantity == 0 {
			_, err = s.store.db.ExecContext(ctx, "DELETE FROM carts WHERE openid=? AND sku_id=?", openid, d.SKUID)
		} else {
			p, e := s.store.product(ctx, d.ProductID, false)
			if e != nil {
				return nil, e
			}
			if p == nil {
				return nil, errors.New("商品已下架")
			}
			ok := false
			for _, sku := range p.SKUs {
				if sku.ID == d.SKUID && d.Quantity <= sku.Stock && d.Quantity <= sku.LimitPerOrder {
					ok = true
				}
			}
			if !ok {
				return nil, errors.New("商品库存或限购数量已变化")
			}
			chosen := true
			if selected.Selected != nil {
				chosen = *selected.Selected
			}
			_, err = s.store.db.ExecContext(ctx, "INSERT INTO carts(openid,sku_id,product_id,quantity,selected) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE quantity=VALUES(quantity),selected=VALUES(selected)", openid, d.SKUID, d.ProductID, d.Quantity, chosen)
		}
		if err != nil {
			return nil, err
		}
		if selected.AddToCart && d.Quantity > 0 {
			if e := s.store.recordCommerceActivity(ctx, openid, true); e != nil {
				log.Printf("commerce add-to-cart recording failed: %v", e)
			}
		}
		return s.store.cart(ctx, openid)
	case "ticket.create":
		return s.createTicket(ctx, openid, nil, req.Data)
	case "ticket.list":
		return s.listTickets(ctx, openid, req.Data)
	case "ticket.read":
		return s.readTicket(ctx, openid, nil, req.Data)
	case "ticket.reply":
		return s.updateTicket(ctx, openid, nil, req.Data)
	case "ticket.returnTracking":
		return s.returnTracking(ctx, openid, req.Data)
	case "liveNotice.status":
		return s.liveNoticeStatus(ctx, openid, req.Data)
	case "liveNotice.subscribe":
		return s.liveNoticeSubscribe(ctx, openid, req.Data)
	case "invoice.titles":
		return s.miniInvoiceTitles(ctx, openid, req.Data)
	case "invoice.apply":
		return s.applyInvoice(ctx, openid, req.Data)
	case "invoice.file":
		return s.invoiceFile(ctx, mustID(req.Data, "orderId"), openid)
	case "coupon.wallet":
		return s.couponWallet(ctx, openid)
	case "coupon.redeem":
		return s.redeemCoupon(ctx, openid, req.Data)
	case "order.quote":
		var d struct {
			AutoCoupon   bool       `json:"autoCoupon"`
			DeliveryDate string     `json:"deliveryDate"`
			CouponCode   string     `json:"couponCode"`
			Items        []CartItem `json:"items"`
			Address      Address    `json:"address"`
		}
		if err = decode(req.Data, &d); err != nil {
			return nil, err
		}
		items, e := s.store.resolveItems(ctx, s.store.db, d.Items, false)
		if e != nil {
			return nil, e
		}
		rule, e := s.store.shipping(ctx)
		if e != nil {
			return nil, e
		}
		q, e := calculateAddressQuote(items, rule, d.Address, d.DeliveryDate, time.Now())
		if e != nil {
			return nil, e
		}
		if d.AutoCoupon {
			wallet, walletErr := s.couponWallet(ctx, openid)
			if walletErr != nil {
				return nil, walletErr
			}
			d.CouponCode = bestWalletCoupon(wallet, q, time.Now())
		}
		e = s.couponQuote(ctx, s.store.db, openid, d.CouponCode, &q, false)
		return q, e
	case "order.create":
		return s.createOrder(ctx, openid, req.Data)
	case "order.recover":
		return s.recoverOrder(ctx, openid, mustID(req.Data, "idempotencyKey"))
	case "payment.create":
		return s.createPayment(ctx, openid, mustID(req.Data, "orderId"))
	case "order.list":
		var d OrderSearch
		if err = decode(req.Data, &d); err != nil {
			return nil, err
		}
		return s.store.searchOrders(ctx, openid, d)
	case "order.search":
		var d OrderSearch
		if err = decode(req.Data, &d); err != nil {
			return nil, err
		}
		return s.store.searchOrdersPage(ctx, openid, d)
	case "order.shippingSubscription.get":
		return s.shippingSubscription(ctx, openid, mustID(req.Data, "orderId"), false)
	case "order.shippingSubscription.accept":
		return s.shippingSubscription(ctx, openid, mustID(req.Data, "orderId"), true)
	case "shippingNotice.list":
		return s.shippingNotices(ctx, openid)
	case "shippingNotice.read":
		return s.readShippingNotice(ctx, openid, mustID(req.Data, "orderId"))
	case "order.get":
		var d struct {
			ID             string `json:"id"`
			RefreshPayment bool   `json:"refreshPayment"`
		}
		_ = decode(req.Data, &d)
		o, e := s.store.order(ctx, s.store.db, d.ID, false)
		if e != nil {
			return nil, e
		}
		if o == nil || o.OpenID != openid {
			return nil, errors.New("订单不存在")
		}
		if d.RefreshPayment && o.PaymentStatus == "UNPAID" && o.PaymentAttempted {
			payment, e := s.queryPayment(ctx, o)
			if e == nil && payment["trade_state"] == "SUCCESS" {
				if e = s.markVerifiedPayment(ctx, o, payment); e == nil {
					o, _ = s.store.order(ctx, s.store.db, o.ID, false)
				}
			}
		}
		return o, nil
	case "order.cancel":
		return s.cancelOrder(ctx, openid, mustID(req.Data, "id"))
	case "order.confirm":
		return s.confirmOrder(ctx, openid, mustID(req.Data, "id"))
	case "refund.list":
		var d struct {
			OrderID string `json:"orderId"`
			Refresh bool   `json:"refresh"`
		}
		if err := decode(req.Data, &d); err != nil {
			return nil, err
		}
		return s.userRefunds(ctx, openid, d.OrderID, d.Refresh)
	case "refund.evidence.upload":
		return s.uploadRefundEvidence(ctx, openid, req.Data)
	case "refund.evidence.delete":
		return s.deleteRefundEvidence(ctx, openid, req.Data)
	case "refund.evidence.get":
		return s.readRefundEvidence(ctx, openid, false, req.Data)
	case "refund.apply":
		return s.applyRefund(ctx, openid, req.Data)
	case "lead.submit":
		return s.submitLead(ctx, req.Data)
	default:
		return nil, fmt.Errorf("未知接口：%s", req.Action)
	}
}
func (s *Server) createOrder(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d CreateOrderInput
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	address, err := validateAddress(d.Address)
	if err != nil {
		return nil, err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	d.Address = address
	existing, err := claimOrderKey(ctx, tx, openid, d.IdempotencyKey, orderFingerprint(d))
	if err != nil {
		return nil, err
	}
	if existing != "" {
		o, e := s.store.order(ctx, tx, existing, false)
		if e != nil {
			return nil, e
		}
		if o == nil || o.OpenID != openid {
			return nil, errors.New("幂等订单不存在")
		}
		if e = tx.Commit(); e != nil {
			return nil, e
		}
		return o, nil
	}
	rule, err := readShipping(ctx, tx)
	if err != nil {
		return nil, err
	}
	items, err := s.store.resolveItems(ctx, tx, d.Items, true)
	if err != nil {
		return nil, err
	}
	quote, err := calculateAddressQuote(items, rule, address, d.DeliveryDate, time.Now())
	if err != nil {
		return nil, err
	}
	if err = s.couponQuote(ctx, tx, openid, d.CouponCode, &quote, true); err != nil {
		return nil, err
	}
	if quote.BlockedReason != "" {
		return nil, errors.New(quote.BlockedReason)
	}
	id := randomID("ORD")
	no := "CJ" + time.Now().Format("20060102150405") + strconv.FormatInt(time.Now().UnixNano()%1000000, 10)
	for _, item := range items {
		result, e := applyInventory(ctx, tx, item.SKUID, "RESERVE", no, "系统", "下单预占", "UPDATE skus SET stock=stock-?,reserved=reserved+? WHERE id=? AND stock>=?", item.Quantity, item.Quantity, item.SKUID, item.Quantity)
		if e != nil {
			return nil, e
		}
		affected, _ := result.RowsAffected()
		if affected != 1 {
			return nil, errors.New("商品库存不足")
		}
	}
	// 内容归因（新增）：从内容页商品入口下单时记录来源文章 ID（末次点击口径）。
	sourceArticleID := ""
	if d.SourceArticleID != "" {
		var saExists int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM articles WHERE id=? AND status='PUBLISHED'", d.SourceArticleID).Scan(&saExists); err != nil {
			return nil, err
		}
		if saExists == 0 {
			return nil, errors.New("来源内容不存在")
		}
		sourceArticleID = d.SourceArticleID
	}
	o := Order{DeliveryDate: d.DeliveryDate, ID: id, OrderNo: no, OpenID: openid, OrderStatus: "PENDING_PAYMENT", PaymentStatus: "UNPAID", FulfillmentStatus: "UNFULFILLED", RefundStatus: "NONE", Items: items, Address: address, Quote: quote, Note: clean(d.Note, 100), SourceArticleID: sourceArticleID, CreatedAt: nowString(), ExpiresAt: time.Now().Add(30 * time.Minute)}
	appendOrderEvent(&o, "ORDER_CREATED", "订单已创建", "用户", "")
	_, err = tx.ExecContext(ctx, "INSERT INTO orders(id,order_no,openid,order_status,payment_status,fulfillment_status,refund_status,total,payload,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?)", o.ID, o.OrderNo, openid, o.OrderStatus, o.PaymentStatus, o.FulfillmentStatus, o.RefundStatus, o.Quote.Total, jsonValue(o), o.ExpiresAt)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE order_idempotency SET order_id=? WHERE openid=? AND idempotency_key=?", o.ID, openid, d.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if _, err = tx.ExecContext(ctx, "DELETE FROM carts WHERE openid=? AND sku_id=?", openid, item.SKUID); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return o, nil
}
func (s *Server) createPayment(ctx context.Context, openid, id string) (any, error) {
	o, err := s.store.order(ctx, s.store.db, id, false)
	if err != nil {
		return nil, err
	}
	if o == nil || o.OpenID != openid {
		return nil, errors.New("订单不存在")
	}
	if o.PaymentStatus == "PAID" {
		return M{"alreadyPaid": true}, nil
	}
	if o.OrderStatus != "PENDING_PAYMENT" || time.Now().After(o.ExpiresAt) {
		return nil, errors.New("订单已失效")
	}
	payment, err := s.wechat.prepay(*o)
	if err != nil {
		s.notePaymentAlert(ctx, "PREPAY_FAILED", o.OrderNo, "微信支付下单失败，请检查支付配置或网络")
		return nil, err
	}
	_, err = s.store.db.ExecContext(ctx, "UPDATE orders SET payment_attempted=1 WHERE id=?", id)
	return payment, err
}
func (s *Server) markPaid(ctx context.Context, id, transactionID string, paidTime ...time.Time) error {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	o, err := s.store.order(ctx, tx, id, true)
	if err != nil {
		return err
	}
	if o == nil {
		return errors.New("订单不存在")
	}
	if o.PaymentStatus == "PAID" {
		if o.TransactionID != transactionID {
			return errors.New("支付交易号与已入账交易不一致")
		}
		return nil
	}
	if o.OrderStatus == "CLOSED" || o.OrderStatus == "CANCELLED" {
		return errors.New("已关闭订单发生支付，需要人工退款")
	}
	for _, item := range o.Items {
		_, err = applyInventory(ctx, tx, item.SKUID, "PAY", o.OrderNo, "系统", "付款确认", "UPDATE skus SET reserved=GREATEST(0,reserved-?),sold=sold+? WHERE id=?", item.Quantity, item.Quantity, item.SKUID)
		if err != nil {
			return err
		}
	}
	o.PaidAt = time.Now().UTC().Format(time.RFC3339)
	if len(paidTime) > 0 {
		o.PaidAt = paidTime[0].UTC().Format(time.RFC3339)
	}
	o.OrderStatus = "CONFIRMED"
	o.PaymentStatus = "PAID"
	o.FulfillmentStatus = "PREPARING"
	appendOrderEvent(o, "PAYMENT_PAID", "付款已确认", "系统", "")
	_, err = tx.ExecContext(ctx, "UPDATE orders SET order_status=?,payment_status=?,fulfillment_status=?,transaction_id=?,payload=? WHERE id=?", o.OrderStatus, o.PaymentStatus, o.FulfillmentStatus, transactionID, jsonValue(o), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Server) releaseOrder(ctx context.Context, id, status string) error {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	o, err := s.store.order(ctx, tx, id, true)
	if err != nil {
		return err
	}
	if o == nil || o.OrderStatus != "PENDING_PAYMENT" || o.PaymentStatus != "UNPAID" {
		return nil
	}
	for _, item := range o.Items {
		if _, err = applyInventory(ctx, tx, item.SKUID, "RELEASE", o.OrderNo, "系统", "订单取消或超时关闭："+status, "UPDATE skus SET stock=stock+?,reserved=GREATEST(0,reserved-?) WHERE id=?", item.Quantity, item.Quantity, item.SKUID); err != nil {
			return err
		}
	}
	o.OrderStatus = status
	appendOrderEvent(o, "ORDER_"+status, "订单取消或超时关闭", "用户/系统", "")
	_, err = tx.ExecContext(ctx, "UPDATE orders SET order_status=?,payload=? WHERE id=?", status, jsonValue(o), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Server) cancelOrder(ctx context.Context, openid, id string) (any, error) {
	o, err := s.store.order(ctx, s.store.db, id, false)
	if err != nil {
		return nil, err
	}
	if o == nil || o.OpenID != openid {
		return nil, errors.New("订单不存在")
	}
	if o.OrderStatus != "PENDING_PAYMENT" || o.PaymentStatus != "UNPAID" {
		return nil, errors.New("当前订单不能取消")
	}
	if o.PaymentAttempted {
		if err = s.wechat.close(o.OrderNo); err != nil {
			payment, e := s.queryPayment(ctx, o)
			if e == nil && payment["trade_state"] == "SUCCESS" {
				_ = s.markVerifiedPayment(ctx, o, payment)
				return nil, errors.New("订单已支付，请申请退款")
			}
			return nil, err
		}
	}
	if err = s.releaseOrder(ctx, id, "CANCELLED"); err != nil {
		return nil, err
	}
	return s.store.order(ctx, s.store.db, id, false)
}
func (s *Server) confirmOrder(ctx context.Context, openid, id string) (any, error) {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	o, err := s.store.order(ctx, tx, id, true)
	if err != nil {
		return nil, err
	}
	if o == nil || o.OpenID != openid || o.FulfillmentStatus != "SHIPPED" || (o.PaymentStatus != "PAID" && o.PaymentStatus != "REFUNDED") {
		return nil, errors.New("当前订单不能确认收货")
	}
	if !packagesShipped(*o) {
		return nil, errors.New("尚有包裹未发货，不能整单收货")
	}
	for i := range o.Packages {
		o.Packages[i].ReceivedAt = nowString()
	}
	appendOrderEvent(o, "ORDER_RECEIVED", "全部包裹确认收货", "用户", "")
	o.OrderStatus = "COMPLETED"
	o.FulfillmentStatus = "RECEIVED"
	_, err = tx.ExecContext(ctx, "UPDATE orders SET order_status=?,fulfillment_status=?,payload=? WHERE id=?", o.OrderStatus, o.FulfillmentStatus, jsonValue(o), id)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	// 订单触达（新增）：确认收货后排入微信+短信通知。
	s.store.notifyOrderEvent(ctx, o.OpenID, notifyKindOrderReceived, o.OrderNo, "已确认收货")
	return o, nil
}
func (s *Server) applyRefund(ctx context.Context, openid string, raw json.RawMessage) (any, error) {
	var d struct {
		OrderID string   `json:"orderId"`
		Amount  *int     `json:"amount"`
		Images  []string `json:"images"`
		Reason  string   `json:"reason"`
		Note    string   `json:"note"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	d.Reason = clean(d.Reason, 80)
	if d.Reason == "" {
		return nil, errors.New("请选择退款原因")
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
	if o == nil || o.OpenID != openid || !canRefund(*o) {
		return nil, errors.New("当前订单不能申请退款")
	}
	if autoInvoiceBusy(o.Invoice) {
		return nil, errors.New("自动开票结果待确认，请联系商家核实后申请退款")
	}
	amount := o.Quote.Total - o.RefundedAmount
	if d.Amount != nil {
		amount = *d.Amount
	}
	if err = validateRefund(*o, amount, d.Reason, d.Images); err != nil {
		return nil, err
	}
	refundID := randomID("RF")
	r := Refund{ID: refundID, OrderID: o.ID, OrderNo: o.OrderNo, OpenID: openid, Amount: amount, Images: d.Images, Reason: clean(d.Reason, 80), Note: clean(d.Note, 200), Status: "APPLIED", CreatedAt: nowString()}
	_, err = tx.ExecContext(ctx, "INSERT INTO refunds(id,order_id,order_no,openid,amount,reason,note,status) VALUES(?,?,?,?,?,?,?,?)", r.ID, r.OrderID, r.OrderNo, r.OpenID, r.Amount, r.Reason, r.Note, r.Status)
	if err != nil {
		return nil, err
	}
	for _, imageID := range d.Images {
		res, e := tx.ExecContext(ctx, "UPDATE refund_evidence SET refund_id=? WHERE id=? AND order_id=? AND openid=? AND refund_id=''", r.ID, imageID, o.ID, openid)
		if e != nil {
			return nil, e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return nil, e
		}
		if n != 1 {
			return nil, errors.New("举证图片不存在、已使用或不属于此订单")
		}
	}
	o.RefundStatus = "APPLIED"
	appendOrderEvent(o, "REFUND_APPLIED", "退款申请已提交", "用户", "")
	_, err = tx.ExecContext(ctx, "UPDATE orders SET refund_status=?,payload=? WHERE id=?", o.RefundStatus, jsonValue(o), o.ID)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return M{"id": refundID, "status": "APPLIED"}, nil
}
func (s *Server) submitLead(ctx context.Context, raw json.RawMessage) (any, error) {
	var l Lead
	if err := decode(raw, &l); err != nil {
		return nil, err
	}
	var consent struct {
		Consent bool `json:"consent"`
	}
	_ = decode(raw, &consent)
	l.Organization = clean(l.Organization, 80)
	l.Contact = clean(l.Contact, 30)
	l.Phone = clean(l.Phone, 20)
	l.Region = clean(l.Region, 50)
	l.Volume = clean(l.Volume, 50)
	l.Channel = clean(l.Channel, 30)
	l.Demand = clean(l.Demand, 300)
	if l.Organization == "" || l.Contact == "" || !phoneRE.MatchString(l.Phone) || l.Region == "" || l.Volume == "" || !consent.Consent {
		return nil, errors.New("合作信息不完整")
	}
	hash := sha256.Sum256([]byte(l.Phone))
	phoneHash := hex.EncodeToString(hash[:])
	var existing string
	err := s.store.db.QueryRowContext(ctx, "SELECT id FROM partner_leads WHERE phone_hash=? AND created_at>DATE_SUB(NOW(3),INTERVAL 5 MINUTE) LIMIT 1", phoneHash).Scan(&existing)
	if err == nil {
		return M{"id": existing, "duplicate": true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	l.Contacts, l.Contracts, l.Receipts = []LeadContact{}, []LeadContract{}, []LeadReceipt{}
	l.ExternalReminder = false
	l.CustomerProfile, l.OpportunityAmount = CustomerProfile{}, 0
	l.ID = randomID("LEAD")
	l.Status = "NEW"
	l.FollowUpNote = ""
	l.FollowUps = []LeadFollowUp{}
	l.OwnerID, l.OwnerName, l.ReminderAt = "", "", ""
	l.ReminderDone, l.Version = false, 0
	l.CreatedAt = nowString()
	_, err = s.store.db.ExecContext(ctx, "INSERT INTO partner_leads(id,phone_hash,status,payload) VALUES(?,?,?,?)", l.ID, phoneHash, l.Status, jsonValue(l))
	return M{"id": l.ID}, err
}
func decodeDataURL(value string) (string, []byte, error) {
	parts := strings.SplitN(value, ",", 2)
	if len(parts) != 2 {
		return "", nil, errors.New("图片格式无效")
	}
	mime := strings.TrimPrefix(strings.TrimSuffix(parts[0], ";base64"), "data:")
	if mime != "image/jpeg" && mime != "image/png" && mime != "image/webp" {
		return "", nil, errors.New("仅支持 JPEG、PNG 或 WebP 图片")
	}
	data, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", nil, err
	}
	if len(data) > 4<<20 {
		return "", nil, errors.New("图片不能超过 4MB")
	}
	return mime, data, nil
}
