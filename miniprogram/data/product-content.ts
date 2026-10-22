import type { Review, Product } from '../utils/types'

export const demoProductContent: Record<string, Pick<Product, 'variety' | 'storage' | 'netWeightNote' | 'traceability' | 'giftCustomization' | 'pairings'>> = Object.fromEntries([
  ['p-shine', '青铜峡合作果园', 'DEMO-SHINE-2026', 'p-kyoho', '巨峰葡萄'],
  ['p-kyoho', '北镇合作果园', 'DEMO-KYOHO-2026', 'p-shine', '阳光玫瑰'],
  ['p-gift', '迟记合作果园', 'DEMO-GIFT-2026', 'p-shine', '阳光玫瑰'],
].map(([id, orchard, batch, productId, name]) => [id, {
  variety: id === 'p-shine' ? '阳光玫瑰' : id === 'p-kyoho' ? '巨峰' : '阳光玫瑰 + 巨峰',
  storage: '示例保存说明：收到后开箱检查，未清洗的果串冷藏保存，避免挤压；食用前清洗，按实际果况尽快食用。',
  netWeightNote: id === 'p-gift' ? '示例规格：两种葡萄各约 1.8kg，合计净重约 3.6kg；不含礼盒、内托及祝福卡。' : '示例净重口径：含果梗，不含外箱、内托及保温材料；重量以所选规格为准。',
  traceability: { orchard: `${orchard}（示例）`, batch: `${batch}-1003`, code: `${batch}-TRACE-1003`, harvestedAt: id === 'p-gift' ? '' : '2026-10-03', packedAt: id === 'p-gift' ? '' : '2026-10-03', packing: id === 'p-gift' ? '双品种独立内托、防震礼盒、提手与祝福卡（示例）' : '果串分选、防震内托、吸水垫、保温外箱，预冷后装箱（示例）', demo: true,
    steps: [{ title: '果园种植', detail: `${orchard}示例地块，记录品种与田间管理。` }, { title: '成熟采收', detail: id === 'p-gift' ? '预售示例：成熟后分批采收，实际日期待确认。' : `示例采收日期 2026-10-03，关联批次 ${batch}-1003。` }, { title: '分选包装', detail: id === 'p-gift' ? '双品种分别分选、称重后放入礼盒内托（示例）。' : '检查果串完整度，称重并完成防震包装（示例）。' }, { title: '产地发出', detail: '出库核对批次与包装（示例），具体物流以订单为准。' }] },
  ...(id === 'p-gift' ? { giftCustomization: { enabled: true, description: '示例定制服务：适用于节日赠礼、企业福利及团购，可沟通包装与祝福卡方案。实际项目与费用由商家确认。', options: ['祝福卡片（示例）', '企业标识（示例）', '礼盒包装（示例）'], minimumQuantity: 20, leadTime: '示例周期：方案确认后约 7–10 天，实际以商家确认及产期为准。' } } : {}),
  pairings: [{ title: '早餐酸奶碗', description: '洗净后切半，按需去皮去籽，搭配原味酸奶与燕麦。' }, { title: `搭配${name}`, description: '分享两种当季风味，也可以分别装盘品尝。', productId }],
}]))

export const demoReviews: Review[] = [
  { id: 'demo-review-1', productId: 'p-shine', rating: 5, content: '示例评价：果肉清脆，搭配酸奶很合适。', createdAt: '2026-09-28T08:00:00Z', demo: true },
  { id: 'demo-review-2', productId: 'p-kyoho', rating: 4, content: '示例评价：葡萄香浓郁，果汁丰富。', createdAt: '2026-09-28T09:00:00Z', demo: true },
]
