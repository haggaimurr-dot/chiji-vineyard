import { callApi } from '../../utils/api'
import { returnStatuses } from '../../utils/after-sales'
import { ticketStatuses, ticketCategories, ticketReminder, validateAttachments, type TicketAttachment, type AfterSalesTicket } from '../../utils/after-sales'
let reminderTimer: ReturnType<typeof setInterval> | undefined
Page({
 returnDraft: false,
 data: {returnStatuses, rateValue: 5, rateComment: '', satisfactionStars: '', carrier:'', carrierCode:'SF', trackingNo:'', returnTracking:null as {message:string;events:Array<{time:string;context:string}>} | null, attachments:[] as TicketAttachment[], orderId:'', tickets:[] as Array<AfterSalesTicket & {statusText:string; categoryText:string; reminderText:string; dueAt:string}>, selected:null as AfterSalesTicket | null, loading:false, busy:false, error:'', content:'', reply:'', category:'OTHER', categoryIndex:3, categoryLabels:['品质问题','物流问题','发票问题','其他问题'], creating:false, offset:0, hasMore:false, demo:getApp().globalData.demoMode},
 onLoad(options: Record<string,string | undefined>) { this.setData({orderId:options.id || ''}); this.load() },
 onShow() { this.load(true); if(reminderTimer) clearInterval(reminderTimer); reminderTimer=setInterval(()=>{ if(!this.data.busy && !this.data.loading && !this.data.creating && !this.data.reply && !this.data.attachments.length && !this.returnDraft) this.load(true) },30000) },
 onHide() { if(reminderTimer) clearInterval(reminderTimer) },
 onUnload() { if(reminderTimer) clearInterval(reminderTimer); for(const ext of ['png','jpg']) wx.getFileSystemManager().unlink({filePath:`${wx.env.USER_DATA_PATH}/ticket-preview.${ext}`,fail:()=>{}}) },
 async load(keepSelection: boolean = false) {
  if (!this.data.orderId) { this.setData({error:'请从订单详情进入售后工单'}); return }
  if (this.data.loading) return
  this.setData({loading:true,error:''})
  try { const tickets=await callApi<AfterSalesTicket[]>('ticket.list',{orderId:this.data.orderId,offset:this.data.offset}); this.setData({tickets:tickets.map(t=>({...t,statusText:ticketStatuses[t.status],categoryText:ticketCategories[t.category],reminderText:ticketReminder(t,'USER').text,dueAt:new Date(ticketReminder(t,'USER').dueAt).toLocaleString()})),hasMore:tickets.length===50,selected:keepSelection === true ? tickets.find(t=>t.id===this.data.selected?.id) || null : null}) }
  catch(e) { this.setData({error:(e as Error).message}) } finally { this.setData({loading:false}) }
 },
 startCreate() { this.setData({creating:true,content:'',attachments:[],error:''}) },
 setContent(event: WechatMiniprogram.Input) { this.setData({content:event.detail.value}) },
 setReply(event: WechatMiniprogram.Input) { this.setData({reply:event.detail.value}) },
 chooseCategory(event: WechatMiniprogram.PickerChange) { const index=Number(event.detail.value); this.setData({categoryIndex:index,category:['QUALITY','DELIVERY','INVOICE','OTHER'][index]}) },
 async select(event: WechatMiniprogram.TouchEvent) { const ticket=this.data.tickets.find(t=>t.id===event.currentTarget.dataset.id); if(ticket) {
  this.returnDraft=false
  this.setData({selected:ticket,returnTracking:null,rateValue:5,rateComment:'',satisfactionStars:ticket.satisfaction ? '★'.repeat(ticket.satisfaction.rating) + '☆'.repeat(5-ticket.satisfaction.rating) : '',carrier:ticket.return?.shipment?.carrier || '',carrierCode:ticket.return?.shipment?.carrierCode || 'SF',trackingNo:ticket.return?.shipment?.trackingNo || '',reply:'',attachments:[],error:'',creating:false,busy:true})
  try { const result=await callApi<AfterSalesTicket>('ticket.read',{id:ticket.id,version:ticket.version,readThrough:ticket.messages.length}); if(this.data.selected?.id===result.id) this.setData({selected:result,satisfactionStars:result.satisfaction ? '★'.repeat(result.satisfaction.rating) + '☆'.repeat(5-result.satisfaction.rating) : ''}); this.setData({tickets:this.data.tickets.map(t=>t.id===result.id ? {...t,...result,reminderText:ticketReminder(result,'USER').text} : t)}) } catch(e) {this.setData({error:(e as Error).message})} finally {this.setData({busy:false})}
 } },
 setRate(e: WechatMiniprogram.TouchEvent) { this.setData({ rateValue: Number(e.currentTarget.dataset.v) }) },
 setRateComment(e: WechatMiniprogram.Input) { this.setData({ rateComment: e.detail.value }) },
 async submitRate() {
  if (this.data.busy || !this.data.selected) return
  this.setData({ busy: true, error: '' })
  try {
   await callApi('ticket.rate', { ticketId: this.data.selected.id, rating: this.data.rateValue, comment: this.data.rateComment })
   wx.showToast({ title: '评价成功，谢谢！', icon: 'success' })
   await this.load(true)
  } catch (e) { this.setData({ error: (e as Error).message }) }
  finally { this.setData({ busy: false }) }
 },
 async create() {
  if(this.data.busy) return
  const content=this.data.content.trim(); if(!content) {this.setData({error:'请填写问题描述'});return}
  this.setData({busy:true,error:''})
  try { await callApi('ticket.create',{orderId:this.data.orderId,category:this.data.category,content,attachments:this.data.attachments}); this.setData({creating:false,attachments:[],offset:0}); await this.load(); wx.showToast({title:'工单已提交',icon:'success'}) }
  catch(e) {this.setData({error:(e as Error).message})} finally {this.setData({busy:false})}
 },
 setReturnField(event:WechatMiniprogram.Input) {
  const field=event.currentTarget.dataset.field
  if(['carrier','carrierCode','trackingNo'].includes(field)) { this.returnDraft=true; this.setData({[field]:event.detail.value}) }
 },
 copyReturnAddress() {
  const a=this.data.selected?.return?.address
  if(a) wx.setClipboardData({data:a.name+' '+a.phone+' '+a.province+a.city+a.district+' '+a.detail})
 },
 copyReturnTracking() {const n=this.data.selected?.return?.shipment?.trackingNo; if(n) wx.setClipboardData({data:n})},
 async shipReturn() {
  const t=this.data.selected; if(!t || this.data.busy) return
  this.setData({busy:true,error:''})
  try {
   const result=await callApi<AfterSalesTicket>('ticket.reply',{id:t.id,version:t.version,return:{action:'SHIP',shipment:{carrier:this.data.carrier,carrierCode:this.data.carrierCode,trackingNo:this.data.trackingNo}}})
   this.returnDraft=false; this.setData({selected:result,returnTracking:null}); await this.load(true); wx.showToast({title:'寄件已登记',icon:'success'})
  }catch(e){this.setData({error:(e as Error).message})}finally{this.setData({busy:false})}
 },
 async traceReturn() {
  if(!this.data.selected || this.data.busy) return
  this.setData({busy:true,error:''})
  try{const result=await callApi<{message:string;events:Array<{time:string;context:string}>}>('ticket.returnTracking',{id:this.data.selected.id});this.setData({returnTracking:result})}
  catch(e){this.setData({error:(e as Error).message})}finally{this.setData({busy:false})}
 },
 async reply() {
  const ticket=this.data.selected,content=this.data.reply.trim(); if(this.data.busy || !ticket) return
  if(!content) {this.setData({error:'请填写补充说明'});return}
  this.setData({busy:true,error:''})
  try { await callApi('ticket.reply',{id:ticket.id,version:ticket.version,content,attachments:this.data.attachments}); this.setData({attachments:[],reply:''}); await this.load(); wx.showToast({title:'说明已提交',icon:'success'}) }
  catch(e) {this.setData({error:(e as Error).message})} finally {this.setData({busy:false})}
 },
 async attach() {
  if(this.data.busy) return
  this.setData({busy:true,error:''})
  try {
   if(this.data.attachments.length>=3) throw new Error('每次最多3张附件')
   const result=await wx.chooseMedia({count:3-this.data.attachments.length,mediaType:['image'],sizeType:['compressed'],sourceType:['album','camera']})
   const files:TicketAttachment[]=[]
   for(const file of result.tempFiles) {
    if(file.size>512*1024) throw new Error('请压缩图片，每次附件合计不得超过512KB')
    const encoded=await new Promise<string>((resolve,reject)=>wx.getFileSystemManager().readFile({filePath:file.tempFilePath,encoding:'base64',success:r=>resolve(String(r.data)),fail:()=>reject(new Error('附件读取失败'))}))
    const mime=encoded.startsWith('iVBOR') ? 'png' : 'jpeg'
    files.push({name:`附件${this.data.attachments.length+files.length+1}.${mime==='png'?'png':'jpg'}`,dataUrl:`data:image/${mime};base64,${encoded}`})
   }
   this.setData({attachments:validateAttachments([...this.data.attachments,...files])})
  } catch(e) { if(!String((e as {errMsg?:string}).errMsg || '').includes('cancel')) this.setData({error:(e as Error).message || '附件选择失败'}) } finally {this.setData({busy:false})}
 },
 removeAttachment(event:WechatMiniprogram.TouchEvent) { if(this.data.busy) return; this.setData({attachments:this.data.attachments.filter((_,i)=>i!==Number(event.currentTarget.dataset.index))}) },
 preview(event:WechatMiniprogram.TouchEvent) {
  const file=this.data.selected?.messages[Number(event.currentTarget.dataset.message)]?.attachments?.[Number(event.currentTarget.dataset.index)]
  if(!file) return
  const [,mime,encoded]=file.dataUrl.match(/^data:image\/(jpeg|png);base64,(.+)$/) || []
  if(!encoded) return
  const target=`${wx.env.USER_DATA_PATH}/ticket-preview.${mime==='png'?'png':'jpg'}`
  wx.getFileSystemManager().writeFile({filePath:target,data:encoded,encoding:'base64',success:()=>wx.previewImage({current:target,urls:[target]}),fail:()=>this.setData({error:'附件预览失败'})})
 },
 previous() { if(this.data.loading || this.data.busy || !this.data.offset) return;this.setData({offset:this.data.offset-50});this.load() },
 next() { if(this.data.loading || this.data.busy || !this.data.hasMore) return;this.setData({offset:this.data.offset+50});this.load() },
})
