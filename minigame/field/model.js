'use strict';

// This is an isolated local prototype. Its clock advances only through actions.
const STORAGE_KEY = 'chiji_vineyard_field_demo_v1';
const SCHEMA_VERSION = 1;
const RULES_VERSION = 'field-v1';
const CHARACTERS = Object.freeze([
  Object.freeze({ id: 'lin', name: '林禾', description: '草帽与青绿工装', color: '#5d8061' }),
  Object.freeze({ id: 'xia', name: '夏青', description: '扎起头发，穿着蓝色工装', color: '#547d9b' }),
  Object.freeze({ id: 'qiao', name: '乔叔', description: '宽檐帽与暖棕工作服', color: '#9b7451' }),
  Object.freeze({ id: 'chen', name: '陈雨', description: '短发与酒红工作服', color: '#995f69' })
]);
const PLOTS = Object.freeze([
  Object.freeze({ id: 'sun', name: '朝阳架', weight: 12, initialBrix: 18.8, ripening: 0.2 }),
  Object.freeze({ id: 'creek', name: '溪边架', weight: 10, initialBrix: 16, ripening: 0.35 }),
  Object.freeze({ id: 'slope', name: '坡上架', weight: 8, initialBrix: 15.2, ripening: 0.3 })
]);
const PLANTING_STEPS = Object.freeze([
  Object.freeze({ targetId: 'seedling', label: '领取葡萄苗', duration: 1.2 }),
  Object.freeze({ targetId: 'plantingSpot', label: '挖种植穴', duration: 2.2 }),
  Object.freeze({ targetId: 'plantingSpot', label: '扶苗回填', duration: 2.2 }),
  Object.freeze({ targetId: 'support', label: '绑好支撑', duration: 1.8 }),
  Object.freeze({ targetId: 'water', label: '装满水壶', duration: 1.4 }),
  Object.freeze({ targetId: 'plantingSpot', label: '浇定根水', duration: 2.2 }),
  Object.freeze({ targetId: 'nameplate', label: '挂上名牌', duration: 1.2 })
]);
const INITIAL_COINS = 700;
const CONTRACT_KG = 18;
const PRICES = Object.freeze({ A: 48, B: 32, C: 14 });
const MAX_HISTORY = 40;
const MAX_INVENTORY = 3000;

class FieldGameError extends Error {
  constructor(code, message) {
    super(message);
    this.name = 'FieldGameError';
    this.code = code;
  }
}
function fail(code, message) { throw new FieldGameError(code, message); }
function clone(value) { return JSON.parse(JSON.stringify(value)); }
function integer(value, min, max) {
  return typeof value === 'number' && Number.isFinite(value) && Number.isInteger(value) && value >= min && value <= max;
}
function number(value, min, max) {
  return typeof value === 'number' && Number.isFinite(value) && value >= min && value <= max;
}
function jsonValue(value, seen) {
  if (value === null || typeof value === 'string' || typeof value === 'boolean') return true;
  if (typeof value === 'number') return Number.isFinite(value);
  if (typeof value !== 'object') return false;
  if (!Array.isArray(value) && Object.getPrototypeOf(value) !== Object.prototype) return false;
  if (seen.has(value)) return false;
  seen.add(value);
  const valid = (Array.isArray(value) ? value : Object.values(value)).every(child => jsonValue(child, seen));
  seen.delete(value);
  return valid;
}
function text(value, max) { return typeof value === 'string' && value.length > 0 && value.length <= max; }
function round(value) { return Math.round(value * 10) / 10; }
function record(state, type, message) {
  state.history.push({ type, text: message, chapter: state.chapter, clock: state.chapter === 'harvest' ? state.rainChallenge.clock : 0 });
  if (state.history.length > MAX_HISTORY) state.history.splice(0, state.history.length - MAX_HISTORY);
  state.lastResult = { type, text: message };
}
function makeChallenge(attempt, facilities) {
  return {
    attempt, clock: 0, rainAt: 6, deadline: 10, status: 'active', rainArrived: false,
    drained: facilities.drainage, protected: facilities.shelter,
    deliveredKg: 0, revenue: 0, costs: 0, gradeSold: { A: 0, B: 0, C: 0 },
    plots: PLOTS.map(plot => ({ id: plot.id, name: plot.name, weight: plot.weight, brix: plot.initialBrix, damage: 0, harvested: false }))
  };
}
function createState() {
  const facilities = { drainage: false, shelter: false };
  return {
    schemaVersion: SCHEMA_VERSION, rulesVersion: RULES_VERSION, version: 0,
    chapter: 'planting', stage: 'character', characterId: null, coins: INITIAL_COINS,
    nursery: { step: 0, name: '我的第一株葡萄', planted: false, supported: false, hasWater: false, watered: false },
    facilities, rainChallenge: makeChallenge(1, facilities), inventory: [],
    completed: { planting: false, harvest: false }, awarded: { planting: false, harvest: false },
    stats: { harvestedKg: 0, soldKg: 0, revenue: 0, costs: 0, rewards: 0, aid: 0 },
    lastResult: null, history: []
  };
}

