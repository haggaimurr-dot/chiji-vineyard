interface IAppOption {
  globalData: {
    demoMode: boolean
    cloudEnv: string
    cloudService: string
    user?: import('../utils/types').UserProfile
  }
}

