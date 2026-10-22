const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
export function newTotpSecret() {
  const bytes = crypto.getRandomValues(new Uint8Array(20))
  let bits = 0, value = 0, result = ''
  for (const byte of bytes) {
    value = (value << 8) | byte; bits += 8
    while (bits >= 5) { bits -= 5; result += alphabet[(value >>> bits) & 31] }
  }
  return result
}
export async function totpCode(secret: string, step: number) {
  let bits = 0, value = 0
  const bytes: number[] = []
  for (const char of secret) {
    const digit = alphabet.indexOf(char)
    if (digit < 0) throw new Error('验证器密钥无效')
    value = (value << 5) | digit; bits += 5
    if (bits >= 8) { bits -= 8; bytes.push((value >>> bits) & 255) }
  }
  const key = await crypto.subtle.importKey('raw', new Uint8Array(bytes), { name: 'HMAC', hash: 'SHA-1' }, false, ['sign'])
  const counter = new Uint8Array(8)
  let count = BigInt(step)
  for (let i = 7; i >= 0; i--) { counter[i] = Number(count & 255n); count >>= 8n }
  const sum = new Uint8Array(await crypto.subtle.sign('HMAC', key, counter))
  const offset = sum[sum.length - 1] & 15
  const number = ((sum[offset] & 127) * 16777216 + sum[offset + 1] * 65536 + sum[offset + 2] * 256 + sum[offset + 3]) % 1000000
  return String(number).padStart(6, '0')
}
export async function matchTotp(secret: string, code: string, last = -1) {
  if (!/^\d{6}$/.test(code)) return -1
  const now = Math.floor(Date.now() / 30000)
  for (const step of [now, now - 1, now + 1]) if (step > last && await totpCode(secret, step) === code) return step
  return -1
}
