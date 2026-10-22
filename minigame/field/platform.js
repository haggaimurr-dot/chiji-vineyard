'use strict';

// Two canvases: the visible WebGL surface and a 2D HUD. On WeChat the HUD is
// composited as a texture, so the game never relies on a DOM overlay.
function createFieldPlatform() {
  const native = typeof wx !== 'undefined' && typeof wx.createCanvas === 'function';
  const canvas = native ? wx.createCanvas() : document.getElementById('field-canvas');
  const hudCanvas = native ? wx.createCanvas() : document.getElementById('field-hud');
  const host = native ? null : document.getElementById('field-controls');
  if (!canvas || !hudCanvas) throw new Error('葡萄园画面未准备好');
  const disposers = [], buttons = new Map();
  let seen = new Set(), input = null, resized = null, shown = null, hidden = null;
  let visible = true;
  // WebGLRenderer registers context loss listeners even on a provided canvas.
  if (!canvas.style) canvas.style = {};
  if (!canvas.addEventListener) canvas.addEventListener = () => {};
  if (!canvas.removeEventListener) canvas.removeEventListener = () => {};
  const api = {
    native, canvas, hudCanvas,
    createCanvas: () => native ? wx.createCanvas() : document.createElement('canvas'),
    info() {
      if (native) {
        const i = typeof wx.getWindowInfo === 'function' ? wx.getWindowInfo() : wx.getSystemInfoSync();
        return { width: i.windowWidth, height: i.windowHeight, dpr: Math.min(1.5, i.pixelRatio || 1),
          safeTop: i.safeArea ? Math.max(0, i.safeArea.top) : 0,
          safeBottom: i.safeArea ? Math.max(0, i.windowHeight - i.safeArea.bottom) : 0 };
      }
      const box = canvas.getBoundingClientRect();
      return { width: Math.max(1, box.width), height: Math.max(1, box.height),
        dpr: Math.min(1.5, window.devicePixelRatio || 1), safeTop: 0, safeBottom: 0 };
    },
    read(key) {
      try {
        const value = native ? wx.getStorageSync(key) : localStorage.getItem(key);
        if (value === undefined || value === null || value === '') return null;
        return typeof value === 'string' ? JSON.parse(value) : value;
      } catch (_) { const e = new Error('无法读取这份下地体验存档。可以重试读取，或确认重新开始。'); e.code = 'STORAGE_READ'; throw e; }
    },
    write(key, value) {
      try {
        const data = JSON.stringify(value);
        if (native) wx.setStorageSync(key, data); else localStorage.setItem(key, data);
      } catch (_) { const e = new Error('进度未能保存，请检查设备空间后重试。'); e.code = 'STORAGE_WRITE'; throw e; }
    },
    frame(callback) {
      if (!visible) return null;
      if (typeof requestAnimationFrame === 'function') return requestAnimationFrame(callback);
      if (canvas.requestAnimationFrame) return canvas.requestAnimationFrame(callback);
      return setTimeout(() => callback(Date.now()), 33);
    },
    cancelFrame(id) {
      if (id === null || id === undefined) return;
      if (typeof cancelAnimationFrame === 'function') cancelAnimationFrame(id);
      else if (canvas.cancelAnimationFrame) canvas.cancelAnimationFrame(id);
      else clearTimeout(id);
    },
    onInput(fn) { input = fn; }, onResize(fn) { resized = fn; },
    onShow(fn) { shown = fn; }, onHide(fn) { hidden = fn; },
    controls: {
      onControlsStart() { seen = new Set(); },
      onControl(control) {
        if (!host) return;
        seen.add(control.id);
        let node = buttons.get(control.id);
        if (!node) {
          node = document.createElement('button'); node.type = 'button';
          node.className = 'field-control'; node.dataset.control = control.id;
          host.appendChild(node); buttons.set(control.id, node);
          node.addEventListener('pointerdown', e => {
            if (control.id !== 'field-interact' || node.disabled) return;
            node._suppressClick = true; node.setPointerCapture(e.pointerId);
            if (input) input({phase:'control-start',controlId:control.id,id:e.pointerId});
          });
          for (const phase of ['pointerup','pointercancel']) node.addEventListener(phase, e => {
            if (control.id === 'field-interact' && input) input({phase:'control-end',controlId:control.id,id:e.pointerId});
          });
          node.addEventListener('click', e => {
            e.stopPropagation();
            if (node._suppressClick) { node._suppressClick = false; return; }
            if (node._control && !node._control.disabled) node._control.onPress();
          });
        }
        node._control = control; node.disabled = !!control.disabled;
        node.setAttribute('aria-label', control.label); node.title = control.label;
        Object.assign(node.style, { left: control.x + 'px', top: control.y + 'px', width: control.width + 'px', height: control.height + 'px' });
      }
    },
    finishControls() { for (const [id, node] of buttons) if (!seen.has(id)) { node.remove(); buttons.delete(id); } },
    announce(message) {
      if (native) return;
      const n = document.getElementById('field-status'); if (n && n.textContent !== message) n.textContent = message;
    },
    fail(message) {
      if (native && typeof wx.showModal === 'function') { wx.showModal({title:'葡萄园暂时无法打开',content:String(message),showCancel:false}); return; }
      const i = api.info(); hudCanvas.width = Math.round(i.width); hudCanvas.height = Math.round(i.height);
      const c = hudCanvas.getContext('2d');
      if (!c) return;
      c.fillStyle = '#183d32'; c.fillRect(0, 0, i.width, i.height); c.fillStyle = '#faf5e9'; c.font = '18px sans-serif';
      c.fillText('葡萄园暂时无法打开', 26, 80); c.font = '13px sans-serif';
      const rows = String(message).match(/.{1,23}/g) || [];
      rows.forEach((row, k) => c.fillText(row, 26, 120 + k * 23)); api.announce(message);
    },
    destroy() {
      disposers.forEach(fn => fn()); buttons.forEach(n => n.remove()); buttons.clear();
      input = resized = shown = hidden = null;
    }
  };
  function dispatch(phase, point, key) {
    if (!input) return;
    input({ phase, id: point.identifier === undefined ? key : point.identifier,
      x: point.clientX === undefined ? point.x : point.clientX,
      y: point.clientY === undefined ? point.y : point.clientY });
  }
  if (native) {
    const handlers = {};
    for (const phase of ['start', 'move', 'end', 'cancel']) {
      const fn = e => (e.changedTouches || e.touches || []).forEach((p, k) => dispatch(phase, p, k));
      handlers[phase] = fn;
      const suffix = phase[0].toUpperCase() + phase.slice(1);
      if (wx['onTouch' + suffix]) wx['onTouch' + suffix](fn);
      disposers.push(() => { if (wx['offTouch' + suffix]) wx['offTouch' + suffix](fn); });
    }
    const resize = () => resized && resized(api.info());
    const show = () => { visible = true; if (shown) shown(); };
    const hide = () => { visible = false; if (hidden) hidden(); };
    if (wx.onWindowResize) wx.onWindowResize(resize); wx.onShow(show); wx.onHide(hide);
    disposers.push(() => { if (wx.offWindowResize) wx.offWindowResize(resize); if (wx.offShow) wx.offShow(show); if (wx.offHide) wx.offHide(hide); });
  } else {
    for (const phase of ['down', 'move', 'up', 'cancel']) {
      const fn = e => {
        const box = canvas.getBoundingClientRect();
        if (phase === 'down') canvas.setPointerCapture(e.pointerId);
        if (input) input({ phase: { down: 'start', move: 'move', up: 'end', cancel: 'cancel' }[phase],
          id: e.pointerId, x: e.clientX - box.left, y: e.clientY - box.top });
        if (phase === 'up' || phase === 'cancel') { if (canvas.hasPointerCapture(e.pointerId)) canvas.releasePointerCapture(e.pointerId); }
      };
      canvas.addEventListener('pointer' + phase, fn);
      disposers.push(() => canvas.removeEventListener('pointer' + phase, fn));
    }
    const keys = new Set(['KeyW','KeyA','KeyS','KeyD','ArrowUp','ArrowDown','ArrowLeft','ArrowRight','KeyE','Escape']);
    const keyDown = e => { if (keys.has(e.code) && !/INPUT|TEXTAREA/.test(e.target.tagName)) { e.preventDefault(); if (input) input({ phase:'key', key:e.code, down:true, repeat:e.repeat }); } };
    const keyUp = e => { if (keys.has(e.code) && input) input({ phase:'key', key:e.code, down:false }); };
    const resize = () => resized && resized(api.info());
    const visibility = () => { visible = !document.hidden; if (visible && shown) shown(); if (!visible && hidden) hidden(); };
    const blur = () => { if (input) input({phase:'blur'}); };
    window.addEventListener('keydown',keyDown);window.addEventListener('keyup',keyUp);window.addEventListener('resize',resize);window.addEventListener('blur',blur);
    document.addEventListener('visibilitychange',visibility);
    disposers.push(() => { window.removeEventListener('keydown',keyDown);window.removeEventListener('keyup',keyUp);window.removeEventListener('resize',resize);window.removeEventListener('blur',blur);document.removeEventListener('visibilitychange',visibility); });
  }
  return api;
}
module.exports = { createFieldPlatform };
