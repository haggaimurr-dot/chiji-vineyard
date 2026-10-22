import { vineyardGameConfig } from '../data/vineyard-game'

export type VineyardEntry = 'home' | 'profile'
export type VineyardEntryResult = 'opened' | 'unavailable' | 'cancelled' | 'failed' | 'busy'

// 两个页面共用一把锁，微信跳转确认和失败重试期间都不能重复打开。
let opening = false

export function openVineyardGame(entry: VineyardEntry): Promise<VineyardEntryResult> {
  if (opening) return Promise.resolve('busy')
  const appId = vineyardGameConfig.appId.trim()
  const envVersion = vineyardGameConfig.envVersion
  if (!appId) {
    wx.showToast({ title: '葡萄园小游戏正在准备，暂未开放', icon: 'none', duration: 2500 })
    return Promise.resolve('unavailable')
  }
  if (typeof wx.navigateToMiniProgram !== 'function') {
    wx.showToast({ title: '请更新微信后再进入葡萄园', icon: 'none' })
    return Promise.resolve('unavailable')
  }

  opening = true
  return new Promise(resolve => {
    let finished = false
    const finish = (result: VineyardEntryResult) => {
      if (finished) return
      finished = true
      opening = false
      resolve(result)
    }
    const navigate = () => {
      let settled = false
      const fail = (error: { errMsg?: string }) => {
        if (settled || finished) return
        settled = true
        if (/cancel/i.test(error?.errMsg || '')) {
          wx.showToast({ title: '已取消，随时可以再来种葡萄', icon: 'none' })
          finish('cancelled')
          return
        }
        try {
          wx.showModal({
            title: '暂时无法进入葡萄园',
            content: '请确认微信版本和网络后重试。',
            confirmText: '重试',
            cancelText: '稍后再说',
            success: response => {
              if (finished) return
              if (response.confirm) navigate()
              else finish('failed')
            },
            fail: () => finish('failed'),
          })
        } catch (_) {
          wx.showToast({ title: '暂时无法进入，请稍后重试', icon: 'none' })
          finish('failed')
        }
      }
      try {
        wx.navigateToMiniProgram({
          appId,
          envVersion,
          // 只传递入口来源，小游戏使用自己的登录；不传商城用户信息或令牌。
          extraData: { source: 'chiji-grape', entry, schemaVersion: 1 },
          success: () => {
            if (settled || finished) return
            settled = true
            finish('opened')
          },
          fail,
        })
      } catch (_) {
        fail({ errMsg: 'navigateToMiniProgram:fail' })
      }
    }
    navigate()
  })
}
