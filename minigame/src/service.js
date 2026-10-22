'use strict';
const model = require('./model');
const { platformError } = require('./platform');

const deepCopy = value => JSON.parse(JSON.stringify(value));
class GameService {
  constructor(platform, config) {
    this.platform = platform; this.config = config; this.state = null; this.token = '';
    this.scope = config.storageNamespace + (config.demo ? ':demo:v1' : ':live:' + config.apiBaseURL);
    this.saveKey = this.scope + ':save'; this.tokenKey = this.scope + ':token'; this.pendingKey = this.scope + ':pending';
  }
  accept(state) {
    if (!model.validateSave(state)) throw platformError('INVALID_SERVER_STATE', '游戏存档格式不正确，请重试或联系支持。');
    if (!this.state || state.version >= this.state.version) this.state = deepCopy(state);
    return this.state;
  }
  checkConfig() {
    if (!this.config.demo && !/^https:\/\/[^\s/]+(?:\/[^\s]*)?$/.test(this.config.apiBaseURL))
      throw platformError('SERVER_NOT_CONFIGURED', '正式游戏服务尚未配置，请联系果园。');
  }
  async signIn() {
    const code = await this.platform.login();
    const result = await this.call('game.login', { code }, false);
    if (!result.token || typeof result.token !== 'string') throw platformError('LOGIN_FAILED', '登录响应不完整，请重试。');
    this.platform.write(this.tokenKey, result.token); this.token = result.token;
  }
  async call(action, data, authenticated = true, retry = true) {
    this.checkConfig();
    if (authenticated && !this.token) await this.signIn();
    const url = this.config.apiBaseURL.replace(/\/+$/, '') + '/game-api';
    const response = await this.platform.request(url, { action, data: data || {} }, authenticated ? this.token : '', this.config.requestTimeoutMs);
    const body = response.body;
    if (response.status === 401 && authenticated && retry) {
      this.platform.remove(this.tokenKey); this.token = ''; await this.signIn();
      return this.call(action, data, authenticated, false);
    }
    if (response.status >= 500) {
      throw platformError('NETWORK_UNCERTAIN', '游戏服务暂时没有确认结果，请重试以核实操作。');
    }
    if (!body || typeof body !== 'object' || typeof body.ok !== 'boolean') {
      throw platformError('NETWORK_UNCERTAIN', '服务器响应不完整，请重试以核实操作结果。');
    }
    if (body.ok !== true) {
      const detail = body && body.error || {};
      if (typeof detail.code !== 'string' || !detail.code) {
        throw platformError('NETWORK_UNCERTAIN', '服务器响应不完整，请重试以核实操作结果。');
      }
      throw platformError(detail.code || 'SERVER_ERROR', detail.message || '游戏服务暂时不可用，请稍后重试。');
    }
    if (!body.data || typeof body.data !== 'object' || Array.isArray(body.data)) {
      throw platformError('NETWORK_UNCERTAIN', '服务器响应不完整，请重试以核实操作结果。');
    }
    return body.data;
  }
  async init() {
    if (this.config.demo) {
      const existing = this.platform.read(this.saveKey);
      if (existing !== null && !model.validateSave(existing)) throw platformError('DAMAGED_SAVE', '本地存档无法读取。可重试，或在手记中确认重新开园。');
      const state = existing || model.createFarm(this.platform.now(), Math.floor(Math.random() * 4294967296));
      const settled = model.settleFarm(state, this.platform.now());
      this.platform.write(this.saveKey, settled); return this.accept(settled);
    }
    this.token = this.platform.read(this.tokenKey) || '';
    const result = await this.call('game.bootstrap', {});
    this.accept(result.state);
    return this.sync();
  }
  async sync() {
    if (this.config.demo) {
      const current = this.platform.read(this.saveKey);
      if (!model.validateSave(current)) throw platformError('DAMAGED_SAVE', '本地存档无法读取，请确认后重新开园。');
      const settled = model.settleFarm(current, this.platform.now());
      this.platform.write(this.saveKey, settled); return this.accept(settled);
    }
    const pending = this.platform.read(this.pendingKey);
    if (pending) {
      if (!pending.requestId || !Number.isInteger(pending.expectedVersion) || !['game.action', 'game.reset'].includes(pending.action))
        throw platformError('DAMAGED_PENDING', '待核实操作记录无法读取，请联系支持。');
      try {
        const result = await this.call(pending.action, pending.data);
        this.accept(result.state); this.platform.remove(this.pendingKey);
      } catch (error) {
        if (error.code === 'NETWORK_UNCERTAIN' || error.code === 'STORAGE_WRITE' || error.code === 'INVALID_SERVER_STATE') throw error;
        this.platform.remove(this.pendingKey);
        const current = await this.call('game.sync', {}); this.accept(current.state);
        throw error;
      }
    }
    const result = await this.call('game.sync', {});
    return this.accept(result.state);
  }
  async command(command) {
    if (!this.state) throw platformError('NOT_READY', '请等待葡萄园加载完成。');
    if (this.config.demo) {
      const current = this.platform.read(this.saveKey);
      if (!model.validateSave(current)) throw platformError('DAMAGED_SAVE', '本地存档无法读取，请确认后重新开园。');
      if (current.version !== this.state.version) {
        this.accept(current); throw platformError('VERSION_CONFLICT', '葡萄园状态已更新，请查看最新状态后再操作。');
      }
      const next = model.applyAction(current, command, this.platform.now());
      this.platform.write(this.saveKey, next); return this.accept(next);
    }
    if (this.platform.read(this.pendingKey)) throw platformError('PENDING_ACTION', '上一笔操作结果需要核实，请先点击重试。');
    const data = { requestId: this.platform.requestId(), expectedVersion: this.state.version, command };
    this.platform.write(this.pendingKey, { action: 'game.action', requestId: data.requestId, expectedVersion: data.expectedVersion, data });
    try {
      const result = await this.call('game.action', data);
      this.accept(result.state); this.platform.remove(this.pendingKey); return this.state;
    } catch (error) {
      if (!['NETWORK_UNCERTAIN', 'STORAGE_WRITE', 'INVALID_SERVER_STATE'].includes(error.code)) {
        this.platform.remove(this.pendingKey);
        if (error.code === 'VERSION_CONFLICT') {
          const result = await this.call('game.sync', {}); this.accept(result.state);
        }
      }
      throw error;
    }
  }
  async reset() {
    if (this.config.demo) {
      // A damaged save is replaced only after the player confirms this operation.
      const fresh = model.createFarm(this.platform.now(), Math.floor(Math.random() * 4294967296));
      let stored = null;
      try { stored = this.platform.read(this.saveKey); } catch (_) { /* Explicit recovery of an unreadable save. */ }
      fresh.version = Math.max(this.state ? this.state.version : 0, model.validateSave(stored) ? stored.version : 0) + 1;
      this.platform.write(this.saveKey, fresh); this.state = null;
      return this.accept(fresh);
    }
    if (!this.state) {
      const result = await this.call('game.sync', {}); this.accept(result.state);
    }
    if (this.platform.read(this.pendingKey)) throw platformError('PENDING_ACTION', '请先核实上一笔操作，再重新开园。');
    const data = { confirm: '清空游戏进度', requestId: this.platform.requestId(), expectedVersion: this.state.version };
    this.platform.write(this.pendingKey, { action: 'game.reset', requestId: data.requestId, expectedVersion: data.expectedVersion, data });
    try {
      const result = await this.call('game.reset', data);
      this.state = null; this.accept(result.state); this.platform.remove(this.pendingKey); return this.state;
    } catch (error) {
      if (!['NETWORK_UNCERTAIN', 'STORAGE_WRITE', 'INVALID_SERVER_STATE'].includes(error.code)) this.platform.remove(this.pendingKey);
      throw error;
    }
  }
}
module.exports = { GameService };
