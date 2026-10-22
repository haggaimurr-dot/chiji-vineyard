export interface AdminUser { id?: string; username: string; role: string; permissions?: string[] }
export const permissionOptions = [
 { key: "article.review", label: "审核种植内容" },
  {
    "key": "dashboard.read",
    "label": "查看经营概览"
  },
  {
    "key": "product.read",
    "label": "查看商品"
  },
  {
    "key": "product.write",
    "label": "编辑商品（需配合调整库存）"
  },
  {
    "key": "inventory.write",
    "label": "调整库存"
  },
  {
    "key": "order.read",
    "label": "查看订单及面单"
  },
  {
    "key": "order.export",
    "label": "导出订单"
  },
  {
    "key": "order.ship",
    "label": "发货及管理物流"
  },
  {
    "key": "refund.read",
    "label": "查看退款及举证"
  },
  {
    "key": "refund.approve",
    "label": "审批退款"
  },
  {
    "key": "invoice.read",
    "label": "下载发票"
  },
  {
    "key": "invoice.write",
    "label": "开票及发送邮件"
  },
  {
    "key": "review.read",
    "label": "查看评价"
  },
  {
    "key": "review.reply",
    "label": "回复评价"
  },
  {
    "key": "article.read",
    "label": "查看内容与统计"
  },
  {
    "key": "article.write",
    "label": "编辑及恢复内容"
  },
  {
    "key": "lead.read",
    "label": "查看合作线索"
  },
  {
    "key": "lead.write",
    "label": "跟进合作线索"
  },
  {
    "key": "media.upload",
    "label": "上传图片及视频"
  },
  {
    "key": "config.read",
    "label": "查看运营配置"
  }
]
export const actionPermissions: Record<string, string> = {
 "admin.order.split":"order.ship", "admin.order.package.ship":"order.ship", "admin.order.packages.sync":"order.ship", "admin.order.events":"order.read",
 "admin.logistics.tasks": "order.read", "admin.logistics.task.save": "order.ship", "admin.order.addParcel": "order.ship", "admin.waybill.cancel": "order.ship", "admin.waybill.history": "order.read",
 "admin.course.list": "article.read", "admin.course.save": "article.write",
	'admin.session.list': 'session', 'admin.session.revoke': 'session', 'admin.logout': 'session',
 "admin.reminder.tasks": "product.read", "admin.reminder.history": "product.read", "admin.reminder.resolve": "product.write",
 "admin.classroom.questions": "article.read", "admin.classroom.answer": "article.write",
 "admin.ticket.returnTracking":"refund.read", "admin.ticket.list":"refund.read", "admin.ticket.read":"refund.read", "admin.ticket.create":"refund.approve", "admin.ticket.update":"refund.approve",
 "admin.invoice.autoConfig":"order.read", "admin.invoice.autoIssue":"invoice.write",
  "admin.waybill.settings": "super", "admin.waybill.settings.save": "super", "admin.waybill.quote": "order.read",
 "admin.payment.financeReconcile": "super", "admin.payment.overview": "super", "admin.payment.reconcile": "super", "admin.payment.resolve": "super", "admin.payment.alertCount": "super",
  "admin.dashboard": "dashboard.read",
  "admin.product.search": "product.read", "admin.product.import": "product.write", "admin.batch.list": "product.read", "admin.batch.save": "product.write",
  "admin.product.list": "product.read",
  "admin.product.save": "product.write",
  "admin.inventory.locations": "product.read", "admin.inventory.lots": "product.read", "admin.inventory.lot.movements": "product.read", "admin.inventory.warehouse.save": "inventory.write", "admin.inventory.receive": "inventory.write", "admin.inventory.loss": "inventory.write", "admin.inventory.allocate": "inventory.write",
 "admin.inventory.set": "inventory.write",
  "admin.inventory.batch": "inventory.write",
  "admin.inventory.movements": "product.read",
  "admin.inventory.alerts": "product.read",
  "admin.inventory.alert.save": "inventory.write",
  "admin.order.list": "order.read",
  "admin.order.search": "order.read",
  "admin.order.tracking": "order.read",
  "admin.order.picking": "order.read",
  "admin.order.export": "order.export",
  "admin.order.batches.options": "order.read", "admin.order.batches.save": "order.ship", "admin.order.ship": "order.ship",
  "admin.order.batchShip": "order.ship",
  "admin.order.retryShippingNotice": "order.ship",
  "admin.order.resyncShipment": "order.ship",
  "admin.order.updateShipment": "order.ship",
  "admin.waybill.config": "order.read",
  "admin.waybill.get": "order.read",
  "admin.waybill.label": "order.read",
  "admin.waybill.create": "order.ship",
  "admin.waybill.sync": "order.ship",
  "admin.refund.list": "refund.read",
  "admin.refund.evidence.get": "refund.read",
  "admin.refund.approve": "refund.approve",
  "admin.refund.reject": "refund.approve",
  "admin.refund.sync": "refund.approve",
  "admin.invoice.file": "invoice.read",
  "admin.invoice.update": "invoice.write",
  "admin.invoice.retryEmail": "invoice.write",
  "admin.invoice.resolveEmail": "invoice.write",
  "admin.review.moderate": "review.reply",
  "admin.review.list": "review.read",
  "admin.review.reply": "review.reply",
  "admin.article.list": "article.read",
  "admin.article.submit": "article.write",
  "admin.article.review": "article.review",
  "admin.content.library": "article.read",
  "admin.content.resource.save": "session",
  "admin.content.resource.delete": "session",
  "admin.article.versions": "article.read",
  "admin.article.stats": "article.read",
  "admin.article.save": "article.write",
  "admin.article.restore": "article.write",
  "admin.lead.funnel": "lead.read",
  "admin.lead.list": "lead.read",
  "admin.lead.search": "lead.read",
  "admin.lead.get": "lead.read",
  "admin.lead.owners": "lead.read",
  "admin.lead.history": "lead.read",
  "admin.lead.reminders": "lead.read",
  "admin.lead.deliveries": "lead.read",
  "admin.lead.update": "lead.write",
  "admin.media.upload": "media.upload",
  "admin.config.get": "config.read",
  "admin.runtime.overview": "super", "admin.runtime.settings": "super", "admin.runtime.migrate": "super",
  "admin.runtime.run": "super", "admin.runtime.mediaCheck": "super", "admin.runtime.backup": "super", "admin.runtime.drill": "super",
  "admin.coupon.get": "config.read",
  "admin.config.save": "super",
  "admin.home.save": "super",
  "admin.privacy.save": "super",
  "admin.coupon.save": "super",
  "admin.config.versions": "super",
  "admin.config.rollback": "super",
  "admin.audit.list": "super",
  "admin.audit.export": "super",
  "admin.config.compare": "super",
  "admin.account.list": "super",
  "admin.account.save": "super",
  "admin.account.resetPassword": "super",
  "admin.account.permissions": "super",
  "admin.security.status": "session", "admin.security.setup": "session", "admin.security.enable": "session", "admin.security.disable": "session", "admin.account.changePassword": "session",
"admin.session": "session"
}
export const defaultPermissions = () => permissionOptions.map(p => p.key)
export function can(user: AdminUser, permission: string) {
  return user.role === 'SUPER_ADMIN' || user.role === 'OPERATOR' && (user.permissions ?? defaultPermissions()).includes(permission)
}
export function requirePermission(user: AdminUser, action: string) {
  const permission = actionPermissions[action]
  if (!permission) throw new Error('权限不足，无法执行此操作')
  if (permission === 'super' && user.role !== 'SUPER_ADMIN') throw new Error('仅超级管理员可以操作')
  if ((action === 'admin.product.save' || action === 'admin.product.import') && !can(user, 'inventory.write')) throw new Error('编辑商品需要同时拥有调整库存权限')
  if (permission !== 'session' && !can(user, permission)) throw new Error('权限不足，无法执行此操作')
}
export function currentAdmin(): AdminUser {
  try { return JSON.parse(localStorage.getItem('chiji_admin_user') || '{}') }
  catch { return { username: '', role: '' } }
}
