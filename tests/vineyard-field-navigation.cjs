'use strict';

const assert = require('node:assert/strict');
const THREE = require('three');
const model = require('../minigame/field/model');
const navigation = require('../minigame/field/navigation');
const { createWorld } = require('../minigame/field/world');
const { BOUNDS, canStand, move, findPath } = navigation;
const world = createWorld({ THREE, renderer: { shadowMap: {} }, width: 390, height: 844 });
const actualObstacles = world.getObstacles();
let count = 0;
function test(name, run) { run(); count += 1; console.log('PASS ' + name); }
function action(state, type, values) { return model.applyAction(state, Object.assign({ type }, values)); }
function distance(a, b) { return Math.hypot(a.x - b.x, a.z - b.z); }
function checkPath(start, target, obstacles) {
  const originalStart = JSON.stringify(start), originalObstacles = JSON.stringify(obstacles);
  const path = findPath(start, target, obstacles);
  assert.ok(path && path.length, 'reachable target must return waypoints: ' + target.id);
  assert.equal(JSON.stringify(start), originalStart);
  assert.equal(JSON.stringify(obstacles), originalObstacles);
  let previous = start;
  for (const point of path) {
    assert.equal(canStand(point.x, point.z, obstacles), true, 'waypoint must be walkable');
    // Test the entire segment, not merely the endpoint, against real geometry.
    const samples = Math.max(1, Math.ceil(distance(previous, point) / 0.04));
    for (let i = 0; i <= samples; i++) {
      const fraction = i / samples;
      assert.equal(canStand(previous.x + (point.x - previous.x) * fraction,
        previous.z + (point.z - previous.z) * fraction, obstacles), true, 'route segment must not cut through a prop');
    }
    previous = point;
  }
  assert.ok(distance(path[path.length - 1], target) <= target.radius, 'route must stop inside interaction range');
  return path;
}

