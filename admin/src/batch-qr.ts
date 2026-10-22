import QRCode from 'qrcode'
import type { ManagedBatch } from './product-management'

export async function generateBatchQR(batch: ManagedBatch, kind: 'internal'|'external'): Promise<string> {
  const value = kind === 'external' ? batch.externalTrace?.url : batch.code || batch.id
  if (!value) throw new Error('该批次尚未配置溯源内容')
  return QRCode.toDataURL(value, { width: 640, margin: 4, errorCorrectionLevel: 'M' })
}
