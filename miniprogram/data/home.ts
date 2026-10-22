import { mockService } from './service'
import type { HomeConfig } from '../utils/types'
export const demoHomeSections = {
 customerService: mockService,
 activities: [{ id: 'demo-autumn-2026', reservationEnabled: true, title: '秋日鲜果分享季（示例）', summary: '和家人分享一串秋天的甜', content: '活动内容示例：查看当季葡萄，按商品页面价格选购。本示例不提供额外折扣。', startDate: '2026-10-01', endDate: '2026-10-31', productId: 'p-shine' }],
 announcements: [{ title: '鲜果签收与保存提醒（示例）', content: '收到鲜果后请及时开箱检查，并冷藏保存。若发现坏果，请保留外箱、面单及果品照片，通过商品详情页客服入口联系商家。', publishedAt: '2026-10-01' }],
 launchCalendar: [
  { variety: '阳光玫瑰', startDate: '2026-08-01', endDate: '2026-10-31', note: '示例产期，实际供应以商品页面为准', productId: 'p-shine' },
  { variety: '巨峰葡萄', startDate: '2026-07-01', endDate: '2026-09-30', note: '示例产期，成熟时间随产地和天气变化', productId: 'p-kyoho' },
 ],
} satisfies Pick<HomeConfig, 'activities' | 'announcements' | 'launchCalendar' | 'customerService'>
export const defaultHome: HomeConfig = { banners:[{title:'把枝头的好味道，送到一家人的餐桌',copy:'当季鲜果 · 按产期发货',image:'',button:'选购当季葡萄',target:'shop',targetId:''}],productIds:[],articleIds:[],storyTitle:'一串葡萄，慢慢长成',storyCopy:'了解葡萄的种植过程与成熟时间。',cooperationTitle:'团购、批发、经销合作',cooperationCopy:'按产期和渠道沟通采购需求'}
// Packaged illustrations are used only in demo mode; formal content comes from the backend.
export const demoHome: HomeConfig = {
 ...defaultHome, ...demoHomeSections,
 banners: [
  { title: '把一串葡萄，慢慢种好。', copy: '种植、照料、采摘，是果园里平常的日子。', image: '/assets/home/harvest-scene.jpg', button: '看看当季葡萄', target: 'shop', targetId: '' },
  { title: '两种风味，一份分享的心意', copy: '双拼葡萄礼盒 · 预约鲜采', image: '/assets/products/gift.jpg', button: '了解团购合作', target: 'cooperation', targetId: '' },
 ],
 storyTitle: '把一串葡萄，慢慢种好。',
 storyCopy: '种植、照料、采摘，是果园里平常的日子。\n在这里，看看葡萄从枝头到你手里的过程。',
}
