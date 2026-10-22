'use strict';

const { RULES, ACTIONS } = require('./rules');

class GameError extends Error {
  constructor(code, message) { super(message); this.name = 'GameError'; this.code = code; }
}
const fail = (code, message) => { throw new GameError(code, message); };
const clone = value => JSON.parse(JSON.stringify(value));
const clamp = (value, min, max) => Math.max(min, Math.min(max, value));
const round = value => Math.round((value + Number.EPSILON) * 100) / 100;
const num = (value, min, max) => typeof value === 'number' && Number.isFinite(value) && value >= min && value <= max;
const int = (value, min, max) => num(value, min, max) && Number.isInteger(value);
const str = (value, max) => typeof value === 'string' && value.length > 0 && value.length <= max;

function getStage(day) {
  if (!int(day, 0, RULES.seasonDays)) fail('INVALID_DAY', '模拟日期无效');
  return Object.assign({}, RULES.stages.find(stage => day >= stage.start && day <= stage.end));
}

function getWeather(seed, day) {
  if (!int(seed, 0, 4294967295) || !int(day, 0, RULES.seasonDays)) fail('INVALID_WEATHER', '天气参数无效');
  let x = (seed ^ Math.imul(day + 1, 0x9E3779B1)) >>> 0;
  x ^= x << 13; x ^= x >>> 17; x ^= x << 5;
  const value = (x >>> 0) / 4294967296;
  if (value < 0.22) return { kind: 'rain', name: '持续降雨', rain: 15, temp: 23, humidity: 90 };
  if (value < 0.38) return { kind: 'shower', name: '阵雨', rain: 8, temp: 25, humidity: 75 };
  if (value < 0.78) return { kind: 'sunny', name: '晴朗', rain: 0, temp: 27, humidity: 48 };
  return { kind: 'hot', name: '晴热', rain: 0, temp: 34, humidity: 35 };
}

function makePlot(id, vigor) {
  return { id, name: ['朝阳园', '溪边园', '南坡园', '老藤园'][id - 1], moisture: 60,
    canopy: 78, load: 84, vigor: round(vigor), berryWeight: 0.8, brix: 5,
    damage: 0, fertility: 58, harvested: false,
    lastActions: { water: -99, fertilize: -99, prune: -99, thin: -99, protect: -99 } };
}

function createFarm(now, seed) {
  if (!int(now, 0, Number.MAX_SAFE_INTEGER)) fail('INVALID_TIME', '进入时间无效');
  if (seed === undefined) seed = 20261012;
  if (!int(seed, 0, 4294967295)) fail('INVALID_SEED', '农场种子无效');
  return { schemaVersion: 1, rulesVersion: RULES.version, season: 1, day: 0, version: 0,
    seed, coins: RULES.initialCoins, totalCosts: 0, totalRevenue: 0, lastSettledAt: now,
    plots: [makePlot(1, 72), makePlot(2, 74), makePlot(3, 70), makePlot(4, 76)],
    inventory: [], history: [], report: null,
    facilities: { drip: false, shelter: false, coldStorage: false },
    stats: { harvestWeight: 0, soldWeight: 0, spoiledWeight: 0 } };
}

function append(state, type, text, extra) {
  state.history.push(Object.assign({ day: state.day, season: state.season, type, text }, extra || {}));
  if (state.history.length > RULES.maxHistory) state.history.splice(0, state.history.length - RULES.maxHistory);
}

