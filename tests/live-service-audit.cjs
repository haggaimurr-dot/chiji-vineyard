const fs = require('node:fs')
const path = require('node:path')
const assert = require('node:assert/strict')
const { chromium } = require('playwright')
const output = path.resolve(__dirname, '../docs/test-evidence/2026-10-09-full-audit')
fs.mkdirSync(output, { recursive: true })
async function main() {
  const evidence = { backend: [], pages: [] }
  const health = await fetch('http://127.0.0.1:8081/healthz')
  assert.equal(health.status, 200); assert.equal(await health.text(), 'ok')
  evidence.backend.push({ action: 'GET /healthz', status: 200 })
  for (const action of ['service.get', 'privacy.get', 'admin.product.list', 'order.list']) {
    const response = await fetch('http://127.0.0.1:8081/api', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action, data: {} }) })
    const result = await response.json()
    if (['admin.product.list', 'order.list'].includes(action)) assert.equal(result.ok, false)
    evidence.backend.push({ action, status: response.status, ok: result.ok, error: result.error?.message })
  }
  const browser = await chromium.launch({ headless: true, executablePath: process.env.PLAYWRIGHT_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe' })
  try {
    const page = await browser.newPage({ viewport: { width: 1360, height: 900 } })
    const errors = []; page.on('pageerror', error => errors.push(error.message))
    await page.goto('http://127.0.0.1:5186', { waitUntil: 'networkidle' })
    await page.getByLabel('用户名', { exact: true }).fill('qa-full-audit')
    await page.getByLabel('密码', { exact: true }).fill('FullAudit2026!')
    await page.getByRole('button', { name: '进入后台', exact: true }).click()
    await page.locator('.sidebar').waitFor()
    const modules = ['账号安全', '经营概览', '商品与库存', '收藏与提醒', '订单与售后', '商品评价', '种植内容', '合作线索', '运营配置', '微信支付', '操作日志']
    for (const [i, name] of modules.entries()) {
      await page.locator('.sidebar').getByRole('button', { name: new RegExp(name) }).click()
      await page.waitForLoadState('networkidle')
      await page.waitForFunction(() => !/(正在加载|加载中[.…]|正在读取|正在更新经营|正在查询|处理中[.…])/.test(document.body.innerText), null, { timeout: 20000 })
      if (name === '商品与库存') assert.ok(await page.locator('.product-cell').count() > 0)
      await page.screenshot({ path: path.join(output, `module-${i + 1}-1360.png`), scale: 'css' })
      const text = await page.locator('body').innerText()
      assert.ok(text.includes(name))
      evidence.pages.push({ name, screenshot: `module-${i + 1}-1360.png`, alerts: await page.getByRole('alert').allTextContents() })
    }
    assert.deepEqual(errors, [])
    evidence.pageErrors = errors
    await page.setViewportSize({ width: 1024, height: 900 })
    await page.locator('.sidebar').getByRole('button', { name: /商品与库存/ }).click()
    await page.waitForLoadState('networkidle')
    await page.waitForFunction(() => !/(正在加载|加载中[.…])/.test(document.body.innerText), null, { timeout: 20000 })
    evidence.viewport1024 = await page.evaluate(() => ({ width: innerWidth, scrollWidth: document.documentElement.scrollWidth }))
    await page.screenshot({ path: path.join(output, 'products-1024.png'), scale: 'css' })
    fs.writeFileSync(path.join(output, 'live-service-results.json'), JSON.stringify(evidence, null, 2))
    console.log('Live backend health/auth checks and 11 admin module smoke checks passed; screenshots saved.')
  } finally { await browser.close() }
}
main().catch(error => { console.error(error); process.exitCode = 1 })
