import type { LogisticsTask, Order, Waybill } from './types'
import { canEditShipment, validateShipment } from './orders'

export function createLogisticsDemo(orders: () => Order[], waybills: Map<string,Waybill>) {
  let tasks: LogisticsTask[] = [], history: Array<{orderId: string; waybill: Waybill; reason: string; actor: string; at: string}> = []
  const storageKey = 'chiji-logistics-demo-v1'
  try { const stored = sessionStorage.getItem(storageKey); if(stored) ({tasks,history} = JSON.parse(stored)) } catch {}
  const persist = () => { if(typeof sessionStorage !== 'undefined') sessionStorage.setItem(storageKey, JSON.stringify({tasks,history})) }
  const parcels = (o: Order) => o.shipment ? [o.shipment, ...(o.shipment.parcels || [])] : []
  return (action: string, data: any, actor: string): any => {
    if (action === 'admin.logistics.tasks') return structuredClone(tasks.filter(t => (!data.status || t.status === data.status) && (!data.orderId || t.orderId === data.orderId)).slice(-200).reverse())
    if (action === 'admin.waybill.history') return structuredClone(history.filter(h => h.orderId === data.orderId).slice(-200).reverse())
    const order = orders().find(o => o.id === data.orderId)
    if (!order) throw new Error('订单不存在')
    if (action === 'admin.order.addParcel') {
      if (!canEditShipment(order)) throw new Error('仅未收货且无进行中退款的已发货订单可增加包裹')
      const parcel = validateShipment(data)
      const same = (p: {carrierCode?: string; trackingNo: string}) => p.carrierCode?.toUpperCase() === parcel.carrierCode && p.trackingNo.toUpperCase() === parcel.trackingNo.toUpperCase()
      if (parcels(order).some(same)) return {shipment:order.shipment}
      if (parcels(order).length >= 10) throw new Error('每笔订单最多10个包裹')
      if (orders().some(o => o.id !== order.id && parcels(o).some(same)) || [...waybills.values()].some(w => w.orderId !== order.id && same(w))) throw new Error('该运单号已被其他订单使用')
      order.shipment!.parcels = [...(order.shipment!.parcels || []), {...parcel,shippedAt:new Date().toISOString()}]
      order.shipment!.syncStatus = 'SUCCESS'
      return {shipment:order.shipment}
    }
    if (action === 'admin.waybill.cancel') {
      const w = waybills.get(order.id)
      if (!w || w.id !== data.waybillId) throw new Error('面单已变化，请刷新')
      if (!data.reason?.trim() || data.reason.trim().length > 100) throw new Error('请填写取消原因，最多100字')
      if (w.status === 'CANCELLED') return w
      if (order.shipment || !['UNFULFILLED','PREPARING'].includes(order.fulfillmentStatus)) throw new Error('已发货订单不能取消面单')
      if (w.status !== 'READY') throw new Error('面单结果尚未确认，不能发起取消')
      w.status = 'CANCELLED'; w.labelUrl = ''; w.updatedAt = new Date().toISOString()
      history.push({orderId:order.id,waybill:structuredClone(w),reason:data.reason.trim(),actor,at:w.updatedAt}); persist()
      return w
    }
    if (action === 'admin.logistics.task.save') {
      if (!['OPEN','PROCESSING','RESOLVED'].includes(data.status) || !['DELAY','EXCEPTION','RETURN','SYNC','NOTICE'].includes(data.kind) || typeof data.owner !== 'string' || data.owner.length > 60 || !data.note?.trim() || data.note.length > 1000) throw new Error('任务类型、状态、负责人或处理说明无效')
      let task = tasks.find(t => t.id === data.id && t.orderId === order.id)
      if (data.id) {
        if (!task) throw new Error('任务不存在')
        if (task.version !== data.version) throw new Error('任务已被修改，请刷新后重试')
      } else {
        const p = parcels(order).find(p => p.trackingNo.toUpperCase() === String(data.trackingNo).toUpperCase() && p.carrierCode === data.carrierCode)
        if (!p) throw new Error('运单不属于该订单')
        if (tasks.some(t => t.orderId === order.id && t.trackingNo === p.trackingNo && t.carrierCode === p.carrierCode && t.kind === data.kind && t.status !== 'RESOLVED')) throw new Error('该运单已有相同类型的未关闭任务，请继续跟进')
        task = {id:`LOG-${Date.now()}-${tasks.length}`,orderId:order.id,orderNo:order.orderNo,carrierCode:p.carrierCode || '',trackingNo:p.trackingNo,kind:data.kind,status:'OPEN',owner:'',note:'',version:0,updatedAt:'',history:[]}
        tasks.push(task)
      }
      task.owner = data.owner; task.note = data.note; task.status = data.status; task.version++; task.updatedAt = new Date().toISOString()
      task.history.push({actor,status:task.status,owner:task.owner,note:task.note,at:task.updatedAt}); persist()
      return structuredClone(task)
    }
    throw new Error('未知物流操作')
  }
}
