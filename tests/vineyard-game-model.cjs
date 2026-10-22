'use strict';

const assert = require('node:assert/strict');
const model = require('../minigame/src/model');
const { createFarm, applyAction, settleFarm, getStage, getWeather, validateSave, RULES } = model;
const HOUR = RULES.offlineDayMs;
const START = 1791763200000;
const copy = value => JSON.parse(JSON.stringify(value));
const command = (state, action) => applyAction(state, action, state.lastSettledAt);
const days = (state, count) => settleFarm(state, state.lastSettledAt + count * HOUR);
const rejection = (fn, code) => assert.throws(fn, error => error.code === code);
const tests = [];
const test = (name, run) => tests.push({ name, run });

function manageFarm(seed = 20261012, options = {}) {
  let state = createFarm(START, seed);
  if (options.fertility !== undefined) state.plots.forEach(plot => { plot.fertility = options.fertility; });
  if (options.drip !== false) state = command(state, { type: 'upgrade', facility: 'drip' });
  for (let day = 0; day < (options.until || 84); day += 1) {
    for (let plotId = 1; plotId <= 4; plotId += 1) {
      if (day === 0 || day === 28) state = command(state, { type: 'prune', plotId });
      if (day === 28 && options.thin !== false) state = command(state, { type: 'thin', plotId });
      if (day === 35 && options.fertility === undefined) state = command(state, { type: 'fertilize', plotId });
      if (day >= 21 && day % 7 === 0 && day < 77 && options.protect !== false) state = command(state, { type: 'protect', plotId });
      if (options.water && state.plots[plotId - 1].moisture < 45) state = command(state, { type: 'water', plotId });
      if (day === 83 && options.harvest !== false) state = command(state, { type: 'harvest', plotId });
    }
    if (day === 83 && options.sell !== false && state.inventory.length) state = command(state, { type: 'sell', channel: options.channel || 'retail' });
    state = days(state, 1);
    assert.equal(validateSave(state), true, `invalid state on day ${state.day}`);
  }
  return state;
}

test('完整一季：物候、农事、A级采收、销售、报告与下一季保留资产', () => {
  const farm = manageFarm();
  assert.equal(farm.day, 84);
  assert.equal(farm.report.bestGrade, 'A');
  assert.ok(farm.totalRevenue > 0);
  assert.ok(farm.plots.every(plot => plot.harvested));
  assert.equal(farm.stats.harvestWeight, farm.stats.soldWeight);
  assert.equal(farm.coins, 18000 + farm.totalRevenue - farm.totalCosts);
  assert.equal(farm.report.profit, farm.totalRevenue - farm.totalCosts);
  const next = command(farm, { type: 'newSeason' });
  assert.equal(next.day, 0); assert.equal(next.season, 2);
  assert.equal(next.coins, farm.coins); assert.equal(next.facilities.drip, true);
  assert.equal(next.report, null); assert.equal(next.totalRevenue, 0);
  assert.ok(next.plots[0].vigor >= farm.plots[0].vigor);
  assert.ok(next.plots.every(plot => !plot.harvested));
});

test('缺水对比：稳定供水的果粒与糖度优于无人管理的缺水地块', () => {
  const dry = manageFarm(20261012, { drip: false, harvest: false, until: 77 });
  const irrigated = manageFarm(20261012, { drip: true, harvest: false, until: 77 });
  assert.ok(irrigated.plots[0].berryWeight > dry.plots[0].berryWeight);
  assert.ok(irrigated.plots[0].brix > dry.plots[0].brix);
  assert.ok(irrigated.plots[0].vigor > dry.plots[0].vigor);
});

test('过肥和过载存在代价，疏果会减少果数', () => {
  const moderate = manageFarm(20261012, { fertility: 58, harvest: false, until: 77 });
  const overfed = manageFarm(20261012, { fertility: 100, harvest: false, until: 77 });
  const overloaded = manageFarm(20261012, { thin: false, fertility: 58, harvest: false, until: 77 });
  assert.ok(overfed.plots[0].canopy > moderate.plots[0].canopy);
  assert.ok(overfed.plots[0].brix < moderate.plots[0].brix);
  assert.ok(overloaded.plots[0].berryWeight < moderate.plots[0].berryWeight);
  assert.ok(overloaded.plots[0].brix < moderate.plots[0].brix);
  assert.ok(overloaded.plots[0].load > moderate.plots[0].load);
});

