'use strict';

const assert = require('node:assert/strict');
const model = require('../minigame/field/model');
const { createState, validateState, applyAction, getObjective, getAvailableInteraction, chapterSummary } = model;
let tests = 0;
function test(name, run) { run(); tests += 1; console.log('PASS ' + name); }
function act(state, type, extra) { return applyAction(state, Object.assign({ type }, extra || {})); }
function selected() { return act(createState(), 'selectCharacter', { characterId: 'lin' }); }
function planted() {
  let state = selected();
  for (const step of model.PLANTING_STEPS) state = act(state, 'interact', { targetId: step.targetId });
  return state;
}
function mature() { return act(planted(), 'nextChapter'); }
function waitTo(state, clock) {
  while (state.rainChallenge.clock < clock) state = act(state, 'wait');
  return state;
}
function error(state, action, code) {
  const before = JSON.stringify(state);
  assert.throws(() => applyAction(state, action), error => error.code === code && /[\u4e00-\u9fff]/.test(error.message));
  assert.equal(JSON.stringify(state), before, 'rejected action must not change source state');
}
function clone(value) { return JSON.parse(JSON.stringify(value)); }

test('isolated JSON save, strict corruption checks, no source mutation', () => {
  const state = createState();
  assert.equal(validateState(state), true);
  assert.equal(validateState(clone(state)), true);
  assert.equal(model.STORAGE_KEY, 'chiji_vineyard_field_demo_v1');
  assert.equal(model.STORAGE_KEY.includes('field_demo'), true);
  const changed = act(state, 'selectCharacter', { characterId: 'lin' });
  assert.equal(state.characterId, null);
  assert.equal(changed.characterId, 'lin');
  for (const modify of [
    s => { s.coins = NaN; }, s => { s.rainChallenge.clock = Infinity; },
    s => { s.nursery.step = 5; }, s => { s.rainChallenge.plots[0].id = 'unknown'; },
    s => { s.rainChallenge.plots.push(s.rainChallenge.plots[0]); },
    s => { s.inventory = [{ id: 'forged', weight: 20 }]; },
    s => { s.stats.rewards = 250; }, s => { s.completed.harvest = true; },
    s => { s.unexpected = { invalid: undefined }; }, s => { s.extra = new Date(); },
    s => { s.extra = () => 1; }, s => { s.extra = s; }
  ]) {
    const bad = clone(state); modify(bad);
    assert.equal(validateState(bad), false);
    assert.throws(() => applyAction(bad, { type: 'selectCharacter', characterId: 'lin' }), error => error.code === 'INVALID_SAVE');
  }
});

test('four characters can be switched without resetting assets', () => {
  assert.deepEqual(model.CHARACTERS.map(character => character.id), ['lin', 'xia', 'qiao', 'chen']);
  let state = mature();
  state = act(state, 'drain');
  const assets = { coins: state.coins, nursery: clone(state.nursery), facilities: clone(state.facilities), clock: state.rainChallenge.clock };
  state = act(state, 'selectCharacter', { characterId: 'xia' });
  assert.equal(state.characterId, 'xia');
  assert.equal(state.coins, assets.coins);
  assert.deepEqual(state.nursery, assets.nursery);
  assert.deepEqual(state.facilities, assets.facilities);
  assert.equal(state.rainChallenge.clock, assets.clock);
  error(state, { type: 'selectCharacter', characterId: 'visitor' }, 'INVALID_CHARACTER');
});

test('planting requires seven physical steps including carry water back to roots', () => {
  let state = selected();
  error(state, { type: 'nextChapter' }, 'CHAPTER_LOCKED');
  error(state, { type: 'interact', targetId: 'plantingSpot' }, 'WRONG_PLANTING_STEP');
  for (let i = 0; i < 5; i++) {
    const step = model.PLANTING_STEPS[i];
    const interaction = getAvailableInteraction(state, step.targetId);
    assert.equal(interaction.enabled, true);
    state = applyAction(state, interaction.action);
  }
  assert.equal(state.nursery.step, 5);
  assert.equal(state.nursery.hasWater, true);
  assert.equal(state.nursery.watered, false);
  assert.equal(getAvailableInteraction(state, 'plantingSpot').label, '浇定根水');
  state = act(state, 'interact', { targetId: 'plantingSpot' });
  assert.equal(state.nursery.hasWater, false);
  assert.equal(state.nursery.watered, true);
  error(state, { type: 'interact', targetId: 'nameplate', name: '\n' }, 'INVALID_NAME');
  state = act(state, 'interact', { targetId: 'nameplate', name: '小青的第一株' });
  assert.equal(state.nursery.name, '小青的第一株');
  assert.equal(state.nursery.step, 7);
  assert.equal(state.coins, 800);
  assert.equal(state.stats.rewards, 100);
  assert.equal(getObjective(state).total, 7);
  error(state, { type: 'interact', targetId: 'nameplate' }, 'WRONG_CHAPTER');
});

