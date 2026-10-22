import { callApi } from '../../utils/api'
interface TraceResult {code:string;number:string;orchard:string;harvestedAt:string;packedAt:string;expiresAt:string;status:string;demo?:boolean;externalTrace?:{platform:string;url:string;code:string}}
Page({
 data:{code:'',result:null as TraceResult|null,busy:false,error:''},
 onLoad(options:Record<string,string>){if(options.code){this.setData({code:options.code});void this.query()}},
 input(e:WechatMiniprogram.Input){this.setData({code:e.detail.value,result:null,error:''})},
 copyExternal(){const url=this.data.result?.externalTrace?.url;if(url)wx.setClipboardData({data:url})},
 async scan(){try{const result=await wx.scanCode({onlyFromCamera:false,scanType:['qrCode','barCode']});this.setData({code:result.result.trim(),result:null});await this.query()}catch(e){if(!String((e as any).errMsg||'').includes('cancel'))this.setData({error:'未能读取溯源码，请手动输入'})}},
 async query(){if(this.data.busy)return;const code=this.data.code.trim();this.setData({result:null,error:'',busy:true});try{if(!code)throw new Error('请输入包装上的溯源码');const result=await callApi<TraceResult>('trace.query',{code});this.setData({result})}catch(e){this.setData({error:(e as Error).message||'查询失败，请重试'})}finally{this.setData({busy:false})}},
})
