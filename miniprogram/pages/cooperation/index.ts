import { callApi } from '../../utils/api'
import type { Product } from '../../utils/types'

Page({
  privacy() { wx.navigateTo({url:'/pages/privacy/index'}) },
  data: {
    giftProductName: '', giftLoading: false, giftError: '', giftProductId: '',
    channels: ['社区团购', '商超/水果店', '企业福利', '电商渠道', '经销代理'], channelIndex: 0,
    form: { organization: '', contact: '', phone: '', region: '', volume: '', demand: '' },
    agreed: false, submitting: false, success: false,
  },
  onLoad(options: Record<string, string>) {
    if (!options.giftProductId) return
    this.setData({ giftProductId: options.giftProductId })
    return this.loadGiftProduct()
  },
  async loadGiftProduct() {
    this.setData({ giftLoading: true, giftError: '' })
    try {
      const product = await callApi<Product>('product.detail', { id: this.data.giftProductId })
      if (!product?.giftCustomization?.enabled) throw new Error('此商品暂未开放礼盒定制，请联系客服')
      const sku = product.skus[0]
      this.setData({ giftProductName: product.name, channelIndex: 2,
        'form.volume': `${product.giftCustomization.minimumQuantity} 盒`,
        'form.demand': `礼盒定制：${product.name}（${product.id}）${sku ? `；参考规格：${sku.name}` : ''}；请补充包装、祝福卡及期望收货时间。`.slice(0, 300) })
      wx.setNavigationBarTitle({ title: '礼盒定制需求' })
    } catch (error) { this.setData({ giftError: (error as Error).message || '商品信息加载失败，请重试' }) }
    finally { this.setData({ giftLoading: false }) }
  },
  field(event: WechatMiniprogram.Input) { this.setData({ [`form.${event.currentTarget.dataset.name}`]: event.detail.value }) },
  chooseChannel(event: WechatMiniprogram.PickerChange) { this.setData({ channelIndex: Number(event.detail.value) }) },
  toggleAgree() { this.setData({ agreed: !this.data.agreed }) },
  async submit() {
    if (this.data.submitting || this.data.success || this.data.giftLoading || this.data.giftError) return
    const form = Object.fromEntries(Object.entries(this.data.form).map(([key, value]) => [key, value.trim()])) as typeof this.data.form
    if (!form.organization || !form.contact || !/^1\d{10}$/.test(form.phone) || !form.region || !form.volume) {
      return wx.showToast({ title: '请完整填写必填信息', icon: 'none' })
    }
    if (!this.data.agreed) return wx.showToast({ title: '请先同意隐私政策', icon: 'none' })
    this.setData({ submitting: true })
    try {
      await callApi('lead.submit', { ...form, channel: this.data.channels[this.data.channelIndex], consent: true })
      this.setData({ success: true })
    } catch (error) { wx.showToast({ title: (error as Error).message, icon: 'none' }) }
    finally { this.setData({ submitting: false }) }
  },
  again() {
    this.setData({ success: false, form: { organization: '', contact: '', phone: '', region: '', volume: '', demand: '' }, agreed: false })
    if (this.data.giftProductId) return this.loadGiftProduct()
  },
})

