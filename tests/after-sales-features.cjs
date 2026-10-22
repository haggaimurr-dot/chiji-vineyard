const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm'), assert = require('node:assert/strict'), ts = require('typescript')
const { webcrypto } = require('node:crypto')
const root = path.resolve(__dirname, '..')
function runtime(globals = {}) {
 const cache = new Map()
 return function load(relative) {
  const file = path.resolve(root,relative); if(cache.has(file)) return cache.get(file).exports
  const module = {exports:{}};cache.set(file,module)
  const code = ts.transpileModule(fs.readFileSync(file,'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2020,module:ts.ModuleKind.CommonJS}}).outputText
  vm.runInNewContext(code,{module,exports:module.exports,console,Error,setTimeout,clearTimeout,structuredClone,crypto:webcrypto,TextEncoder,btoa,atob,...globals,require:name=>load(path.resolve(path.dirname(file),name+'.ts'))},{filename:file})
  return module.exports
 }
}
async function main() {
 const storage = new Map(), wx = {getStorageSync:key=>structuredClone(storage.get(key)),setStorageSync:(key,value)=>storage.set(key,structuredClone(value))}
 const {callApi} = runtime({wx,getApp:()=>({globalData:{demoMode:true}})})('miniprogram/utils/api.ts')
 wx.setStorageSync('chiji_demo_orders',[{id:'o1',orderNo:'CJ1',paymentStatus:'PAID',items:[],quote:{total:10000}},{id:'unpaid',paymentStatus:'UNPAID'}])
 await assert.rejects(()=>callApi('ticket.create',{orderId:'unpaid',category:'OTHER',content:'问题'}),/支付/)
 const attachment={name:'evidence.png',dataUrl:'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aPrsAAAAASUVORK5CYII='}
 const ticket = await callApi('ticket.create',{orderId:'o1',category:'QUALITY',content:'部分坏果',attachments:[attachment]})
 assert.equal(ticket.messages[0].attachments[0].name,'evidence.png'); assert.ok(Date.parse(ticket.dueAt)>Date.now()+47*3600000)
 const {ticketReminder}=runtime()('miniprogram/utils/after-sales.ts')
 assert.equal(ticketReminder({...ticket,messages:[...ticket.messages,{role:'ADMIN',content:'回复'}]},'USER').unread,1)
 assert.equal(ticketReminder({...ticket,dueAt:'2020-01-01T00:00:00Z'},'USER').overdue,true)
 await assert.rejects(()=>callApi('ticket.reply',{id:ticket.id,version:1,content:'bad',attachments:[attachment,attachment,attachment,attachment]}),/最多3/)
 await callApi('ticket.read',{id:ticket.id,version:1,readThrough:1}); assert.equal((await callApi('ticket.list',{orderId:'o1'}))[0].version,1)
 await assert.rejects(()=>callApi('ticket.create',{orderId:'o1',category:'OTHER',content:'duplicate'}),/未关闭/)
 await assert.rejects(()=>callApi('ticket.reply',{id:ticket.id,version:1,status:'CLOSED',content:'spoof'}),/仅可/)
 await callApi('ticket.reply',{id:ticket.id,version:1,content:'补充说明'})
 await assert.rejects(()=>callApi('ticket.reply',{id:ticket.id,version:1,content:'stale'}),/已更新/)
 assert.equal((await callApi('ticket.list',{orderId:'o1'}))[0].messages.length,2)
 assert.equal((await callApi('ticket.list',{orderId:'other'})).length,0)
 await assert.rejects(()=>callApi('ticket.list',{offset:-1}),/分页/)
 const {mockCall} = runtime()('admin/src/mock.ts')
 const existing = (await mockCall('admin.ticket.list')).find(t=>t.orderId==='o-3')
 await assert.rejects(()=>mockCall('admin.ticket.update',{id:existing.id,version:1,status:'CLOSED',content:'skip'}),/状态/)
 await assert.rejects(()=>mockCall('admin.ticket.update',{id:existing.id,version:1,status:'RESOLVED'}),/处理结果/)
 await mockCall('admin.ticket.read',{id:existing.id,version:1,readThrough:existing.messages.length,status:'CLOSED'})
 assert.equal((await mockCall('admin.ticket.list')).find(t=>t.id===existing.id).status,'OPEN')
 await assert.rejects(()=>mockCall('admin.ticket.update',{id:existing.id,version:1,dueAt:'2020-01-01T00:00:00Z'}),/处理期限/)
 const processing = await mockCall('admin.ticket.update',{id:existing.id,version:1,status:'PROCESSING',content:'正在核实'})
 await assert.rejects(()=>mockCall('admin.ticket.update',{id:existing.id,version:1,content:'stale'}),/已更新/)
 const resolved=await mockCall('admin.ticket.update',{id:existing.id,version:processing.version,status:'RESOLVED',content:'已处理'})
 await mockCall('admin.ticket.update',{id:existing.id,version:resolved.version,status:'CLOSED',content:'完成'})
 await assert.rejects(()=>mockCall('admin.ticket.update',{id:existing.id,version:4,content:'late'}),/已关闭/)
 const invoice=await mockCall('admin.invoice.autoIssue',{orderId:'o-1'})
 assert.equal(invoice.status,'ISSUED');assert.equal(invoice.autoStatus,'ISSUED');assert.equal(invoice.emailStatus,'SENT')
 assert.match(atob((await mockCall('admin.invoice.file',{orderId:'o-1'})).fileData),/DEMO ONLY/)
 const duplicate=await mockCall('admin.invoice.autoIssue',{orderId:'o-1'});assert.equal(duplicate.autoKey,invoice.autoKey);assert.equal(duplicate.number,invoice.number)
 // Exercise the mini page using persistent demo storage, including preview and reopening.
 const stored = wx.getStorageSync('chiji_demo_tickets'); stored[0].messages.push({role:'ADMIN',content:'商家回复',createdAt:new Date().toISOString(),attachments:[attachment]}); stored[0].version++; stored[0].status='RESOLVED'; wx.setStorageSync('chiji_demo_tickets',stored)
 let page, previewed = false
 const miniWx={...wx,env:{USER_DATA_PATH:'/private'},showToast:()=>{},chooseMedia:async()=>({tempFiles:[{size:100,tempFilePath:'/picked.png'}]}),getFileSystemManager:()=>({readFile:r=>r.success({data:attachment.dataUrl.split(',')[1]}),writeFile:r=>r.success(),unlink:()=>{}}),previewImage:()=>{previewed=true}}
 runtime({wx:miniWx,getApp:()=>({globalData:{demoMode:true}}),Page:config=>{page=config}})('miniprogram/pages/after-sales/index.ts')
 page.setData=values=>Object.assign(page.data,values);page.data.orderId='o1'
 await page.load();assert.match(page.data.tickets[0].reminderText,/1条未读商家回复/)
 await page.select({currentTarget:{dataset:{id:ticket.id}}});assert.equal(page.data.tickets[0].reminderText,'')
 page.preview({currentTarget:{dataset:{message:2,index:0}}});assert.equal(previewed,true)
 await page.attach();assert.equal(page.data.attachments.length,1)
 page.setReply({detail:{value:'问题仍未解决'}});await page.reply()
 const reopened=(await callApi('ticket.list',{orderId:'o1'}))[0];assert.equal(reopened.status,'PROCESSING');assert.equal(reopened.messages.at(-1).attachments.length,1);assert.ok(Date.parse(reopened.dueAt)>Date.now()+47*3600000)
 console.log('Ticket submission, followups, duplicate/conflict guards, resolution/closure and automatic invoice demo flows passed.')
}
main().catch(e=>{console.error(e);process.exitCode=1})
