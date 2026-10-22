'use strict';

function platformError(code, message) {
  const error = new Error(message); error.code = code; return error;
}

function createPlatform(options) {
  options = options || {};
  const native = typeof wx !== 'undefined' && typeof wx.createCanvas === 'function';
  const canvas = native ? wx.createCanvas() : document.getElementById('vineyard-canvas');
  if (!canvas) throw new Error('游戏画布未准备好');
  let visible = true;
  let touchHandler = null;
  let resizeHandler = null;
  let showHandler = null;
  let hideHandler = null;
  const disposers = [];
  const buttonMap = new Map();
  let seen = new Set();
  const controlsHost = native ? null : document.getElementById('vineyard-controls');
  const api = {
    native, canvas,
    now: () => Date.now(),
    info() {
      if (native) {
        const value = typeof wx.getWindowInfo === 'function' ? wx.getWindowInfo() : wx.getSystemInfoSync();
        const area = value.safeArea;
        return { width: value.windowWidth, height: value.windowHeight, dpr: Math.min(2, value.pixelRatio || 1),
          safeTop: area ? Math.max(0, area.top) : 0,
          safeBottom: area ? Math.max(0, value.windowHeight - area.bottom) : 0 };
      }
      const box = canvas.getBoundingClientRect();
      return { width: box.width, height: box.height, dpr: Math.min(2, window.devicePixelRatio || 1), safeTop: 0, safeBottom: 0 };
    },
    requestId() {
      return 'vg_' + Date.now().toString(36) + '_' + Math.random().toString(36).slice(2, 12) + '_' + Math.random().toString(36).slice(2, 8);
    },
    read(key) {
      try {
        const value = native ? wx.getStorageSync(key) : window.localStorage.getItem(key);
        if (value === '' || value === null || value === undefined) return null;
        return typeof value === 'string' ? JSON.parse(value) : value;
      } catch (error) { throw platformError('STORAGE_READ', '无法读取游戏存档，请检查设备存储或浏览器权限。'); }
    },
    write(key, value) {
      try {
        const serialized = JSON.stringify(value);
        if (native) wx.setStorageSync(key, serialized);
        else window.localStorage.setItem(key, serialized);
      } catch (error) { throw platformError('STORAGE_WRITE', '游戏进度未能保存，请检查设备空间后重试。'); }
    },
    remove(key) {
      try {
        if (native) wx.removeStorageSync(key);
        else window.localStorage.removeItem(key);
      } catch (error) { throw platformError('STORAGE_WRITE', '无法更新存档，请检查设备空间后重试。'); }
    },
    async login() {
      if (!native) throw platformError('WECHAT_LOGIN_REQUIRED', '正式游戏请在微信小游戏中打开。');
      return new Promise((resolve, reject) => wx.login({
        success: result => result.code ? resolve(result.code) : reject(platformError('LOGIN_FAILED', '微信登录未成功，请重试。')),
        fail: () => reject(platformError('LOGIN_FAILED', '微信登录暂时失败，请稍后重试。'))
      }));
    },
    async request(url, body, token, timeout) {
      if (native) {
        return new Promise((resolve, reject) => wx.request({
          url, method: 'POST', timeout,
          header: { 'Content-Type': 'application/json', ...(token ? { Authorization: 'Bearer ' + token } : {}) },
          data: body,
          success: response => resolve({ status: response.statusCode, body: response.data }),
          fail: () => reject(platformError('NETWORK_UNCERTAIN', '网络暂时没有回应。操作结果需要核实，请点击重试。'))
        }));
      }
      const aborter = new AbortController();
      const timer = setTimeout(() => aborter.abort(), timeout);
      try {
        const response = await fetch(url, { method: 'POST',
          headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: 'Bearer ' + token } : {}) },
          body: JSON.stringify(body), signal: aborter.signal });
        let data;
        try { data = await response.json(); }
        catch (_) { throw platformError('NETWORK_UNCERTAIN', '服务器响应不完整，请重试以核实操作结果。'); }
        return { status: response.status, body: data };
      } catch (error) {
        if (error.code) throw error;
        throw platformError('NETWORK_UNCERTAIN', '网络暂时没有回应。操作结果需要核实，请点击重试。');
      } finally { clearTimeout(timer); }
    },
    frame(callback) {
      if (!visible) return null;
      if (typeof requestAnimationFrame === 'function') return requestAnimationFrame(callback);
      if (typeof canvas.requestAnimationFrame === 'function') return canvas.requestAnimationFrame(callback);
      return setTimeout(() => callback(Date.now()), 33);
    },
    cancelFrame(id) {
      if (id === null || id === undefined) return;
      if (typeof cancelAnimationFrame === 'function') cancelAnimationFrame(id);
      else if (typeof canvas.cancelAnimationFrame === 'function') canvas.cancelAnimationFrame(id);
      else clearTimeout(id);
    },
    onTouch(callback) { touchHandler = callback; },
    onResize(callback) { resizeHandler = callback; },
    onShow(callback) { showHandler = callback; },
    onHide(callback) { hideHandler = callback; },
    controls: {
      onControlsStart() { seen = new Set(); },
      onControl(control) {
        if (!controlsHost) return;
        seen.add(control.id);
        let node = buttonMap.get(control.id);
        if (!node) {
          node = document.createElement('button');
          node.type = 'button'; node.className = 'canvas-control'; node.dataset.control = control.id;
          controlsHost.appendChild(node); buttonMap.set(control.id, node);
          node.addEventListener('click', event => {
            event.stopPropagation();
            if (node._control && !node._control.disabled) node._control.onPress();
          });
        }
        node._control = control;
        node.setAttribute('aria-label', control.label);
        node.title = control.label;
        node.disabled = !!control.disabled;
        Object.assign(node.style, { left: control.x + 'px', top: control.y + 'px', width: control.width + 'px', height: control.height + 'px' });
      }
    },
    finishControls() {
      for (const [key, node] of buttonMap) if (!seen.has(key)) { node.remove(); buttonMap.delete(key); }
    },
    announce(message) {
      if (!native) {
        const node = document.getElementById('vineyard-status');
        if (node && node.textContent !== message) node.textContent = message;
      }
    },
    destroy() {
      disposers.forEach(dispose => dispose());
      buttonMap.forEach(node => node.remove()); buttonMap.clear();
      touchHandler = resizeHandler = showHandler = hideHandler = null;
    }
  };
  if (native) {
    const touch = event => {
      const point = (event.changedTouches || event.touches || [])[0];
      if (point && touchHandler) touchHandler(point.clientX === undefined ? point.x : point.clientX, point.clientY === undefined ? point.y : point.clientY);
    };
    const resized = () => resizeHandler && resizeHandler(api.info());
    const shown = () => { visible = true; if (showHandler) showHandler(); };
    const hidden = () => { visible = false; if (hideHandler) hideHandler(); };
    wx.onTouchEnd(touch);
    if (typeof wx.onWindowResize === 'function') wx.onWindowResize(resized);
    wx.onShow(shown); wx.onHide(hidden);
    disposers.push(() => {
      if (typeof wx.offTouchEnd === 'function') wx.offTouchEnd(touch);
      if (typeof wx.offWindowResize === 'function') wx.offWindowResize(resized);
      if (typeof wx.offShow === 'function') wx.offShow(shown);
      if (typeof wx.offHide === 'function') wx.offHide(hidden);
    });
  } else {
    const touch = event => {
      const box = canvas.getBoundingClientRect();
      if (touchHandler) touchHandler(event.clientX - box.left, event.clientY - box.top);
    };
    const resized = () => resizeHandler && resizeHandler(api.info());
    const visibility = () => {
      visible = !document.hidden;
      if (visible && showHandler) showHandler();
      else if (!visible && hideHandler) hideHandler();
    };
    canvas.addEventListener('pointerup', touch); window.addEventListener('resize', resized);
    document.addEventListener('visibilitychange', visibility);
    disposers.push(() => { canvas.removeEventListener('pointerup', touch); window.removeEventListener('resize', resized); document.removeEventListener('visibilitychange', visibility); });
  }
  return api;
}

module.exports = { createPlatform, platformError };