function validateSave(state) {
  if (!state || typeof state !== 'object' || state.schemaVersion !== 1 || state.rulesVersion !== RULES.version) return false;
  if (!int(state.season, 1, 10000) || !int(state.day, 0, 84) || !int(state.version, 0, 1000000000) ||
    !int(state.seed, 0, 4294967295) || !int(state.lastSettledAt, 0, Number.MAX_SAFE_INTEGER) ||
    !int(state.coins, 0, 1000000000) || !int(state.totalCosts, 0, 1000000000) || !int(state.totalRevenue, 0, 1000000000)) return false;
  if (!state.facilities || ['drip', 'shelter', 'coldStorage'].some(key => typeof state.facilities[key] !== 'boolean')) return false;
  if (!Array.isArray(state.plots) || state.plots.length !== 4) return false;
  const ids = new Set();
  for (const plot of state.plots) {
    if (!plot || !int(plot.id, 1, 4) || ids.has(plot.id) || !str(plot.name, 24) || typeof plot.harvested !== 'boolean') return false;
    ids.add(plot.id);
    for (const key of ['moisture', 'canopy', 'load', 'vigor', 'damage', 'fertility']) if (!num(plot[key], 0, 100)) return false;
    if (!num(plot.berryWeight, 0, 12) || !num(plot.brix, 0, 24) || !plot.lastActions) return false;
    for (const key of ['water', 'fertilize', 'prune', 'thin', 'protect']) if (!int(plot.lastActions[key], -99, state.day)) return false;
  }
  if (!Array.isArray(state.inventory) || state.inventory.length > 4 || !Array.isArray(state.history) || state.history.length > RULES.maxHistory) return false;
  const batchIds = new Set(), batchPlots = new Set();
  for (const batch of state.inventory) {
    if (!batch || !str(batch.id, 40) || batchIds.has(batch.id) || !int(batch.plotId, 1, 4) || batchPlots.has(batch.plotId) ||
      !['A', 'B', 'C'].includes(batch.grade) || !int(batch.weight, 1, 200000) || !int(batch.initialWeight, batch.weight, 200000) ||
      !num(batch.brix, 15, 24) || !int(batch.harvestDay, 56, state.day) || batch.harvestSeason !== state.season ||
      !state.plots.find(plot => plot.id === batch.plotId).harvested) return false;
    batchIds.add(batch.id); batchPlots.add(batch.plotId);
  }
  for (const entry of state.history) {
    if (!entry || !int(entry.day, 0, 84) || !int(entry.season, 1, state.season) || !str(entry.type, 32) || !str(entry.text, 300) ||
      (entry.cost !== undefined && !int(entry.cost, 0, 1000000000)) || (entry.revenue !== undefined && !int(entry.revenue, 0, 1000000000))) return false;
  }
  if (!state.stats || ['harvestWeight', 'soldWeight', 'spoiledWeight'].some(key => !int(state.stats[key], 0, 800000))) return false;
  const storedWeight = state.inventory.reduce((sum, batch) => sum + batch.weight, 0);
  if (state.stats.harvestWeight !== state.stats.soldWeight + state.stats.spoiledWeight + storedWeight) return false;
  if (state.report !== null) {
    const report = state.report;
    if (!report || report.season !== state.season || state.day !== 84 || !Array.isArray(report.notes) || report.notes.length > 8 ||
      report.notes.some(note => !str(note, 300)) || !num(report.avgBrix, 0, 24) || !['A', 'B', 'C', '—'].includes(report.bestGrade)) return false;
    for (const key of ['harvestWeight', 'soldWeight', 'spoiledWeight', 'totalRevenue', 'totalCosts']) if (!int(report[key], 0, 1000000000)) return false;
    if (!int(report.profit, -1000000000, 1000000000)) return false;
  }
  return true;
}

function gradeFor(plot) {
  if (plot.brix >= 17.2 && plot.berryWeight >= 6 && plot.damage <= 14) return 'A';
  if (plot.brix >= 15 && plot.berryWeight >= 4.5 && plot.damage <= 30) return 'B';
  return 'C';
}

function getReport(state) {
  const harvested = state.plots.filter(plot => plot.harvested);
  const grades = harvested.map(gradeFor);
  const notes = [];
  if (state.stats.harvestWeight === 0) notes.push('本季没有商品果入库；成熟期留意糖度并及时采收。');
  if (state.plots.some(plot => plot.damage > 20)) notes.push('病害或水分波动降低了商品率；通风、防护与稳定供水需要配合。');
  if (state.plots.some(plot => plot.load > 78)) notes.push('留果偏多会拖慢糖度积累，适度疏果可改善粒重与成熟。');
  if (state.stats.spoiledWeight > 0) notes.push('采后存放造成损耗；及时销售或建设冷库可减少损失。');
  if (state.inventory.length) notes.push('库存仍可继续销售；售空后才能开启下一季。');
  if (state.totalCosts > state.totalRevenue) notes.push('本季支出超过销售收入；设施建设费用计入当季，可在后续季节继续使用。');
  if (!notes.length) notes.push('完成了从田间管理到采收销售的一季经营，下一季可继续优化成本与品质。');
  return { season: state.season, harvestWeight: state.stats.harvestWeight, soldWeight: state.stats.soldWeight,
    spoiledWeight: state.stats.spoiledWeight, totalRevenue: state.totalRevenue, totalCosts: state.totalCosts,
    profit: state.totalRevenue - state.totalCosts,
    avgBrix: harvested.length ? round(harvested.reduce((sum, plot) => sum + plot.brix, 0) / harvested.length) : 0,
    bestGrade: grades.includes('A') ? 'A' : grades.includes('B') ? 'B' : grades.includes('C') ? 'C' : '—', notes };
}

