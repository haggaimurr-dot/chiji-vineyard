const runs: Record<string, unknown>[] = []
const financeRuns: Record<string, unknown>[] = []
const alerts = [{ id: 'demo-payment-alert', kind: 'BILL_DIFFERENCE', target: 'DEMO-CJ001', message: '演示：微信已支付，本地订单未入账', status: 'OPEN', occurrences: 1, lastSeen: new Date().toISOString(), note: '' }]

export function paymentDemo(action: string, data: { billDate?: string; id?: string; note?: string; kind?: string }) {
  if (action === 'admin.payment.alertCount') return { count: alerts.filter(a => a.status === 'OPEN').length }
  if (action === 'admin.payment.overview') return structuredClone({ runs, financeRuns, alerts, openAlerts: alerts.filter(a => a.status === 'OPEN').length })
  if (action === 'admin.payment.resolve') {
    const alert = alerts.find(a => a.id === data.id && a.status === 'OPEN')
    if (!alert) throw new Error('告警不存在或已处理')
    const note = String(data.note || '').trim()
    if (!note || note.length > 240) throw new Error('请填写不超过240字的告警处理备注')
    alert.status = 'RESOLVED'; alert.note = note
    return { resolved: true }
  }
  if (action === 'admin.payment.reconcile' || action === 'admin.payment.financeReconcile') {
    const date = data.billDate || ''
    const today = new Date().toLocaleDateString('en-CA', { timeZone: 'Asia/Shanghai' })
    const start = new Date(`${today}T00:00:00Z`); start.setUTCMonth(start.getUTCMonth() - 3)
    if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || !Number.isFinite(Date.parse(date)) || new Date(date).toISOString().slice(0, 10) !== date || date >= today || Date.parse(date) < start.getTime()) throw new Error('请选择近三个月内、今天之前的账单日期')
    if (action === 'admin.payment.financeReconcile') {
      const kind = data.kind || ''
      if (!['REFUND', 'BASIC', 'FEES', 'OPERATION'].includes(kind)) throw new Error('请选择退款账单或资金账户')
      const refund = kind === 'REFUND'
      const totals = refund
        ? { refundRequested: 8990, refundSettled: 8990, refundSuccessful: 1, feesCharged: 0, feesReturned: 54, netFees: -54, income: 0, expense: 0, net: 0, settlementGross: 0, settlementRefund: 0, settlementNet: 0 }
        : kind === 'BASIC'
          ? { income: 26970, expense: 9152, net: 17818, feesCharged: 162, feesReturned: 54, netFees: 108, settlementGross: 26916, settlementRefund: 8990, settlementNet: 17818 }
          : { income: 0, expense: 0, net: 0, feesCharged: 0, feesReturned: 0, netFees: 0, settlementGross: 0, settlementRefund: 0, settlementNet: 0 }
      const run = { billDate: date, kind, status: refund ? 'MATCHED' : 'COMPLETED', checked: refund ? 1 : kind === 'BASIC' ? 6 : 0, differences: 0, totals, details: [], finishedAt: new Date().toISOString(), message: '模拟账单处理完成' }
      const index = financeRuns.findIndex(r => r.billDate === date && r.kind === kind)
      if (index >= 0) financeRuns.splice(index, 1)
      financeRuns.unshift(run); financeRuns.sort((a,b) => String(b.billDate).localeCompare(String(a.billDate))); financeRuns.splice(124)
      return run
    }
    const run = { billDate: date, status: 'MATCHED', checked: 3, differences: 0, finishedAt: new Date().toISOString(), message: '模拟对账完成' }
    const index = runs.findIndex(r => r.billDate === date)
    if (index >= 0) runs.splice(index, 1)
    runs.unshift(run); runs.sort((a, b) => String(b.billDate).localeCompare(String(a.billDate))); runs.splice(31)
    return run
  }
  throw new Error('未知支付操作')
}