test('提前采收、农事窗口、冷却期、余额不足均明确拒绝', () => {
  const farm = createFarm(START, 20261012);
  rejection(() => command(farm, { type: 'harvest', plotId: 1 }), 'WRONG_STAGE');
  rejection(() => command(days(farm, 56), { type: 'harvest', plotId: 1 }), 'NOT_RIPE');
  rejection(() => command(farm, { type: 'thin', plotId: 1 }), 'WRONG_STAGE');
  const pruned = command(farm, { type: 'prune', plotId: 1 });
  rejection(() => command(pruned, { type: 'prune', plotId: 1 }), 'ACTION_COOLDOWN');
  const poor = copy(farm); poor.coins = 69;
  rejection(() => command(poor, { type: 'water', plotId: 1 }), 'INSUFFICIENT_COINS');
  assert.equal(farm.plots[0].canopy, 78);
  assert.equal(poor.coins, 69);
  rejection(() => command(farm, { type: 'water', plotId: '1' }), 'INVALID_PLOT');
  rejection(() => command(farm, { type: '__proto__' }), 'UNKNOWN_ACTION');
});

test('采后库存守恒，已损伤只在采收时计一次，销售不可重复', () => {
  let farm = manageFarm(20261012, { sell: false });
  const initial = farm.stats.harvestWeight;
  const weight = farm.inventory.reduce((sum, batch) => sum + batch.weight, 0);
  assert.equal(initial, weight + farm.stats.spoiledWeight);
  assert.ok(farm.stats.spoiledWeight > 0);
  assert.ok(farm.inventory.every(batch => batch.initialWeight >= batch.weight));
  rejection(() => command(farm, { type: 'newSeason' }), 'UNSOLD_INVENTORY');
  farm = command(farm, { type: 'sell', channel: 'wholesale' });
  assert.equal(farm.stats.harvestWeight, farm.stats.soldWeight + farm.stats.spoiledWeight);
  assert.equal(farm.inventory.length, 0);
  rejection(() => command(farm, { type: 'sell', channel: 'wholesale' }), 'NO_INVENTORY');
  const unchanged = days(farm, 1000);
  assert.deepEqual(unchanged, farm);
});

test('冷库减少库存损耗，避免库存为零仍损耗', () => {
  const day83 = manageFarm(20261012, { until: 83, harvest: false, protect: false });
  let ordinary = command(day83, { type: 'harvest', plotId: 1 });
  let cold = command(ordinary, { type: 'upgrade', facility: 'coldStorage' });
  ordinary = days(ordinary, 1); cold = days(cold, 1);
  assert.ok(cold.stats.spoiledWeight < ordinary.stats.spoiledWeight);
  assert.ok(cold.inventory[0].weight > ordinary.inventory[0].weight);
});

test('离线分段结算一致，保留不足一小时的余量，超长离线不跳过库存', () => {
  const farm = createFarm(START, 20261012);
  const once = settleFarm(farm, START + 14.75 * HOUR);
  const part = settleFarm(settleFarm(farm, START + 7.25 * HOUR), START + 14.75 * HOUR);
  once.version = 0; part.version = 0;
  assert.deepEqual(once, part);
  assert.equal(once.day, 14); assert.equal(once.lastSettledAt, START + 14 * HOUR);
  const later = settleFarm(once, START + 15 * HOUR);
  assert.equal(later.day, 15);
  const season = settleFarm(farm, START + 1000000 * HOUR);
  assert.equal(season.day, 84); assert.equal(season.lastSettledAt, START + 84 * HOUR);
  assert.equal(validateSave(season), true);
});

test('所有公开变更函数都不修改输入，天气确定且没有Math.random', () => {
  const farm = createFarm(START, 12345), original = copy(farm);
  command(farm, { type: 'water', plotId: 1 }); days(farm, 3);
  assert.deepEqual(farm, original);
  assert.deepEqual(getWeather(12345, 30), getWeather(12345, 30));
  assert.equal(getStage(84).id, 'closed');
  const again = command(command(farm, { type: 'advance' }), { type: 'advance' });
  assert.equal(again.day, 14);
  assert.equal(again.lastSettledAt, START);
});

test('损坏与超值存档、NaN、Infinity以及反向时间拒绝', () => {
  const cases = [
    state => { state.coins = NaN; }, state => { state.day = 85; }, state => { state.totalCosts = -1; },
    state => { state.seed = Infinity; }, state => { state.plots[0].moisture = 101; },
    state => { state.plots[0].lastActions.water = 1; }, state => { state.inventory = null; },
    state => { state.history = null; }, state => { state.plots[1].id = 1; },
    state => { state.stats.harvestWeight = 1; }, state => { state.facilities.drip = 1; },
    state => { state.report = { profit: NaN }; }
  ];
  for (const damage of cases) {
    const farm = createFarm(START, 20261012); damage(farm);
    assert.equal(validateSave(farm), false);
    rejection(() => settleFarm(farm, START), 'INVALID_SAVE');
  }
  rejection(() => settleFarm(createFarm(START, 1), START - 1), 'INVALID_TIME');
  rejection(() => createFarm(NaN, 1), 'INVALID_TIME');
  rejection(() => createFarm(START, 4294967296), 'INVALID_SEED');
});

