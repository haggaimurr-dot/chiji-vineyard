// Isolate production coupon sources while unrelated backend modules are incomplete.
const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')
const { spawnSync } = require('node:child_process')
const root = path.resolve(__dirname, '..', 'backend')
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'chiji-coupons-'))
function declaration(source, prefix) {
  const start = source.indexOf(prefix)
  if (start < 0) throw new Error(`Missing declaration: ${prefix}`)
  const tail = source.slice(start)
  const next = /\n(?:func |type |var )/.exec(tail)
  return next ? tail.slice(0, next.index) : tail
}
try {
  for (const file of ['coupon_wallet.go', 'coupon_wallet_test.go', 'coupon_wallet_storage_test.go']) fs.copyFileSync(path.join(root, file), path.join(temp, file))
  const options = fs.readFileSync(path.join(root, 'checkout_options.go'), 'utf8')
  const model = fs.readFileSync(path.join(root, 'model.go'), 'utf8')
  const source = `package main
import("context";"database/sql";"encoding/json";"errors";"regexp";"strings";"time")
type Store struct{ db *sql.DB }
type Server struct{ store *Store }
type Quote struct{ Subtotal,Freight,Discount,Total int; CouponCode string }
${declaration(options, 'type Coupon struct')}
${declaration(options, 'type CouponConfig struct')}
${declaration(options, 'var couponRE =')}
${declaration(options, 'func chinaDay(')}
${declaration(options, 'func discountCoupon(')}
${declaration(options, 'func (s *Server) couponQuote(')}
${declaration(model, 'func decode(')}
`
  fs.writeFileSync(path.join(temp, 'dependencies.go'), source)
  const result = spawnSync('go', ['test', '-v', '.'], { cwd: temp, env: { ...process.env, GO111MODULE: 'off' }, encoding: 'utf8' })
  process.stdout.write(result.stdout || ''); process.stderr.write(result.stderr || '')
  if (result.error) throw result.error
  process.exitCode = result.status || 0
} finally {
  const relative = path.relative(path.resolve(os.tmpdir()), path.resolve(temp))
  if (!relative || relative.startsWith('..') || path.isAbsolute(relative) || !path.basename(temp).startsWith('chiji-coupons-')) throw new Error('Unsafe temporary directory')
  fs.rmSync(temp, { recursive: true, force: true })
}