function validateState(state) {
  try {
    if (!state || typeof state !== 'object' || Array.isArray(state) || !jsonValue(state, new Set()) ||
      state.schemaVersion !== SCHEMA_VERSION || state.rulesVersion !== RULES_VERSION ||
      !integer(state.version, 0, 1000000) || !['planting', 'harvest'].includes(state.chapter) ||
      !['character', 'playing', 'complete'].includes(state.stage) || !integer(state.coins, 0, 1000000000)) return false;
    if (state.characterId !== null && !CHARACTERS.some(character => character.id === state.characterId)) return false;
    if ((state.stage === 'character') !== (state.characterId === null)) return false;
    const nursery = state.nursery;
    if (!nursery || !integer(nursery.step, 0, 7) || !text(nursery.name, 18) ||
      nursery.planted !== (nursery.step >= 3) || nursery.supported !== (nursery.step >= 4) ||
      nursery.hasWater !== (nursery.step === 5) || nursery.watered !== (nursery.step >= 6)) return false;
    for (const mapName of ['completed', 'awarded']) {
      const map = state[mapName];
      if (!map || typeof map.planting !== 'boolean' || typeof map.harvest !== 'boolean') return false;
    }
    if (state.completed.planting !== (nursery.step === 7) || state.awarded.planting !== state.completed.planting ||
      state.awarded.harvest !== state.completed.harvest || (state.completed.harvest && !state.completed.planting)) return false;
    if (state.chapter === 'planting' && (state.completed.harvest || ((state.stage === 'complete') !== state.completed.planting))) return false;
    if (state.chapter === 'harvest' && (!state.completed.planting || state.stage === 'character')) return false;
    const facilities = state.facilities;
    if (!facilities || typeof facilities.drainage !== 'boolean' || typeof facilities.shelter !== 'boolean') return false;
    const rain = state.rainChallenge;
    if (!rain || !integer(rain.attempt, 1, 1000) || !integer(rain.clock, 0, 10) || rain.rainAt !== 6 || rain.deadline !== 10 ||
      !['active', 'passed', 'failed'].includes(rain.status) || rain.rainArrived !== (rain.clock >= rain.rainAt) ||
      rain.drained !== facilities.drainage || rain.protected !== facilities.shelter ||
      !integer(rain.deliveredKg, 0, 30) || !integer(rain.revenue, 0, 1000000000) || !integer(rain.costs, 0, 1000000000)) return false;
    if (rain.status === 'active' && (rain.clock >= rain.deadline || rain.deliveredKg >= CONTRACT_KG)) return false;
    if (rain.status === 'passed' && (rain.deliveredKg < CONTRACT_KG || !state.completed.harvest)) return false;
    if (rain.status === 'failed' && (rain.clock !== rain.deadline || rain.deliveredKg >= CONTRACT_KG)) return false;
    if (state.chapter === 'harvest' && ((state.stage === 'complete') !== (rain.status === 'passed'))) return false;
    if (!Array.isArray(rain.plots) || rain.plots.length !== PLOTS.length) return false;
    const plotIds = new Set();
    for (const plot of rain.plots) {
      const definition = PLOTS.find(item => item.id === plot.id);
      if (!definition || plotIds.has(plot.id) || plot.name !== definition.name || plot.weight !== definition.weight ||
        !number(plot.brix, 0, 24) || !integer(plot.damage, 0, 100) || typeof plot.harvested !== 'boolean') return false;
      plotIds.add(plot.id);
      if (!rain.rainArrived && plot.damage !== 0) return false;
    }
    if (!rain.gradeSold || ['A', 'B', 'C'].some(grade => !integer(rain.gradeSold[grade], 0, 30000))) return false;
    const stats = state.stats;
    if (!stats || ['harvestedKg', 'soldKg', 'revenue', 'costs', 'rewards', 'aid'].some(key => !integer(stats[key], 0, 1000000000))) return false;
    if (stats.rewards !== (state.awarded.planting ? 100 : 0) + (state.awarded.harvest ? 250 : 0) ||
      state.coins !== INITIAL_COINS + stats.revenue + stats.rewards + stats.aid - stats.costs ||
      stats.revenue < rain.revenue || stats.costs < rain.costs) return false;
    if (!Array.isArray(state.inventory) || state.inventory.length > MAX_INVENTORY) return false;
    const batchIds = new Set();
    let stockKg = 0;
    for (const batch of state.inventory) {
      const definition = PLOTS.find(plot => plot.id === batch.plotId);
      if (!batch || !definition || !integer(batch.attempt, 1, rain.attempt) ||
        batch.id !== 'harvest-' + batch.attempt + '-' + batch.plotId || batchIds.has(batch.id) ||
        !integer(batch.weight, 1, definition.weight) || !['A', 'B', 'C'].includes(batch.grade) ||
        !number(batch.brix, 0, 24) || !integer(batch.damage, 0, 100) || !integer(batch.harvestClock, 0, 9)) return false;
      if (batch.attempt === rain.attempt && !rain.plots.find(plot => plot.id === batch.plotId).harvested) return false;
      batchIds.add(batch.id);
      stockKg += batch.weight;
    }
    if (stats.harvestedKg !== stats.soldKg + stockKg || stats.harvestedKg > rain.attempt * 30) return false;
    if (!Array.isArray(state.history) || state.history.length > MAX_HISTORY || state.history.some(entry =>
      !entry || !text(entry.type, 40) || !text(entry.text, 500) || !['planting', 'harvest'].includes(entry.chapter) || !integer(entry.clock, 0, 10))) return false;
    if (state.lastResult !== null && (!state.lastResult || !text(state.lastResult.type, 40) || !text(state.lastResult.text, 500))) return false;
    return true;
  } catch (_) { return false; }
}

