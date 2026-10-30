package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

func (s *Server) admin(r *http.Request, req Request) (any, error) {
	ctx := r.Context()
	if req.Action == "admin.bootstrap" {
		var d struct{ BootstrapToken, Username, Password string }
		if err := decode(req.Data, &d); err != nil {
			return nil, err
		}
		if os.Getenv("ADMIN_BOOTSTRAP_TOKEN") == "" || d.BootstrapToken != os.Getenv("ADMIN_BOOTSTRAP_TOKEN") {
			return nil, errors.New("初始化口令错误")
		}
		var count int
		if err := s.store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM admins").Scan(&count); err != nil {
			return nil, err
		}
		if count != 0 {
			return nil, errors.New("管理员已经初始化")
		}
		hash, err := hashPassword(d.Password)
		if err != nil {
			return nil, err
		}
		id := randomID("ADM")
		name := clean(d.Username, 40)
		if name == "" {
			return nil, errors.New("请输入用户名")
		}
		_, err = s.store.db.ExecContext(ctx, "INSERT INTO admins(id,username,password_hash,role,password_changed_at) VALUES(?,?,?,'SUPER_ADMIN',NOW(3))", id, name, hash)
		return M{"id": id, "username": name}, err
	}
	if req.Action == "admin.login" {
		var d struct{ Username, Password, Code string }
		if err := decode(req.Data, &d); err != nil {
			return nil, err
		}
		var a Admin
		var passwordChangedAt sql.NullTime
		err := s.store.db.QueryRowContext(ctx, "SELECT id,username,password_hash,role,active,password_changed_at FROM admins WHERE username=?", clean(d.Username, 40)).Scan(&a.ID, &a.Username, &a.PasswordHash, &a.Role, &a.Active, &passwordChangedAt)
		if err == nil {
			if e := s.store.limitLogin(r, "admin-resolved-account", a.ID, 5, 900); e != nil {
				return nil, e
			}
		}
		if err != nil || !a.Active || !verifyPassword(d.Password, a.PasswordHash) {
			return nil, errors.New("用户名或密码错误")
		}
		// 密码过期：签发仅可改密的受限会话，要求先修改密码。
		if passwordExpired(passwordChangedAt, time.Now()) {
			limited, err := s.issueSession(r, Claims{Subject: a.ID, Kind: "admin", Username: a.Username, Role: a.Role, Credential: adminCredential(a.PasswordHash), Limited: "pwd-change", Exp: time.Now().Add(10 * time.Minute).Unix()})
			if err != nil {
				return nil, err
			}
			return M{"passwordExpired": true, "token": limited}, nil
		}
		required, err := s.loginSecondFactor(ctx, a, strings.TrimSpace(d.Code))
		if err != nil {
			return nil, err
		}
		if required {
			return M{"mfaRequired": true}, nil
		}
		// 全员强制二次验证：未绑定则签发仅可绑定的受限会话，引导完成绑定。
		if mfaEnforced() {
			bound, err := func() (bool, error) {
				tx, err := s.store.db.BeginTx(ctx, nil)
				if err != nil {
					return false, err
				}
				defer tx.Rollback()
				return mfaBound(ctx, tx, a.ID)
			}()
			if err != nil {
				return nil, err
			}
			if !bound {
				limited, err := s.issueSession(r, Claims{Subject: a.ID, Kind: "admin", Username: a.Username, Role: a.Role, Credential: adminCredential(a.PasswordHash), Limited: "mfa-setup", Exp: time.Now().Add(10 * time.Minute).Unix()})
				if err != nil {
					return nil, err
				}
				return M{"mfaSetupRequired": true, "token": limited}, nil
			}
		}
		permissions, err := s.store.adminPermissions(ctx, a.ID, a.Role)
		if err != nil {
			return nil, err
		}
		token, err := s.issueSession(r, Claims{Subject: a.ID, Kind: "admin", Username: a.Username, Role: a.Role, Credential: adminCredential(a.PasswordHash), Exp: time.Now().Add(8 * time.Hour).Unix()})
		if err != nil {
			return nil, err
		}
		_, _ = s.store.db.ExecContext(ctx, "UPDATE admins SET last_login_at=NOW(3) WHERE id=?", a.ID)
		s.store.audit(ctx, Claims{Subject: a.ID, Username: a.Username}, "ADMIN_LOGIN", a.ID, M{"role": a.Role})
		return M{"token": token, "user": M{"id": a.ID, "username": a.Username, "role": a.Role, "permissions": permissions}}, nil
	}
	actor, err := s.store.requireAdmin(r)
	if err != nil {
		return nil, err
	}
	// 受限会话（密码过期/强制 MFA 绑定）：只允许完成对应的安全操作。
	if actor.Limited != "" {
		allowed := false
		switch actor.Limited {
		case "pwd-change":
			allowed = req.Action == "admin.account.changePassword"
		case "mfa-setup":
			allowed = req.Action == "admin.security.status" || req.Action == "admin.security.setup" || req.Action == "admin.security.enable"
		}
		if !allowed {
			return nil, errors.New("请先完成安全验证（修改密码或绑定二次验证）")
		}
	}
	if err = s.store.authorizeAdmin(r, actor, req.Action); err != nil {
		return nil, err
	}
	super := func() error {
		if actor.Role != "SUPER_ADMIN" {
			return errors.New("仅超级管理员可以操作")
		}
		return nil
	}
	switch req.Action {
	case "admin.reminder.tasks":
		return s.store.reminderTasks(ctx, req.Data)
	case "admin.reminder.resolve":
		return s.store.resolveReminderTask(ctx, actor, req.Data)
	case "admin.reminder.stats":
		return s.store.reminderStats(ctx)
	case "admin.reminder.history":
		var d struct {
			OpenID string `json:"openid"`
		}
		if e := decode(req.Data, &d); e != nil {
			return nil, e
		}
		if d.OpenID == "" {
			return nil, errors.New("请选择提醒用户")
		}
		return s.store.attentionHistory(ctx, d.OpenID, req.Data)
	case "admin.session.list", "admin.session.revoke", "admin.logout":
		return s.personalSessions(r, actor, req)
	case "admin.login.logs":
		return s.loginLogs(r, actor, req.Data)
	case "admin.course.list":
		return s.store.courseList(ctx)
	case "admin.course.save":
		return s.store.courseSave(ctx, actor, req.Data)
	case "admin.classroom.questions":
		return s.store.classroomQuestions(ctx, "", req.Data, true)
	case "admin.classroom.answer":
		return s.store.classroomAnswer(ctx, actor, req.Data)
	case "admin.security.status", "admin.security.setup", "admin.security.enable", "admin.security.disable", "admin.account.changePassword":
		return s.personalSecurity(r, actor, req)
	case "admin.session":
		permissions, e := s.store.adminPermissions(ctx, actor.Subject, actor.Role)
		return M{"id": actor.Subject, "username": actor.Username, "role": actor.Role, "permissions": permissions}, e
	case "admin.account.permissions":
		return s.saveAccountPermissions(ctx, actor, req.Data)
	case "admin.account.handover":
		return s.accountHandover(ctx, actor, req.Data)
	case "admin.liveNotice.send":
		if err = super(); err != nil {
			return nil, err
		}
		return s.sendLiveNotices(ctx)
	case "admin.alert.thresholds":
		return s.getAlertThresholds(ctx)
	case "admin.alert.thresholds.save":
		if err = super(); err != nil {
			return nil, err
		}
		return s.saveAlertThresholds(ctx, actor, req.Data)
	case "admin.payment.financeReconcile", "admin.payment.overview", "admin.payment.reconcile", "admin.payment.resolve", "admin.payment.alertCount":
		if err = super(); err != nil {
			return nil, err
		}
		return s.paymentOperations(ctx, actor, req)
	case "admin.media.upload":
		var d struct {
			DataURL string `json:"dataUrl"`
		}
		if err = decode(req.Data, &d); err != nil {
			return nil, err
		}
		mime, content, e := decodeDataURL(d.DataURL)
		if e != nil {
			return nil, e
		}
		base := strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/")
		if base == "" {
			return nil, errors.New("PUBLIC_BASE_URL 未配置")
		}
		// 上传时压缩：大图等比缩小到 1600px 以内、JPEG 重编码，减小存储与加载体积。
		originalBytes := len(content)
		content, mime = compressImage(content, mime)
		id := randomID("IMG")
		_, err = s.store.db.ExecContext(ctx, "INSERT INTO media(id,mime,content) VALUES(?,?,?)", id, mime, content)
		if err != nil {
			return nil, err
		}
		s.store.audit(ctx, actor, "MEDIA_UPLOAD", id, M{"bytes": len(content), "originalBytes": originalBytes})
		// CDN 域名由用户提供后配 MEDIA_CDN_BASE（如 https://cdn.example.com），返回 CDN 地址加速加载。
		if cdn := strings.TrimRight(os.Getenv("MEDIA_CDN_BASE"), "/"); cdn != "" {
			return M{"fileID": cdn + "/media/" + id}, nil
		}
		return M{"fileID": base + "/media/" + id}, nil
	case "admin.review.list":
		return s.adminReviews(ctx, req.Data)
	case "admin.review.reply":
		return s.replyReview(ctx, actor, req.Data)
	case "admin.review.moderate":
		return s.moderateReview(ctx, actor, req.Data)
	case "admin.review.alerts":
		return s.reviewAlerts(ctx, req.Data)
	case "admin.review.alertCount":
		return s.reviewAlertCount(ctx)
	case "admin.review.alert.resolve":
		return s.resolveReviewAlert(ctx, actor, req.Data)
	case "admin.dashboard":
		return s.dashboard(ctx, req.Data)
	case "admin.dashboard.export":
		return s.dashboardExport(ctx, actor, req.Data)
	case "admin.product.search":
		return s.searchAdminProducts(ctx, req.Data)
	case "admin.product.import":
		return s.importProducts(ctx, actor, req.Data)
	case "admin.order.batches.save":
		return s.saveOrderBatches(ctx, actor, req.Data)
	case "admin.order.batches.options":
		return s.listProductBatches(ctx, req.Data)
	case "admin.batch.list":
		return s.listProductBatches(ctx, req.Data)
	case "admin.batch.save":
		return s.saveProductBatch(ctx, actor, req.Data)
	case "admin.product.list":
		return s.store.products(ctx, "", true)
	case "admin.product.save":
		return s.saveProduct(ctx, actor, req.Data)
	case "admin.product.batchStatus":
		return s.batchProductStatus(ctx, actor, req.Data)
	case "admin.product.duplicate":
		return s.duplicateProduct(ctx, actor, req.Data)
	case "admin.inventory.locations":
		return s.inventoryLocations(ctx)
	case "admin.inventory.lots":
		return s.inventoryLots(ctx, req.Data)
	case "admin.inventory.lot.movements":
		return s.inventoryLotMovements(ctx, req.Data)
	case "admin.inventory.resolveScan":
		return s.resolveScan(ctx, req.Data)
	case "admin.inventory.warehouse.save":
		return s.saveWarehouse(ctx, actor, req.Data)
	case "admin.inventory.receive", "admin.inventory.loss", "admin.inventory.allocate":
		return s.changeInventoryLot(ctx, actor, req.Action, req.Data)
	case "admin.inventory.batch":
		return s.batchInventory(ctx, actor, req.Data)
	case "admin.inventory.movements":
		return s.inventoryMovements(ctx, req.Data)
	case "admin.inventory.alerts":
		return s.inventoryAlerts(ctx)
	case "admin.inventory.alert.save":
		return s.saveInventoryAlert(ctx, actor, req.Data)
	case "admin.order.split":
		return s.splitOrder(ctx, actor, req.Data)
	case "admin.order.package.ship":
		return s.shipPackage(ctx, actor, req.Data)
	case "admin.order.packages.sync":
		return s.syncPackages(ctx, actor, mustID(req.Data, "orderId"))
	case "admin.order.events":
		return s.orderEvents(ctx, mustID(req.Data, "orderId"), "")
	case "admin.order.tracking":
		return s.tracking(ctx, mustID(req.Data, "orderId"), "", mustID(req.Data, "trackingNo"), mustID(req.Data, "carrierCode"), mustID(req.Data, "packageId"))
	case "admin.activity.oversellCheck":
		return s.activityOversellCheck(ctx)
	case "admin.home.save", "admin.privacy.save":
		if err = super(); err != nil {
			return nil, err
		}
		key := "home"
		if req.Action == "admin.privacy.save" {
			key = "privacy"
		}
		return s.saveOperationalConfig(ctx, actor, req.Data, key)
	case "admin.inventory.set":
		var d struct {
			SKUID string `json:"skuId"`
			Stock int    `json:"stock"`
		}
		if err = decode(req.Data, &d); err != nil {
			return nil, err
		}
		if d.Stock < 0 || d.Stock > 2147483647 {
			return nil, errors.New("库存无效")
		}
		tx, e := s.store.db.BeginTx(ctx, nil)
		if e != nil {
			return nil, e
		}
		defer tx.Rollback()
		_, e = applyInventory(ctx, tx, d.SKUID, "ADJUST", "", actor.Username, "单项库存调整", "UPDATE skus SET stock=? WHERE id=? AND active=1", d.Stock, d.SKUID)
		if e != nil {
			return nil, e
		}
		if e = tx.Commit(); e != nil {
			return nil, e
		}
		s.store.audit(ctx, actor, "INVENTORY_SET", d.SKUID, M{"stock": d.Stock})
		return M{"id": d.SKUID, "stock": d.Stock}, nil
	case "admin.faq.list":
		return s.store.faqList(ctx, false)
	case "admin.faq.manage":
		return s.store.faqList(ctx, true)
	case "admin.faq.save":
		return s.saveFaq(ctx, actor, req.Data)
	case "admin.faq.delete":
		return s.deleteFaq(ctx, actor, req.Data)
	case "admin.ticket.list":
		return s.listTickets(ctx, "", req.Data)
	case "admin.ticket.create":
		return s.createTicket(ctx, "", &actor, req.Data)
	case "admin.ticket.read":
		return s.readTicket(ctx, "", &actor, req.Data)
	case "admin.ticket.sla":
		return s.ticketSLAReport(ctx, req.Data)
	case "admin.ticket.sla.save":
		return s.saveTicketSLAConfig(ctx, actor, req.Data)
	case "admin.ticket.update":
		return s.updateTicket(ctx, "", &actor, req.Data)
	case "admin.ticket.returnTracking":
		return s.returnTracking(ctx, "", req.Data)
	case "admin.invoice.autoConfig":
		_, _, e := autoInvoiceConfig()
		return M{"configured": e == nil, "enabled": os.Getenv("AUTO_INVOICE_ENABLED") == "true"}, nil
	case "admin.invoice.autoIssue":
		return s.issueAutoInvoice(ctx, mustID(req.Data, "orderId"), &actor, true)
	case "admin.invoice.update":
		result, e := s.updateInvoice(ctx, req.Data)
		if e == nil {
			i := result.(*Invoice)
			s.store.audit(ctx, actor, "INVOICE_UPDATE", mustID(req.Data, "orderId"), M{"status": i.Status, "emailStatus": i.EmailStatus})
		}
		return result, e
	case "admin.invoice.retryEmail":
		id := mustID(req.Data, "orderId")
		result, e := s.sendInvoiceEmail(ctx, id)
		if e == nil {
			s.store.audit(ctx, actor, "INVOICE_EMAIL_RETRY", id, M{"emailStatus": result.(*Invoice).EmailStatus})
		}
		return result, e
	case "admin.invoice.file":
		return s.invoiceFile(ctx, mustID(req.Data, "orderId"), "")
	case "admin.invoice.resolveEmail":
		return s.resolveInvoiceEmail(ctx, actor, req.Data)
	case "admin.coupon.get":
		return s.store.config(ctx, "coupons")
	case "admin.coupon.stats":
		return s.couponStats(ctx)
	case "admin.coupon.save":
		if e := super(); e != nil {
			return nil, e
		}
		return s.saveCoupons(ctx, actor, req.Data)
	case "admin.order.list":
		var d OrderSearch
		if err = decode(req.Data, &d); err != nil {
			return nil, err
		}
		if d.Limit <= 0 || d.Limit > 500 {
			d.Limit = 200
		}
		return s.store.searchOrders(ctx, "", d)
	case "admin.order.search":
		var d OrderSearch
		if err = decode(req.Data, &d); err != nil {
			return nil, err
		}
		return s.store.searchOrdersPage(ctx, "", d)
	case "admin.order.retryShippingNotice":
		id := mustID(req.Data, "orderId")
		result, e := s.sendShippingNotice(ctx, id)
		if e == nil {
			s.store.audit(ctx, actor, "SHIPPING_NOTICE_RETRY", id, M{"status": result["status"]})
		}
		return result, e
	case "admin.order.ship":
		return s.shipOrder(ctx, actor, req.Data, false)
	case "admin.order.batchShip":
		return s.batchShipOrders(ctx, actor, req.Data)
	case "admin.order.picking":
		return s.pickingOrders(ctx, req.Data)
	case "admin.order.export":
		return s.exportOrders(ctx, actor, req.Data)
	case "admin.logistics.tasks":
		return s.logisticsTasks(ctx, req.Data)
	case "admin.logistics.task.save":
		return s.saveLogisticsTask(ctx, actor, req.Data)
	case "admin.order.addParcel":
		return s.changeShipment(ctx, actor, req.Data, "append")
	case "admin.waybill.cancel":
		return s.cancelWaybill(ctx, actor, req.Data)
	case "admin.waybill.history":
		return s.waybillHistory(ctx, mustID(req.Data, "orderId"))
	case "admin.waybill.config":
		c, _, e := loadWaybillSettings(ctx, s.store.db)
		return waybillConfigFor(c, e), nil
	case "admin.waybill.settings":
		a, e := readWaybillSettings(ctx, s.store.db, false)
		if e != nil {
			return nil, e
		}
		return redactedWaybillSettings(a), nil
	case "admin.waybill.settings.save":
		return s.saveWaybillSettings(ctx, actor, req.Data)
	case "admin.waybill.quote":
		return s.quoteWaybillRates(ctx, req.Data)
	case "admin.waybill.get":
		return s.getWaybill(ctx, mustID(req.Data, "orderId"))
	case "admin.waybill.batchGet":
		return s.batchWaybillPrint(ctx, req.Data)
	case "admin.waybill.create":
		return s.createWaybill(ctx, actor, req.Data)
	case "admin.waybill.sync":
		return s.syncWaybill(ctx, actor, mustID(req.Data, "orderId"))
	case "admin.waybill.label":
		return s.readWaybillLabel(ctx, mustID(req.Data, "orderId"))
	case "admin.order.resyncShipment":
		return s.shipOrder(ctx, actor, req.Data, true)
	case "admin.order.updateShipment":
		return s.changeShipment(ctx, actor, req.Data, "update")
	case "admin.refund.evidence.get":
		return s.readRefundEvidence(ctx, "", true, req.Data)
	case "admin.refund.list":
		return s.store.refunds(ctx)
	case "admin.refund.reject":
		return s.rejectRefund(ctx, actor, req.Data)
	case "admin.refund.sync":
		id := mustID(req.Data, "refundId")
		result, e := s.syncRefund(ctx, id)
		if e == nil {
			s.store.audit(ctx, actor, "REFUND_SYNC", id, M{"synced": true})
		}
		return result, e
	case "admin.refund.approve":
		return s.approveRefund(ctx, actor, req.Data)
	case "admin.article.submit", "admin.article.review":
		return s.reviewArticle(ctx, actor, req.Action, req.Data)
	case "admin.content.library", "admin.content.resource.save", "admin.content.resource.delete":
		return s.contentLibrary(ctx, actor, req.Action, req.Data)
	case "admin.article.list":
		return s.store.articles(ctx, "", "", true)
	case "admin.article.save":
		return s.saveArticle(ctx, actor, req.Data)
	case "admin.article.versions":
		return s.store.articleVersions(ctx, mustID(req.Data, "articleId"))
	case "admin.article.restore":
		return s.restoreArticle(ctx, actor, req.Data)
	case "admin.article.stats":
		return s.store.articleStats(ctx, mustID(req.Data, "articleId"))
	case "admin.lead.list", "admin.lead.search":
		var d LeadSearch
		if err = decode(req.Data, &d); err != nil {
			return nil, err
		}
		return s.store.searchLeads(ctx, d)
	case "admin.lead.funnel":
		var d LeadSearch
		if err = decode(req.Data, &d); err != nil {
			return nil, err
		}
		return s.store.leadFunnel(ctx, d)
	case "admin.lead.get":
		return s.store.getLead(ctx, mustID(req.Data, "leadId"))
	case "admin.lead.owners":
		return s.store.leadOwners(ctx)
	case "admin.lead.history":
		return s.store.leadHistory(ctx, mustID(req.Data, "leadId"))
	case "admin.lead.deliveries":
		var d struct {
			LeadID string `json:"leadId"`
		}
		if err := json.Unmarshal(req.Data, &d); err != nil {
			return nil, err
		}
		return s.store.leadDeliveries(ctx, d.LeadID)
	case "admin.lead.reminders":
		return s.store.leadReminders(ctx)
	case "admin.lead.publicPool":
		return s.leadPublicPool(ctx)
	case "admin.lead.publicPool.save":
		return s.savePublicPoolConfig(ctx, actor, req.Data)
	case "admin.lead.duplicates":
		return s.leadDuplicates(ctx, req.Data)
	case "admin.lead.merge":
		return s.mergeLeads(ctx, actor, req.Data)
	case "admin.lead.update":
		return s.updateLead(ctx, actor, req.Data)
	case "admin.config.get":
		shipping, e := s.store.shipping(ctx)
		if e != nil {
			return nil, e
		}
		home, e := s.store.home(ctx)
		if e != nil {
			return nil, e
		}
		privacy, e := s.store.privacy(ctx)
		return M{"shipping": shipping, "home": home, "privacy": privacy}, e
	case "admin.config.save":
		if err = super(); err != nil {
			return nil, err
		}
		var d struct {
			Shipping json.RawMessage `json:"shipping"`
		}
		if err = decode(req.Data, &d); err != nil {
			return nil, err
		}
		result, e := s.saveVersionedConfig(ctx, actor, "shipping", d.Shipping)
		return M{"shipping": result.Payload}, e
	case "admin.config.compare":
		if err = super(); err != nil {
			return nil, err
		}
		return s.store.compareConfigs(ctx, req.Data)
	case "admin.audit.export":
		if err = super(); err != nil {
			return nil, err
		}
		return s.exportAudit(ctx, actor, req.Data)
	case "admin.config.submit":
		return s.submitConfigChange(ctx, actor, req.Data)
	case "admin.config.changeOrders":
		if err = super(); err != nil {
			return nil, err
		}
		return s.configChangeOrders(ctx, req.Data)
	case "admin.config.decide":
		if err = super(); err != nil {
			return nil, err
		}
		return s.decideConfigChange(ctx, actor, req.Data)
	case "admin.config.versions":
		if err = super(); err != nil {
			return nil, err
		}
		return s.store.configVersions(ctx, req.Data)
	case "admin.config.rollback":
		if err = super(); err != nil {
			return nil, err
		}
		return s.rollbackConfig(ctx, actor, req.Data)
	case "admin.audit.list":
		if err = super(); err != nil {
			return nil, err
		}
		return s.store.auditLogs(ctx, req.Data)
	case "admin.account.list":
		if err = super(); err != nil {
			return nil, err
		}
		return s.accounts(ctx)
	case "admin.account.save":
		if err = super(); err != nil {
			return nil, err
		}
		return s.saveAccount(ctx, actor, req.Data)
	case "admin.account.resetPassword":
		if err = super(); err != nil {
			return nil, err
		}
		return s.resetAccountPassword(ctx, actor, req.Data)
	default:
		return nil, fmt.Errorf("未知管理接口：%s", req.Action)
	}
}

