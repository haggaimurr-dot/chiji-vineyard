'use strict';
const defaultConfig = require('./config');
const { createPlatform } = require('./platform');
const { GameService } = require('./service');
const { createRenderer } = require('./renderer');
const { RULES, ACTIONS } = require('./rules');

function start(options) {
  options = options || {};
  const platform = options.platform || createPlatform(options);
  const config = Object.assign({}, defaultConfig, options.config || {});
  const service = new GameService(platform, config);
  const renderer = createRenderer(platform.canvas, platform.controls);
  let frameId = null, running = true, destroyed = false, lastFrame = 0;
  const view = { state: null, tab: 'farm', selectedPlot: 1, busy: true, status: '正在走进葡萄园…',
    error: '', demo: config.demo, modal: null,
    onSelectPlot(id) { if (!view.busy) { view.selectedPlot = id; view.error = ''; } },
    onTab(tab) { if (!view.busy) { view.tab = tab; view.error = ''; } },
    onAction(command) {
      const name = (ACTIONS[command.type] || {}).name || '农事';
      return run(() => service.command(command), name + '完成');
    },
    onAdvance() {
      const remaining = RULES.seasonDays - view.state.day;
      confirm('推进这一季', '将推进 ' + Math.min(RULES.advanceDays, remaining) + ' 个模拟日。天气、水分和果实会继续变化，仓库中的葡萄也会产生损耗。\n\n先检查地块，再安排下一回合。', '推进回合',
        () => run(() => service.command({ type: 'advance' }), '已推进一个经营回合'));
    },
    onHelp() {
      confirm('种好这一季', '先修剪整枝，保持通风；坐果后适度疏果，兼顾产量和粒重。留意土壤水分，避免过量施肥。\n\n成熟后采收，在仓库按渠道销售；到第 84 天查看季末复盘。离开后每现实小时生长一天。\n\n本版使用教学游戏参数，具体种植以当地品种和经验为准。', '开始种植', null, '');
    },
    onReset() {
      confirm('重新开园', '将清空这份游戏中的地块进度、经营币、设施、仓库和手记，从第一季重新开始。', '确认重新开园',
        () => run(() => service.reset(), '新的葡萄园已准备好'));
    },
    onRetry() {
      return run(() => service.state ? service.sync() : service.init(), '葡萄园已同步');
    },
    onDismissError() { view.error = ''; },
    onSell(channel) {
      const total = view.state.inventory.reduce((sum, batch) => sum + batch.weight, 0);
      if (!total) return;
      const name = channel === 'retail' ? '零售渠道' : '批发渠道';
      confirm('出售库存', '将通过' + name + '出售全部 ' + (total / 1000).toFixed(1) + ' 千克库存，售价按葡萄等级计算，并扣除对应渠道成本。', '确认出售',
        () => run(() => service.command({ type: 'sell', channel }), '库存已售出，收入记入经营账本'));
    },
    onUpgrade(facility) {
      const rule = RULES.facilities[facility];
      if (!rule) return;
      confirm('建设' + rule.name, '建造需要 ' + rule.cost + ' 经营币，使用期间每日最多产生 ' + rule.dailyCost + ' 经营币费用。设施会保留到下一季。', '确认建设',
        () => run(() => service.command({ type: 'upgrade', facility }), rule.name + '已建成'));
    },
    onNewSeason() {
      confirm('开始下一季', '保留经营币、设施和葡萄树势，开始新的生长季。请先售空本季库存。', '开始下一季',
        () => run(() => service.command({ type: 'newSeason' }), '新一季开始了'));
    }
  };
  function confirm(title, body, confirmText, action, cancelText) {
    if (view.busy || view.modal) return;
    view.modal = { title, body, confirmText, cancelText: cancelText === undefined ? '再看看' : cancelText,
      onConfirm() { view.modal = null; if (action) action(); },
      onCancel: cancelText === '' ? null : function () { view.modal = null; } };
  }
  async function run(work, message) {
    if (view.busy && view.state) return;
    view.busy = true; view.error = ''; view.status = '正在照料葡萄园…';
    try {
      const next = await work();
      if (destroyed) return;
      view.state = next; view.status = message || '葡萄园已准备好';
      if (message === '新的葡萄园已准备好') { view.tab = 'farm'; view.selectedPlot = 1; }
    } catch (error) {
      if (destroyed) return;
      if (service.state) view.state = service.state;
      view.error = error.message || '操作未完成，请重试。'; view.status = '';
    } finally {
      if (!destroyed) {
        view.busy = false;
        platform.announce(view.error || view.status);
      }
    }
  }
  function resize(info) { renderer.resize(info.width, info.height, info.dpr, info.safeTop, info.safeBottom); }
  function draw(timestamp) {
    if (!running || destroyed) return;
    const now = Number.isFinite(timestamp) ? timestamp : platform.now();
    if (!lastFrame || now - lastFrame >= 32) {
      renderer.render(view, now); platform.finishControls(); lastFrame = now;
    }
    frameId = platform.frame(draw);
  }
  platform.onTouch((x, y) => {
    if (view.busy) return;
    renderer.hit(x, y);
  });
  platform.onResize(resize);
  platform.onHide(() => { running = false; platform.cancelFrame(frameId); frameId = null; });
  platform.onShow(() => {
    if (destroyed) return;
    running = true; if (frameId === null) frameId = platform.frame(draw);
    if (!view.busy) run(() => service.state ? service.sync() : service.init(), '欢迎回到葡萄园');
  });
  resize(platform.info()); frameId = platform.frame(draw);
  const ready = run(() => service.init(), '葡萄园已准备好');
  return {
    ready,
    getState() { return view.state ? JSON.parse(JSON.stringify(view.state)) : null; },
    getStatus() { return { tab: view.tab, selectedPlot: view.selectedPlot, busy: view.busy, error: view.error, modal: view.modal && view.modal.title, demo: config.demo }; },
    getControls() { return renderer.getControls().map(control => ({
      id: control.id, label: control.label, x: control.x, y: control.y,
      width: control.width, height: control.height, disabled: control.disabled
    })); },
    destroy() { destroyed = true; running = false; platform.cancelFrame(frameId); platform.destroy(); }
  };
}
module.exports = { start };