function award(state, chapter, coins) {
  if (state.awarded[chapter]) return 0;
  state.awarded[chapter] = true;
  state.completed[chapter] = true;
  state.coins += coins;
  state.stats.rewards += coins;
  return coins;
}
function pay(state, coins) {
  if (state.coins < coins) fail('INSUFFICIENT_COINS', '预算不足；可以先出售已采的葡萄，失败后也可带着已有资产进入下一采收季。');
  state.coins -= coins;
  state.stats.costs += coins;
  state.rainChallenge.costs += coins;
}
function gradeFor(plot) {
  if (plot.brix >= 18 && plot.damage <= 10) return 'A';
  if (plot.brix >= 17 && plot.damage <= 25) return 'B';
  return 'C';
}
function advanceClock(state, cost) {
  const rain = state.rainChallenge;
  rain.clock = Math.min(rain.deadline, rain.clock + cost);
  if (!rain.rainArrived && rain.clock >= rain.rainAt) {
    rain.rainArrived = true;
    const damage = rain.drained && rain.protected ? 5 : rain.protected ? 12 : rain.drained ? 18 : 40;
    for (const plot of rain.plots) if (!plot.harvested) plot.damage = damage;
  }
  for (const plot of rain.plots) {
    if (!plot.harvested) {
      const definition = PLOTS.find(item => item.id === plot.id);
      plot.brix = round(Math.min(20, definition.initialBrix + rain.clock * definition.ripening));
    }
  }
  if (rain.status === 'active' && rain.clock >= rain.deadline) {
    rain.status = 'failed';
    record(state, 'challengeFailed', '交货时限已到，本次未完成 18 公斤 A/B 级订单。土地、人物、设施、金币和库存都保留；可补救出售，或进入下一成熟采收季再挑战。');
  }
}
function activeChallenge(state, cost) {
  if (state.chapter !== 'harvest') fail('WRONG_CHAPTER', '先亲手完成定植，再进入已有成熟葡萄园的采收章节。');
  if (state.rainChallenge.status !== 'active') fail('CHALLENGE_ENDED', '这一季的挑战已经结算，可出售库存或进入下一成熟采收季。');
  if (cost && state.rainChallenge.clock + cost > state.rainChallenge.deadline) fail('NOT_ENOUGH_TIME', '剩余行动时间不足以完成这项工作；可以先交货，或等待后进入下一成熟采收季。');
}
function validateAction(action) {
  if (!action || typeof action !== 'object' || Array.isArray(action) || !text(action.type, 40)) fail('INVALID_ACTION', '操作无效，请重新选择。');
}

