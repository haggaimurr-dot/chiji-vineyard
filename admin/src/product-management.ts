import type { Product } from './types'
export const productCSVHeader = '商品分组,商品名称,副标题,分类,销售模式,预计发货日期,封面,产地,规格名称,售价元,库存,限购,重量说明'
export const productCSVTemplate = '\ufeff'+productCSVHeader+'\r\n示例A,阳光玫瑰,当季葡萄,family,spot,,https://example.com/grape.jpg,合作果园,家庭装,89.90,20,5,净重约2kg\r\n'
export interface ExternalTrace { platform: string; url: string; code: string }
export interface ManagedBatch { externalTrace?: ExternalTrace; code?: string; id: string; productId: string; number: string; orchard: string; harvestedAt: string; packedAt: string; expiresAt: string; note: string; status: 'ACTIVE'|'ARCHIVED'; version: number }
export interface ImportPreview { products: Product[]; errors: string[]; rows: number }
export function searchAdminProducts(products: Product[], data: any) {
 const words=String(data.keyword||'').trim().toLowerCase().split(/\s+/).filter(Boolean)
 if (String(data.keyword||'').trim().length>60 || !['','all','family','gift'].includes(data.category||'') || !['','DRAFT','PUBLISHED'].includes(data.status||'') || !['','spot','presale'].includes(data.saleMode||'')) throw new Error('筛选参数无效')
 const items=products.filter(p=>(!data.category||data.category==='all'||p.category===data.category)&&(!data.status||p.status===data.status)&&(!data.saleMode||p.saleMode===data.saleMode)&&words.every(w=>[p.name,p.subtitle,p.origin,...p.badges,...p.skus.filter(s=>s.active!==false).flatMap(s=>[s.name,s.weightLabel])].join(' ').toLowerCase().includes(w))).sort((a,b)=>a.sortOrder-b.sortOrder||a.id.localeCompare(b.id))
 const limit=Number.isInteger(data.limit)&&data.limit>0?Math.min(data.limit,50):20,offset=Math.max(0,data.offset||0)
 return {items:items.slice(offset,offset+limit),total:items.length,limit,offset}
}
// RFC 4180 quoting, including escaped quotes, commas and multiline fields.
export function parseProductCSV(content: string): ImportPreview {
 const out:ImportPreview={products:[],errors:[],rows:0},rows:string[][]=[]
 if (new TextEncoder().encode(content).length>1024*1024) return {...out,errors:['文件不能超过 1MB']}
 const text=content.replace(/^\ufeff/,''),groups=new Map<string,Product>();let row:string[]=[],field='',quoted=false,closed=false
 try { for(let i=0;i<text.length;i++){const c=text[i];if(quoted){if(c==='"'){if(text[i+1]==='"'){field+='"';i++}else{quoted=false;closed=true}}else field+=c;continue}if(c==='"'){if(field||closed)throw new Error('引号格式无效');quoted=true}else if(c===','){row.push(field);field='';closed=false}else if(c==='\n'||c==='\r'){if(c==='\r'&&text[i+1]==='\n')i++;row.push(field);if(row.some(x=>x!==''))rows.push(row);row=[];field='';closed=false}else{if(closed)throw new Error('引号后存在多余字符');field+=c}}if(quoted)throw new Error('引号未闭合');if(field||row.length){row.push(field);rows.push(row)}}catch(e){return {...out,errors:[(e as Error).message]}}
 if(rows.shift()?.join(',')!==productCSVHeader)return {...out,errors:['请使用下载的 CSV 模板，保留表头']}
 const validDate=(v:string)=>/^\d{4}-\d{2}-\d{2}$/.test(v)&&Number.isFinite(Date.parse(v))&&new Date(v).toISOString().slice(0,10)===v
 for(const [i,values]of rows.entries()){out.rows++;const fail=(why:string)=>out.errors.push(`第 ${i+2} 行：${why}`);if(i>=500){fail('一次最多导入 500 个规格');break}if(values.length!==13){fail('CSV 列数无效');continue}const r=values.map(x=>x.trim()),price=Number(r[9]),stock=Number(r[10]),limit=Number(r[11]),cents=Math.round(price*100)
 if(!r[0]||r[0].length>80||!r[1]||r[1].length>80||r[2].length>200||!r[6]||r[6].length>500||r[7].length>200||!r[8]||r[8].length>60||r[12].length>80||!['family','gift'].includes(r[3])||!['spot','presale'].includes(r[4])||!/^\d+(\.\d{1,2})?$/.test(r[9])||!Number.isInteger(cents)||cents<1||cents>2147483647||!/^\d+$/.test(r[10])||!Number.isInteger(stock)||stock>2147483647||!/^\d+$/.test(r[11])||!Number.isInteger(limit)||limit<1||limit>2147483647){fail('名称、封面、分类、模式、售价、库存或限购无效');continue}
 if(r[4]==='presale'&&!validDate(r[5])){fail('预售需要有效发货日期');continue}
 let p=groups.get(r[0]);if(!p){p={id:'',name:r[1],subtitle:r[2],category:r[3] as Product['category'],saleMode:r[4] as Product['saleMode'],expectedShipDate:r[5],cover:r[6],origin:r[7],status:'DRAFT',sortOrder:100,gallery:[],badges:[],highlights:[],description:'',skus:[]};groups.set(r[0],p);out.products.push(p)}
 if([p.name,p.subtitle,p.category,p.saleMode,p.expectedShipDate||'',p.cover,p.origin].join('\u0000')!==r.slice(1,8).join('\u0000')){fail('同一商品分组的商品资料必须一致');continue}
 if(p.skus.some(s=>s.name===r[8])){fail('同一商品内规格名称重复');continue}p.skus.push({id:'',name:r[8],price:cents,stock,limitPerOrder:limit,weightLabel:r[12],active:true})
 }
 if(!out.rows)out.errors.push('没有商品数据');return out
}

export function externalTraceError(trace?: ExternalTrace): string {
 if (!trace) return ''
 try {
  const url = new URL(trace.url)
  if (!trace.platform.trim() || [...trace.platform].length > 80 || [...trace.code].length > 120 || trace.url.length > 1000 || /\s/.test(trace.url) || url.protocol !== 'https:' || !url.hostname || url.username || url.password) throw new Error()
  return ''
 } catch { return '外部溯源须填写平台名称和有效 HTTPS 链接，平台编码最多 120 字' }
}
