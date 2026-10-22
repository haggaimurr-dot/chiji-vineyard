'use strict';

// Real user-input QA inventory:
// help/journal-help -> readable modal, close; plot-1..4 -> selection;
// water/fertilize/prune/thin/protect/harvest -> paid, saved state changes;
// early harvest and cooldown -> disabled, no state mutation;
// advance -> cancel leaves state unchanged, confirm advances seven days;
// all four tabs -> view change; wholesale/retail -> cancel/confirm sale;
// all three upgrades -> cancel/confirm, debit, built-state disabled;
// reset -> cancel/confirm; season-sell -> stock reminder; new-season ->
// cancel/confirm, season two, reload persistence; modal blocks base controls;
// retry/dismiss-error -> recovery; corrupt JSON save -> explicit reset only.
// Visual claims: four illustrated plots, dense inventory, completed facilities,
// season report, modal, 390x844 / 320x568 / desktop viewport fit and no pageerrors.
// All gameplay input uses actual DOM buttons or touchscreen taps. evaluate only
// reads public state/status and DOM bounds; storage fault injection is confined
// to isolated addInitScript fixtures, never to game action/state APIs.

const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { spawn } = require('node:child_process');
const { RULES } = require('../minigame/src/rules');
const root = path.resolve(__dirname, '..');
const artifacts = path.join(root, 'docs/test-evidence/2026-10-12-vineyard-game');
fs.mkdirSync(artifacts, { recursive: true });
const url = process.env.VINEYARD_PREVIEW_URL || 'http://127.0.0.1:5199';
const results = {
  runtime: 'Headless Chromium browser preview; no WeChat device acceptance',
  previewURL: url, checks: [], controls: {}, screenshots: [], pageErrors: [],
  exclusions: ['WeChat developer tools, device performance, production AppID, production login and deployment'],
};
let browser, server;
const contexts = [];
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
const check = message => { results.checks.push(message); console.log('PASS ' + message); };
const state = page => page.evaluate(() => window.vineyardGame.getState());
const status = page => page.evaluate(() => window.vineyardGame.getStatus());
const control = (page, id) => page.locator(`[data-control="${id}"]`);