function spend(state, cost) {
  if (state.coins < cost) fail('INSUFFICIENT_COINS', '经营币不足，请先销售库存或调整计划');
  state.coins -= cost; state.totalCosts += cost;
}

function simulateDay(state) {
  state.day += 1;
  const weatherSeed = (state.seed ^ Math.imul(state.season - 1, 0x85EBCA6B)) >>> 0;
  const weather = getWeather(weatherSeed, state.day);
  const stage = getStage(state.day);
  let upkeep = 0;
  for (const key of ['drip', 'shelter', 'coldStorage']) {
    if (state.facilities[key] && (key !== 'coldStorage' || state.inventory.length)) upkeep += RULES.facilities[key].dailyCost;
  }
  const maintained = upkeep === 0 || state.coins >= upkeep;
  if (maintained && upkeep) { spend(state, upkeep); append(state, 'upkeep', '设施日常维护', { cost: upkeep }); }
  if (!maintained) append(state, 'warning', '经营币不足，今日设施未运行');
  const drip = state.facilities.drip && maintained;
  const shelter = state.facilities.shelter && maintained;
  for (const plot of state.plots) {
    const beforeMoisture = plot.moisture;
    const rain = weather.rain * (shelter ? 0.2 : 1);
    const afterWeather = plot.moisture + rain - (weather.kind === 'hot' ? 10 : 7);
    const irrigation = drip ? clamp(62 - afterWeather, 0, 7) : 0;
    plot.moisture = round(clamp(afterWeather + irrigation, 0, 100));
    plot.fertility = round(clamp(plot.fertility - (state.day >= 21 && state.day <= 55 ? 0.62 : 0.24), 0, 100));
    const dry = clamp((28 - plot.moisture) / 28, 0, 1);
    const wet = clamp((plot.moisture - 88) / 12, 0, 1);
    const excess = clamp((plot.fertility - 78) / 22, 0, 1);
    const nutrient = clamp(plot.fertility / 42, 0.4, 1);
    const canopyFactor = clamp(1 - Math.abs(plot.canopy - 62) / 100, 0.35, 1);
    const overload = clamp((plot.load - plot.vigor * 0.95) / 40, 0, 1);
    const waterFactor = 1 - dry * 0.72 - wet * 0.24;
    const health = clamp(1 - plot.damage / 140, 0.35, 1);
    if (!plot.harvested && state.day < 84) {
      if (state.day >= 7 && state.day <= 55) plot.canopy = round(clamp(plot.canopy + 0.32 + excess * 0.65, 0, 100));
      if (state.day >= 21) {
        const protectedNow = state.day - plot.lastActions.protect <= 7;
        let infection = weather.humidity >= 75 ? 0.8 + Math.max(0, plot.canopy - 72) * 0.035 : 0.08;
        if (shelter) infection *= 0.52;
        if (protectedNow) infection *= 0.28;
        const cracking = state.day >= 56 && beforeMoisture < 32 && rain >= 8 ? 2.8 : 0;
        const sunburn = weather.kind === 'hot' && plot.canopy < 42 ? (42 - plot.canopy) * 0.07 : 0;
        plot.damage = round(clamp(plot.damage + infection + cracking + sunburn, 0, 100));
      }
      const growth = state.day >= 35 && state.day <= 55 ? 0.26 : state.day >= 21 && state.day <= 34 ? 0.07 : state.day >= 56 ? 0.045 : 0;
      plot.berryWeight = round(clamp(plot.berryWeight + growth * waterFactor * nutrient * canopyFactor * (1 - overload * 0.5) * health, 0, 12));
      if (state.day >= 56) {
        const sugar = 0.72 * waterFactor * canopyFactor * health * (1 - overload * 0.42) * (1 - excess * 0.24) * (shelter ? 0.97 : 1);
        plot.brix = round(clamp(plot.brix + sugar, 0, 24));
      }
      plot.vigor = round(clamp(plot.vigor - dry * 0.42 - overload * 0.12 - excess * 0.14 + (dry === 0 && nutrient >= 0.9 ? 0.045 : 0), 0, 100));
    } else if (plot.harvested) {
      plot.vigor = round(clamp(plot.vigor + (dry === 0 ? 0.18 : -0.12), 0, 100));
    }
  }
  const cold = state.facilities.coldStorage && maintained;
  let spoiled = 0;
  for (const batch of state.inventory) {
    const rate = cold ? 0.008 : weather.kind === 'hot' ? 0.065 : 0.04;
    const loss = Math.min(batch.weight, Math.ceil(batch.weight * rate));
    batch.weight -= loss; spoiled += loss;
  }
  state.inventory = state.inventory.filter(batch => batch.weight > 0);
  state.stats.spoiledWeight += spoiled;
  if (spoiled) append(state, 'spoilage', `采后损耗 ${spoiled} 克，库存已同步减少`);
  if (RULES.stages.some(item => item.start === state.day)) append(state, 'stage', `进入${stage.name}阶段`);
  if (state.day === 84) {
    state.report = getReport(state);
    append(state, 'seasonEnd', '本季生长结束，剩余库存可继续销售');
  }
}

