'use strict';
const assert = require('node:assert/strict');
const { start } = require('../minigame/src/main');
const saved = new Map(), events = {}, frames = new Map();
let nextFrame = 0, touchCalls = 0, transforms = 0;
const ctx = new Proxy({}, {
  get(target, key) {
    if (key in target) return target[key];
    if (key === 'measureText') return value => ({ width: String(value).length * 6 });
    if (key === 'createLinearGradient' || key === 'createRadialGradient') return () => ({ addColorStop() {} });
    if (key === 'setTransform') return () => { transforms++; };
    return () => {};
  },
  set(target, key, value) { target[key] = value; return true; }
});
const canvas = {
  width: 0, height: 0, getContext: () => ctx,
  requestAnimationFrame(fn) { frames.set(++nextFrame, fn); return nextFrame; },
  cancelAnimationFrame(id) { frames.delete(id); }
};
global.wx = {
  createCanvas: () => canvas,
  getWindowInfo: () => ({ windowWidth: 390, windowHeight: 844, pixelRatio: 2, safeArea: { top: 20, bottom: 824 } }),
  getStorageSync: key => saved.get(key) || '', setStorageSync: (key, value) => saved.set(key, value),
  removeStorageSync: key => saved.delete(key),
  onTouchEnd(fn) { events.touch = fn; }, offTouchEnd() { events.touch = null; },
  onWindowResize(fn) { events.resize = fn; }, offWindowResize() { events.resize = null; },
  onShow(fn) { events.show = fn; }, offShow() { events.show = null; },
  onHide(fn) { events.hide = fn; }, offHide() { events.hide = null; }
};
let clock = 1000;
function paint() { const item = frames.entries().next().value; if (item) { frames.delete(item[0]); item[1](clock += 40); } }
async function waitAction() { await new Promise(resolve => setImmediate(resolve)); paint(); }
function tap(game, id) {
  const c = game.getControls().find(item => item.id === id);
  assert.ok(c, 'Native control exists: ' + id); assert.equal(c.disabled, false, 'Native control enabled: ' + id);
  touchCalls++; events.touch({ changedTouches: [{ clientX: c.x + c.width / 2, clientY: c.y + c.height / 2 }] });
}
(async () => {
  const game = start(); await game.ready; paint();
  assert.equal(typeof document, 'undefined', 'Native startup has no DOM dependency');
  assert.equal(canvas.width, 780); assert.equal(canvas.height, 1688);
  for (const control of game.getControls()) {
    assert.ok(control.x >= 0 && control.y >= 20);
    assert.ok(control.x + control.width <= 390.01 && control.y + control.height <= 824.01);
  }
  tap(game, 'plot-3'); paint(); assert.equal(game.getStatus().selectedPlot, 3);
  const before = game.getState().coins; tap(game, 'action-water'); await waitAction();
  assert.equal(game.getState().coins, before - 70, 'Native touch executes exactly one action');
  const disabled = game.getControls().find(c => c.id === 'action-water');
  assert.equal(disabled.disabled, true);
  events.touch({ changedTouches: [{ clientX: disabled.x + 4, clientY: disabled.y + 4 }] }); await waitAction();
  assert.equal(game.getState().coins, before - 70, 'Disabled native action does not execute');
  tap(game, 'advance'); paint();
  assert.ok(game.getStatus().modal); assert.ok(game.getControls().every(c => c.id.startsWith('modal-')));
  tap(game, 'modal-cancel'); paint(); assert.equal(game.getState().day, 0);
  tap(game, 'advance'); paint(); tap(game, 'modal-confirm'); await waitAction(); assert.equal(game.getState().day, 7);
  events.hide(); assert.equal(frames.size, 0, 'Background cancels rendering');
  events.show(); await waitAction(); assert.ok(frames.size > 0, 'Foreground resumes rendering');
  assert.ok(transforms > 0); assert.ok(saved.size > 0); assert.ok(touchCalls >= 5);
  game.destroy(); assert.equal(frames.size, 0); assert.equal(events.touch, null);
  console.log('PASS 原生微信Canvas无DOM启动、DPR/安全区、真实触摸回调、禁用与单次操作、回合确认、前后台与资源释放');
  delete global.wx;
})().catch(error => { delete global.wx; console.error(error); process.exitCode = 1; });