async function ensurePreview() {
  const alive = async () => {
    try { const response = await fetch(url, { signal: AbortSignal.timeout(2000) }); return response.ok; }
    catch (_) { return false; }
  };
  if (await alive()) return;
  if (process.env.VINEYARD_PREVIEW_URL) throw new Error('Configured preview URL is unavailable: ' + url);
  server = spawn(process.execPath, [path.join(root, 'minigame/scripts/preview.cjs')], {
    cwd: root, env: { ...process.env, VINEYARD_PREVIEW_PORT: '5199' }, windowsHide: true, stdio: 'ignore',
  });
  for (let i = 0; i < 40; i++) { if (await alive()) return; await delay(200); }
  throw new Error('Could not start the vineyard preview server');
}
async function idle(page) {
  await page.waitForFunction(() => window.vineyardGame && !window.vineyardGame.getStatus().busy);
  // The game redraws every 32ms. Wait for the real controls to reflect state.
  await page.waitForTimeout(45);
}
async function fresh(name, options, initScript) {
  const context = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2,
    isMobile: true, hasTouch: true, timezoneId: 'Asia/Shanghai', ...options });
  contexts.push(context);
  if (initScript) await context.addInitScript(initScript);
  const page = await context.newPage();
  page.on('pageerror', error => results.pageErrors.push({ scenario: name, message: error.message }));
  await page.goto(url, { waitUntil: 'networkidle' }); await idle(page);
  return page;
}
async function click(page, id) {
  const node = control(page, id);
  await node.waitFor({ state: 'visible' });
  assert.equal(await node.isDisabled(), false, `${id} is enabled for this user action`);
  await node.click();
  results.controls[id] = (results.controls[id] || 0) + 1;
  await idle(page);
}
async function disabledTap(page, id) {
  const node = control(page, id); await node.waitFor({ state: 'visible' });
  assert.equal(await node.isDisabled(), true, `${id} must be disabled`);
  const before = await state(page), box = await node.boundingBox();
  await page.touchscreen.tap(box.x + box.width / 2, box.y + box.height / 2); await idle(page);
  assert.deepEqual(await state(page), before, `disabled ${id} cannot change the save`);
  results.controls[id + ':disabled'] = (results.controls[id + ':disabled'] || 0) + 1;
}
async function select(page, id) {
  await click(page, `plot-${id}`);
  assert.equal((await status(page)).selectedPlot, id);
  const selected = (await state(page)).plots.find(plot => plot.id === id);
  assert.match(await control(page, 'action-water').getAttribute('aria-label'), new RegExp(selected.name));
}
async function action(page, type) {
  const before = await state(page);
  await click(page, `action-${type}`);
  const after = await state(page);
  assert.equal((await status(page)).error, '', `successful ${type} has no error`);
  assert.equal(after.version, before.version + 1, `${type} saves a new state version`);
  assert(after.coins < before.coins, `${type} deducts its actual cost`);
}
async function modal(page, confirm) {
  assert((await status(page)).modal, 'confirmation is displayed');
  const ids = await page.locator('[data-control]').evaluateAll(nodes => nodes.map(node => node.dataset.control));
  assert(ids.every(id => id.startsWith('modal-')), 'modal hides underlying actionable controls');
  await click(page, confirm ? 'modal-confirm' : 'modal-cancel');
  assert.equal((await status(page)).modal, null);
}
async function advance(page) {
  const before = await state(page); await click(page, 'advance'); await modal(page, true);
  const after = await state(page);
  assert.equal(after.day, Math.min(84, before.day + 7));
  assert.equal(after.version, before.version + 1);
  assert.equal((await status(page)).error, '');
}
async function tab(page, name) {
  await click(page, `tab-${name}`); assert.equal((await status(page)).tab, name);
}
async function bounds(page) {
  const geometry = await page.evaluate(() => {
    const canvas = document.getElementById('vineyard-canvas').getBoundingClientRect();
    return { width: innerWidth, height: innerHeight,
      canvas: { x: canvas.x, y: canvas.y, right: canvas.right, bottom: canvas.bottom },
      controls: [...document.querySelectorAll('[data-control]')].map(node => {
        const b = node.getBoundingClientRect();
        return { id: node.dataset.control, x: b.x, y: b.y, right: b.right, bottom: b.bottom };
      }), overflow: document.documentElement.scrollWidth > innerWidth || document.documentElement.scrollHeight > innerHeight,
    };
  });
  assert.equal(geometry.overflow, false, 'preview page has no viewport overflow');
  for (const box of geometry.controls) {
    assert(box.x >= geometry.canvas.x - 1 && box.y >= geometry.canvas.y - 1 &&
      box.right <= geometry.canvas.right + 1 && box.bottom <= geometry.canvas.bottom + 1, `${box.id} is within canvas bounds`);
    assert(box.x >= -1 && box.y >= -1 && box.right <= geometry.width + 1 && box.bottom <= geometry.height + 1,
      `${box.id} is within the visible viewport`);
  }
  return { width: geometry.width, height: geometry.height, count: geometry.controls.length };
}
async function capture(page, name) {
  await idle(page); const viewport = await bounds(page);
  const filename = name + '.png';
  await page.screenshot({ path: path.join(artifacts, filename), scale: 'css' });
  results.screenshots.push({ filename, ...viewport });
  console.log('SCREENSHOT ' + path.join(artifacts, filename));
}
async function grow(page, allPlots) {
  await tab(page, 'farm');
  const ids = allPlots ? [1, 2, 3, 4] : [1];
  for (let day = 0; day <= 77; day += 7) {
    if (day) await advance(page);
    for (const id of ids) {
      await select(page, id); await action(page, 'water');
      if (day === 0 || day === 28) await action(page, 'prune');
      if (day === 7 && id === 1) await action(page, 'fertilize');
      if (day === 28) { await action(page, 'thin'); await action(page, 'protect'); }
    }
  }
  assert.equal((await state(page)).day, 77);
}

