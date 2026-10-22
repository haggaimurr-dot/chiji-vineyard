const { chromium } = require('playwright')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')

const artifacts = path.join(__dirname, 'artifacts')
fs.mkdirSync(artifacts, { recursive: true })
const inventory = `# 配置历史与操作日志浏览器验收

使用独立浏览器上下文和演示账号，通过实际页面控件操作；不调用 API 代替交互。

| 声明/控件 | 功能检查 | 画面检查与证据 |
| --- | --- | --- |
| 登录与超级管理员导航 | 用户名、密码、登录、配置与日志入口 | 登录和两页初始视图 |
| 保存与完整版本 | 保存运费，出现 BASELINE v1 与 SAVE v2；类型切换、刷新 | 版本表和完整快照 |
| 回滚确认与表单同步 | 原因必填、取消、关闭、确认生成 v3；运费恢复，优惠券/首页旧草稿消失 | 回滚确认与成功状态 |
| 版本与日志分页 | 10/20/50切换、上一页/下一页 | 多条记录列表 |
| 日志查询 | action、username、targetId、起止日、查询、刷新、重置 | CONFIG_ROLLBACK 详情包含原因 |
| 非正常路径 | 无结果筛选；起始日晚于结束日被拦截；当前版本不能回滚 | 空状态、浏览器日期校验 |
| 窗口宽度 | 1360 与 1024，两页与抽屉边界检查 | 对应截图及水平溢出数据 |

探索覆盖：反复切换配置类型/页长、打开当前快照、取消回滚后重开、查询空结果后重置和日期反向输入。
`
fs.writeFileSync(path.join(artifacts, 'config-history-ui-inventory.md'), inventory)

const results = { checks: [], screenshots: [], metrics: [], errors: [] }
const pass = (name) => { results.checks.push(name); console.log('PASS ' + name) }
let browser, page
async function screenshot(name) {
  const file = path.join(artifacts, `config-history-ui-${name}.png`)
  await page.locator('.drawer').evaluateAll(async elements => {
    await Promise.all(elements.flatMap(el => el.getAnimations().map(animation => animation.finished.catch(() => {}))))
  })
  await page.screenshot({ path: file, scale: 'css' })
  results.screenshots.push(file)
  console.log('SCREENSHOT ' + file)
}
async function fit(name) {
  const metric = await page.evaluate(() => ({
    innerWidth, clientWidth: document.documentElement.clientWidth, scrollWidth: document.documentElement.scrollWidth,
    horizontalOverflow: document.documentElement.scrollWidth > document.documentElement.clientWidth,
    regions: [...document.querySelectorAll('.history-panel,.audit-filters,.table-panel,.drawer')].map(el => {
      const r = el.getBoundingClientRect()
      return { class: el.className, x: Math.round(r.x), right: Math.round(r.right), width: Math.round(r.width) }
    }),
  }))
  results.metrics.push({ name, ...metric })
  console.log('FIT ' + name + ' ' + JSON.stringify(metric))
  assert.equal(metric.horizontalOverflow, false, `${name}: horizontal overflow`)
  assert.ok(metric.regions.every(region => region.x >= 0 && region.right <= metric.clientWidth + 1), `${name}: clipped region`)
}
async function visibleText(locator, text) {
  await page.locator('.history-panel tbody [role="status"], .audit-table tbody [role="status"]').waitFor({ state: 'hidden' })
  await locator.filter({ hasText: text }).first().waitFor({ state: 'visible' })
}