func (s *Server) saveProduct(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		Product Product `json:"product"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := s.saveProductTx(ctx, tx, actor, d.Product)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "PRODUCT_SAVE", p.ID, jsonValue(M{"name": p.Name})); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.store.product(ctx, p.ID, true)
}
func (s *Server) saveProductTx(ctx context.Context, tx *sql.Tx, actor Claims, p Product) (*Product, error) {
	if p.ID == "" {
		p.ID = randomID("PRD")
	}
	p.Name = clean(p.Name, 80)
	p.Cover = clean(p.Cover, 500)
	if p.Name == "" || p.Cover == "" || len(p.SKUs) == 0 {
		return nil, errors.New("商品名称、封面和规格不能为空")
	}
	if p.Category != "gift" {
		p.Category = "family"
	}
	if p.SaleMode != "presale" {
		p.SaleMode = "spot"
	}
	if p.Status != "DRAFT" {
		p.Status = "PUBLISHED"
	}
	if err := validateProductBatch(p.Traceability); err != nil {
		return nil, err
	}
	if err := validateGiftCustomization(p.GiftCustomization); err != nil {
		return nil, err
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO products(id,category,status,sort_order,payload) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE category=VALUES(category),status=VALUES(status),sort_order=VALUES(sort_order),payload=VALUES(payload)", p.ID, p.Category, p.Status, p.SortOrder, jsonValue(p))
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for i := range p.SKUs {
		x := &p.SKUs[i]
		if x.ID == "" {
			x.ID = randomID("SKU")
		}
		x.ProductID = p.ID
		x.Name = clean(x.Name, 60)
		if x.Name == "" || x.Price < 1 || x.Price > 2147483647 || x.Stock < 0 || x.Stock > 2147483647 || x.LimitPerOrder < 1 || x.LimitPerOrder > 2147483647 {
			return nil, errors.New("SKU 名称、价格、库存或限购无效")
		}
		if ids[x.ID] {
			return nil, errors.New("规格编号重复")
		}
		var owner string
		var liveStock int
		e := tx.QueryRowContext(ctx, "SELECT product_id,stock FROM skus WHERE id=? FOR UPDATE", x.ID).Scan(&owner, &liveStock)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		if e == nil {
			if owner != p.ID {
				return nil, errors.New("规格属于其他商品")
			}
			if x.ExpectedStock != nil && *x.ExpectedStock != liveStock {
				return nil, errors.New("库存已变化，请刷新后重新编辑；商品未保存")
			}
			if x.ExpectedStock == nil {
				x.Stock = liveStock
			}
		}
		ids[x.ID] = true
		_, err = applyInventory(ctx, tx, x.ID, "PRODUCT_SAVE", p.ID, actor.Username, "商品保存", "INSERT INTO skus(id,product_id,name,price,compare_at_price,stock,limit_per_order,weight_label,active,sort_order) VALUES(?,?,?,?,?,?,?,?,1,?) ON DUPLICATE KEY UPDATE name=VALUES(name),price=VALUES(price),compare_at_price=VALUES(compare_at_price),stock=VALUES(stock),limit_per_order=VALUES(limit_per_order),weight_label=VALUES(weight_label),active=1,sort_order=VALUES(sort_order)", x.ID, p.ID, x.Name, x.Price, x.CompareAtPrice, x.Stock, x.LimitPerOrder, x.WeightLabel, x.SortOrder)
		if err != nil {
			return nil, err
		}
	}
	rows, e := tx.QueryContext(ctx, "SELECT id FROM skus WHERE product_id=?", p.ID)
	if e != nil {
		return nil, e
	}
	old := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		if !ids[id] {
			old = append(old, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range old {
		if _, err = tx.ExecContext(ctx, "UPDATE skus SET active=0 WHERE id=?", id); err != nil {
			return nil, err
		}
	}
	return &p, nil
}

func (s *Server) shipOrder(ctx context.Context, actor Claims, raw json.RawMessage, resync bool) (any, error) {
	if resync {
		return s.changeShipment(ctx, actor, raw, "resync")
	}
	return s.changeShipment(ctx, actor, raw, "ship")
}

func (s *Server) rejectRefund(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct{ RefundID, Reason string }
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	d.Reason = clean(d.Reason, 160)
	if d.Reason == "" {
		return nil, errors.New("请输入拒绝原因")
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rf, err := s.store.refund(ctx, tx, d.RefundID, true)
	if err != nil {
		return nil, err
	}
	if rf == nil || rf.Status != "APPLIED" {
		return nil, errors.New("退款申请状态已变化")
	}
	o, err := s.store.order(ctx, tx, rf.OrderID, true)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, errors.New("订单不存在")
	}
	o.RefundStatus = "REJECTED"
	appendOrderEvent(o, "REFUND_REJECTED", d.Reason, actor.Username, "")
	_, err = tx.ExecContext(ctx, "UPDATE orders SET refund_status=?,payload=? WHERE id=?", o.RefundStatus, jsonValue(o), o.ID)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE refunds SET status='REJECTED',reject_reason=?,reviewed_by=? WHERE id=?", clean(d.Reason, 160), actor.Subject, rf.ID)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.store.audit(ctx, actor, "REFUND_REJECT", rf.ID, M{"reason": d.Reason})
	return M{"status": "REJECTED"}, nil
}
func (s *Server) approveRefund(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	id := mustID(raw, "refundId")
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rf, err := s.store.refund(ctx, tx, id, true)
	if err != nil {
		return nil, err
	}
	if rf == nil || (rf.Status != "APPLIED" && rf.Status != "APPROVED") {
		return nil, errors.New("退款申请状态已变化")
	}
	o, err := s.store.order(ctx, tx, rf.OrderID, true)
	if err != nil {
		return nil, err
	}
	if o != nil && autoInvoiceBusy(o.Invoice) {
		return nil, errors.New("自动开票结果待确认，请先核实原请求")
	}
	if o == nil || (o.PaymentStatus != "PAID" && o.PaymentStatus != "REFUNDING") || (o.FulfillmentStatus != "UNFULFILLED" && o.FulfillmentStatus != "PREPARING" && o.FulfillmentStatus != "SHIPPED" && o.FulfillmentStatus != "RECEIVED") || rf.Amount <= 0 || rf.Amount > o.Quote.Total-o.RefundedAmount {
		return nil, errors.New("订单不支持退款")
	}
	// Validate configuration before reserving the application. Once the network request
	// starts, any error is ambiguous: keep shipping/rejection blocked until reconciled.
	if s.wechat.privateKey == nil || s.wechat.publicKey == nil || os.Getenv("WECHAT_MCH_ID") == "" || os.Getenv("WECHAT_MCH_SERIAL_NO") == "" || os.Getenv("WECHAT_REFUND_NOTIFY_URL") == "" {
		return nil, errors.New("微信退款配置未完成")
	}
	rf.Status = "APPROVED"
	o.RefundStatus = "APPROVED"
	o.PaymentStatus = "REFUNDING"
	if _, err = tx.ExecContext(ctx, "UPDATE refunds SET status='APPROVED',reviewed_by=? WHERE id=?", actor.Subject, id); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE orders SET refund_status=?,payment_status=?,payload=? WHERE id=?", o.RefundStatus, o.PaymentStatus, jsonValue(o), o.ID); err != nil {
		return nil, err
	}
	// Persist approval before moving money. The same out_refund_no is reused on recovery.
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	provider, requestErr := s.wechat.refund(*o, *rf)
	s.store.audit(ctx, actor, "REFUND_APPROVE", id, M{"submissionUncertain": requestErr != nil})
	if requestErr != nil {
		return nil, fmt.Errorf("退款提交结果待确认，请查询进度；未创建时可按原退款单重试：%w", requestErr)
	}
	if err = validateRefundResult(*rf, provider, false); err != nil {
		return nil, err
	}
	status, _ := provider["status"].(string)
	providerID, _ := provider["refund_id"].(string)
	if err = s.recordRefundResult(ctx, id, status, providerID, provider); err != nil {
		return nil, err
	}
	return s.store.refund(ctx, s.store.db, id, false)
}
func (s *Server) accounts(ctx context.Context) ([]Admin, error) {
	rows, err := s.store.db.QueryContext(ctx, "SELECT id,username,role,active,created_at,last_login_at FROM admins ORDER BY created_at,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Admin{}
	for rows.Next() {
		var a Admin
		var created time.Time
		var last sql.NullTime
		if err = rows.Scan(&a.ID, &a.Username, &a.Role, &a.Active, &created, &last); err != nil {
			return nil, err
		}
		a.CreatedAt = created.UTC().Format(time.RFC3339Nano)
		if last.Valid {
			v := last.Time.UTC().Format(time.RFC3339Nano)
			a.LastLoginAt = &v
		}
		out = append(out, a)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range out {
		out[i].Permissions, err = s.store.adminPermissions(ctx, out[i].ID, out[i].Role)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (s *Server) saveAccount(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		Account struct {
			ID, Username, Password, Role string
			Permissions                  *[]string `json:"permissions"`
			Active                       *bool
		} `json:"account"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	a := d.Account
	if a.Permissions != nil {
		if err := validatePermissions(*a.Permissions); err != nil {
			return nil, err
		}
	}
	name := clean(a.Username, 40)
	if name == "" {
		return nil, errors.New("管理员用户名不能为空")
	}
	if a.Role != "SUPER_ADMIN" && a.Role != "OPERATOR" {
		return nil, errors.New("管理员角色无效")
	}
	// Lock the account set in a consistent order so concurrent demotions and
	// deactivations cannot remove the final active super administrator.
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	accounts, err := lockedAdminAccounts(ctx, tx, actor)
	if err != nil {
		return nil, err
	}
	id := a.ID
	var existing *Admin
	for i := range accounts {
		if accounts[i].ID == id {
			existing = &accounts[i]
		}
		if strings.EqualFold(accounts[i].Username, name) && accounts[i].ID != id {
			return nil, errors.New("管理员用户名已存在")
		}
	}
	if id != "" && existing == nil {
		return nil, errors.New("管理员账号不存在")
	}
	active := true
	hash := ""
	if existing != nil {
		active, hash = existing.Active, existing.PasswordHash
	}
	if a.Active != nil {
		active = *a.Active
	}
	if id == actor.Subject && !active {
		return nil, errors.New("不能停用当前账号")
	}
	if existing != nil && existing.Active && existing.Role == "SUPER_ADMIN" && (!active || a.Role != "SUPER_ADMIN") {
		remaining := 0
		for _, account := range accounts {
			if account.ID != id && account.Active && account.Role == "SUPER_ADMIN" {
				remaining++
			}
		}
		if remaining == 0 {
			return nil, errors.New("必须保留至少一个启用的超级管理员")
		}
	}
	if a.Password != "" {
		hash, err = hashPassword(a.Password)
		if err != nil {
			return nil, err
		}
	}
	if hash == "" {
		return nil, errors.New("新账号必须设置至少 10 位密码")
	}
	if existing == nil {
		id = randomID("ADM")
		_, err = tx.ExecContext(ctx, "INSERT INTO admins(id,username,password_hash,role,active,password_changed_at) VALUES(?,?,?,?,?,NOW(3))", id, name, hash, a.Role, active)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE admins SET username=?,password_hash=?,role=?,active=? WHERE id=?", name, hash, a.Role, active, id)
	}
	if err != nil {
		var mysqlError *mysql.MySQLError
		if errors.As(err, &mysqlError) && mysqlError.Number == 1062 {
			return nil, errors.New("管理员用户名已存在")
		}
		return nil, err
	}
	if a.Permissions != nil && a.Role == "OPERATOR" {
		permissions := *a.Permissions
		if permissions == nil {
			permissions = []string{}
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO configs(config_key,payload) VALUES(?,?) ON DUPLICATE KEY UPDATE payload=VALUES(payload)", "auth.permissions."+id, jsonValue(permissions)); err != nil {
			return nil, err
		}
	}
	detail := M{"username": name, "role": a.Role, "active": active, "passwordChanged": a.Password != "", "permissions": a.Permissions}
	if existing != nil {
		var beforePerms []string
		var rawPerms []byte
		if e := tx.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key=?", "auth.permissions."+id).Scan(&rawPerms); e == nil {
			_ = json.Unmarshal(rawPerms, &beforePerms)
		}
		detail["before"] = M{"role": existing.Role, "active": existing.Active, "permissions": beforePerms}
		detail["after"] = M{"role": a.Role, "active": active, "permissions": a.Permissions}
	}
	if existing != nil && existing.Active && !active {
		if err = revokeAdminSessions(ctx, tx, id); err != nil {
			return nil, err
		}
		detail["sessionsRevoked"] = true
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "ADMIN_ACCOUNT_SAVE", id, jsonValue(detail)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return M{"id": id, "username": name, "role": a.Role, "active": active, "sessionRevoked": existing != nil && a.Password != "", "currentSessionRevoked": id == actor.Subject && a.Password != ""}, nil
}