try {
  test('body clearance blocks circular props, rectangular props and garden boundaries', () => {
    assert.equal(canStand(0, 7, actualObstacles), true);
    assert.equal(canStand(-4, 4, actualObstacles), false, 'nursery bench is solid');
    assert.equal(canStand(-6.5, 5.3, actualObstacles), false, 'tool shed is solid');
    assert.equal(canStand(5.1, 4.05, actualObstacles), false, 'water tank is solid');
    assert.equal(canStand(0, 5, actualObstacles), false, 'packing table is solid');
    assert.equal(canStand(BOUNDS.maxX + 0.01, 0, actualObstacles), false);
    assert.equal(canStand(0, BOUNDS.minZ - 0.01, actualObstacles), false);
    assert.equal(canStand(1.26, 0, [{ x: 0, z: 0, width: 2, depth: 2 }]), false, 'character body has clearance');
    assert.equal(canStand(1.3, 0, [{ x: 0, z: 0, width: 2, depth: 2 }]), true);
  });

  test('invalid and non-finite coordinates cannot become walkable positions', () => {
    for (const point of [{ x: NaN, z: 0 }, { x: 0, z: NaN }, { x: Infinity, z: 0 }, { x: 0, z: -Infinity }, { x: '0', z: 0 }, { x: null, z: 0 }]) {
      assert.equal(canStand(point.x, point.z, actualObstacles), false);
    }
  });

  test('diagonal movement slides along a wall while preserving source position', () => {
    const wall = [{ minX: 0, maxX: 0.35, minZ: -3, maxZ: 3 }];
    const start = { x: -0.34, z: -1 };
    const next = move(start, 0.12, 0.12, wall);
    assert.deepEqual(start, { x: -0.34, z: -1 });
    assert.equal(next.x, start.x, 'blocked component must stop');
    assert.ok(next.z > start.z, 'free component must slide');
    assert.equal(canStand(next.x, next.z, wall), true);
    let position = next;
    for (let i = 0; i < 18; i++) {
      position = move(position, 0.12, 0.12, wall);
      assert.equal(canStand(position.x, position.z, wall), true);
      assert.ok(position.x < 0, 'small movement steps must never tunnel through a wall');
    }
  });

  test('all seven planting steps can be reached in actual task order, including return from water source', () => {
    let state = action(model.createState(), 'selectCharacter', { characterId: 'lin' });
    let position = { x: 0, z: 7 };
    const visited = [];
    for (const step of model.PLANTING_STEPS) {
      const targets = world.getTargets(state);
      assert.equal(targets.length, 1);
      assert.equal(targets[0].id, step.targetId);
      const target = targets[0], path = checkPath(position, target, actualObstacles);
      position = path[path.length - 1];
      assert.equal(model.getAvailableInteraction(state, target.id).enabled, true);
      visited.push(target.id);
      state = action(state, 'interact', { targetId: target.id });
    }
    assert.deepEqual(visited, ['seedling', 'plantingSpot', 'plantingSpot', 'support', 'water', 'plantingSpot', 'nameplate']);
    assert.equal(state.nursery.watered, true);
  });

  test('all mature-garden targets are reachable from the chapter entry', () => {
    let state = action(model.createState(), 'selectCharacter', { characterId: 'chen' });
    for (const step of model.PLANTING_STEPS) state = action(state, 'interact', { targetId: step.targetId });
    state = action(state, 'nextChapter');
    const targets = world.getTargets(state);
    assert.deepEqual(targets.map(target => target.id), ['sun', 'creek', 'slope', 'drain', 'shelter', 'delivery', 'clock']);
    for (const target of targets) checkPath({ x: 0, z: 5.8 }, target, actualObstacles);
    for (const start of [{ x: -6, z: -2 }, { x: 6, z: -6 }, { x: -1.5, z: 8 }, { x: 2, z: 2 }]) {
      assert.equal(canStand(start.x, start.z, actualObstacles), true);
      for (const target of targets) checkPath(start, target, actualObstacles);
    }
  });

  test('delivery remains reachable after closing a passed chapter report with leftover stock', () => {
    let state = action(model.createState(), 'selectCharacter', { characterId: 'xia' });
    for (const step of model.PLANTING_STEPS) state = action(state, 'interact', { targetId: step.targetId });
    state = action(state, 'nextChapter');
    state = action(state, 'harvest', { plotId: 'slope' });
    state = action(state, 'harvest', { plotId: 'sun' });
    state = action(state, 'wait');
    state = action(state, 'harvest', { plotId: 'creek' });
    state = action(state, 'deliver', { channel: 'contract' });
    assert.equal(state.stage, 'complete');
    assert.equal(state.inventory.length, 1);
    assert.equal(state.inventory[0].grade, 'C');
    const delivery = world.getTargets(state).find(target => target.id === 'delivery');
    assert.ok(delivery, 'completed harvest chapter must keep a delivery target for salvage');
    assert.equal(model.getAvailableInteraction(state, delivery.id).enabled, true);
    checkPath({ x: -1.5, z: 0.6 }, delivery, actualObstacles);
    const sold = action(state, 'deliver', { channel: 'wholesale' });
    assert.equal(sold.inventory.length, 0);
    assert.equal(sold.stats.rewards, state.stats.rewards);
    assert.equal(sold.rainChallenge.clock, state.rainChallenge.clock);
  });

  test('route goes around a barrier and stops beside a solid interaction object', () => {
    const obstacles = [{ minX: -1, maxX: 1, minZ: -1, maxZ: 1 }, { x: 0, z: -3, radius: 0.45 }];
    const target = { id: 'solid-target', x: 0, z: -3, radius: 1.3 };
    const path = checkPath({ x: 0, z: 3 }, target, obstacles);
    assert.ok(path.some(point => Math.abs(point.x) > 1.2), 'must route around expanded wall footprint');
    assert.ok(distance(path[path.length - 1], target) > 0.7, 'must not stand inside the solid prop');
  });

  test('enclosed target returns no path rather than crossing a wall', () => {
    const obstacles = [
      { minX: -1.8, maxX: 1.8, minZ: -1.8, maxZ: -1.5 },
      { minX: -1.8, maxX: 1.8, minZ: 1.5, maxZ: 1.8 },
      { minX: -1.8, maxX: -1.5, minZ: -1.8, maxZ: 1.8 },
      { minX: 1.5, maxX: 1.8, minZ: -1.8, maxZ: 1.8 }
    ];
    assert.equal(findPath({ x: 0, z: 4 }, { x: 0, z: 0, radius: 1.1 }, obstacles), null);
  });

  test('small-step movement can follow actual nursery route without becoming stuck', () => {
    let position = { x: 0, z: 7 };
    let state = action(model.createState(), 'selectCharacter', { characterId: 'lin' });
    const target = world.getTargets(state)[0], path = checkPath(position, target, actualObstacles);
    for (const point of path) {
      let iterations = 0;
      while (distance(position, point) > 0.025) {
        const length = distance(position, point), stride = Math.min(0.1, length);
        const next = move(position, (point.x - position.x) / length * stride, (point.z - position.z) / length * stride, actualObstacles);
        assert.ok(distance(next, position) > 0.00001, 'actual movement must not stick on a valid route');
        assert.equal(canStand(next.x, next.z, actualObstacles), true);
        position = next;
        assert.ok(++iterations < 100, 'waypoint must be reached promptly');
      }
    }
    assert.ok(distance(position, target) < target.radius);
  });
} finally { world.dispose(); }

console.log('Vineyard field navigation: ' + count + ' tests passed.');
