const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const assert = require('node:assert/strict')
const ts = require('typescript')

async function run() {
  let page, apiError, writeError, openError, requests = 0
  const files = new Map(), opened = []
  const fileData = Buffer.from('%PDF-1.4\n%%EOF').toString('base64')
  const wx = {
    env: { USER_DATA_PATH: '/user' },
    getFileSystemManager: () => ({
      writeFile: options => {
        if (writeError) return options.fail(new Error('存储空间不足'))
        files.set(options.filePath, options.data); options.success()
      },
      unlink: () => { throw new Error('Document must remain available for save/share') },
    }),
    openDocument: async options => {
      if (openError) throw new Error('PDF 打开失败')
      assert.equal(files.get(options.filePath), fileData)
      opened.push(options)
    },
  }
  const source = ts.transpileModule(fs.readFileSync(path.resolve(__dirname, '../miniprogram/pages/order-detail/index.ts'), 'utf8'), {
    compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS },
  }).outputText
  vm.runInNewContext(source, {
    exports: {}, wx, getApp: () => ({ globalData: { demoMode: false } }),
    Page: spec => { page = spec; page.setData = updates => Object.assign(page.data, updates) },
    require: () => ({ callApi: async (action, data) => {
      requests++; assert.equal(action, 'invoice.file'); assert.equal(data.orderId, 'o1')
      if (apiError) throw new Error('发票文件不存在')
      return { fileName: '发票.pdf', fileData }
    } }),
  })
  page.data.order = { id: 'o1', invoice: { status: 'ISSUED', fileName: '发票.pdf' } }
  await page.viewInvoice()
  assert.equal(opened.length, 1); assert.equal(opened[0].showMenu, true)
  assert.equal(opened[0].fileType, 'pdf'); assert.equal(files.size, 1)
  assert.equal(page.data.invoiceBusy, false); assert.equal(page.data.invoiceError, '')
  await page.viewInvoice()
  assert.equal(files.size, 1, 'Repeated downloads must not accumulate PDF files')
  page.data.invoiceBusy = true
  await page.viewInvoice(); assert.equal(requests, 2)
  page.data.invoiceBusy = false
  for (const failure of ['api', 'write', 'open']) {
    apiError = failure === 'api'; writeError = failure === 'write'; openError = failure === 'open'
    await page.viewInvoice()
    assert.ok(page.data.invoiceError); assert.equal(page.data.invoiceBusy, false)
  }
  apiError = writeError = openError = false
  await page.viewInvoice(); assert.equal(page.data.invoiceError, '')
  page.data.order.invoice.fileName = ''
  const before = requests
  await page.viewInvoice(); assert.equal(requests, before)
  console.log('Invoice download, save/share retention, bounded storage and error recovery passed.')
}
run().catch(error => { console.error(error); process.exitCode = 1 })