func lockedAdminAccounts(ctx context.Context, tx *sql.Tx, actor Claims) ([]Admin, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id,username,password_hash,role,active FROM admins ORDER BY id FOR UPDATE")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []Admin{}
	authorized := false
	for rows.Next() {
		var account Admin
		if err = rows.Scan(&account.ID, &account.Username, &account.PasswordHash, &account.Role, &account.Active); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
		if account.ID == actor.Subject && account.Active && account.Role == "SUPER_ADMIN" {
			authorized = actor.Credential == adminCredential(account.PasswordHash)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if !authorized {
		return nil, errors.New("管理员登录已失效，请重新登录")
	}
	return accounts, nil
}

func (s *Server) resetAccountPassword(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		AccountID string `json:"accountId"`
		Password  string `json:"password"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.AccountID == "" {
		return nil, errors.New("请选择需要重置密码的账号")
	}
	hash, err := hashPassword(d.Password)
	if err != nil {
		return nil, err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	accounts, err := lockedAdminAccounts(ctx, tx, actor)
	if err != nil {
		return nil, err
	}
	var target *Admin
	for i := range accounts {
		if accounts[i].ID == d.AccountID {
			target = &accounts[i]
			break
		}
	}
	if target == nil {
		return nil, errors.New("管理员账号不存在")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE admins SET password_hash=? WHERE id=?", hash, target.ID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "ADMIN_PASSWORD_RESET", target.ID, jsonValue(M{"username": target.Username, "sessionsRevoked": true})); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return M{"id": target.ID, "sessionRevoked": true, "currentSessionRevoked": target.ID == actor.Subject}, nil
}
