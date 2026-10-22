'use strict';
const assert = require('node:assert/strict');
const { GameService } = require('../minigame/src/service');
const { createFarm, applyAction } = require('../minigame/src/model');
const baseConfig = require('../minigame/src/config');
const copy = value => JSON.parse(JSON.stringify(value));
const startAt = 1700000000000;
const tests = [];
function test(name, fn) { tests.push([name, fn]); }
function memoryPlatform() {
  const values = new Map();
  return {
    values, time: startAt, requests: [], count: 0, writesFailed: false,
    now() { return this.time; },
    read(key) { return values.has(key) ? copy(values.get(key)) : null; },
    write(key, value) {
      if (this.writesFailed) { const e = new Error('存储写入失败'); e.code = 'STORAGE_WRITE'; throw e; }
      values.set(key, copy(value));
    },
    remove(key) { values.delete(key); },
    requestId() { return 'fixture-request-' + (++this.count); },
    async login() { return 'fixture-wechat-code'; },
    async request(url, body, token) { this.requests.push({ url, body: copy(body), token }); return this.server(body); }
  };
}
function serverFixture() {
  const p = memoryPlatform();
  let serverState = createFarm(startAt, 20261012), executions = 0;
  const seen = new Map();
  p.server = async body => {
    if (body.action === 'game.login') return { status: 200, body: { ok: true, data: { token: 'fixture-game-session' } } };
    if (body.action === 'game.bootstrap' || body.action === 'game.sync') return { status: 200, body: { ok: true, data: { state: copy(serverState) } } };
    if (body.action === 'game.action' || body.action === 'game.reset') {
      if (seen.has(body.data.requestId)) return copy(seen.get(body.data.requestId));
      if (body.data.expectedVersion !== serverState.version)
        return { status: 409, body: { ok: false, error: { code: 'VERSION_CONFLICT', message: '状态已更新' } } };
      if (body.action === 'game.action') serverState = applyAction(serverState, body.data.command, p.time);
      else { const version = serverState.version; serverState = createFarm(p.time, 20261013); serverState.version = version + 1; }
      executions++;
      const response = { status: 200, body: { ok: true, data: { state: copy(serverState) } } };
      seen.set(body.data.requestId, copy(response)); return response;
    }
    throw new Error('Unexpected action ' + body.action);
  };
  return {
    p, config: { ...baseConfig, demo: false, apiBaseURL: 'https://game.fixture.example' },
    state: () => copy(serverState), executions: () => executions,
    advanceOther() { serverState = applyAction(serverState, { type: 'water', plotId: 2 }, p.time); }
  };
}

