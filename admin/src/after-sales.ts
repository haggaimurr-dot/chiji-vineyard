export interface TicketAttachment { name: string; dataUrl: string }
export interface AfterSalesTicket {
 return?: TicketReturn
 id: string; orderId: string; orderNo: string; refundId?: string; category: string; status: string; owner?: string; version: number; createdAt: string; updatedAt: string
 dueAt?: string; userRead?: number; adminRead?: number
 messages: Array<{role: string; content: string; createdAt: string; attachments?: TicketAttachment[]}>
}
export const ticketStatuses: Record<string,string> = { OPEN: '待处理', PROCESSING: '处理中', RESOLVED: '已解决', CLOSED: '已关闭' }
export const ticketCategories: Record<string,string> = { QUALITY: '品质问题', DELIVERY: '物流问题', INVOICE: '发票问题', OTHER: '其他问题' }
export function ticketReminder(ticket: AfterSalesTicket, role: 'USER' | 'ADMIN', now = Date.now()) {
 const read = role === 'USER' ? ticket.userRead || 0 : ticket.adminRead || 0
 const unread = ticket.messages.slice(read).filter(m => m.role !== role).length
 const dueAt = ticket.dueAt || new Date(Date.parse(ticket.createdAt) + 48 * 3600000).toISOString()
 const overdue = ['OPEN','PROCESSING'].includes(ticket.status) && Date.parse(dueAt) < now
 return { unread, dueAt, overdue, text: [unread ? `${unread}条未读${role === 'USER' ? '商家回复' : '用户消息'}` : '', overdue ? '处理已超时' : ''].filter(Boolean).join(' · ') }
}
export function validateAttachments(items: TicketAttachment[] = []) {
 if (!Array.isArray(items) || items.length > 3) throw new Error('每次最多3张附件')
 let size = 0
 for (const item of items) {
  if (!item || typeof item.name !== 'string' || !item.name.trim() || [...item.name].length > 100 || typeof item.dataUrl !== 'string') throw new Error('附件名称无效')
  if (!/^data:image\/(jpeg|png);base64,[A-Za-z0-9+/]+={0,2}$/.test(item.dataUrl)) throw new Error('附件仅支持JPEG或PNG图片')
  const encoded = item.dataUrl.split(',')[1]; size += encoded.length * 3 / 4 - (encoded.endsWith('==') ? 2 : encoded.endsWith('=') ? 1 : 0)
 }
 if (size > 512 * 1024) throw new Error('每次附件合计不得超过512KB')
 return items
}
export function ticketDeadline(value: string) {
 const time = Date.parse(value)
 if (!Number.isFinite(time) || time <= Date.now() || time > Date.now() + 30 * 86400000) throw new Error('处理期限须在未来30天以内')
 return new Date(time).toISOString()
}

export function validateEvidenceBudget(ticket: AfterSalesTicket, added: TicketAttachment[]) {
 const size = ticket.messages.reduce((n,m) => n + (m.attachments || []).reduce((bytes,a) => bytes+a.dataUrl.length,0),0) + added.reduce((n,a) => n+a.dataUrl.length,0)
 if (size > 8*1024*1024) throw new Error('工单附件累计已达6MB，请联系商家处理')
}

export interface ReturnItem { skuId: string; name?: string; quantity: number; restock: number }
export interface TicketReturn {
 status: 'AWAITING_SHIPMENT' | 'SHIPPED' | 'RECEIVED'
 address: {name:string; phone:string; province:string; city:string; district:string; detail:string}
 items: ReturnItem[]; shipment?: {carrier:string; carrierCode:string; trackingNo:string}
 authorizedAt:string; shippedAt?:string; receivedAt?:string; inspection?:string; receivedBy?:string
}
export const returnStatuses: Record<string,string> = {AWAITING_SHIPMENT:'待寄回',SHIPPED:'已寄回 / 待验收',RECEIVED:'已验收入库'}
export function demoReturnChange(ticket: AfterSalesTicket, data: any, admin: boolean, orderItems: Array<{skuId:string; quantity:number; name?:string; skuName?:string}> = [], previous: AfterSalesTicket[] = []): TicketReturn {
 const d=data, old=ticket.return, now=new Date().toISOString()
 if(d.action==='SHIP') {
  if(!old || !['AWAITING_SHIPMENT','SHIPPED'].includes(old.status)) throw new Error('当前退货不能登记寄件')
  const p={carrier:String(d.shipment?.carrier || '').trim(),carrierCode:String(d.shipment?.carrierCode || '').trim().toUpperCase(),trackingNo:String(d.shipment?.trackingNo || '').trim()}
  if(!p.carrier || p.carrier.length>40 || /[<>\u0000-\u001f]/.test(p.carrier) || !/^[A-Z0-9_-]{1,20}$/.test(p.carrierCode) || !/^[A-Za-z0-9_-]{4,50}$/.test(p.trackingNo)) throw new Error('请填写有效的物流公司、编码和运单号')
  return {...old,status:'SHIPPED',shipment:p,shippedAt:now}
 }
 if(!admin) throw new Error('仅商家可确认退货地址或验收入库')
 if(d.action==='AUTHORIZE' && old) throw new Error('此工单已登记退货，地址与明细不可覆盖')
 if(d.action==='RECEIVE' && old?.status!=='SHIPPED') throw new Error('请先登记退货寄件，已验收的退货不能重复入库')
 const available:Record<string,number>={}
 const source=d.action==='RECEIVE' ? old!.items : orderItems
 source.forEach(i=>available[i.skuId]=(available[i.skuId] || 0)+i.quantity)
 if(d.action==='AUTHORIZE') previous.filter(t=>t.id!==ticket.id && t.orderId===ticket.orderId).forEach(t=>t.return?.items.forEach(i=>available[i.skuId]=(available[i.skuId]||0)-i.quantity))
 const items:ReturnItem[]=d.items || [], seen=new Set<string>()
 if(!items.length || items.length>100) throw new Error('请选择退货明细')
 for(const i of items) {
  if(seen.has(i.skuId) || !Number.isInteger(i.quantity) || i.quantity<=0 || i.quantity>available[i.skuId] || !Number.isInteger(i.restock) || i.restock<0 || i.restock>i.quantity || (d.action==='AUTHORIZE' && i.restock!==0)) throw new Error('退货规格、数量或可售入库数量无效')
  seen.add(i.skuId)
 }
 if(d.action==='AUTHORIZE') {
  const a=d.address
  if(!a || !a.name?.trim() || !/^1[3-9]\d{9}$/.test(a.phone) || !a.province?.trim() || !a.city?.trim() || !a.detail?.trim()) throw new Error('退货地址不完整')
  return {status:'AWAITING_SHIPMENT',address:{...a},items:items.map(i=>({...i,name:orderItems.find(o=>o.skuId===i.skuId)?.name})),authorizedAt:now}
 }
 if(d.action==='RECEIVE') {
  if(items.length!==old!.items.length || items.some(i=>i.quantity!==available[i.skuId])) throw new Error('请验收全部退货明细，数量须一致')
  if(!d.inspection?.trim() || [...d.inspection].length>1000) throw new Error('请填写验收结果，最多1000字')
  return {...old!,status:'RECEIVED',items:old!.items.map(i=>({...i,restock:items.find(n=>n.skuId===i.skuId)!.restock})),receivedAt:now,inspection:d.inspection.trim()}
 }
 throw new Error('退货操作无效')
}