test('每周回合基础玩法：不用设施也可四地块收获、售出且经营收益为正', () => {
  let farm = createFarm(START, 20261012);
  for (let day = 0; day < 84; day += 7) {
    for (let plotId = 1; plotId <= 4; plotId += 1) {
      farm = command(farm, { type: 'water', plotId });
      if (day === 0 || day === 28) farm = command(farm, { type: 'prune', plotId });
      if (day === 28) farm = command(farm, { type: 'thin', plotId });
      if (day === 77) farm = command(farm, { type: 'harvest', plotId });
    }
    if (day === 77) farm = command(farm, { type: 'sell', channel: 'retail' });
    farm = command(farm, { type: 'advance' });
  }
  assert.equal(farm.plots.filter(plot => plot.harvested).length, 4);
  assert.ok(farm.stats.soldWeight > 100000);
  assert.ok(farm.report.profit > 0);
  assert.ok(farm.coins > RULES.initialCoins);
  assert.equal(farm.stats.harvestWeight, farm.stats.soldWeight);
});

function generateFixtures() {
  const fs = require('node:fs'), path = require('node:path');
  const fixtures = [];
  function fixture(name, seed, plan) {
    let state = createFarm(START, seed);
    const steps = [];
    const act = action => { const now = state.lastSettledAt; steps.push({ command: action, now }); state = applyAction(state, action, now); };
    const offline = count => { const now = state.lastSettledAt + count * HOUR; steps.push({ command: null, now }); state = settleFarm(state, now); };
    plan(act, offline, () => state);
    fixtures.push({ name, now: START, seed, steps, expected: state });
  }
  fixture('initial', 20261012, () => {});
  fixture('offline-fraction-and-cap', 4321, (act, offline) => { offline(7.5); offline(7.25); offline(10000); });
  fixture('all-actions-inventory-and-two-seasons', 20261012, (act, offline, current) => {
    act({ type: 'upgrade', facility: 'drip' });
    for (let plotId = 1; plotId <= 4; plotId += 1) { act({ type: 'water', plotId }); act({ type: 'prune', plotId }); }
    for (let index = 0; index < 3; index += 1) act({ type: 'advance' });
    for (let plotId = 1; plotId <= 4; plotId += 1) { act({ type: 'thin', plotId }); act({ type: 'protect', plotId }); }
    act({ type: 'advance' });
    for (let plotId = 1; plotId <= 4; plotId += 1) act({ type: 'prune', plotId });
    act({ type: 'advance' });
    for (let plotId = 1; plotId <= 4; plotId += 1) act({ type: 'fertilize', plotId });
    while (current().day < 77) {
      for (let plotId = 1; plotId <= 4; plotId += 1) act({ type: 'protect', plotId });
      act({ type: 'advance' });
    }
    act({ type: 'harvest', plotId: 1 });
    act({ type: 'sell', channel: 'retail' });
    for (let plotId = 2; plotId <= 4; plotId += 1) act({ type: 'harvest', plotId });
    act({ type: 'upgrade', facility: 'coldStorage' });
    act({ type: 'advance' });
    act({ type: 'sell', channel: 'retail' });
    act({ type: 'newSeason' });
    act({ type: 'upgrade', facility: 'shelter' });
    offline(84);
  });
  fixture('wholesale-season-close', 20261012, (act, offline, current) => {
    act({ type: 'upgrade', facility: 'drip' });
    for (let plotId = 1; plotId <= 4; plotId += 1) act({ type: 'prune', plotId });
    offline(28);
    for (let plotId = 1; plotId <= 4; plotId += 1) { act({ type: 'thin', plotId }); act({ type: 'prune', plotId }); }
    offline(55);
    for (let plotId = 1; plotId <= 4; plotId += 1) act({ type: 'harvest', plotId });
    act({ type: 'sell', channel: 'wholesale' }); offline(1);
    assert.equal(current().day, 84);
  });
  const file = path.resolve(__dirname, '../minigame/tests/fixtures/v1.json');
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, JSON.stringify(fixtures, null, 2) + '\n');
  console.log(`Generated ${fixtures.length} deterministic Go/JS parity fixtures.`);
}

let failures = 0;
for (const entry of tests) {
  try { entry.run(); console.log(`PASS ${entry.name}`); }
  catch (error) { failures += 1; console.error(`FAIL ${entry.name}\n${error.stack}`); }
}
if (failures) process.exitCode = 1;
else { console.log(`Passed ${tests.length} vineyard model tests.`); if (process.argv.includes('--fixtures')) generateFixtures(); }
