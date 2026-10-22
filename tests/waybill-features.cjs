const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const storage = new Map()
function load(file) {
  const module = { exports: {} }
  const source = ts.transpileModule(fs.readFileSync(path.join(__dirname, '..', file), 'utf8'), {compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText
  vm.runInNewContext(source, {module, exports:module.exports, structuredClone, sessionStorage:{getItem:k=>storage.get(k),setItem:(k,v)=>storage.set(k,v)}, localStorage:{getItem:()=>null}})
  return module.exports
}
const {createWaybillDemo} = load('admin/src/waybill-demo.ts')
const carriers = [{code:'SF',name:'顺丰速运'},{code:'JD',name:'京东物流'},{code:'EMS',name:'EMS'}]
const demo = createWaybillDemo(carriers)
let config = demo.get()
config.rates = [
  {carrierCode:'SF',province:'',firstWeight:1,firstFee:1800,additionalWeight:1,additionalFee:500,surcharge:0,delivery:'2天'},
  {carrierCode:'JD',province:'',firstWeight:1,firstFee:800,additionalWeight:1,additionalFee:200,surcharge:0,delivery:'3天'},
  {carrierCode:'JD',province:'浙江省',firstWeight:1,firstFee:1000,additionalWeight:0.5,additionalFee:150,surcharge:100,delivery:'2天'},
]
config.accounts.SF.partnerKey = 'must-not-be-stored'
const saved = demo.save(config)
assert.equal(saved.revision, 1)
assert.equal(saved.accounts.SF.partnerKey, '')
assert.equal(saved.accounts.SF.partnerKeySet, true)
assert.ok(![...storage.values()].join('').includes('must-not-be-stored'))
assert.throws(()=>demo.save(config), /已被修改/)
let quote = demo.quote('浙江省', 2)
assert.equal(quote.items[0].carrierCode, 'JD')
assert.equal(quote.items[0].fee, 1400)
assert.equal(quote.items[2].available, false)
assert.equal(demo.quote('浙江省', 2.000001).items[0].fee, 1550)
assert.equal(demo.quote('广东省', 2).items[0].fee, 1000)
assert.throws(()=>demo.quote('浙江省', NaN), /重量/)
assert.throws(()=>demo.quote('浙江省', 101), /重量/)
const invalid = demo.get(); invalid.rates.push({...invalid.rates[0]})
assert.throws(()=>demo.save(invalid), /只能配置一条/)
config = demo.get(); config.accounts.SF.disabled = true
demo.save(config)
assert.ok(!demo.carriers().some(c=>c.code==='SF'))
assert.ok(!demo.quote('浙江省',2).items.some(c=>c.carrierCode==='SF'))
assert.equal(createWaybillDemo(carriers).get().revision, 2)
const {requirePermission} = load('admin/src/permissions.ts')
assert.throws(()=>requirePermission({role:'OPERATOR',username:'operator'},'admin.waybill.settings'),/超级管理员/)
requirePermission({role:'SUPER_ADMIN',username:'admin'},'admin.waybill.settings.save')
requirePermission({role:'OPERATOR',username:'operator',permissions:['order.read']},'admin.waybill.quote')
assert.throws(()=>requirePermission({role:'OPERATOR',username:'operator',permissions:[]},'admin.waybill.quote'),/权限不足/)
console.log('Waybill config redaction, retention, revision conflicts, destination rates, weight rounding, sorting, disabling and permissions passed.')
