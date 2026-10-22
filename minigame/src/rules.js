'use strict';

// Playable teaching parameters, not a calibrated recommendation for a real cultivar.
const RULES = {
  version: 'v1', seasonDays: 84, offlineDayMs: 3600000, advanceDays: 7,
  initialCoins: 18000, maxHistory: 120,
  stages: [
    { id: 'dormant', name: '冬剪准备', start: 0, end: 6 },
    { id: 'budbreak', name: '萌芽展叶', start: 7, end: 20 },
    { id: 'flowering', name: '开花坐果', start: 21, end: 34 },
    { id: 'enlargement', name: '果粒膨大', start: 35, end: 55 },
    { id: 'ripening', name: '转色成熟', start: 56, end: 69 },
    { id: 'harvest', name: '采收销售', start: 70, end: 83 },
    { id: 'closed', name: '本季结束', start: 84, end: 84 }
  ],
  facilities: {
    drip: { name: '滴灌系统', cost: 4200, dailyCost: 36 },
    shelter: { name: '避雨设施', cost: 6500, dailyCost: 12 },
    coldStorage: { name: '小型冷库', cost: 4800, dailyCost: 18 }
  },
  prices: { wholesale: { A: 66, B: 43, C: 23 }, retail: { A: 91, B: 60, C: 32 } },
  channelCost: { wholesale: 2, retail: 7 },
  minHarvestBrix: 15, plantUnits: 9500,
  note: '本版基于农艺因果关系构建，参数用于游戏体验，尚未按具体产区与品种校准。'
};

const ACTIONS = {
  water: { name: '灌溉', cost: 70, start: 0, end: 83, cooldown: 1 },
  fertilize: { name: '施肥', cost: 140, start: 0, end: 55, cooldown: 7 },
  prune: { name: '整枝通风', cost: 110, start: 0, end: 55, cooldown: 7 },
  thin: { name: '疏果控产', cost: 130, start: 21, end: 55, cooldown: 7 },
  protect: { name: '巡园防护', cost: 120, start: 21, end: 83, cooldown: 7 },
  harvest: { name: '采收', cost: 220, start: 56, end: 83, cooldown: 0 },
  sell: { name: '销售' }, upgrade: { name: '建设设施' },
  advance: { name: '推进 7 天' }, newSeason: { name: '开启下一季' }
};

module.exports = { RULES, ACTIONS };