function applyAction(original, action) {
  if (!validateState(original)) fail('INVALID_SAVE', '这份下地体验存档已损坏，请保留原存档并明确选择重新开园。');
  validateAction(action);
  const state = clone(original);
  const rain = state.rainChallenge;
  switch (action.type) {
    case 'selectCharacter': {
      const character = CHARACTERS.find(item => item.id === action.characterId);
      if (!character) fail('INVALID_CHARACTER', '请从四位内置人物中选择。');
      state.characterId = character.id;
      if (state.stage === 'character') state.stage = 'playing';
      record(state, 'selectCharacter', '已选择 ' + character.name + '，人物更换不会改变葡萄园资产。');
      break;
    }
    case 'interact': {
      if (state.chapter !== 'planting' || state.stage !== 'playing') fail('WRONG_CHAPTER', '当前不在定植操作阶段。');
      const expected = PLANTING_STEPS[state.nursery.step];
      if (!expected || action.targetId !== expected.targetId) fail('WRONG_PLANTING_STEP', '请先完成“' + (expected ? expected.label : '定植') + '”。');
      if (action.targetId === 'nameplate' && action.name !== undefined) {
        if (typeof action.name !== 'string' || !action.name.trim() || action.name.trim().length > 18 || /[\r\n\u0000-\u001f]/.test(action.name)) fail('INVALID_NAME', '名牌请填写 1 到 18 个字，不要包含换行。');
        state.nursery.name = action.name.trim();
      }
      state.nursery.step += 1;
      state.nursery.planted = state.nursery.step >= 3;
      state.nursery.supported = state.nursery.step >= 4;
      state.nursery.hasWater = state.nursery.step === 5;
      state.nursery.watered = state.nursery.step >= 6;
      if (state.nursery.step === 7) {
        const reward = award(state, 'planting', 100);
        state.stage = 'complete';
        record(state, 'plantingComplete', '“' + state.nursery.name + '”已经安家。定植章完成，获得 ' + reward + ' 金币；幼苗会保留在育苗角，下一章前往已有成熟葡萄园。');
      } else record(state, 'plantingStep', expected.label + '完成。下一步：' + PLANTING_STEPS[state.nursery.step].label + '。');
      break;
    }
    case 'nextChapter':
      if (state.chapter !== 'planting' || !state.completed.planting) fail('CHAPTER_LOCKED', '请先完成第一株葡萄的定植。');
      state.chapter = 'harvest';
      state.stage = 'playing';
      record(state, 'enterHarvest', '第二章：前往已有多年生的成熟葡萄园。这里的果实不是刚种下的幼苗长出的。6 个行动时刻后强降雨，10 个行动时刻内交付 18 公斤 A/B 级葡萄。');
      break;
    case 'drain':
      activeChallenge(state, 2);
      if (state.facilities.drainage) fail('ALREADY_DRAINED', '排水沟已疏通，设施会保留到下一采收季。');
      pay(state, 180);
      state.facilities.drainage = true;
      rain.drained = true;
      record(state, 'drain', '花费 180 金币疏通排水，耗时 2 个行动时刻。雨前完成可减轻未采果实的损伤。');
      advanceClock(state, 2);
      break;
    case 'protect':
      activeChallenge(state, 2);
      if (state.facilities.shelter) fail('ALREADY_PROTECTED', '避雨设施已搭好，会保留到下一采收季。');
      pay(state, 350);
      state.facilities.shelter = true;
      rain.protected = true;
      record(state, 'protect', '花费 350 金币搭好避雨设施，耗时 2 个行动时刻。与排水配合可保住更多果实品质。');
      advanceClock(state, 2);
      break;
    case 'harvest': {
      activeChallenge(state, 1);
      const plot = rain.plots.find(item => item.id === action.plotId);
      if (!plot) fail('INVALID_PLOT', '请选择朝阳架、溪边架或坡上架。');
      if (plot.harvested) fail('ALREADY_HARVESTED', '这一批果已经采完，不能重复采收。');
      if (state.inventory.length >= MAX_INVENTORY) fail('STORAGE_FULL', '库存已满，请先去交货点出售。');
      pay(state, 40);
      const grade = gradeFor(plot);
      state.inventory.push({ id: 'harvest-' + rain.attempt + '-' + plot.id, attempt: rain.attempt,
        plotId: plot.id, weight: plot.weight, grade, brix: plot.brix, damage: plot.damage, harvestClock: rain.clock });
      plot.harvested = true;
      state.stats.harvestedKg += plot.weight;
      record(state, 'harvest', plot.name + '采收 ' + plot.weight + ' 公斤，分级为 ' + grade + ' 级；本次采收花费 40 金币和 1 个行动时刻。' + (grade === 'C' ? '这批果暂不符合订单品质，可走批发补救销售。' : '可交付 A/B 级订单。'));
      advanceClock(state, 1);
      break;
    }
    case 'wait':
      activeChallenge(state, 1);
      record(state, 'wait', '观察并等待了 1 个行动时刻。未采果实继续成熟，天气与交货期限也在靠近。');
      advanceClock(state, 1);
      break;
    case 'deliver': {
      if (state.chapter !== 'harvest') fail('WRONG_CHAPTER', '交货点会在成熟葡萄园章节开放。');
      if (!['contract', 'wholesale'].includes(action.channel)) fail('INVALID_CHANNEL', '请选择订单交货或批发销售。');
      if (action.channel === 'contract' && rain.status !== 'active') fail('ORDER_CLOSED', '本次订单已结束，仍可通过批发销售库存。');
      const eligible = state.inventory.filter(batch => action.channel === 'wholesale' || (batch.attempt === rain.attempt && batch.grade !== 'C'));
      if (!eligible.length) fail('NO_ELIGIBLE_STOCK', action.channel === 'contract' ? '篮中还没有本季 A/B 级葡萄；先判断成熟度再采收，也可把 C 级果批发出售。' : '还没有可出售的葡萄。');
      let weight = 0;
      let revenue = 0;
      for (const batch of eligible) {
        const price = action.channel === 'contract' ? PRICES[batch.grade] : Math.floor(PRICES[batch.grade] * (rain.clock >= 7 ? 0.6 : 0.8) * (batch.attempt < rain.attempt ? 0.6 : 1));
        weight += batch.weight;
        revenue += batch.weight * price;
        rain.gradeSold[batch.grade] += batch.weight;
      }
      const ids = new Set(eligible.map(batch => batch.id));
      state.inventory = state.inventory.filter(batch => !ids.has(batch.id));
      state.coins += revenue;
      state.stats.soldKg += weight;
      state.stats.revenue += revenue;
      rain.revenue += revenue;
      if (action.channel === 'contract') rain.deliveredKg += weight;
      if (action.channel === 'contract' && rain.deliveredKg >= CONTRACT_KG) {
        rain.status = 'passed';
        state.stage = 'complete';
        const reward = award(state, 'harvest', 250);
        record(state, 'challengePassed', '按时交付 ' + rain.deliveredKg + ' 公斤 A/B 级葡萄，订单收入 ' + revenue + ' 金币。第二章通关' + (reward ? '，首次奖励 ' + reward + ' 金币。' : '；重复挑战不会重复发放通关奖励。'));
      } else record(state, 'deliver', (action.channel === 'contract' ? '订单交付' : '批发售出') + ' ' + weight + ' 公斤，收入 ' + revenue + ' 金币。' + (action.channel === 'wholesale' ? '批发不会增加订单交付量；第 7 个行动时刻起批发行情下降。' : '订单还差 ' + (CONTRACT_KG - rain.deliveredKg) + ' 公斤。'));
      if (rain.status !== 'failed' && (state.stage !== 'complete' || action.channel === 'contract')) advanceClock(state, 1);
      break;
    }
    case 'retry':
      if (state.chapter !== 'harvest' || rain.status === 'active') fail('RETRY_UNAVAILABLE', '本次挑战尚未结束；先完成交货或等到时限再进入下一采收季。');
      if (rain.attempt >= 1000) fail('SEASON_LIMIT', '已完成本原型可保存的全部采收季。');
      state.rainChallenge = makeChallenge(rain.attempt + 1, state.facilities);
      state.stage = 'playing';
      if (state.coins < 120) {
        const aid = 120 - state.coins;
        state.coins += aid;
        state.stats.aid += aid;
      }
      record(state, 'retry', '明确跳转到下一成熟采收季。人物、第一株苗、金币、设施与未售库存全部保留；旧库存可批发，但不能充作本季新鲜订单。已领取的通关奖励不会再次发放。');
      break;
    default: fail('UNKNOWN_ACTION', '暂不支持这一操作。');
  }
  state.version += 1;
  if (!validateState(state)) fail('INVALID_RESULT', '操作结果未通过存档检查，本次操作没有保存。');
  return state;
}

