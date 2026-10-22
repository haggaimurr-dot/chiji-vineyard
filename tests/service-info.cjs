const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')

function runtime(globals, overrides = {}) {
  const cache = new Map()
  function load(filename) {
    filename = path.resolve(root, filename)
    if (overrides[filename]) return overrides[filename]
    if (cache.has(filename)) return cache.get(filename).exports
    const module = { exports: {} }; cache.set(filename, module)
    const source = ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText
    vm.runInNewContext(source, { module, exports: module.exports, ...globals, require: name => load(path.resolve(path.dirname(filename), name + '.ts')) }, { filename })
    return module.exports
  }
  return load
}

async function run() {
  let spec, demo = true, fail = false, dialed = 0, toast = '', deferred = false
  const pending = []
  const service = { enabled: true, wechat: true, phone: '400-000-0000', hours: '工作日', responseTime: '1个工作日' }
  const policy = { operator: '演示主体', address: '演示地址', contact: 'privacy@example.com' }
  const callApi = async action => {
    if (deferred) return new Promise(resolve => pending.push({ action, resolve }))
    if (fail) throw new Error('offline')
    return action === 'service.get' ? { ...service } : { ...policy }
  }
  const load = runtime({ getApp: () => ({ globalData: { demoMode: demo } }), Component: value => { spec = value }, wx: { showToast: args => { toast = args.title }, makePhoneCall: () => { dialed++ }, navigateTo() {} } }, { [path.join(root, 'miniprogram/utils/api.ts')]: { callApi } })
  load('miniprogram/components/service-info/index.ts')
  const component = { data: structuredClone(spec.data), setData(value) { Object.assign(this.data, value) }, ...spec.methods }
  await component.load()
  assert.equal(component.data.policy.operator, '演示主体')
  component.phone(); component.mockContact()
  assert.equal(dialed, 0); assert.match(toast, /演示/)
  demo = false; await component.load(); component.phone(); assert.equal(dialed, 1)
  service.enabled = false; await component.load(); component.phone(); assert.equal(dialed, 1)
  fail = true; await component.load()
  assert.equal(component.data.policy, null); assert.equal(component.data.service.enabled, false); assert.match(component.data.error, /加载失败/)
  fail = false; deferred = true
  const old = component.load(), latest = component.load()
  pending[2].resolve({ ...service, hours: '最新时间' }); pending[3].resolve(policy); await latest
  pending[0].resolve({ ...service, hours: '旧时间' }); pending[1].resolve(policy); await old
  assert.equal(component.data.service.hours, '最新时间')
  const hidden = component.load(); spec.pageLifetimes.hide.call(component)
  pending[4].resolve({ ...service, hours: '隐藏期间旧结果' }); pending[5].resolve(policy); await hidden
  assert.equal(component.data.service.hours, '')
  const { validateService } = load('admin/src/service-validation.ts')
  validateService(undefined); validateService({ ...service, enabled: true })
  assert.throws(() => validateService({ ...service, enabled: true, wechat: false, phone: '' }), /启用客服/)
  assert.throws(() => validateService({ ...service, phone: '123' }), /号码格式/)
  console.log('客服资料：演示隔离、真实拨号开关、错误恢复、过期请求及配置校验通过')
}
run().catch(error => { console.error(error); process.exitCode = 1 })
