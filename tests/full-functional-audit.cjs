const fs = require('node:fs')
const path = require('node:path')
const { spawnSync } = require('node:child_process')
const root = path.resolve(__dirname, '..')
const output = path.join(root, 'docs', 'test-evidence', '2026-10-09-full-audit')
fs.mkdirSync(output, { recursive: true })
const results = []
function run(name, command, args, cwd = root) {
  const startedAt = new Date().toISOString()
  const start = Date.now()
  const result = spawnSync(command, args, {
    cwd, encoding: 'utf8', windowsHide: true, timeout: 120000,
    maxBuffer: 16 * 1024 * 1024,
    env: { ...process.env, ADMIN_UI_URL: 'http://127.0.0.1:5186' },
  })
  const log = `${result.stdout || ''}\n${result.stderr || ''}\n${result.error || ''}`
  fs.writeFileSync(path.join(output, name + '.log'), log)
  const entry = { name, startedAt, durationMs: Date.now() - start,
    status: result.status === 0 && !result.error ? 'PASS' : 'FAIL',
    exitCode: result.status, error: result.error?.message || '', log: name + '.log' }
  results.push(entry)
  fs.writeFileSync(path.join(output, 'results.json'), JSON.stringify(results, null, 2))
  console.log(`${entry.status} ${name} (${entry.durationMs}ms)`)
}
const files = fs.readdirSync(__dirname).filter(file => file.endsWith('.cjs') && file !== path.basename(__filename)).sort()
for (const file of files) run(file.slice(0, -4), process.execPath, [path.join(__dirname, file)])
run('go-test', 'go', ['test', '-count=1', '-json', './...'], path.join(root, 'backend'))
console.log(JSON.stringify({ total: results.length, passed: results.filter(x => x.status === 'PASS').length, failed: results.filter(x => x.status === 'FAIL').length }))
process.exitCode = results.some(x => x.status === 'FAIL') ? 1 : 0
