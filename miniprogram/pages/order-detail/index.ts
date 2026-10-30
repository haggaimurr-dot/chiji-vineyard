import { logisticsAlerts, orderProgress, type ProgressNode } from '../../utils/order-progress'
import { canApplyRefund } from '../../utils/refund'
import { validateInvoice } from '../../utils/checkout'
import { buyAgain } from '../../utils/orders'
import { callApi, payOrder } from '../../utils/api'
import { money, orderPresentation } from '../../utils/format'
import type { Order, Tracking, ShippingSubscription, Review } from '../../utils/types'

Page({
  trace(event:WechatMiniprogram.BaseEvent){wx.navigateTo({url:'/pages/trace/index?code='+encodeURIComponent(event.currentTarget.dataset.code||'')})},
  async rebuy() {
    if (this.data.rebuyBusy || !this.data.order) return
    this.setData({ rebuyBusy: true })
    try { await buyAgain(this.data.order) }
    catch (e) { wx.showToast({ title: (e as Error).message, icon: 'none' }) }
    finally { this.setData({ rebuyBusy: false }) }
  },
  async subscribeShipping() {
    const config = this.data.subscription
    if (!config?.enabled || this.data.subscribeBusy || !this.data.order || !orderPresentation(this.data.order).canSubscribeShipping) return
    this.setData({ subscribeBusy: true, subscriptionError: '' })
    try {
      if (config.demo) {
        const result = await wx.showModal({ title: '演示发货订阅', content: '仅演示订阅状态，不向微信发送消息。', confirmText: '模拟同意' })
        if (!result.confirm) return
      } else {
        // Use the config fetched on page load: the request starts in the tap handler.
        const result = await wx.requestSubscribeMessage({ tmplIds: [config.templateId] })
        if (result[config.templateId] !== 'accept') {
          this.setData({ subscriptionError: '未开启微信提醒，仍可在「我的—发货通知」查看发货信息' }); return
        }
      }
      const subscription = await callApi<ShippingSubscription>('order.shippingSubscription.accept', { orderId: this.data.order.id })
      this.setData({ subscription })
      wx.showToast({ title: subscription.demo ? '已模拟订阅' : '已订阅发货通知', icon: 'success' })
    } catch (e) { this.setData({ subscriptionError: (e as Error).message || '订阅失败，请重试' }) }
    finally { this.setData({ subscribeBusy: false }) }
  },
  async demoShip() {
    if (this.data.loading || this.data.actionBusy || !this.data.demo || !this.data.order?.canDemoShip) return
    this.setData({ loading: true })
    try { await callApi('order.demoShip', { orderId: this.data.order.id }); await this.load() }
    catch (e) { wx.showToast({ title: (e as Error).message, icon: 'none' }) }
    finally { this.setData({ loading: false }) }
  },
  data: { packageBusy: '', packageError: '', packageTracking: {} as Record<string, Tracking>, parcelIndex: 0, parcelOptions: [] as string[], parcels: [] as Array<{carrier: string; carrierCode?: string; trackingNo: string}>, selectedParcel: null as any, progressNodes: [] as ProgressNode[], logisticsAlerts: [] as string[], demo: getApp().globalData.demoMode, rebuyBusy: false, subscribeBusy: false, subscription: null as ShippingSubscription | null, subscriptionError: '', invoiceOpen: false, invoiceKind: 'personal', invoiceTitle: '', invoiceTaxId: '', invoiceEmail: '', invoiceBusy: false, invoiceError: '', invoiceHistory: [] as Array<{kind: string; title: string; taxId: string; email: string}>, invoiceHistoryIndex: -1, order: null as any, loading: false, loadError: '', requestVersion: 0, trackingVersion: 0, actionBusy: false, tracking:null as Tracking|null,trackingLoading:false,trackingError:'', myReviews: [] as Review[], reviewsLoadError: '', reviewId: '', reviewImages: [] as Array<{path: string; dataUrl: string}>, reviewImageBusy: false, reviewProductId: '', reviewName: '', reviewRating: 5, reviewContent: '', reviewBusy: false, reviewError: '', reviewStars: [1,2,3,4,5] },
  onLoad(options: Record<string, string>) { (this as any).orderId = options.id; (this as any).enterTab = options.tab || '' },
  onShow() { return this.load().then(() => { if ((this as any).enterTab === 'review') { (this as any).enterTab = ''; wx.createSelectorQuery().in(this).select('#review-section').boundingClientRect(r => { if (r) wx.pageScrollTo({ scrollTop: (r as any).top, duration: 300 }) }).exec() } }) },
  onUnload() { this.setData({ requestVersion: this.data.requestVersion + 1, trackingVersion: this.data.trackingVersion + 1 }) },
  async load() {
    const version = this.data.requestVersion + 1
    this.setData({ requestVersion: version, loading: true, loadError: '' })
    try {
      const order = await callApi<Order>('order.get', { id: (this as any).orderId })
      if (version !== this.data.requestVersion) return
      if (!order) throw new Error('订单不存在或已不可查看')
      const previousShipment = this.data.order?.shipment
      const shipmentChanged = JSON.stringify(order.shipment?.parcels) !== JSON.stringify(previousShipment?.parcels) || order.id !== this.data.order?.id || order.shipment?.carrier !== previousShipment?.carrier || order.shipment?.trackingNo !== previousShipment?.trackingNo
      const parcels = order.shipment ? [order.shipment, ...(order.shipment.parcels || [])] : []
      const parcelIndex = shipmentChanged ? 0 : Math.min(this.data.parcelIndex, Math.max(0, parcels.length - 1))
      this.setData({ parcels, parcelIndex, parcelOptions: parcels.map((p, i) => `包裹${i+1} · ${p.carrier} · ${p.trackingNo}`), selectedParcel: parcels[parcelIndex] || null, progressNodes: orderProgress(order, shipmentChanged ? null : this.data.tracking), logisticsAlerts: logisticsAlerts(order, shipmentChanged ? null : this.data.tracking), order: { ...order, invoiceAmountText: money(order.invoice?.amount || 0), invoiceEmailText: ({ SENT: '发票邮件已提交至邮件服务，请查收邮箱（含垃圾邮件）', FAILED: '邮件发送失败，商家将处理；可先查看电子发票', UNCERTAIN: '邮件发送结果核实中，可先查看电子发票', SENDING: '发票邮件正在发送', READY: '发票邮件待发送' } as Record<string,string>)[order.invoice?.emailStatus || ''] || '由商家处理并发送至申请邮箱', ...orderPresentation(order), canRefund: canApplyRefund(order), refundedText: money(order.refundedAmount || 0), totalText: money(order.quote.total), subtotalText: money(order.quote.subtotal), freightText: money(order.quote.freight), discountText: money(order.quote.discount || 0), items: order.items.map((x) => ({ ...x, lineText: money((x.sku?.price || 0) * x.quantity) })) },
        ...(shipmentChanged ? { tracking: null, trackingError: '', trackingLoading: false, trackingVersion: this.data.trackingVersion + 1 } : {}) })
      await this.loadMyReviews()
      if (order.shipment && !this.data.tracking) this.track()
      try {
        const subscription = await callApi<ShippingSubscription>('order.shippingSubscription.get', { orderId: order.id })
        if (version === this.data.requestVersion) this.setData({ subscription })
      } catch (_) { if (version === this.data.requestVersion) this.setData({ subscription: null }) }
      if (order.shipment) { try { await callApi('shippingNotice.read', { orderId: order.id }) } catch (_) {} }
    } catch (e) { if (version === this.data.requestVersion) this.setData({ loadError: (e as Error).message || '订单加载失败，请重试' }) }
    finally { if (version === this.data.requestVersion) this.setData({ loading: false }) }
  },
  async track() {
    if (!this.data.order?.shipment || this.data.trackingLoading) return
    const version = this.data.trackingVersion + 1, orderId = this.data.order.id
    this.setData({ trackingVersion: version, trackingLoading: true, trackingError: '' })
    try { const tracking = await callApi<Tracking>('order.tracking', { orderId, trackingNo:this.data.selectedParcel?.trackingNo, carrierCode:this.data.selectedParcel?.carrierCode }); if (version === this.data.trackingVersion) this.setData({ tracking, progressNodes: orderProgress(this.data.order, tracking), logisticsAlerts: logisticsAlerts(this.data.order, tracking) }) }
    catch (e) { if (version === this.data.trackingVersion) this.setData({ trackingError: (e as Error).message }) }
    finally { if (version === this.data.trackingVersion) this.setData({ trackingLoading: false }) }
  },
  async chooseParcel(event: WechatMiniprogram.PickerChange) {
    const index = Number(event.detail.value)
    if (!this.data.parcels[index]) return
    this.setData({parcelIndex:index, selectedParcel:this.data.parcels[index], tracking:null, trackingError:'', trackingLoading:false, trackingVersion:this.data.trackingVersion+1})
    await this.track()
  },
  async trackPackage(event: WechatMiniprogram.BaseEvent) {
    const packageId=event.currentTarget.dataset.id, orderId=this.data.order?.id, version=this.data.requestVersion
    if(!orderId || this.data.packageBusy) return
    this.setData({packageBusy:packageId,packageError:''})
    try {const result=await callApi<Tracking>('order.tracking',{orderId,packageId});if(version===this.data.requestVersion) this.setData({['packageTracking.'+packageId]:result})}
    catch(e) {if(version===this.data.requestVersion) this.setData({packageError:(e as Error).message})}
    finally {if(version===this.data.requestVersion) this.setData({packageBusy:''})}
  },
  copyPackage(event: WechatMiniprogram.BaseEvent) {const p=this.data.order?.packages?.find((p:any)=>p.id===event.currentTarget.dataset.id); if(p?.shipment) wx.setClipboardData({data:p.shipment.trackingNo})},
  copyTracking(){if(this.data.selectedParcel) wx.setClipboardData({data:this.data.selectedParcel.trackingNo})},
  async pay() {
    if (this.data.loading || this.data.actionBusy || !this.data.order?.canPay) return
    this.setData({ actionBusy: true })
    try { await payOrder(this.data.order.id); await this.load() }
    catch (e) { wx.showToast({ title: (e as Error).message, icon: 'none' }) }
    finally { this.setData({ actionBusy: false }) }
  },
  async cancel() {
    if (this.data.loading || this.data.actionBusy || !this.data.order?.canPay) return
    this.setData({ actionBusy: true })
    try { const res = await wx.showModal({ title: '取消订单', content: '库存会自动释放，确定取消吗？' }); if (res.confirm) { await callApi('order.cancel', { id: this.data.order.id }); await this.load() } }
    catch (e) { wx.showToast({ title: (e as Error).message, icon: 'none' }) }
    finally { this.setData({ actionBusy: false }) }
  },
  async confirm() {
    if (this.data.loading || this.data.actionBusy || !this.data.order?.canConfirm) return
    this.setData({ actionBusy: true })
    try { const res = await wx.showModal({ title: '确认收货', content: this.data.order.packages?.length ? '请确认全部包裹均已收到且葡萄完好。' : '请确认已经收到完好的葡萄。' }); if (res.confirm) { await callApi('order.confirm', { id: this.data.order.id }); await this.load() } }
    catch (e) { wx.showToast({ title: (e as Error).message, icon: 'none' }) }
    finally { this.setData({ actionBusy: false }) }
  },
  openInvoice() {
    const i = this.data.order?.invoice
    const opening = !this.data.invoiceOpen
    this.setData({ invoiceOpen: opening, invoiceError: '', ...(i?.status === 'REJECTED' ? { invoiceKind: i.kind, invoiceTitle: i.title, invoiceTaxId: i.taxId, invoiceEmail: i.email } : {}) })
    if (opening) this.loadInvoiceHistory()
  },
  async loadInvoiceHistory() {
    try {
      const res = await callApi<{ last: { kind: string; title: string; taxId: string; email: string } | null; items: Array<{ kind: string; title: string; taxId: string; email: string }> }>('invoice.titles', {})
      const items = res.items || []
      if (!this.data.order?.invoice && res.last) {
        this.setData({ invoiceKind: res.last.kind, invoiceTitle: res.last.title, invoiceTaxId: res.last.taxId, invoiceEmail: res.last.email, invoiceHistoryIndex: 0 })
      }
      this.setData({ invoiceHistory: items })
    } catch { /* 历史抬头加载失败不阻塞开票 */ }
  },
  pickInvoiceTitle(event: WechatMiniprogram.PickerChange) {
    const idx = Number(event.detail.value)
    const h = this.data.invoiceHistory[idx]
    if (h) this.setData({ invoiceHistoryIndex: idx, invoiceKind: h.kind, invoiceTitle: h.title, invoiceTaxId: h.taxId, invoiceEmail: h.email })
  },
  async viewInvoice() {
    if (this.data.invoiceBusy || !this.data.order?.invoice?.fileName) return
    this.setData({ invoiceBusy: true, invoiceError: '' })
    // Keep one bounded download so the document menu can still save/share it.
    // openDocument resolves when the viewer opens, not when the user closes it.
    const path = `${wx.env.USER_DATA_PATH}/invoice-download.pdf`
    try {
      const result = await callApi<{fileData: string}>('invoice.file', { orderId: this.data.order.id })
      await new Promise<void>((resolve,reject) => wx.getFileSystemManager().writeFile({ filePath: path, data: result.fileData, encoding: 'base64', success: () => resolve(), fail: reject }))
      await wx.openDocument({ filePath: path, fileType: 'pdf', showMenu: true })
    } catch (e) { this.setData({ invoiceError: (e as Error).message || '发票下载失败，请稍后重试' }) }
    finally { this.setData({ invoiceBusy: false }) }
  },
  invoiceKind(event: WechatMiniprogram.PickerChange) { this.setData({ invoiceKind: Number(event.detail.value) === 1 ? 'company' : 'personal' }) },
  invoiceField(event: WechatMiniprogram.Input) { const key = event.currentTarget.dataset.field; if (['invoiceTitle','invoiceTaxId','invoiceEmail'].includes(key)) this.setData({ [key]: event.detail.value }) },
  async submitInvoice() {
    if (this.data.invoiceBusy) return
    this.setData({ invoiceBusy: true, invoiceError: '' })
    try { const invoice = validateInvoice({ kind: this.data.invoiceKind, title: this.data.invoiceTitle, taxId: this.data.invoiceTaxId, email: this.data.invoiceEmail, status: '' }); await callApi('invoice.apply', { orderId: this.data.order.id, invoice }); this.setData({ invoiceOpen: false }); await this.load(); wx.showToast({ title: '申请已提交', icon: 'success' }) }
    catch(e) { this.setData({ invoiceError: (e as Error).message }) }
    finally { this.setData({ invoiceBusy: false }) }
  },
  afterSales() { wx.navigateTo({url: `/pages/after-sales/index?id=${this.data.order.id}`}) },
  refund() { wx.navigateTo({ url: `/pages/refund/index?id=${this.data.order.id}` }) },
  previewReviewImage(e: WechatMiniprogram.BaseEvent) { wx.previewImage({ current: e.currentTarget.dataset.url, urls: e.currentTarget.dataset.urls }) },
  async loadMyReviews() {
    const orderId = this.data.order?.id, version = this.data.requestVersion
    if (!orderId) return
    try { const items = await callApi<Review[]>('review.order', { orderId }); if (this.data.order?.id === orderId && version === this.data.requestVersion) this.setData({ myReviews: items, reviewsLoadError: '', 'order.items': this.data.order.items.map((item: any) => { const review = items.find(r => r.productId === item.productId); return { ...item, reviewLabel: review?.appendAt ? '已完成追评' : review ? '追加评价' : '评价', reviewDone: !!review?.appendAt } }) }) }
    catch (e) { if (version === this.data.requestVersion) this.setData({ reviewsLoadError: (e as Error).message || '评价加载失败，请重试' }) }
  },
  async chooseReviewImages() {
    if (this.data.reviewBusy || this.data.reviewImageBusy || this.data.reviewImages.length >= 6) return
    this.setData({ reviewImageBusy: true, reviewError: '' })
    try {
      const selected = await wx.chooseMedia({ count: 6-this.data.reviewImages.length, mediaType: ['image'], sourceType: ['album','camera'], sizeType: ['compressed'] })
      for (const file of selected.tempFiles) {
        let path = file.tempFilePath
        if (file.size > 512*1024) path = (await wx.compressImage({ src: path, quality: 35, compressedWidth: 1280, compressedHeight: 1280 })).tempFilePath
        const base64 = await new Promise<string>((resolve,reject) => wx.getFileSystemManager().readFile({ filePath: path, encoding: 'base64', success: r => resolve(r.data as string), fail: reject }))
        if (base64.length > Math.ceil(512*1024/3)*4) throw new Error('图片压缩后仍超过512KB，请选择较小图片')
        const mime = base64.startsWith('iVBOR') ? 'image/png' : base64.startsWith('UklGR') ? 'image/webp' : 'image/jpeg'
        this.setData({ reviewImages: [...this.data.reviewImages, { path, dataUrl: `data:${mime};base64,${base64}` }] })
      }
    } catch (e) { const message = (e as any).message || (e as any).errMsg || '选图失败'; if (!message.includes('cancel')) this.setData({ reviewError: message }) }
    finally { this.setData({ reviewImageBusy: false }) }
  },
  removeReviewImage(e: WechatMiniprogram.BaseEvent) {
    if (!this.data.reviewBusy && !this.data.reviewImageBusy) this.setData({ reviewImages: this.data.reviewImages.filter((_, i) => i !== Number(e.currentTarget.dataset.index)) })
  },
  startReview(event: WechatMiniprogram.BaseEvent) {
    if (this.data.reviewBusy || this.data.reviewImageBusy || this.data.reviewsLoadError) return
    const item = this.data.order.items.find((i: any) => i.productId === event.currentTarget.dataset.id)
    const existing = this.data.myReviews.find(r => r.productId === item?.productId)
    if (existing?.appendAt) { wx.showToast({ title: '已完成追评', icon: 'none' }); return }
    if (item) this.setData({ reviewId: existing?.id || '', reviewImages: [], reviewProductId: item.productId, reviewName: item.product?.name || item.name, reviewRating: 5, reviewContent: '', reviewError: '' })
  },
  setReviewRating(event: WechatMiniprogram.BaseEvent) { if (!this.data.reviewBusy) this.setData({ reviewRating: Number(event.currentTarget.dataset.rating) }) },
  setReviewContent(event: WechatMiniprogram.Input) { this.setData({ reviewContent: event.detail.value }) },
  cancelReview() { if (!this.data.reviewBusy && !this.data.reviewImageBusy) this.setData({ reviewProductId: '', reviewImages: [] }) },
  async submitReview() {
    if (this.data.reviewBusy || this.data.reviewImageBusy) return
    if (!this.data.reviewContent.trim()) { this.setData({ reviewError: '请填写品尝感受' }); return }
    this.setData({ reviewBusy: true, reviewError: '' })
    try {
      await callApi(this.data.reviewId ? 'review.append' : 'review.create', { id: this.data.reviewId, images: this.data.reviewImages.map(x => x.dataUrl), orderId: this.data.order.id, productId: this.data.reviewProductId, rating: this.data.reviewRating, content: this.data.reviewContent.trim() })
      this.setData({ reviewProductId: '' })
      await this.loadMyReviews()
      wx.showToast({ title: '已提交，待审核', icon: 'success' })
    } catch (e) { this.setData({ reviewError: (e as Error).message || '提交失败，请重试' }) }
    finally { this.setData({ reviewBusy: false }) }
  },

})


