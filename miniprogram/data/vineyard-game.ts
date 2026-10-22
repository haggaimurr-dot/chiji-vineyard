export interface VineyardGameConfig {
  appId: string
  envVersion: 'release' | 'develop' | 'trial'
}

// 填写独立微信小游戏的 AppID。发布正式版后使用 release，联调时可用 develop 或 trial。
// 这里的配置独立于商城的云环境；空 AppID 会保留入口并显示“暂未开放”。
export const vineyardGameConfig: VineyardGameConfig = {
  appId: '',
  envVersion: 'release',
}
