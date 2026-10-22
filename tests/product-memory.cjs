const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')
const root = path.resolve(__dirname, '..')
const storage = new Map(), pages = [], toasts = [], navigation = []
const app = { globalData: { demoMode: true } }
let modal, failWrite = false, api = async () => null
const wx = {
  getStorageSync: k => storage.get(k),
  setStorageSync: (k, v) => { if (failWrite) throw new Error('存储失败'); storage.set(k, JSON.parse(JSON.stringify(v))) },
  showToast: v => toasts.push(v.title), showModal: v => { modal = v }, setNavigationBarTitle() {}, stopPullDownRefresh() {},
  navigateTo: v => navigation.push(v.url), switchTab: v => navigation.push(v.url),
}
const cache = new Map()
function load(file) {
  file = path.resolve(root, file)
  if (cache.has(file)) return cache.get(file).exports
  const module = { exports: {} }; cache.set(file, module)
  const code = ts.transpileModule(fs.readFileSync(file, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText
  vm.runInNewContext(code, { module, exports: module.exports, wx, getApp: () => app, setTimeout, clearTimeout,
    require: name => name.endsWith('/api') ? { callApi: (...args) => api(...args) } : load(path.resolve(path.dirname(file), name + '.ts')),
    Page: spec => {
      spec.setData = patch => { for (const [k,v] of Object.entries(patch)) {
        const m = k.match(/^(entries|products)\[(\d+)\]\.(\w+)$/)
        if (m) spec.data[m[1]][+m[2]][m[3]] = v
        else spec.data[k] = v
      } }; pages.push(spec)
    },
  }, { filename: file })
  return module.exports
}
const product = id => ({ id, name: `葡萄 ${id}`, cover: 'demo.jpg', gallery: [], skus: [{ id: `s-${id}`, name: '家庭装', weightLabel: '2斤', price: 9900, stock: 5, limitPerOrder: 2 }], origin: '果园', variety: '阳光玫瑰', saleMode: 'spot', storage: '冷藏', netWeightNote: '净重' })
const event = id => ({ currentTarget: { dataset: { id } } })
async function run() {
  const memory = load('miniprogram/utils/product-memory.ts')
  for (let i=0; i<55; i++) memory.recordView(product(String(i)))
  assert.equal(memory.readMemory('history').length, 50)
  assert.equal(memory.readMemory('history')[0].id, '54')
  memory.recordView(product('10')); assert.equal(memory.readMemory('history')[0].id, '10')
  assert.equal(memory.readMemory('history').filter(p => p.id === '10').length, 1)
  for (let i=0; i<4; i++) assert.equal(memory.toggleCompare(product(String(i))), true)
  assert.throws(() => memory.toggleCompare(product('4')), /最多对比/)
  assert.equal(memory.toggleCompare(product('0')), false)
  assert.equal(memory.toggleCompare(product('4')), true)
  memory.removeMemory('compare', '1'); assert.equal(memory.readMemory('compare').length, 3)
  app.globalData.demoMode = false
  assert.equal(memory.readMemory('history').length, 0); assert.equal(memory.readMemory('compare').length, 0)
  app.globalData.demoMode = true
  memory.clearMemory('history'); memory.recordView(product('a')); memory.recordView(product('b')); memory.recordView(product('c'))
  api = async (_, {id}) => { if (id === 'b') throw new Error('网络错误'); return id === 'c' ? null : product(id) }
  load('miniprogram/pages/product-memory/index.ts'); const page = pages.at(-1)
  page.onLoad({kind:'history'}); await page.onShow()
  assert.equal(page.data.entries.length,3)
  assert.equal(page.data.entries[0].error,'商品已下架或不存在')
  assert.equal(page.data.entries[1].error,'网络错误')
  assert.equal(page.data.entries[2].priceText,'99.00')
  page.openProduct(event('c')); assert.equal(navigation.length,0)
  page.openProduct(event('a')); assert.ok(navigation[0].includes('id=a'))
  page.clear(); modal.success({confirm:false}); assert.equal(memory.readMemory('history').length,3)
  page.clear(); modal.success({confirm:true}); await new Promise(r => setTimeout(r,0))
  assert.equal(page.data.entries.length,0)
  memory.clearMemory('compare'); memory.toggleCompare(product('a')); memory.toggleCompare(product('b'))
  page.onLoad({kind:'compare'}); api = async (_, {id}) => ({...product(id), skus:[{...product(id).skus[0], price:12345, stock:0}]})
  await page.onShow(); assert.equal(page.data.rows[0].values[0].text,'¥123.45 起'); assert.equal(page.data.rows[4].values[0].text,'暂时售罄')
  assert.ok(page.data.rows[3].values[0].text.includes('2斤'))
  await page.remove(event('a')); assert.equal(page.data.entries.length,1)
  // A response from a hidden page must not overwrite a newer refresh.
  let resolveOld; api = () => new Promise(r => { resolveOld = r })
  const old = page.load(); page.onHide(); api = async (_, {id}) => product(id)
  await page.onShow(); resolveOld({...product('b'),name:'过期名称'}); await old
  assert.equal(page.data.entries[0].name,'葡萄 b')
  failWrite = true; await page.remove(event('b')); assert.equal(page.data.entries.length,1); assert.equal(toasts.at(-1),'存储失败')
  failWrite = false
  storage.set('chiji_demo_product_history', [{id:'x',name:'x',cover:'',viewedAt:1}, {id:'x',name:'x',cover:'',viewedAt:1}, null, {id:2}])
  assert.equal(memory.readMemory('history').length,1)
  // Detail visits record only successfully loaded products.
  memory.clearMemory('history'); memory.clearMemory('compare')
  api = async (action, data) => {
    if (action === 'product.detail') return data.id === 'gone' ? null : product(data.id)
    if (action === 'product.reviews') return {items:[],page:1,total:0,average:0,hasMore:false}
    return {enabled:false}
  }
  load('miniprogram/pages/product/index.ts'); const detailPage = pages.at(-1)
  await detailPage.onLoad({id:'detail'})
  assert.equal(memory.readMemory('history')[0].id,'detail')
  detailPage.toggleCompare(); assert.equal(detailPage.data.comparing,true)
  await detailPage.onLoad({id:'gone'}); assert.equal(memory.readMemory('history').length,1)
  // Comparison selection from mall survives a refresh.
  api = async () => ({items:[product('detail'),product('next')],total:2,offset:0,limit:20})
  load('miniprogram/pages/shop/index.ts'); const mall = pages.at(-1)
  await mall.onShow(); assert.equal(mall.data.products[0].comparing,true)
  mall.toggleCompare(event('next')); assert.equal(mall.data.compareCount,2)
  await mall.onShow(); assert.equal(mall.data.products[1].comparing,true)
  mall.openHistory(); assert.equal(navigation.at(-1),'/pages/product-memory/index?kind=history')
  const appConfig = JSON.parse(fs.readFileSync(path.join(root,'miniprogram/app.json'),'utf8'))
  assert.ok(appConfig.pages.includes('pages/product-memory/index'))
  console.log('Product memory: deduplication, 50/4 limits, persistence, mode isolation, current prices, removed products, errors, clear confirmation, stale responses and storage failures passed.')
}
run().catch(e => {console.error(e);process.exitCode=1})