;(async () => {
  browser = await chromium.launch({ headless: true, executablePath: process.env.PLAYWRIGHT_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe' })
  const context = await browser.newContext({ viewport: { width: 1360, height: 900 }, timezoneId: 'Asia/Shanghai' })
  page = await context.newPage()
  page.setDefaultTimeout(30000)
  page.on('pageerror', e => results.errors.push(e.message))
  await page.goto(process.env.ADMIN_UI_URL || 'http://127.0.0.1:5178', { waitUntil: 'networkidle' })
  await page.getByLabel('用户名', { exact: true }).fill('qa-config-history')
  await page.getByLabel('密码', { exact: true }).fill('ConfigHistory2026!')
  await screenshot('login-1360')
  await page.getByRole('button', { name: '进入后台', exact: true }).click()
  await page.getByRole('button', { name: '运营配置', exact: false }).click()
  const history = page.locator('.history-panel')
  const shipping = page.locator('.settings-grid form').first()
  const freight = shipping.locator('input').first()
  await freight.waitFor()
  await page.waitForFunction(() => !document.querySelector('.settings-grid form input').disabled)
  await visibleText(history.locator('td'), '暂无配置版本')
  const originalFreight = await freight.inputValue()
  const originalStoryTitle = await page.getByLabel('果园介绍标题', { exact: true }).inputValue()
  const originalCouponCount = await page.locator('.coupon-editor').count()
  pass('首次登录、超级管理员导航与配置空状态')
  await freight.fill('55.55')
  await shipping.getByRole('button', { name: '保存配置', exact: true }).click()
  await visibleText(history.locator('tr'), '初始快照')
  await visibleText(history.locator('tr'), '保存配置')
  assert.equal(await history.locator('tbody tr').count(), 2)
  pass('运费保存生成 BASELINE v1 与 SAVE v2')

  await history.getByLabel('配置类型').selectOption('shipping')
  await history.getByRole('button', { name: '刷新版本', exact: true }).click()
  await visibleText(history.locator('tr'), '初始快照')
  await history.locator('tbody tr').filter({ hasText: '当前版本' }).getByRole('button', { name: '查看快照' }).click()
  let drawer = page.getByRole('dialog')
  assert.equal(await drawer.getByRole('button', { name: '回滚至此版本' }).isDisabled(), true)
  await drawer.getByRole('button', { name: '关闭快照', exact: true }).click()
  pass('类型筛选、刷新、当前版本回滚禁用与快照关闭')

  await page.getByLabel('果园介绍标题', { exact: true }).fill('浏览器验收未保存首页草稿')
  await page.locator('.coupon-settings').getByRole('button', { name: '添加优惠券', exact: true }).click()
  assert.equal(await page.locator('.coupon-editor').count(), originalCouponCount + 1)
  await history.locator('tbody tr').filter({ hasText: '初始快照' }).getByRole('button', { name: '查看快照' }).click()
  drawer = page.getByRole('dialog')
  const snapshot = JSON.parse(await drawer.locator('pre').innerText())
  assert.equal(snapshot.freight, Math.round(Number(originalFreight) * 100))
  await drawer.getByRole('button', { name: '回滚至此版本' }).click()
  assert.equal(await drawer.getByRole('button', { name: '确认回滚', exact: true }).isDisabled(), true)
  await drawer.getByRole('button', { name: '取消', exact: true }).click()
  assert.equal(await drawer.getByLabel('回滚原因', { exact: true }).count(), 0)
  await drawer.getByRole('button', { name: '关闭', exact: true }).click()
  assert.equal(await freight.inputValue(), '55.55')
  pass('完整 baseline 快照、原因必填、取消回滚未更改配置')

  await history.locator('tbody tr').filter({ hasText: '初始快照' }).getByRole('button', { name: '查看快照' }).click()
  drawer = page.getByRole('dialog')
  await drawer.getByRole('button', { name: '回滚至此版本' }).click()
  await drawer.getByLabel('回滚原因', { exact: true }).fill('浏览器验收：恢复初始运费')
  await screenshot('rollback-confirm-1360')
  await drawer.getByRole('button', { name: '确认回滚', exact: true }).click()
  await drawer.waitFor({ state: 'hidden' })
  await page.waitForFunction(expected => document.querySelector('.settings-grid form input')?.value === expected && !document.querySelector('.settings-grid form input')?.disabled, originalFreight)
  await page.getByLabel('果园介绍标题', { exact: true }).waitFor()
  assert.equal(await page.getByLabel('果园介绍标题', { exact: true }).inputValue(), originalStoryTitle)
  assert.equal(await page.locator('.coupon-editor').count(), originalCouponCount)
  await visibleText(history.locator('tr'), '回滚配置')
  await page.evaluate(() => window.scrollTo(0, 0))
  await screenshot('settings-1360')
  await fit('settings-1360')
  pass('确认回滚生成新 v3、运费回读正确、首页/优惠券草稿失效')

  for (let index = 0; index < 9; index++) {
    await freight.fill((20 + index).toFixed(2))
    await shipping.getByRole('button', { name: '保存配置', exact: true }).click()
    await page.waitForFunction(() => !document.querySelector('.settings-grid form input').disabled)
    await history.getByRole('button', { name: '刷新版本', exact: true }).waitFor()
    await page.waitForFunction(() => !document.querySelector('.history-panel .panel-head button').disabled)
  }
  await history.getByLabel('每页条数').selectOption('10')
  await visibleText(history.locator('.history-pagination'), '第 1 / 2 页')
  assert.equal(await history.locator('tbody tr').count(), 10)
  await history.getByRole('button', { name: '下一页', exact: true }).click()
  await visibleText(history.locator('.history-pagination'), '第 2 / 2 页')
  assert.equal(await history.locator('tbody tr').count(), 2)
  await history.getByRole('button', { name: '上一页', exact: true }).click()
  await visibleText(history.locator('.history-pagination'), '第 1 / 2 页')
  await history.getByLabel('每页条数').selectOption('50')
  await visibleText(history.locator('.history-pagination'), '第 1 / 1 页')
  await history.getByLabel('配置类型').selectOption('privacy')
  await visibleText(history.locator('td'), '暂无配置版本')
  await history.getByLabel('配置类型').selectOption('')
  await visibleText(history.locator('tr'), '回滚配置')
  await history.getByLabel('每页条数').selectOption('20')
  pass('版本分页、页长切换、筛选空状态与恢复全部配置')

  await page.getByRole('button', { name: '操作日志', exact: false }).click()
  await page.getByLabel('操作类型').fill('CONFIG_ROLLBACK')
  await page.getByLabel('操作人', { exact: true }).fill('qa-config-history')
  await page.getByLabel('目标 ID', { exact: true }).fill('shipping')
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai' }).format(new Date())
  await page.getByLabel('开始日期', { exact: true }).fill(today)
  await page.getByLabel('结束日期').fill(today)
  await page.getByRole('button', { name: '查询日志', exact: true }).click()
  await visibleText(page.locator('.table-panel tbody tr'), 'CONFIG_ROLLBACK')
  assert.equal(await page.locator('.table-panel tbody tr').count(), 1)
  await screenshot('audit-1360')
  await fit('audit-1360')
  await page.getByRole('button', { name: '查看详情', exact: true }).click()
  drawer = page.getByRole('dialog')
  const detail = JSON.parse(await drawer.locator('pre').innerText())
  assert.equal(detail.reason, '浏览器验收：恢复初始运费')
  assert.equal(detail.sourceVersion, 1)
  await screenshot('audit-detail-1360')
  await drawer.getByRole('button', { name: '关闭', exact: true }).click()
  pass('日志五项组合筛选含当日、回滚详情与关闭')

  await page.getByLabel('操作人', { exact: true }).fill('no-such-operator')
  await page.getByRole('button', { name: '查询日志', exact: true }).click()
  await visibleText(page.locator('td'), '暂无符合条件的操作日志')
  await screenshot('audit-empty-1360')
  await page.getByRole('button', { name: '重置筛选', exact: true }).click()
  await visibleText(page.locator('.table-panel tbody tr'), 'CONFIG_ROLLBACK')
  await page.getByLabel('开始日期', { exact: true }).fill('2026-10-05')
  await page.getByLabel('结束日期').fill('2026-10-04')
  await page.getByRole('button', { name: '查询日志', exact: true }).click()
  const invalidDates = await page.locator('input[type=date]').evaluateAll(inputs => inputs.some(input => !input.validity.valid))
  assert.equal(invalidDates, true)
  assert.notEqual(await page.locator('.table-panel tbody tr').count(), 0)
  await screenshot('audit-invalid-date-1360')
  await page.getByRole('button', { name: '重置筛选', exact: true }).click()
  await page.getByRole('button', { name: '刷新日志', exact: true }).click()
  await page.getByLabel('每页').selectOption('10')
  await visibleText(page.locator('.audit-pagination'), '第 1 / 2 页')
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  await visibleText(page.locator('.audit-pagination'), '第 2 / 2 页')
  await page.getByRole('button', { name: '上一页', exact: true }).click()
  await visibleText(page.locator('.audit-pagination'), '第 1 / 2 页')
  await page.getByLabel('每页').selectOption('20')
  pass('日志空状态、重置、日期反向校验、刷新与分页')

  await page.setViewportSize({ width: 1024, height: 900 })
  await page.evaluate(() => window.scrollTo(0, 0))
  await screenshot('audit-1024')
  await fit('audit-1024')
  await page.getByRole('button', { name: '操作日志', exact: false }).click()
  await page.getByLabel('操作类型').fill('CONFIG_ROLLBACK')
  await page.getByRole('button', { name: '查询日志', exact: true }).click()
  await page.getByRole('button', { name: '查看详情', exact: true }).click()
  await screenshot('audit-detail-1024')
  await fit('audit-detail-1024')
  await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).click()
  await page.getByRole('button', { name: '运营配置', exact: false }).click()
  await visibleText(page.locator('.history-panel tbody tr'), '回滚配置')
  await page.evaluate(() => window.scrollTo(0, 0))
  await screenshot('settings-1024')
  await fit('settings-1024')
  pass('1360 与 1024 配置、日志及抽屉截图和边界指标')

  assert.deepEqual(results.errors, [])
  fs.rmSync(path.join(artifacts, 'config-history-ui-failure.png'), { force: true })
  console.log('COMPLETE ' + JSON.stringify(results))
})().catch(async error => {
  results.failure = error.stack
  console.error(error.stack)
  if (page) await screenshot('failure').catch(() => {})
  process.exitCode = 1
}).finally(async () => {
  fs.writeFileSync(path.join(artifacts, 'config-history-ui-results.json'), JSON.stringify(results, null, 2))
  if (browser) await browser.close()
})
