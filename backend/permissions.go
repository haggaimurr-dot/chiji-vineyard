package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
)

var errPermission = errors.New("权限不足，无法执行此操作")

// Every admin action must be explicitly registered. Unknown actions fail closed.
var actionPermissions = map[string]string{
	"admin.logistics.tasks": "order.read", "admin.logistics.task.save": "order.ship", "admin.order.addParcel": "order.ship", "admin.waybill.cancel": "order.ship", "admin.waybill.history": "order.read",
	"admin.course.list": "article.read", "admin.course.save": "article.write",
	"admin.reminder.tasks": "product.read", "admin.reminder.history": "product.read", "admin.reminder.resolve": "product.write",
	"admin.classroom.questions": "article.read", "admin.classroom.answer": "article.write",
	"admin.ticket.list": "refund.read", "admin.ticket.read": "refund.read", "admin.ticket.create": "refund.approve", "admin.ticket.update": "refund.approve", "admin.ticket.sla": "refund.read", "admin.ticket.sla.save": "super", "admin.ticket.returnTracking": "refund.read",
	"admin.invoice.autoConfig": "order.read", "admin.invoice.autoIssue": "invoice.write",
	"admin.article.submit": "article.write", "admin.article.review": "article.review",
	"admin.content.library": "article.read", "admin.content.resource.save": "session", "admin.content.resource.delete": "session",
	"admin.waybill.settings": "super", "admin.waybill.settings.save": "super", "admin.waybill.quote": "order.read",
	"admin.payment.financeReconcile": "super", "admin.payment.overview": "super", "admin.payment.reconcile": "super", "admin.payment.resolve": "super", "admin.payment.alertCount": "super",
	"admin.dashboard":      "dashboard.read",
	"admin.product.search": "product.read", "admin.product.import": "product.write", "admin.batch.list": "product.read", "admin.batch.save": "product.write",
	"admin.product.list": "product.read", "admin.product.save": "product.write", "admin.product.batchStatus": "product.write", "admin.product.duplicate": "product.write",
	"admin.inventory.locations": "product.read", "admin.inventory.lots": "product.read", "admin.inventory.lot.movements": "product.read", "admin.inventory.warehouse.save": "inventory.write", "admin.inventory.receive": "inventory.write", "admin.inventory.loss": "inventory.write", "admin.inventory.allocate": "inventory.write",
	"admin.inventory.set": "inventory.write", "admin.inventory.batch": "inventory.write",
	"admin.inventory.movements": "product.read", "admin.inventory.alerts": "product.read", "admin.inventory.alert.save": "inventory.write", "admin.inventory.resolveScan": "product.read",
	"admin.order.split": "order.ship", "admin.order.package.ship": "order.ship", "admin.order.packages.sync": "order.ship", "admin.order.events": "order.read",
	"admin.order.list": "order.read", "admin.order.search": "order.read", "admin.order.tracking": "order.read",
	"admin.order.picking":         "order.read",
	"admin.order.export":          "order.export",
	"admin.order.batches.options": "order.read", "admin.order.batches.save": "order.ship", "admin.order.ship": "order.ship", "admin.order.batchShip": "order.ship", "admin.order.retryShippingNotice": "order.ship", "admin.order.resyncShipment": "order.ship", "admin.order.updateShipment": "order.ship",
	"admin.waybill.config": "order.read", "admin.waybill.get": "order.read", "admin.waybill.batchGet": "order.ship", "admin.waybill.label": "order.read", "admin.waybill.create": "order.ship", "admin.waybill.sync": "order.ship",
	"admin.refund.list": "refund.read", "admin.refund.evidence.get": "refund.read", "admin.refund.approve": "refund.approve", "admin.refund.reject": "refund.approve", "admin.refund.sync": "refund.approve",
	"admin.invoice.file": "invoice.read", "admin.invoice.update": "invoice.write", "admin.invoice.retryEmail": "invoice.write", "admin.invoice.resolveEmail": "invoice.write",
	"admin.review.moderate": "review.reply", "admin.review.list": "review.read", "admin.review.alerts": "review.read", "admin.review.alertCount": "review.read", "admin.review.alert.resolve": "review.reply", "admin.review.reply": "review.reply",
	"admin.article.list": "article.read", "admin.article.versions": "article.read", "admin.article.stats": "article.read", "admin.article.save": "article.write", "admin.article.restore": "article.write",
	"admin.lead.funnel": "lead.read", "admin.lead.list": "lead.read", "admin.lead.search": "lead.read", "admin.lead.get": "lead.read", "admin.lead.owners": "lead.read", "admin.lead.history": "lead.read", "admin.lead.reminders": "lead.read", "admin.lead.deliveries": "lead.read", "admin.lead.update": "lead.write", "admin.lead.publicPool": "lead.read", "admin.lead.publicPool.save": "super", "admin.lead.duplicates": "lead.read", "admin.lead.merge": "lead.write",
	"admin.media.upload": "media.upload",
	"admin.config.get":   "config.read", "admin.coupon.get": "config.read", "admin.coupon.stats": "config.read",
	"admin.config.save": "super", "admin.home.save": "super", "admin.activity.oversellCheck": "config.read", "admin.privacy.save": "super", "admin.coupon.save": "super", "admin.config.versions": "super", "admin.config.rollback": "super", "admin.audit.list": "super", "admin.audit.export": "super", "admin.config.compare": "super",
	"admin.account.list": "super", "admin.account.save": "super", "admin.account.resetPassword": "super", "admin.account.permissions": "super",
	"admin.security.status": "session", "admin.security.setup": "session", "admin.security.enable": "session", "admin.security.disable": "session", "admin.account.changePassword": "session",
	"admin.session": "session", "admin.session.list": "session", "admin.session.revoke": "session", "admin.logout": "session",
	"admin.login.logs": "session",
}