function getObjective(state) {
  if (!validateState(state)) fail('INVALID_SAVE', '下地体验存档无效。');
  if (state.stage === 'character') return { title: '选择一位下地的人物', text: '四位人物共用农事能力，可在之后更换。', progress: 0, total: 1, status: 'active' };
  if (state.chapter === 'planting') {
    if (state.nursery.step === 7) return { title: '第一株葡萄已经安家', text: '幼苗留在育苗角。下一章进入已有成熟葡萄园，学习雨前采收与交货。', progress: 7, total: 7, status: 'passed' };
    return { title: '第一章 · 亲手栽下第一株葡萄', text: '下一步：' + PLANTING_STEPS[state.nursery.step].label + '。走到对应位置，按住操作。', progress: state.nursery.step, total: 7, status: 'active' };
  }
  const rain = state.rainChallenge;
  if (rain.status === 'passed') return { title: '第二章 · 雨前交货成功', text: '按时交付 ' + rain.deliveredKg + ' 公斤 A/B 级葡萄。库存仍可批发，或带着资产挑战下一成熟采收季。', progress: rain.deliveredKg, total: CONTRACT_KG, status: 'passed' };
  if (rain.status === 'failed') return { title: '订单超时，葡萄园仍然保留', text: '本次交付 ' + rain.deliveredKg + '/18 公斤。去交货点补救批发，或进入下一成熟采收季；设施、金币与库存都保留。', progress: rain.deliveredKg, total: CONTRACT_KG, status: 'failed' };
  const weather = rain.rainArrived ? '强降雨已到，未采果实按防护情况受损。' : '距强降雨 ' + (rain.rainAt - rain.clock) + ' 个行动时刻。';
  return { title: '第二章 · 成熟成园，雨前交货', text: weather + '时限还剩 ' + (rain.deadline - rain.clock) + ' 个行动时刻；交付 ' + rain.deliveredKg + '/18 公斤 A/B 级葡萄。第 7 时刻起批发价下降。', progress: rain.deliveredKg, total: CONTRACT_KG, status: 'active' };
}