function assertState(state) { if (!validateSave(state)) fail('INVALID_SAVE', '农场存档无效，请恢复有效存档'); }
function assertTime(now, state) {
  if (!int(now, 0, Number.MAX_SAFE_INTEGER) || now < state.lastSettledAt) fail('INVALID_TIME', '结算时间无效');
}

function settleFarm(state, now) {
  assertState(state); assertTime(now, state);
  const next = clone(state);
  const days = Math.min(84 - next.day, Math.floor((now - next.lastSettledAt) / RULES.offlineDayMs));
  for (let index = 0; index < days; index += 1) simulateDay(next);
  if (days) { next.lastSettledAt += days * RULES.offlineDayMs; next.version += 1; }
  return next;
}

function applyAction(state, action, now) {
  if (!action || typeof action !== 'object' || typeof action.type !== 'string' || !Object.prototype.hasOwnProperty.call(ACTIONS, action.type)) fail('UNKNOWN_ACTION', '不支持的农场操作');
  const next = settleFarm(state, now);
  const type = action.type;
  if (type === 'advance') {
    if (next.day === 84) fail('SEASON_CLOSED', '本季已结束，请先销售库存再开启下一季');
    const days = Math.min(RULES.advanceDays, 84 - next.day);
    for (let index = 0; index < days; index += 1) simulateDay(next);
    next.lastSettledAt = now;
    append(next, type, `推进 ${days} 天，水分、果实与库存已结算`);
  } else if (type === 'newSeason') {
    if (next.day !== 84) fail('SEASON_NOT_OVER', '请先完成本季经营');
    if (next.inventory.length) fail('UNSOLD_INVENTORY', '还有葡萄库存，请先销售后开启下一季');
    if (next.season >= 10000) fail('LIMIT_REACHED', '已达到存档季数上限');
    next.season += 1; next.day = 0; next.lastSettledAt = now;
    next.plots = next.plots.map(plot => makePlot(plot.id, clamp(plot.vigor + 5, 45, 95)));
    next.totalCosts = 0; next.totalRevenue = 0; next.report = null;
    next.stats = { harvestWeight: 0, soldWeight: 0, spoiledWeight: 0 };
    append(next, type, '开启下一季，保留经营币、设施与多年生树势');
  } else if (type === 'sell') {
    if (!['wholesale', 'retail'].includes(action.channel)) fail('INVALID_CHANNEL', '请选择批发或零售渠道');
    if (!next.inventory.length) fail('NO_INVENTORY', '暂无可销售的葡萄库存');
    let revenue = 0, cost = 0, weight = 0;
    for (const batch of next.inventory) {
      revenue += Math.floor(batch.weight * RULES.prices[action.channel][batch.grade] / 1000);
      cost += Math.ceil(batch.weight * RULES.channelCost[action.channel] / 1000);
      weight += batch.weight;
    }
    if (next.coins + revenue < cost) fail('INSUFFICIENT_COINS', '无法支付销售渠道费用');
    next.coins += revenue - cost; next.totalRevenue += revenue; next.totalCosts += cost;
    next.stats.soldWeight += weight; next.inventory = [];
    append(next, type, `${action.channel === 'retail' ? '零售' : '批发'}售出 ${weight} 克葡萄`, { revenue, cost });
  } else if (type === 'upgrade') {
    if (next.day === 84) fail('SEASON_CLOSED', '请下一季再建设设施');
    if (typeof action.facility !== 'string' || !Object.prototype.hasOwnProperty.call(RULES.facilities, action.facility)) fail('INVALID_FACILITY', '设施类型无效');
    if (next.facilities[action.facility]) fail('ALREADY_UPGRADED', '这项设施已经建成');
    const facility = RULES.facilities[action.facility]; spend(next, facility.cost);
    next.facilities[action.facility] = true;
    append(next, type, `建设${facility.name}，运行需要日常维护费用`, { cost: facility.cost });
  } else {
    if (!int(action.plotId, 1, 4)) fail('INVALID_PLOT', '请选择有效地块');
    const plot = next.plots.find(item => item.id === action.plotId);
    const rule = ACTIONS[type];
    if (next.day < rule.start || next.day > rule.end) fail('WRONG_STAGE', `${rule.name}不在当前农事窗口`);
    if (plot.harvested) fail('ALREADY_HARVESTED', '此地块已采收，下一季继续管理');
    if (type !== 'harvest' && next.day - plot.lastActions[type] < rule.cooldown) fail('ACTION_COOLDOWN', '这项农事刚完成，请等待下一个管理窗口');
    if (type === 'harvest' && plot.brix < RULES.minHarvestBrix) fail('NOT_RIPE', `糖度未到 ${RULES.minHarvestBrix}，继续成熟后采收`);
    if (type === 'thin' && plot.load <= 24) fail('TOO_FEW_FRUITS', '留果量已经很低，不宜继续疏果');
    if (type === 'prune' && plot.canopy <= 24) fail('TOO_FEW_LEAVES', '叶量已经很少，不宜继续整枝');
    spend(next, rule.cost);
    let text = `${plot.name}完成${rule.name}`;
    if (type === 'water') {
      plot.moisture = round(clamp(plot.moisture + 34, 0, 100));
      text += '，补充水分；持续过湿会影响生长';
    } else if (type === 'fertilize') {
      plot.fertility = round(clamp(plot.fertility + 27, 0, 100));
      text += '，养分补充；过量会使枝叶旺长、成熟变慢';
    } else if (type === 'prune') {
      plot.canopy = round(clamp(plot.canopy - 18, 0, 100));
      text += '，改善通风；叶量过低会影响糖度和日灼风险';
    } else if (type === 'thin') {
      plot.load = round(clamp(plot.load - 20, 0, 100));
      text += '，减轻负载；总果数随之下降';
    } else if (type === 'protect') {
      text += '，未来 7 天降低病害新增风险，已有损伤不会消失';
    } else if (type === 'harvest') {
      const weight = Math.max(1, Math.floor(RULES.plantUnits * plot.load / 100 * plot.berryWeight * (1 - plot.damage / 100)));
      const grade = gradeFor(plot);
      next.inventory.push({ id: `s${next.season}-p${plot.id}`, plotId: plot.id, grade, weight, initialWeight: weight,
        brix: plot.brix, harvestDay: next.day, harvestSeason: next.season });
      next.stats.harvestWeight += weight; plot.harvested = true;
      text += `，${grade} 级 ${weight} 克，糖度 ${plot.brix}`;
    }
    if (type !== 'harvest') plot.lastActions[type] = next.day;
    append(next, type, text, { cost: rule.cost });
  }
  if (next.day === 84) next.report = getReport(next);
  next.version += 1;
  assertState(next);
  return next;
}

module.exports = { GameError, createFarm, applyAction, settleFarm, getStage, getWeather, getReport, validateSave, RULES, ACTIONS };