async function run() {
  await ensurePreview();
  const candidates = [process.env.PLAYWRIGHT_BROWSER,
    'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
    'C:/Users/86182/AppData/Local/ms-playwright/chromium-1248/chrome-win64/chrome.exe'].filter(Boolean);
  const executablePath = candidates.find(file => fs.existsSync(file));
  if (!executablePath) throw new Error('No installed Chromium or Edge browser found');
  results.browserExecutable = executablePath;
  browser = await chromium.launch({ headless: true, executablePath });

  const page = await fresh('whole-season');
  const initial = await state(page);
  assert.equal(initial.day, 0); assert.equal(initial.season, 1); assert.equal(initial.coins, RULES.initialCoins);
  assert.equal(initial.plots.length, 4);
  await capture(page, 'initial-390x844');
  await page.setViewportSize({ width: 320, height: 568 }); await capture(page, 'initial-320x568');
  await page.setViewportSize({ width: 1360, height: 900 }); await capture(page, 'initial-desktop-1360x900');
  await page.setViewportSize({ width: 390, height: 844 }); await idle(page);
  check('Initial illustrated farm and all controls fit 390x844, 320x568 and desktop');
  await click(page, 'help'); await capture(page, 'help-390x844'); await modal(page, true);
  assert.deepEqual(await state(page), initial, 'reading help does not mutate the farm');
  await disabledTap(page, 'action-harvest');
  await click(page, 'advance'); await modal(page, false);
  assert.deepEqual(await state(page), initial, 'cancelled round leaves all state unchanged');
  check('Help modal blocks underlying controls; premature harvest and cancelled round cannot change progress');
  await grow(page, true);
  await disabledTap(page, 'action-water');
  await capture(page, 'harvest-77-days');
  for (const id of [1, 2, 3, 4]) { await select(page, id); await action(page, 'harvest'); }
  const harvested = await state(page);
  assert.equal(harvested.inventory.length, 4);
  assert(harvested.plots.every(plot => plot.harvested));
  assert(harvested.inventory.every(batch => batch.brix >= RULES.minHarvestBrix));
  check('Four real UI plots complete irrigation, pruning, fertilizing, thinning, protection and mature harvest by day 77');
  await tab(page, 'inventory'); await capture(page, 'inventory-four-batches');
  await click(page, 'sell-retail'); await modal(page, false);
  assert.deepEqual(await state(page), harvested, 'cancelled sale leaves inventory and balance untouched');
  await click(page, 'sell-retail'); await modal(page, true);
  const sold = await state(page);
  assert.equal(sold.inventory.length, 0); assert(sold.coins > harvested.coins);
  assert.equal(sold.stats.soldWeight, harvested.stats.harvestWeight);
  await disabledTap(page, 'sell-retail'); await disabledTap(page, 'sell-wholesale');
  check('Retail cancellation/confirmation, selling all four batches, recorded proceeds and empty-stock disabled controls');
  await tab(page, 'farm'); await advance(page); await capture(page, 'report-84-days');
  assert.equal((await state(page)).day, 84); assert((await state(page)).report);
  const completed = await state(page);
  await click(page, 'new-season'); await modal(page, false);
  assert.deepEqual(await state(page), completed);
  await click(page, 'new-season'); await modal(page, true);
  const nextSeason = await state(page);
  assert.equal(nextSeason.season, 2); assert.equal(nextSeason.day, 0);
  await page.reload({ waitUntil: 'networkidle' }); await idle(page);
  assert.deepEqual(await state(page), nextSeason, 'reload restores the saved second season');
  check('Day 84 report, next-season cancel/confirm and reload persistence');

  const facilityPage = await fresh('facilities-reset');
  await tab(facilityPage, 'facilities');
  for (const facility of ['drip', 'shelter', 'coldStorage']) {
    const before = await state(facilityPage);
    await click(facilityPage, 'upgrade-' + facility); await modal(facilityPage, false);
    assert.deepEqual(await state(facilityPage), before, 'cancelled construction is free');
    await click(facilityPage, 'upgrade-' + facility); await modal(facilityPage, true);
    const after = await state(facilityPage);
    assert.equal(after.facilities[facility], true);
    assert.equal(after.coins, before.coins - RULES.facilities[facility].cost);
    await disabledTap(facilityPage, 'upgrade-' + facility);
  }
  await capture(facilityPage, 'facilities-all-built');
  await tab(facilityPage, 'journal'); await capture(facilityPage, 'journal-construction');
  await click(facilityPage, 'journal-help'); await modal(facilityPage, true);
  const beforeReset = await state(facilityPage);
  await click(facilityPage, 'reset'); await modal(facilityPage, false);
  assert.deepEqual(await state(facilityPage), beforeReset);
  await click(facilityPage, 'reset'); await modal(facilityPage, true);
  const reset = await state(facilityPage);
  assert.equal(reset.coins, RULES.initialCoins); assert.equal(reset.day, 0); assert.equal(reset.season, 1);
  assert.deepEqual(reset.facilities, { drip: false, shelter: false, coldStorage: false });
  check('All tabs, three facility cancel/confirm/debit/built states, journal help and reset cancel/confirm');

  const wholesalePage = await fresh('wholesale-season-stock');
  await grow(wholesalePage, false); await select(wholesalePage, 1); await action(wholesalePage, 'harvest');
  await advance(wholesalePage); assert.equal((await state(wholesalePage)).day, 84);
  assert((await state(wholesalePage)).inventory.length > 0);
  assert.equal(await control(wholesalePage, 'new-season').count(), 0, 'remaining stock requires sale before a new season');
  await click(wholesalePage, 'season-sell'); assert.equal((await status(wholesalePage)).tab, 'inventory');
  const beforeWholesale = await state(wholesalePage);
  await click(wholesalePage, 'sell-wholesale'); await modal(wholesalePage, true);
  const afterWholesale = await state(wholesalePage);
  assert.equal(afterWholesale.inventory.length, 0); assert(afterWholesale.coins > beforeWholesale.coins);
  check('Season-end remaining stock routes to sales and wholesale is usable through actual UI confirmation');

  const damaged = await fresh('damaged-save', {}, () => {
    const key = 'chiji_vineyard:demo:v1:save';
    if (!sessionStorage.getItem('qa-damaged-save')) {
      localStorage.setItem(key, '{broken-json'); sessionStorage.setItem('qa-damaged-save', '1');
    }
  });
  assert.equal(await state(damaged), null); assert((await status(damaged)).error);
  await capture(damaged, 'damaged-save-recovery');
  await click(damaged, 'retry'); assert.equal(await state(damaged), null);
  await click(damaged, 'reset'); await modal(damaged, false); assert.equal(await state(damaged), null);
  await click(damaged, 'reset'); await modal(damaged, true);
  assert.equal((await state(damaged)).day, 0); assert.equal((await status(damaged)).error, '');
  await damaged.reload({ waitUntil: 'networkidle' }); await idle(damaged); assert(await state(damaged));
  check('Malformed stored JSON shows recovery; retry/cancel retain it, explicit reset restores a durable farm');

  const readFailure = await fresh('temporary-storage-read', {}, () => {
    const original = Storage.prototype.getItem; let reads = 0;
    Storage.prototype.getItem = function (key) {
      if (key === 'chiji_vineyard:demo:v1:save' && [2, 4].includes(++reads)) return '{fault-fixture';
      return original.call(this, key);
    };
  });
  const beforeFailure = await state(readFailure);
  await click(readFailure, 'action-water'); assert((await status(readFailure)).error);
  assert.deepEqual(await state(readFailure), beforeFailure, 'read failure does not award an unsaved action');
  await capture(readFailure, 'error-banner'); await click(readFailure, 'dismiss-error');
  assert.equal((await status(readFailure)).error, '');
  await action(readFailure, 'water');
  await click(readFailure, 'action-prune'); assert((await status(readFailure)).error);
  await click(readFailure, 'retry'); assert.equal((await status(readFailure)).error, '');
  check('Temporary storage read failures show retry/dismiss; failed actions leave state unchanged and sync recovers');

  assert.deepEqual(results.pageErrors, [], 'no uncaught browser page errors');
  results.completed = true;
  check('No browser page errors across normal, narrow-screen, dense-state and recovery scenarios');
}

run().catch(error => {
  results.completed = false; results.failure = error.stack || String(error);
  console.error(error); process.exitCode = 1;
}).finally(async () => {
  fs.writeFileSync(path.join(artifacts, 'ui-results.json'), JSON.stringify(results, null, 2));
  for (const context of contexts) await context.close().catch(() => {});
  if (browser) await browser.close().catch(() => {});
  if (server) server.kill();
  console.log(JSON.stringify({ completed: results.completed, checks: results.checks.length,
    controls: Object.keys(results.controls).length, screenshots: results.screenshots.length,
    pageErrors: results.pageErrors.length, evidence: artifacts }, null, 2));
});