function getAvailableInteraction(state, targetId) {
  if (!validateState(state)) return null;
  if (state.stage === 'character') return null;
  if (state.chapter === 'planting') {
    if (state.nursery.step >= 7) return null;
    const step = PLANTING_STEPS[state.nursery.step];
    if (!PLANTING_STEPS.some(item => item.targetId === targetId)) return null;
    return { label: step.targetId === targetId ? step.label : '先完成前一步', action: { type: 'interact', targetId },
      hint: step.targetId === targetId ? '按住完成“' + step.label + '”' : '当前需要：' + step.label,
      duration: step.duration, enabled: step.targetId === targetId };
  }
  const rain = state.rainChallenge;
  const plot = rain.plots.find(item => item.id === targetId);
  if (plot) return { label: plot.harvested ? '已经采完' : '采收' + plot.name, action: { type: 'harvest', plotId: targetId },
    hint: plot.name + ' · 糖度 ' + plot.brix.toFixed(1) + ' · 预计 ' + gradeFor(plot) + ' 级 · ' + plot.weight + ' 公斤；采收花费 40 金币、1 时刻。',
    duration: 1.8, enabled: rain.status === 'active' && !plot.harvested && state.coins >= 40 };
  if (targetId === 'drain') return { label: rain.drained ? '排水已完成' : '疏通排水沟', action: { type: 'drain' },
    hint: '180 金币、2 个行动时刻；雨前完成可减轻损伤，设施保留。', duration: 2.2,
      enabled: rain.status === 'active' && !rain.drained && state.coins >= 180 && rain.clock + 2 <= rain.deadline };
  if (targetId === 'shelter') return { label: rain.protected ? '避雨设施已完成' : '搭好避雨设施', action: { type: 'protect' },
    hint: '350 金币、2 个行动时刻；与排水配合保护品质，设施保留。', duration: 2.2,
      enabled: rain.status === 'active' && !rain.protected && state.coins >= 350 && rain.clock + 2 <= rain.deadline };
  if (targetId === 'delivery') return { label: '选择交货渠道', action: { type: 'deliver', channel: rain.status === 'active' ? 'contract' : 'wholesale' },
    hint: '订单只收本季 A/B 级并计入通关；批发收所有等级，第 7 时刻起降价。', duration: 1.2, enabled: state.inventory.length > 0 };
  if (targetId === 'clock') return { label: '观察天气与成熟度', action: { type: 'wait' },
    hint: '等待 1 个行动时刻；果实成熟，降雨和订单期限也会靠近。离线与走动不计时。', duration: 1.2, enabled: rain.status === 'active' };
  return null;
}

