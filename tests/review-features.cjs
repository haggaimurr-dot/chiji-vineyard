const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm'), assert = require('node:assert/strict'), ts = require('typescript')
const { webcrypto } = require('node:crypto')
const root = path.resolve(__dirname, '..')
const mini = new Map(), files = new Map()
const localStorage = { getItem: k => mini.get('local:'+k) || null, setItem: (k,v) => mini.set('local:'+k,String(v)), removeItem: k => mini.delete('local:'+k) }
const sessionStorage = { getItem: k => mini.get('session:'+k) || null, setItem: (k,v) => mini.set('session:'+k,String(v)), removeItem: k => mini.delete('session:'+k) }
const wx = { getStorageSync: k => structuredClone(mini.get(k)), setStorageSync: (k,v) => mini.set(k,structuredClone(v)), env: {USER_DATA_PATH:'/demo'}, getFileSystemManager: () => ({writeFile: options => { files.set(options.filePath,options.data); options.success() }}) }
function runtime() {
 const cache = new Map()
 function load(relative) {
  const filename = path.resolve(root,relative)
  if(cache.has(filename)) return cache.get(filename).exports
  const module = {exports:{}}; cache.set(filename,module)
  const code = ts.transpileModule(fs.readFileSync(filename,'utf8'), {compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText
  vm.runInNewContext(code,{ module,exports:module.exports,console,wx,getApp:()=>({globalData:{demoMode:true}}),setTimeout,clearTimeout,localStorage,sessionStorage,structuredClone,TextEncoder,Uint8Array,crypto:webcrypto,require:name=>load(path.resolve(path.dirname(filename),name+'.ts')) },{filename})
  return module.exports
 }
 return load
}
async function main(){
 const load = runtime(), call = load('miniprogram/utils/api.ts').callApi
 const order = {id:'o1',orderStatus:'COMPLETED',fulfillmentStatus:'RECEIVED',paymentStatus:'PAID',items:[{productId:'p-shine'}]}
 mini.set('chiji_demo_orders',[order])
 const image = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jWZkAAAAASUVORK5CYII='
 const review = await call('review.create',{orderId:'o1',productId:'p-shine',rating:5,content:'鲜甜',images:[image]})
 assert.equal(review.status,'PENDING')
 assert.ok(!(await call('product.reviews',{productId:'p-shine'})).items.some(r=>r.id===review.id),'pending creation stays private')
 assert.equal(review.images.length,1); assert.ok(files.has(review.images[0]),'demo images use persistent local files supported by WeChat')
 await assert.rejects(call('review.create',{orderId:'o1',productId:'p-shine',rating:5,content:'重复'}),/已评价/)
 await assert.rejects(call('review.append',{id:'missing',content:'追评'}),/不存在/)
 await assert.rejects(call('review.append',{id:review.id,content:'追评',images:Array(7).fill(image)}),/最多6张/)
 await assert.rejects(call('review.append',{id:review.id,content:' '}),/1–500/)
 const appended = await call('review.append',{id:review.id,content:'仍然新鲜',images:[image]})
 assert.equal(appended.content,'鲜甜'); assert.equal(appended.rating,5); assert.equal(appended.appendImages.length,1)
 await assert.rejects(call('review.append',{id:review.id,content:'重复追评'}),/仅可追评一次/)
 const own = await runtime()('miniprogram/utils/api.ts').callApi('review.order',{orderId:'o1'})
 assert.equal(own[0].appendContent,'仍然新鲜','reload persists append')
 const publicList = await call('product.reviews',{productId:'p-shine'})
 assert.ok(!publicList.items.some(r=>r.id===review.id),'pending append stays private')
 assert.equal(own[0].status,'PENDING','buyer can read pending review')
 const admin = runtime()('admin/src/mock.ts').mockCall
 const login = await admin('admin.login',{username:'owner',password:'demo-initial'})
 localStorage.setItem('chiji_admin_token',login.token)
 const list = await admin('admin.review.list',{page:1})
 assert.ok(list.items.length)
 const item = list.items[0]
 await assert.rejects(admin('admin.review.reply',{id:item.id,content:' '}),/1–500/)
 await admin('admin.review.reply',{id:item.id,content:'感谢支持'})
 await assert.rejects(admin('admin.review.reply',{id:item.id,content:'再次回复'}),/已回复/)
 const reloaded = await runtime()('admin/src/mock.ts').mockCall('admin.review.list',{page:1})
 assert.equal(reloaded.items.find(r=>r.id===item.id).reply,'感谢支持')
 await assert.rejects(admin('admin.review.moderate',{id:item.id,status:'REJECTED',reason:' '}),/驳回原因/)
 await assert.rejects(admin('admin.review.moderate',{id:item.id,status:'PENDING'}),/审核状态/)
 await assert.rejects(admin('admin.review.list',{status:'INVALID'}),/评价状态/)
 const rejected = await admin('admin.review.moderate',{id:item.id,status:'REJECTED',reason:'包含个人信息'})
 assert.equal(rejected.reply,'感谢支持','moderation preserves reply')
 assert.equal(rejected.rejectReason,'包含个人信息')
 await assert.rejects(admin('admin.review.moderate',{id:item.id,status:'REJECTED',reason:'重复'}),/已处于/)
 const rejectedList = await runtime()('admin/src/mock.ts').mockCall('admin.review.list',{status:'REJECTED'})
 assert.ok(rejectedList.items.some(r=>r.id===item.id),'moderation persists across reload')
 assert.ok(!(await admin('admin.review.list',{status:'APPROVED'})).items.some(r=>r.id===item.id))
 const approved = await admin('admin.review.moderate',{id:item.id,status:'APPROVED'})
 assert.equal(approved.rejectReason,''); assert.ok(approved.moderatedAt)
 // Demo stores are independent: emulate an approved server response in the mini store.
 mini.set('chiji_demo_reviews',[{...own[0],status:'APPROVED'}])
 const visible = await call('product.reviews',{productId:'p-shine'})
 assert.equal(visible.total,publicList.total+1)
 assert.equal(visible.items.find(r=>r.id===review.id).appendImages[0],appended.appendImages[0])
 mini.set('chiji_demo_reviews',[{...own[0],status:'REJECTED',rejectReason:'包含个人信息'}])
 assert.equal((await call('product.reviews',{productId:'p-shine'})).total,publicList.total)
 assert.equal((await call('review.order',{orderId:'o1'}))[0].rejectReason,'包含个人信息')
 console.log('Review features passed: image persistence, creation, duplicate prevention, append validation, reload, public list and merchant replies.')
}
main().catch(error=>{console.error(error);process.exitCode=1})