test('second chapter explicitly enters existing mature vines, no offline progression', () => {
  const state = mature();
  assert.match(state.lastResult.text, /已有多年生/);
  assert.match(getObjective(state).text, /18 公斤 A\/B/);
  assert.equal(state.nursery.step, 7);
  assert.equal(state.rainChallenge.clock, 0);
  assert.equal(state.rainChallenge.plots[0].brix, 18.8);
  const restored = JSON.parse(JSON.stringify(state));
  assert.deepEqual(restored, state);
  for (let i = 0; i < 20; i++) {
    getObjective(restored);
    getAvailableInteraction(restored, 'clock');
    chapterSummary(restored);
  }
  assert.equal(restored.rainChallenge.clock, 0);
  assert.equal('lastSettledAt' in restored, false);
});

test('maturity drives grading, early harvest and market channels have consequences', () => {
  let state = mature();
  state = act(state, 'harvest', { plotId: 'slope' });
  assert.equal(state.inventory[0].grade, 'C');
  assert.equal(state.inventory[0].brix, 15.2);
  error(state, { type: 'deliver', channel: 'contract' }, 'NO_ELIGIBLE_STOCK');
  error(state, { type: 'harvest', plotId: 'slope' }, 'ALREADY_HARVESTED');
  state = act(state, 'deliver', { channel: 'wholesale' });
  assert.equal(state.rainChallenge.deliveredKg, 0);
  assert.equal(state.rainChallenge.revenue, 8 * 11);
  assert.equal(state.inventory.length, 0);
  assert.equal(state.rainChallenge.clock, 2);
});

test('rain occurs at six, drainage and shelter preserve different grades', () => {
  const exposed = waitTo(mature(), 6);
  assert.equal(exposed.rainChallenge.rainArrived, true);
  assert.equal(exposed.rainChallenge.plots[1].damage, 40);
  assert.equal(act(exposed, 'harvest', { plotId: 'creek' }).inventory[0].grade, 'C');
  let drained = act(mature(), 'drain');
  drained = waitTo(drained, 6);
  assert.equal(drained.rainChallenge.plots[1].damage, 18);
  assert.equal(act(drained, 'harvest', { plotId: 'creek' }).inventory[0].grade, 'B');
  let sheltered = act(mature(), 'drain');
  sheltered = act(sheltered, 'protect');
  sheltered = waitTo(sheltered, 6);
  assert.equal(sheltered.rainChallenge.plots[1].damage, 5);
  assert.equal(act(sheltered, 'harvest', { plotId: 'creek' }).inventory[0].grade, 'A');
  assert.equal(sheltered.stats.costs, 530);
  error(sheltered, { type: 'protect' }, 'ALREADY_PROTECTED');
});

test('wholesale price falls at seven, contract quality price remains fixed', () => {
  let state = act(mature(), 'harvest', { plotId: 'sun' });
  const beforeDrop = act(waitTo(state, 6), 'deliver', { channel: 'wholesale' });
  const afterDrop = act(waitTo(state, 7), 'deliver', { channel: 'wholesale' });
  assert.equal(beforeDrop.rainChallenge.revenue, 12 * 38);
  assert.equal(afterDrop.rainChallenge.revenue, 12 * 28);
  assert.equal(afterDrop.rainChallenge.deliveredKg, 0);
  const order = act(waitTo(state, 7), 'deliver', { channel: 'contract' });
  assert.equal(order.rainChallenge.revenue, 12 * 48);
  assert.equal(order.rainChallenge.deliveredKg, 12);
});

test('deadline delivery is accepted exactly at ten, late work is rejected', () => {
  let state = act(mature(), 'harvest', { plotId: 'sun' });
  state = act(state, 'drain');
  state = act(state, 'harvest', { plotId: 'creek' });
  state = waitTo(state, 9);
  error(state, { type: 'protect' }, 'NOT_ENOUGH_TIME');
  const passed = act(state, 'deliver', { channel: 'contract' });
  assert.equal(passed.rainChallenge.clock, 10);
  assert.equal(passed.rainChallenge.status, 'passed');
  const failed = act(state, 'wait');
  assert.equal(failed.rainChallenge.status, 'failed');
  error(failed, { type: 'deliver', channel: 'contract' }, 'ORDER_CLOSED');
  error(failed, { type: 'harvest', plotId: 'slope' }, 'CHALLENGE_ENDED');
});