test('演示存档持久化，并保留离线不足一小时的余量', async () => {
  const p = memoryPlatform(), service = new GameService(p, baseConfig);
  await service.init();
  await service.command({ type: 'water', plotId: 1 });
  const saved = copy(service.state);
  const restored = new GameService(p, baseConfig); await restored.init();
  assert.deepEqual(restored.state, saved);
  p.time += 5400000; await restored.sync();
  assert.equal(restored.state.day, 1);
  assert.equal(restored.state.lastSettledAt, startAt + 3600000);
});
test('另一页面更新后拒绝旧版本，刷新本地状态而不重复扣币', async () => {
  const p = memoryPlatform(), a = new GameService(p, baseConfig), b = new GameService(p, baseConfig);
  await a.init(); await b.init();
  await a.command({ type: 'water', plotId: 1 });
  await assert.rejects(() => b.command({ type: 'prune', plotId: 1 }), { code: 'VERSION_CONFLICT' });
  assert.equal(b.state.coins, a.state.coins);
  assert.equal(b.state.version, a.state.version);
});
test('写入失败不会把未保存的状态当作已完成', async () => {
  const p = memoryPlatform(), service = new GameService(p, baseConfig); await service.init();
  const original = copy(service.state); p.writesFailed = true;
  await assert.rejects(() => service.command({ type: 'water', plotId: 1 }), { code: 'STORAGE_WRITE' });
  assert.deepEqual(service.state, original); assert.deepEqual(p.read(service.saveKey), original);
});
test('损坏存档不会静默清空，显式重开后才替换', async () => {
  const p = memoryPlatform(), service = new GameService(p, baseConfig);
  p.write(service.saveKey, { schemaVersion: -1 });
  await assert.rejects(() => service.init(), { code: 'DAMAGED_SAVE' });
  assert.deepEqual(p.read(service.saveKey), { schemaVersion: -1 });
  await service.reset(); assert.equal(service.state.day, 0);
});
test('未配置正式HTTPS服务时不降级为演示或发网络请求', async () => {
  const p = memoryPlatform(), service = new GameService(p, { ...baseConfig, demo: false });
  await assert.rejects(() => service.init(), { code: 'SERVER_NOT_CONFIGURED' });
  assert.equal(p.requests.length, 0); assert.equal(service.state, null);
});
test('丢响应保留请求号，重试只执行一次，并阻止新的操作', async () => {
  const f = serverFixture(), service = new GameService(f.p, f.config); await service.init();
  const normal = f.p.server;
  let lose = true;
  f.p.server = async body => {
    const response = await normal(body);
    if (body.action === 'game.action' && lose) { lose = false; const e = new Error('网络未确认'); e.code = 'NETWORK_UNCERTAIN'; throw e; }
    return response;
  };
  await assert.rejects(() => service.command({ type: 'water', plotId: 1 }), { code: 'NETWORK_UNCERTAIN' });
  const pending = f.p.read(service.pendingKey);
  await assert.rejects(() => service.command({ type: 'prune', plotId: 1 }), { code: 'PENDING_ACTION' });
  await service.sync();
  const commands = f.p.requests.filter(r => r.body.action === 'game.action');
  assert.equal(commands.length, 2);
  assert.deepEqual(commands[0].body.data, commands[1].body.data);
  assert.equal(commands[0].body.data.requestId, pending.requestId);
  assert.equal(f.executions(), 1);
  assert.equal(f.p.read(service.pendingKey), null);
  assert.deepEqual(service.state, f.state());
});
test('重新打开后恢复未确认请求，不重复动作或覆盖新存档', async () => {
  const f = serverFixture(), first = new GameService(f.p, f.config); await first.init();
  const normal = f.p.server;
  let lost = false;
  f.p.server = async body => {
    const response = await normal(body);
    if (body.action === 'game.action' && !lost) { lost = true; const e = new Error('lost'); e.code = 'NETWORK_UNCERTAIN'; throw e; }
    return response;
  };
  await assert.rejects(() => first.command({ type: 'water', plotId: 1 }));
  f.advanceOther();
  const reopened = new GameService(f.p, f.config); await reopened.init();
  assert.equal(f.executions(), 1); assert.deepEqual(reopened.state, f.state());
  assert.equal(reopened.state.plots[1].lastActions.water, 0);
});
test('版本冲突返回最新状态，明确拒绝结果清理待核实记录', async () => {
  const f = serverFixture(), service = new GameService(f.p, f.config); await service.init(); f.advanceOther();
  await assert.rejects(() => service.command({ type: 'prune', plotId: 1 }), { code: 'VERSION_CONFLICT' });
  assert.equal(f.p.read(service.pendingKey), null); assert.deepEqual(service.state, f.state());
});
test('服务器500可能是提交结果不确定，保留同一请求等待核实', async () => {
  const f = serverFixture(), service = new GameService(f.p, f.config); await service.init();
  const normal = f.p.server;
  f.p.server = async body => body.action === 'game.action' ? { status: 500, body: { ok: false, error: { code: 'INTERNAL_ERROR' } } } : normal(body);
  await assert.rejects(() => service.command({ type: 'water', plotId: 1 }), { code: 'NETWORK_UNCERTAIN' });
  assert.ok(f.p.read(service.pendingKey)); assert.equal(f.executions(), 0);
});
test('非法服务器状态不会覆盖正确快照或丢掉待核实动作', async () => {
  const f = serverFixture(), service = new GameService(f.p, f.config); await service.init();
  const original = copy(service.state), normal = f.p.server;
  f.p.server = async body => body.action === 'game.action' ? { status: 200, body: { ok: true, data: { state: { coins: -5 } } } } : normal(body);
  await assert.rejects(() => service.command({ type: 'water', plotId: 1 }), { code: 'INVALID_SERVER_STATE' });
  assert.deepEqual(service.state, original); assert.ok(f.p.read(service.pendingKey));
});
test('成功状态码但响应不完整时保留请求号，防止重复提交', async () => {
  for (const malformed of ['<html>proxy response</html>', { ok: true }, { ok: false }, { ok: true, data: [] }]) {
    const f = serverFixture(), service = new GameService(f.p, f.config); await service.init();
    const original = copy(service.state), normal = f.p.server;
    f.p.server = async body => body.action === 'game.action' ? { status: 200, body: malformed } : normal(body);
    await assert.rejects(() => service.command({ type: 'water', plotId: 1 }), { code: 'NETWORK_UNCERTAIN' });
    assert.deepEqual(service.state, original); assert.ok(f.p.read(service.pendingKey));
  }
});
test('正式重开使用独立确认、请求号和版本，并递增版本', async () => {
  const f = serverFixture(), service = new GameService(f.p, f.config); await service.init();
  await service.command({ type: 'water', plotId: 1 }); const version = service.state.version;
  await service.reset();
  const request = f.p.requests.find(r => r.body.action === 'game.reset');
  assert.equal(request.body.data.confirm, '清空游戏进度');
  assert.equal(request.body.data.expectedVersion, version);
  assert.equal(service.state.version, version + 1); assert.equal(service.state.coins, 18000);
});
(async () => {
  for (const [name, fn] of tests) { await fn(); console.log('PASS ' + name); }
  console.log('Passed ' + tests.length + ' vineyard service tests.');
})().catch(error => { console.error(error); process.exitCode = 1; });
