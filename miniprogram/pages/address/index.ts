import { callApi } from '../../utils/api'
import { parseAddress } from '../../utils/address-parse'
import type { Address } from '../../utils/types'
const emptyForm = (): Address => ({ name: '', phone: '', province: '', city: '', district: '', detail: '', isDefault: false })
Page({
  data: { addresses: [] as Address[], form: emptyForm(), region: [] as string[], editing: false, selecting: false, selectedId: '', loading: true, error: '', busy: false },
  onLoad(options: Record<string, string>) { this.setData({ selecting: options.select === '1', selectedId: options.id || '' }); this.loadAddresses() },
  async loadAddresses() {
    this.setData({ loading: true, error: '' })
    try { this.setData({ addresses: await callApi<Address[]>('address.list') }) }
    catch (error) { this.setData({ error: (error as Error).message }) }
    finally { this.setData({ loading: false }) }
  },
  privacy() { wx.navigateTo({ url: '/pages/privacy/index' }) },
  personalData() { wx.navigateTo({ url: '/pages/personal-data/index' }) },
  onShow() { this.setData({ editing: false, form: emptyForm(), region: [] }); this.loadAddresses() },
  add() { if (!this.data.busy) this.setData({ editing: true, form: { ...emptyForm(), isDefault: !this.data.addresses.length }, region: [] }) },
  edit(event: any) {
    if (this.data.busy) return
    const form = this.data.addresses.find(a => a.id === event.currentTarget.dataset.id)
    if (form) this.setData({ editing: true, form: { ...form }, region: [form.province, form.city, form.district] })
  },
  cancel() { if (!this.data.busy) this.setData({ editing: false }) },
  field(event: WechatMiniprogram.Input) { this.setData({ [`form.${event.currentTarget.dataset.name}`]: event.detail.value }) },
  chooseRegion(event: any) { const [province, city, district] = event.detail.value; this.setData({ region: event.detail.value, 'form.province': province, 'form.city': city, 'form.district': district }) },
  toggleDefault(event: any) { this.setData({ 'form.isDefault': event.detail.value }) },
  // 粘贴智能识别：从剪贴板读取"姓名+手机+省市区+详细"文本并自动拆分填入
  async smartPaste() {
    try {
      const { data } = await wx.getClipboardData()
      const text = String(data || '').trim()
      if (!text) return wx.showToast({ title: '剪贴板是空的', icon: 'none' })
      const parsed = parseAddress(text)
      if (!parsed.phone && !parsed.province && !parsed.detail) return wx.showToast({ title: '没识别出地址信息', icon: 'none' })
      const form = { ...this.data.form }
      if (parsed.name) form.name = parsed.name
      if (parsed.phone) form.phone = parsed.phone
      if (parsed.province) form.province = parsed.province
      if (parsed.city) form.city = parsed.city
      if (parsed.district) form.district = parsed.district
      if (parsed.detail) form.detail = parsed.detail
      this.setData({ form, region: [form.province, form.city, form.district] })
      wx.showToast({ title: '已智能填入，请核对', icon: 'none' })
    } catch (_) { wx.showToast({ title: '读取剪贴板失败', icon: 'none' }) }
  },
  select(event: any) {
    if (!this.data.selecting || this.data.busy) return
    const address = this.data.addresses.find(a => a.id === event.currentTarget.dataset.id)
    if (!address) return
    this.getOpenerEventChannel?.().emit?.('addressSelected', address)
    wx.navigateBack()
  },
  async save() {
    if (this.data.busy) return
    const form = { ...this.data.form, name: this.data.form.name.trim(), phone: this.data.form.phone.trim(), detail: this.data.form.detail.trim() }
    if (!form.name || !/^1\d{10}$/.test(form.phone) || !form.province || !form.city || !form.detail) return wx.showToast({ title: '请填写完整地址和正确手机号', icon: 'none' })
    this.setData({ busy: true })
    try {
      await callApi<Address>('address.save', { address: form })
      this.setData({ editing: false }); await this.loadAddresses()
      wx.showToast({ title: '已保存' })
    } catch (error) { wx.showToast({ title: (error as Error).message, icon: 'none' }) }
    finally { this.setData({ busy: false }) }
  },
  async setDefault(event: any) { await this.mutate('address.default', event.currentTarget.dataset.id) },
  remove(event: any) {
    if (this.data.busy) return
    wx.showModal({ title: '删除地址', content: '确认删除此收货地址？历史订单的配送信息会保留。', success: result => { if (result.confirm) this.mutate('address.delete', event.currentTarget.dataset.id) } })
  },
  async mutate(action: string, id: string) {
    if (this.data.busy) return
    this.setData({ busy: true })
    try { this.setData({ addresses: await callApi<Address[]>(action, { id }) }) }
    catch (error) { wx.showToast({ title: (error as Error).message, icon: 'none' }) }
    finally { this.setData({ busy: false }) }
  },
  async importWechat() {
    if (this.data.busy) return
    this.setData({ busy: true })
    try {
      if (wx.requirePrivacyAuthorize) await new Promise<void>((resolve, reject) => wx.requirePrivacyAuthorize({ success: () => resolve(), fail: reject }))
      const imported = await new Promise<WechatMiniprogram.ChooseAddressSuccessCallbackResult>((resolve, reject) => wx.chooseAddress({ success: resolve, fail: reject }))
      this.setData({ editing: true, form: { ...emptyForm(), name: imported.userName, phone: imported.telNumber.replace(/^\+?86[\s-]*/, '').replace(/[\s-]/g, ''), province: imported.provinceName, city: imported.cityName, district: imported.countyName, detail: imported.detailInfo, isDefault: !this.data.addresses.length }, region: [imported.provinceName, imported.cityName, imported.countyName] })
    } catch (error) {
      const message = (error as any).errMsg || (error as Error).message || ''
      if (!/cancel/.test(message)) {
        if (/auth deny|auth denied|authorize/.test(message)) wx.showModal({ title: '未获地址授权', content: '可在微信设置中允许收货地址访问，或手动填写。', confirmText: '去设置', success: result => { if (result.confirm) wx.openSetting({}) } })
        else wx.showToast({ title: '暂时无法导入，请手动填写', icon: 'none' })
      }
    } finally { this.setData({ busy: false }) }
  },
})
