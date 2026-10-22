const DAY = 24 * 60 * 60 * 1000
const BEIJING_OFFSET = 8 * 60 * 60 * 1000

export function beijingDate(value: string | number | Date = new Date()): string {
  const date = new Date(value)
  return Number.isFinite(date.getTime()) ? new Date(date.getTime() + BEIJING_OFFSET).toISOString().slice(0, 10) : ''
}

export function beijingInput(value?: string): string {
  if (!value) return ''
  const date = new Date(value)
  return Number.isFinite(date.getTime()) ? new Date(date.getTime() + BEIJING_OFFSET).toISOString().slice(0, 16) : ''
}

export function scheduleISO(value: string): string {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(value)) throw new Error('请选择完整的定时发布时间')
  const date = new Date(`${value}:00+08:00`)
  if (!Number.isFinite(date.getTime()) || beijingInput(date.toISOString()) !== value) throw new Error('定时发布时间无效')
  if (date.getTime() <= Date.now()) throw new Error('定时发布时间必须晚于当前时间（北京时间）')
  return date.toISOString()
}

export function publicationDate(value?: string): string {
  if (!value) return ''
  if (/^\d{4}-\d{2}-\d{2}$/.test(value)) {
    const date = new Date(`${value}T00:00:00+08:00`)
    return Number.isFinite(date.getTime()) && beijingDate(date) === value ? value : ''
  }
  return beijingDate(value)
}

export function recentBeijingDates(): string[] {
  const today = new Date(`${beijingDate()}T00:00:00+08:00`).getTime()
  return Array.from({ length: 14 }, (_, index) => beijingDate(today - (13 - index) * DAY))
}