function chapterSummary(state) {
  if (!validateState(state)) fail('INVALID_SAVE', '下地体验存档无效。');
  const rain = state.rainChallenge;
  return { chapter: state.chapter, status: state.chapter === 'planting' ? (state.completed.planting ? 'passed' : 'active') : rain.status,
    title: state.chapter === 'planting' ? '第一株葡萄的定植记录' : '第 ' + rain.attempt + ' 个成熟采收季',
    name: state.nursery.name, characterId: state.characterId, coins: state.coins,
    deliveredKg: rain.deliveredKg, targetKg: CONTRACT_KG, clock: rain.clock, deadline: rain.deadline,
    revenue: rain.revenue, costs: rain.costs, gradeSold: clone(rain.gradeSold),
    inventoryKg: state.inventory.reduce((sum, batch) => sum + batch.weight, 0),
    firstRewardReceived: state.awarded[state.chapter], facilities: clone(state.facilities),
    notes: ['原型规则用于体验，不作为实际农事指导。', '关闭游戏、重新载入与走动不会推进天气或生长。', '下一采收季保留已有资产，通关奖励每章只领取一次。'] };
}

module.exports = { STORAGE_KEY, SCHEMA_VERSION, RULES_VERSION, CHARACTERS, PLOTS, PLANTING_STEPS,
  FieldGameError, createState, validateState, applyAction, getObjective, getAvailableInteraction, chapterSummary };
