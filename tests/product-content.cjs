const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')
const cache = new Map()
function load(relative) {
  const filename = path.resolve(root, relative)
  if (cache.has(filename)) return cache.get(filename).exports
  const module = { exports: {} }
  cache.set(filename, module)
  const source = ts.transpileModule(fs.readFileSync(filename, 'utf8'), {
    compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS },
  }).outputText
  vm.runInNewContext(source, { module, exports: module.exports, structuredClone, console, setTimeout, clearTimeout,
    require: name => load(path.resolve(path.dirname(filename), `${name}.ts`)),
  }, { filename })
  return module.exports
}
async function run() {
  const { mockCall } = load('admin/src/mock.ts')
  const { demoProducts } = load('miniprogram/data/demo.ts')
  const products = await mockCall('admin.product.list')
  for (const product of products) {
    const mini = demoProducts.find(p => p.id === product.id)
    assert.equal(JSON.stringify(product.traceability), JSON.stringify(mini.traceability))
    assert.equal(product.variety, mini.variety)
    assert.ok(product.storage && product.netWeightNote && product.imageNote)
    assert.equal(product.traceability.demo, true)
    assert.match(product.traceability.batch, /^DEMO-/)
    assert.match(product.traceability.code, /^DEMO-/)
    assert.equal(JSON.stringify(product.giftCustomization), JSON.stringify(mini.giftCustomization))
  }
  const edited = structuredClone(products[0])
  edited.traceability.batch = 'DEMO-UPDATED'
  edited.traceability.code = 'DEMO-TRACE-UPDATED'
  edited.giftCustomization = { enabled: true, description: '测试定制服务', options: ['祝福卡'], minimumQuantity: 30, leadTime: '确认后 10 天' }
  edited.traceability.packedAt = '2026-10-04'
  edited.storage = '更新后的示例保存说明'
  edited.gallery = ['https://example.com/mock-detail.jpg']
  await mockCall('admin.product.save', { product: edited })
  const restored = (await mockCall('admin.product.list')).find(p => p.id === edited.id)
  assert.equal(restored.traceability.batch, 'DEMO-UPDATED')
  assert.equal(restored.traceability.code, 'DEMO-TRACE-UPDATED')
  assert.deepEqual(restored.giftCustomization, edited.giftCustomization)
  assert.equal(restored.traceability.packedAt, '2026-10-04')
  assert.equal(restored.storage, edited.storage)
  assert.equal(restored.gallery[0], edited.gallery[0])
  for (const invalid of [
    { harvestedAt: '2026-02-30', packedAt: '' },
    { harvestedAt: '', packedAt: '2026-10-04' },
    { harvestedAt: '2026-10-05', packedAt: '2026-10-04' },
  ]) await assert.rejects(mockCall('admin.product.save', { product: { ...edited, traceability: { ...edited.traceability, ...invalid } } }), /日期/)
  assert.equal((await mockCall('admin.product.list')).find(p => p.id === edited.id).traceability.batch, 'DEMO-UPDATED')
  const gift = products.find(p => p.id === 'p-gift')
  assert.equal(gift.traceability.harvestedAt, '')
  assert.equal(gift.traceability.packedAt, '')
  assert.equal(gift.giftCustomization.enabled, true)
  for (const invalid of [{minimumQuantity:0}, {minimumQuantity:1.5}, {description:''}, {leadTime:''}, {options:['']}]) {
    await assert.rejects(mockCall('admin.product.save', { product: { ...edited, giftCustomization: { ...edited.giftCustomization, ...invalid } } }), /定制/)
  }
  await assert.rejects(mockCall('admin.product.save', {product:{...edited, traceability:{...edited.traceability, batch:''}}}), /溯源码/)
  console.log('Product content: shared mock data, save round trip, presale and batch date validation passed')
}
run().catch(error => { console.error(error); process.exitCode = 1 })