test('failed season preserves assets and stock, allows salvage, then a real new-season retry', () => {
  let state = act(mature(), 'drain');
  state = act(state, 'harvest', { plotId: 'sun' });
  state = waitTo(state, 10);
  assert.equal(state.rainChallenge.status, 'failed');
  assert.equal(state.coins, 580);
  assert.equal(state.inventory[0].weight, 12);
  assert.equal(getAvailableInteraction(state, 'delivery').enabled, true);
  const retry = act(state, 'retry');
  assert.equal(retry.rainChallenge.attempt, 2);
  assert.equal(retry.rainChallenge.clock, 0);
  assert.equal(retry.characterId, state.characterId);
  assert.equal(retry.coins, state.coins);
  assert.deepEqual(retry.nursery, state.nursery);
  assert.deepEqual(retry.facilities, state.facilities);
  assert.deepEqual(retry.inventory, state.inventory);
  assert.equal(retry.rainChallenge.plots.some(plot => plot.harvested), false);
  error(retry, { type: 'deliver', channel: 'contract' }, 'NO_ELIGIBLE_STOCK');
  const salvage = act(state, 'deliver', { channel: 'wholesale' });
  assert.equal(salvage.rainChallenge.clock, 10);
  assert.equal(salvage.rainChallenge.status, 'failed');
  assert.equal(salvage.coins, 580 + 12 * 28);
  assert.equal(salvage.inventory.length, 0);
});

test('passing again pays actual new-season produce but never duplicates first rewards', () => {
  function pass(state) {
    state = act(state, 'harvest', { plotId: 'sun' });
    state = waitTo(state, 3);
    state = act(state, 'harvest', { plotId: 'creek' });
    return act(state, 'deliver', { channel: 'contract' });
  }
  let state = pass(mature());
  assert.equal(state.rainChallenge.status, 'passed');
  assert.equal(state.rainChallenge.deliveredKg, 22);
  assert.equal(state.stats.rewards, 350);
  assert.deepEqual(state.completed, { planting: true, harvest: true });
  const before = state.coins;
  error(state, { type: 'deliver', channel: 'contract' }, 'ORDER_CLOSED');
  state = pass(act(state, 'retry'));
  assert.equal(state.stats.rewards, 350);
  assert.equal(state.coins, before - 80 + 896);
  assert.match(state.lastResult.text, /重复挑战不会重复/);
});

test('low budget recovery funds allow harvesting without clearing inventory or facilities', () => {
  let state = act(mature(), 'drain');
  state = act(state, 'protect');
  for (const plot of model.PLOTS) state = act(state, 'harvest', { plotId: plot.id });
  state = waitTo(state, 10);
  state = act(state, 'retry');
  for (const plot of model.PLOTS) state = act(state, 'harvest', { plotId: plot.id });
  state = waitTo(state, 10);
  assert.equal(state.coins, 30);
  const recovered = act(state, 'retry');
  assert.equal(recovered.coins, 120);
  assert.equal(recovered.stats.aid, 90);
  assert.deepEqual(recovered.inventory, state.inventory);
  assert.deepEqual(recovered.facilities, state.facilities);
  assert.equal(recovered.stats.rewards, 100);
  assert.equal(validateState(recovered), true);
});

test('actions, reports and JSON reloads stay valid throughout multiple mixed seasons', () => {
  let state = mature();
  for (let attempt = 0; attempt < 12; attempt++) {
    for (const plot of model.PLOTS) state = act(state, 'harvest', { plotId: plot.id });
    if (attempt % 2 === 0) state = act(state, 'deliver', { channel: 'wholesale' });
    state = waitTo(state, 10);
    assert.equal(validateState(clone(state)), true);
    assert.equal(chapterSummary(state).status, 'failed');
    state = act(state, 'retry');
    assert.equal(validateState(state), true);
  }
  assert.equal(state.history.length, 40);
  assert.equal(state.rainChallenge.attempt, 13);
  assert.equal(state.stats.harvestedKg, state.stats.soldKg + chapterSummary(state).inventoryKg);
});

console.log('Vineyard field model: ' + tests + ' tests passed.');
