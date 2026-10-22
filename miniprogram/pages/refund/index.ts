import { callApi } from '../../utils/api'
import { money, refundStatusText } from '../../utils/format'
import { canApplyRefund, refundCents } from '../../utils/refund'
import type { Order, Refund } from '../../utils/types'

Page({
  data: { refunds: [] as any[], demo: false, reasons: ['不想要了', '地址填写错误', '重复下单', '发货时间不合适', '坏果/品质问题', '破损/配送问题', '其他原因'], reasonIndex: 0, note: '', amount: '', remaining: 0, remainingText: '', shipped: false, eligible: false, loading: false, error: '', submitting: false, imageBusy: false, images: [] as Array<{id: string; path: string}> },
  onLoad(options: Record<string,string>) { (this as any).orderId=options.id; this.setData({demo:getApp<IAppOption>().globalData.demoMode}) },
  onShow() { this.load() },
  refresh() { this.load(true) },
  async load(refresh = false) {
    if(this.data.loading||this.data.imageBusy)return
    this.setData({loading:true,error:'',eligible:false})
    try { const refunds=await callApi<Refund[]>('refund.list',{orderId:(this as any).orderId,refresh}); const order=await callApi<Order>('order.get',{id:(this as any).orderId})
      if(!order)throw new Error('订单不存在')
      const remaining=order.quote.total-(order.refundedAmount||0)
      this.setData({refunds:refunds.map(r=>({...r,statusText:refundStatusText(r.status),amountText:money(r.amount),createdText:new Date(r.createdAt).toLocaleString(),updatedText:new Date(r.updatedAt||r.createdAt).toLocaleString(),steps:[{title:'提交申请',done:true},{title:r.status==='REJECTED'?'审核未通过':'商家审核',done:r.status!=='APPLIED'},{title:r.status==='FAILED'?'退款异常待处理':'微信原路退款',done:r.status==='SUCCEEDED'}]})),remaining,remainingText:money(remaining),amount:(this as any).loaded && this.data.remaining===remaining ? this.data.amount : money(remaining),eligible:canApplyRefund(order),shipped:['SHIPPED','RECEIVED'].includes(order.fulfillmentStatus)})
      ;(this as any).loaded=true
    } catch(e) {this.setData({error:(e as Error).message})} finally {this.setData({loading:false})}
  },
  reason(e:WechatMiniprogram.PickerChange){this.setData({reasonIndex:Number(e.detail.value)})},
  note(e:WechatMiniprogram.Input){this.setData({note:e.detail.value})},
  amount(e:WechatMiniprogram.Input){this.setData({amount:e.detail.value})},
  async chooseImages() {
    if(this.data.imageBusy||this.data.submitting||this.data.images.length>=6)return
    this.setData({imageBusy:true,error:''})
    try {
      const selected=await wx.chooseMedia({count:6-this.data.images.length,mediaType:['image'],sourceType:['album','camera'],sizeType:['compressed']})
      for(const file of selected.tempFiles) {
        let path=file.tempFilePath
        if(file.size>512*1024){const compressed=await wx.compressImage({src:path,quality:35,compressedWidth:1280,compressedHeight:1280});path=compressed.tempFilePath}
        const base64=await new Promise<string>((resolve,reject)=>wx.getFileSystemManager().readFile({filePath:path,encoding:'base64',success:r=>resolve(r.data as string),fail:reject}))
        if(base64.length>Math.ceil(512*1024/3)*4)throw new Error('图片压缩后仍超过512KB，请选择较小图片')
        const mime=base64.startsWith('iVBOR')?'image/png':base64.startsWith('UklGR')?'image/webp':'image/jpeg'
        const image=await callApi<{id:string}>('refund.evidence.upload',{orderId:(this as any).orderId,dataUrl:`data:${mime};base64,${base64}`})
        this.setData({images:[...this.data.images,{id:image.id,path}]})
      }
    }catch(e){const message=(e as any).message||(e as any).errMsg||'上传失败，请重试';if(!message.includes('cancel'))this.setData({error:message})}
    finally{this.setData({imageBusy:false})}
  },
  async removeImage(e:WechatMiniprogram.BaseEvent){
    if(this.data.imageBusy||this.data.submitting)return
    this.setData({imageBusy:true,error:''})
    try{await callApi('refund.evidence.delete',{id:e.currentTarget.dataset.id});this.setData({images:this.data.images.filter(x=>x.id!==e.currentTarget.dataset.id)})}
    catch(e){this.setData({error:(e as Error).message})}finally{this.setData({imageBusy:false})}
  },
  previewImage(e:WechatMiniprogram.BaseEvent){wx.previewImage({current:e.currentTarget.dataset.path,urls:this.data.images.map(x=>x.path)})},
  async previewEvidence(e:WechatMiniprogram.BaseEvent) {
    if(this.data.imageBusy)return
    this.setData({imageBusy:true,error:''})
    try {
      const image=await callApi<{dataUrl:string}>('refund.evidence.get',{id:e.currentTarget.dataset.id})
      const match=/^data:image\/(jpeg|png|webp);base64,(.+)$/.exec(image.dataUrl)
      if(!match)throw new Error('图片数据无效')
      const path=`${wx.env.USER_DATA_PATH}/chiji-evidence-${Date.now()}.${match[1]}`
      await new Promise<void>((resolve,reject)=>wx.getFileSystemManager().writeFile({filePath:path,data:match[2],encoding:'base64',success:()=>resolve(),fail:reject}))
      ;(this as any).previewFiles=[...((this as any).previewFiles||[]),path]
      await wx.previewImage({current:path,urls:[path]})
    }catch(e){this.setData({error:(e as Error).message||'图片预览失败'})}finally{this.setData({imageBusy:false})}
  },
  onUnload(){for(const filePath of ((this as any).previewFiles||[]))wx.getFileSystemManager().unlink({filePath,fail:()=>{}})},
  async submit() {
    if(this.data.submitting||this.data.loading||this.data.imageBusy||!this.data.eligible)return
    this.setData({submitting:true,error:''})
    try {
      const amount=refundCents(this.data.amount,this.data.remaining)
      if(this.data.shipped&&!this.data.images.length)throw new Error('已发货售后请上传举证图片')
      await callApi('refund.apply',{orderId:(this as any).orderId,amount,reason:this.data.reasons[this.data.reasonIndex],note:this.data.note,images:this.data.images.map(x=>x.id)})
      this.setData({eligible:false,images:[],note:''});wx.showToast({title:'售后已提交'});await this.load()
    }catch(e){this.setData({error:(e as Error).message})}finally{this.setData({submitting:false})}
  },
})