func defaultPermissions(role string) []string {
	out := []string{}
	if role != "OPERATOR" && role != "SUPER_ADMIN" {
		return out
	}
	for _, p := range actionPermissions {
		if p != "super" && p != "session" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

func validatePermissions(values []string) error {
	for _, p := range values {
		if !slices.Contains(defaultPermissions("OPERATOR"), p) {
			return errors.New("权限标识无效：" + p)
		}
	}
	return nil
}

func (s *Store) adminPermissions(ctx context.Context, id, role string) ([]string, error) {
	if role == "SUPER_ADMIN" {
		return defaultPermissions(role), nil
	}
	if role != "OPERATOR" {
		return []string{}, nil
	}
	var raw []byte
	err := s.db.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key=?", "auth.permissions."+id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultPermissions(role), nil
	}
	if err != nil {
		return nil, err
	}
	var permissions []string
	if err = json.Unmarshal(raw, &permissions); err != nil {
		return nil, err
	}
	if err = validatePermissions(permissions); err != nil {
		return nil, err
	}
	if permissions == nil {
		permissions = []string{}
	}
	return permissions, nil
}

func (s *Store) authorizeAdmin(r *http.Request, actor Claims, action string) error {
	p, registered := actionPermissions[action]
	if !registered {
		return errPermission
	}
	if actor.Role == "SUPER_ADMIN" || p == "session" {
		return nil
	}
	if p == "super" {
		return fmt.Errorf("%w：仅超级管理员可以操作", errPermission)
	}
	permissions, err := s.adminPermissions(r.Context(), actor.Subject, actor.Role)
	if err != nil {
		return err
	}
	if !slices.Contains(permissions, p) {
		return errPermission
	}
	// Product saves also create/change SKU stock; prevent bypassing inventory grants.
	if (action == "admin.product.save" || action == "admin.product.import" || action == "admin.product.batchStatus" || action == "admin.product.duplicate") && !slices.Contains(permissions, "inventory.write") {
		return errPermission
	}
	return nil
}

func (s *Server) saveAccountPermissions(ctx context.Context, actor Claims, raw json.RawMessage) (any, error) {
	var d struct {
		AccountID   string   `json:"accountId"`
		Permissions []string `json:"permissions"`
	}
	if err := decode(raw, &d); err != nil {
		return nil, err
	}
	if d.AccountID == "" || d.Permissions == nil {
		return nil, errors.New("请选择账号并提供权限列表")
	}
	if err := validatePermissions(d.Permissions); err != nil {
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
	found := false
	for _, a := range accounts {
		if a.ID == d.AccountID {
			if a.Role != "OPERATOR" {
				return nil, errors.New("超级管理员拥有全部权限，无需设置")
			}
			found = true
		}
	}
	if !found {
		return nil, errors.New("管理员账号不存在")
	}
	slices.Sort(d.Permissions)
	d.Permissions = slices.Compact(d.Permissions)
	var before []string
	var rawBefore []byte
	if e := tx.QueryRowContext(ctx, "SELECT payload FROM configs WHERE config_key=?", "auth.permissions."+d.AccountID).Scan(&rawBefore); e == nil {
		_ = json.Unmarshal(rawBefore, &before)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO configs(config_key,payload) VALUES(?,?) ON DUPLICATE KEY UPDATE payload=VALUES(payload)", "auth.permissions."+d.AccountID, jsonValue(d.Permissions)); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(admin_id,username,action,target_id,detail) VALUES(?,?,?,?,?)", actor.Subject, actor.Username, "ADMIN_PERMISSIONS_SAVE", d.AccountID, jsonValue(M{"before": before, "after": d.Permissions})); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return M{"id": d.AccountID, "permissions": d.Permissions}, nil
}
