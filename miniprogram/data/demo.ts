import { demoProductContent } from './product-content'
import type { Article, Product } from '../utils/types'

const grapeSvg = (title: string, color: string) =>
  `data:image/svg+xml,${encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" width="900" height="700"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop stop-color="${color}"/><stop offset="1" stop-color="#efe2c3"/></linearGradient></defs><rect width="900" height="700" fill="url(#g)"/><circle cx="650" cy="210" r="105" fill="#ffffff" opacity=".14"/><circle cx="720" cy="300" r="82" fill="#fff" opacity=".12"/><path d="M520 90c70 30 110 80 130 150" fill="none" stroke="#31583f" stroke-width="20" stroke-linecap="round"/><text x="60" y="560" fill="#fff" font-size="72" font-family="sans-serif" font-weight="700">${title}</text><text x="64" y="620" fill="#fff" opacity=".85" font-size="28" font-family="sans-serif">迟记葡萄牙 · 产地鲜采</text></svg>`)}`

export const demoProducts: Product[] = [
  {
    ...demoProductContent['p-shine'],
    id: 'p-shine', name: '阳光玫瑰', subtitle: '清甜玫瑰香，脆嫩无籽', category: 'family', saleMode: 'spot',
    cover: '/assets/products/shine.jpg', gallery: [], imageNote: '商品示意图', badges: ['当季鲜采', '家庭装'], origin: '宁夏青铜峡',
    description: '清晨采摘，当日预冷发出。每一串经过糖度、果粉和完整度筛选。',
    highlights: ['自然成熟', '冷链包装', '坏果包赔'],
    skus: [
      { id: 's-shine-2', sold: 268, name: '家庭装', price: 8990, compareAtPrice: 10990, stock: 86, limitPerOrder: 5, weightLabel: '净重约2kg' },
      { id: 's-shine-4', sold: 95, name: '分享装', price: 15990, stock: 42, limitPerOrder: 3, weightLabel: '净重约4kg' },
    ],
  },
  {
    ...demoProductContent['p-kyoho'],
    id: 'p-kyoho', name: '巨峰葡萄', subtitle: '浓郁多汁，经典葡萄香', category: 'family', saleMode: 'spot',
    cover: '/assets/products/kyoho.jpg', gallery: [], imageNote: '商品示意图', badges: ['人气款', '顺丰优先'], origin: '辽宁北镇',
    description: '果肉柔软多汁，酸甜平衡，适合一家人的日常水果篮。',
    highlights: ['藤上熟', '单串套袋', '产地直发'],
    skus: [{ id: 's-kyoho-25', sold: 520, name: '家庭装', price: 6990, stock: 120, limitPerOrder: 6, weightLabel: '净重约2.5kg' }],
  },
  {
    ...demoProductContent['p-gift'],
    id: 'p-gift', name: '双拼葡萄礼盒', subtitle: '两种风味，一份体面心意', category: 'gift', saleMode: 'presale', expectedShipDate: '2026-09-28',
    cover: '/assets/products/gift.jpg', gallery: [], imageNote: '商品示意图', badges: ['节礼装', '预约鲜采'], origin: '迟记合作果园',
    description: '阳光玫瑰与巨峰双拼，礼盒、防震内托和祝福卡一次配齐。',
    highlights: ['礼盒包装', '预约发货', '企业团购'],
    skus: [{ id: 's-gift-36', sold: 128, name: '臻选礼盒', price: 19900, compareAtPrice: 22900, stock: 30, limitPerOrder: 5, weightLabel: '净重约3.6kg' }],
  },
]

export const demoArticles: Article[] = [
  {
    id: 'a-prune', title: '秋季修剪：来年结果枝怎么留', summary: '从枝条成熟度、芽眼位置到留枝长度，一次讲清。',
    cover: grapeSvg('秋季修剪', '#5b7145'), variety: '通用', stage: '采后管理', type: 'article', publishedAt: '2026-09-12',
    content: ['采收结束并不意味着管理结束。叶片仍在为枝条成熟积累养分。', '先去除病弱枝和交叉枝，再根据架形保留结果母枝。剪口保持平整，并在晴朗天气完成。', '修剪后同步清园，记录今年产量与病害，为来年负载量提供依据。'],
  },
  {
    id: 'a-sugar', title: '葡萄转色期，糖度管理的三个关键', summary: '水分、叶果比和昼夜温差，如何共同影响风味。',
    cover: grapeSvg('糖度管理', '#71405f'), variety: '阳光玫瑰', stage: '转色成熟', type: 'video', duration: '04:36', publishedAt: '2026-09-08',
    content: ['转色期管理的目标不是单纯追求高糖，而是让香气、酸度和果肉质地同步成熟。', '控制无效旺长，保持合理叶果比；临近采收避免忽干忽湿；通过分批测糖决定采收顺序。'],
  },
  {
    id: 'a-disease', title: '雨后巡园：霜霉病早期识别', summary: '看叶背、看新梢、看天气，尽早发现风险。',
    cover: grapeSvg('雨后巡园', '#3c6b59'), variety: '巨峰', stage: '病虫防治', type: 'article', publishedAt: '2026-08-30',
    content: ['连续阴雨后应缩短巡园间隔，重点观察叶背是否出现白色霉层。', '发现疑似叶片后及时标记区域、改善通风并咨询当地植保人员。具体用药遵循登记范围、标签说明和安全间隔期。'],
  },
]

