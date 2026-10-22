const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm')
const assert = require('node:assert/strict'), ts = require('typescript'), jsQR = require('jsqr'), { PNG } = require('pngjs')
const filename = path.resolve(__dirname, '../admin/src/batch-qr.ts')
const moduleContext = { exports: {} }
const source = ts.transpileModule(fs.readFileSync(filename, 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, esModuleInterop: true } }).outputText
vm.runInNewContext(source, { module: moduleContext, exports: moduleContext.exports, require })
const { generateBatchQR } = moduleContext.exports
;(async () => {
  const batch = { id: 'BAT-test-001', externalTrace: { url: 'https://trace.example.com/batch/001?code=EXT-01' } }
  for (const kind of ['internal', 'external']) {
    const image = await generateBatchQR(batch, kind)
    const png = PNG.sync.read(Buffer.from(image.split(',')[1], 'base64'))
    assert.equal(png.width, 640)
    const decoded = jsQR(new Uint8ClampedArray(png.data), png.width, png.height)
    assert.ok(decoded, 'generated PNG must decode')
    assert.equal(decoded.data, kind === 'internal' ? batch.id : batch.externalTrace.url)
  }
  await assert.rejects(() => generateBatchQR({ id: 'batch' }, 'external'), /溯源内容/)
  console.log('Batch QR passed: internal/external PNG decode and missing link errors.')
})().catch(e => { console.error(e); process.exitCode = 1 })
