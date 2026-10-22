'use strict';

const FONT = '"PingFang SC","Microsoft YaHei",sans-serif';
const COLORS = {
  ink: '#183D2D', muted: '#647568', leaf: '#376D49', light: '#E8F0DF',
  paper: 'rgba(252,253,245,0.96)', glass: 'rgba(251,253,244,0.89)',
  stroke: 'rgba(58,91,57,0.16)', white: '#FFFFFF', gold: '#B68C43', danger: '#B45D43'
};
const clamp = (value, min, max) => Math.max(min, Math.min(max, Number(value) || 0));
const number = value => Math.round(Number(value) || 0).toLocaleString('en-US');

function createHUD(options) {
  options = options || {};
  const canvas = options.canvas;
  if (!canvas || typeof canvas.getContext !== 'function') throw new Error('HUD needs a canvas');
  const ctx = canvas.getContext('2d');
  if (!ctx) throw new Error('HUD needs a 2D context');
  const hooks = options.controls || {};
  let width = 390, height = 844, dpr = 1, safeTop = 0, safeBottom = 0;
  let controls = [], view = {}, modal = false, disposed = false;

  function resize(w, h, ratio, top, bottom) {
    width = Math.max(1, Number(w) || 390); height = Math.max(1, Number(h) || 844);
    dpr = Math.max(1, Number(ratio) || 1);
    safeTop = Math.max(0, Number(top) || 0); safeBottom = Math.max(0, Number(bottom) || 0);
    const pixelWidth = Math.round(width * dpr), pixelHeight = Math.round(height * dpr);
    if (canvas.width !== pixelWidth) canvas.width = pixelWidth;
    if (canvas.height !== pixelHeight) canvas.height = pixelHeight;
  }
  function font(size, weight) { ctx.font = `${weight || 400} ${size}px ${FONT}`; }
  function text(value, x, y, size, color, weight, align) {
    font(size || 13, weight); ctx.fillStyle = color || COLORS.ink;
    ctx.textAlign = align || 'left'; ctx.textBaseline = 'middle';
    ctx.fillText(value == null ? '' : String(value), x, y);
  }
  function path(x, y, w, h, radius) {
    const r = Math.min(radius || 0, w / 2, h / 2);
    ctx.beginPath(); ctx.moveTo(x + r, y); ctx.lineTo(x + w - r, y);
    ctx.quadraticCurveTo(x + w, y, x + w, y + r); ctx.lineTo(x + w, y + h - r);
    ctx.quadraticCurveTo(x + w, y + h, x + w - r, y + h); ctx.lineTo(x + r, y + h);
    ctx.quadraticCurveTo(x, y + h, x, y + h - r); ctx.lineTo(x, y + r);
    ctx.quadraticCurveTo(x, y, x + r, y); ctx.closePath();
  }
  function box(x, y, w, h, radius, fill, stroke, shadow) {
    ctx.save();
    if (shadow) { ctx.shadowColor = 'rgba(11,36,19,0.13)'; ctx.shadowBlur = 20; ctx.shadowOffsetY = 5; }
    path(x, y, w, h, radius);
    if (fill) { ctx.fillStyle = fill; ctx.fill(); }
    ctx.shadowBlur = 0; ctx.shadowOffsetY = 0;
    if (stroke) { ctx.strokeStyle = stroke; ctx.lineWidth = 1; ctx.stroke(); }
    ctx.restore();
  }
  function circle(x, y, r, fill, stroke, thickness) {
    ctx.beginPath(); ctx.arc(x, y, r, 0, Math.PI * 2);
    if (fill) { ctx.fillStyle = fill; ctx.fill(); }
    if (stroke) { ctx.strokeStyle = stroke; ctx.lineWidth = thickness || 1; ctx.stroke(); }
  }
  function line(x1, y1, x2, y2, color, thickness) {
    ctx.beginPath(); ctx.moveTo(x1, y1); ctx.lineTo(x2, y2);
    ctx.strokeStyle = color || COLORS.stroke; ctx.lineWidth = thickness || 1; ctx.stroke();
  }
  function short(value, maxWidth, size, weight) {
    font(size || 13, weight); let result = String(value || '');
    if (ctx.measureText(result).width <= maxWidth) return result;
    while (result && ctx.measureText(result + '…').width > maxWidth) result = Array.from(result).slice(0, -1).join('');
    return result + '…';
  }
  function wrap(value, x, y, maxWidth, size, color, lineHeight, maxLines, weight) {
    font(size || 13, weight); const rows = [];
    String(value || '').split('\n').forEach(paragraph => {
      let row = ''; Array.from(paragraph).forEach(char => {
        if (row && ctx.measureText(row + char).width > maxWidth) {
          if ('，。！？；：、）】》'.includes(char)) { rows.push(row + char); row = ''; }
          else { rows.push(row); row = char; }
        } else row += char;
      }); if (row || !paragraph) rows.push(row);
    });
    const limit = Math.max(1, maxLines || rows.length), visible = rows.slice(0, limit);
    if (rows.length > limit) visible[visible.length - 1] = short(visible[visible.length - 1] + '…', maxWidth, size, weight);
    visible.forEach((row, index) => text(row, x, y + index * (lineHeight || 19), size, color, weight));
    return visible.length;
  }
  function control(id, label, x, y, w, h, onPress, disabled) {
    const item = { id, label, x, y, width: w, height: h, disabled: !!disabled,
      onPress: typeof onPress === 'function' ? onPress : function () {} };
    item.rect = { x, y, width: w, height: h }; controls.push(item);
    if (typeof hooks.onControl === 'function') hooks.onControl(item);
  }
  function button(id, label, x, y, w, h, onPress, config) {
    config = config || {}; const disabled = !!config.disabled;
    box(x, y, w, h, config.radius || 13,
      disabled ? '#E0E7DA' : config.primary ? COLORS.ink : config.danger ? '#F5E6DD' : COLORS.light,
      config.primary || disabled ? null : COLORS.stroke);
    const color = disabled ? '#8A9889' : config.primary ? COLORS.white : config.danger ? COLORS.danger : COLORS.ink;
    text(short(label, w - 20, config.size || 14, 600), x + w / 2, y + h / 2, config.size || 14, color, 600, 'center');
    control(id, config.label || label, x, y, w, h, onPress, disabled);
  }
  function leafIcon(x, y, color, size) {
    const s = size || 1; ctx.save(); ctx.translate(x, y); ctx.scale(s, s);
    ctx.beginPath(); ctx.moveTo(-6, 6); ctx.bezierCurveTo(-11, -5, -2, -12, 9, -10);
    ctx.bezierCurveTo(11, 2, 5, 9, -6, 6); ctx.fillStyle = color || COLORS.leaf; ctx.fill();
    line(-6, 7, 5, -5, '#FCFDF6', 1.2); ctx.restore();
  }
  function characters() {
    const list = view.model && view.model.CHARACTERS;
    return Array.isArray(list) ? list : list && typeof list === 'object' ? Object.keys(list).map(id => Object.assign({ id }, list[id])) : [];
  }
  function selectedCharacter() {
    const list = characters();
    return view.previewCharacter || (view.state && view.state.characterId) || (list[0] && list[0].id);
  }
  function getCharacter(id) { return characters().find(item => item.id === id) || {}; }
  function callback(name, argument) { return function () { if (typeof view[name] === 'function') view[name](argument); }; }
  function getStage() { return view.stage || (view.state && view.state.stage); }
  function getMenu() { return view.menu || (getStage() === 'character' ? 'characters' : null); }
  function getJoystickRect() {
    return { x: width >= 740 ? 88 : 79, y: height - safeBottom - (width >= 740 ? 81 : 83), radius: width >= 740 ? 42 : 43 };
  }
  function panel(h, preferredWidth) {
    const wide = width >= 740, maxHeight = Math.max(80, height - safeTop - safeBottom - 32);
    const w = Math.min(preferredWidth || 388, width - 32), ph = Math.min(h, maxHeight);
    const x = wide ? width - w - 28 : (width - w) / 2;
    const y = wide ? safeTop + (height - safeTop - safeBottom - ph) / 2 : Math.max(safeTop + 12, height - safeBottom - ph - 18);
    box(x, y, w, ph, 24, COLORS.paper, COLORS.stroke, true);
    return { x, y, w, h: ph, compact: ph < h - 8 };
  }
  function panelHeading(p, eyebrow, title, subtitle, closable) {
    text(eyebrow, p.x + 23, p.y + 26, 10, COLORS.leaf, 700);
    text(title, p.x + 23, p.y + 54, 23, COLORS.ink, 700);
    if (subtitle) wrap(subtitle, p.x + 23, p.y + 85, p.w - 46, 12, COLORS.muted, 18, 2);
    if (closable) {
      circle(p.x + p.w - 29, p.y + 28, 14, COLORS.light);
      line(p.x + p.w - 33, p.y + 24, p.x + p.w - 25, p.y + 32, COLORS.muted, 1.5);
      line(p.x + p.w - 25, p.y + 24, p.x + p.w - 33, p.y + 32, COLORS.muted, 1.5);
      control('field-menu-close', '关闭菜单', p.x + p.w - 48, p.y + 9, 38, 38, callback('onCloseMenu'));
    }
  }

  function drawCharacters() {
    const p = panel(446, 376), list = characters().slice(0, 4), selected = selectedCharacter();
    panelHeading(p, 'MEET YOUR GROWER', '选一位园艺伙伴', '点击预览人物，再一起走进葡萄园。', getStage() !== 'character');
    const gap = 11, cardW = (p.w - 46 - gap) / 2;
    const cardH = Math.min(110, Math.max(52, (p.h - 204) / 2));
    const startY = p.y + 116;
    list.forEach((item, index) => {
      const x = p.x + 23 + (index % 2) * (cardW + gap), y = startY + Math.floor(index / 2) * (cardH + gap), active = selected === item.id;
      box(x, y, cardW, cardH, 15, active ? '#E5EFDA' : '#F1F3E8', active ? COLORS.leaf : COLORS.stroke);
      const color = item.color || item.primaryColor || ['#7B9059', '#B98C58', '#788E91', '#997581'][index];
      circle(x + 23, y + 27, 12, color); text(String(item.name || '园艺伙伴').slice(0, 1), x + 23, y + 27, 11, '#FFFFFF', 600, 'center');
      text(short(item.name || item.id, cardW - 54, 14, 600), x + 43, y + 27, 14, COLORS.ink, 600);
      wrap(item.role || item.subtitle || item.description || item.desc || '从这一株葡萄开始', x + 13, y + 54, cardW - 26, 11, COLORS.muted, 17, Math.max(1, Math.floor((cardH - 49) / 17)));
      if (active) { circle(x + cardW - 14, y + 13, 4, COLORS.leaf); }
      control('field-character-' + item.id, '预览人物：' + (item.name || item.id), x, y, cardW, cardH, callback('onPreviewCharacter', item.id), view.busy);
    });
    const chosen = getCharacter(selected);
    const footerY = p.y + p.h - 65;
    button('field-choose', (getStage() === 'character' ? '和' : '切换为') + (chosen.name || '伙伴') + (getStage() === 'character' ? '一起出发' : ''), p.x + 23, footerY, p.w - 46, 44, callback('onChooseCharacter', selected), { primary: true, disabled: !selected || view.busy });
    if (width >= 740) {
      text('葡萄园里的日常', 36, safeTop + 42, 24, '#FCFDF3', 700);
      text('从一位园艺伙伴，一株葡萄开始。', 36, safeTop + 75, 13, 'rgba(255,255,255,0.8)');
    }
  }

  function drawTop() {
    const state = view.state || {}, objective = view.objective || {};
    const top = safeTop + 14, margin = width < 360 ? 12 : 18;
    const taskW = Math.min(352, width >= 740 ? width * 0.43 : width - margin * 2), taskH = width >= 740 ? 108 : 106;
    box(margin, top, taskW, taskH, 19, COLORS.glass, COLORS.stroke, true);
    leafIcon(margin + 24, top + 25, COLORS.leaf, 0.7);
    const chapter = state.chapter || state.chapterIndex || 0;
    text('葡萄园日记  /  ' + (chapter === 'planting' ? '第一章 · 定植' : chapter === 'harvest' ? '第二章 · 雨前采收' : typeof chapter === 'number' ? '第 ' + (chapter + 1) + ' 章' : '田间任务'), margin + 39, top + 25, 10, COLORS.muted, 600);
    text(short(objective.title || '走进葡萄园', taskW - 34, 17, 600), margin + 17, top + 50, 17, COLORS.ink, 600);
    text(short(objective.text || '靠近葡萄架，开始今天的照料。', taskW - 34, 11), margin + 17, top + 74, 11, COLORS.muted);
    const total = Math.max(1, Number(objective.total) || 1), progress = clamp(objective.progress, 0, total);
    box(margin + 17, top + 90, taskW - 34, 4, 2, '#D9E4CF');
    if (progress) box(margin + 17, top + 90, (taskW - 34) * progress / total, 4, 2, COLORS.leaf);
    const toolsY = width >= 740 ? top + 1 : top + taskH + 10;
    const toolsRight = width - margin, name = getCharacter(state.characterId).name || '人物';
    button('field-characters', short(name, 52, 12), toolsRight - 151, toolsY, 68, 35, callback('onCharacterMenu'), { size: 12, disabled: view.busy });
    button('field-guide', view.guide ? '收起指南' : '任务指南', toolsRight - 76, toolsY, 76, 35, callback('onToggleGuide'), { size: 11, disabled: view.busy });
    const weather = state.weather || view.weather || {};
    const rain = state.chapter === 'harvest' && state.rainChallenge;
    const weatherName = rain ? rain.rainArrived ? '强降雨已到' : '雨前 ' + Math.max(0, rain.rainAt - rain.clock) + ' 时刻' : typeof weather === 'string' ? weather : weather.name || weather.label || '';
    const remaining = rain ? Math.max(0, rain.deadline - rain.clock) : state.remaining == null ? (state.remainingDays == null ? null : state.remainingDays) : state.remaining;
    const tags = [];
    if (weatherName) tags.push(weatherName);
    if (remaining != null) tags.push('剩余 ' + Math.max(0, Math.round(remaining)) + (rain ? ' 时刻' : ' 天'));
    if (rain) tags.push('交付 ' + rain.deliveredKg + '/18 kg');
    else if (state.coins != null) tags.push(number(state.coins) + ' 经营币');
    if (tags.length) {
      const tagY = top + taskH + (width >= 740 ? 12 : 53), label = tags.join('  ·  ');
      const tagW = Math.min(width - margin * 2, ctx.measureText(label).width + 28);
      box(margin, tagY, tagW, 29, 14, 'rgba(25,55,36,0.64)');
      text(short(label, tagW - 24, 10), margin + 12, tagY + 15, 10, '#F6F9EE', 500);
    }
    if (rain && state.coins != null) {
      const coinLabel = number(state.coins) + ' 经营币', coinY = toolsY + (width >= 740 ? 44 : 79);
      const coinW = Math.max(91, ctx.measureText(coinLabel).width + 22);
      if (!view.guide || width >= 740) {
        box(toolsRight - coinW, coinY, coinW, 26, 13, 'rgba(25,55,36,0.64)');
        text(coinLabel, toolsRight - 11, coinY + 13, 10, '#F6F9EE', 500, 'right');
      }
    }
  }

  function navigationTargets() {
    const objective = view.objective || {};
    let list = view.targets || objective.targets || objective.targetIds;
    const state = view.state || {}, model = view.model || {};
    let priority;
    if (state.chapter === 'planting' && model.PLANTING_STEPS) {
      const step = model.PLANTING_STEPS[state.nursery && state.nursery.step || 0];
      priority = step && step.targetId;
      if (!Array.isArray(list)) {
        const names = { seedling: '育苗台', plantingSpot: '种植穴', support: '支撑杆', water: '水源', nameplate: '名牌台' };
        list = model.PLANTING_STEPS.map(item => ({ id: item.targetId, label: names[item.targetId] || item.label }));
      }
    }
    if (!Array.isArray(list) && state.chapter === 'harvest' && state.rainChallenge) {
      const rain = state.rainChallenge;
      priority = state.inventory && state.inventory.length ? 'delivery' : rain.plots.find(plot => !plot.harvested) && rain.plots.find(plot => !plot.harvested).id;
      list = rain.plots.map(plot => ({ id: plot.id, label: plot.name })).concat([{ id: 'drain', label: '排水沟' }, { id: 'shelter', label: '避雨设施' }, { id: 'delivery', label: '交货点' }, { id: 'clock', label: '天气牌' }]);
    }
    if (!Array.isArray(list)) list = objective.targetId ? [objective.targetId] : view.nearest && view.nearest.id ? [view.nearest.id] : [];
    const seen = new Set();
    const normalized = list.map(item => typeof item === 'string' ? { id: item, label: item } : item).filter(item => { if (!item || !item.id || seen.has(item.id)) return false; seen.add(item.id); return true; });
    if (priority) normalized.sort((a, b) => Number(b.id === priority) - Number(a.id === priority));
    return normalized;
  }
  function drawGuide() {
    if (!view.guide) return;
    const w = Math.min(281, width - 36), targets = navigationTargets();
    const compact = height - safeTop - safeBottom < 650;
    const columns = targets.length > 6 && compact && width < 740 ? 3 : targets.length > 3 ? 2 : 1;
    const rowH = compact ? 37 : 43, h = (compact ? 94 : 108) + Math.ceil(targets.length / columns) * rowH;
    const x = width >= 740 ? (width - w) / 2 : 18;
    const y = width >= 740 ? Math.max(safeTop + 124, height - safeBottom - h - 27) : safeTop + (compact ? 204 : 220);
    box(x, y, w, h, 18, COLORS.paper, COLORS.stroke, true);
    text('沿着任务，慢慢种好', x + 17, y + 22, 13, COLORS.ink, 600);
    text('自动走过去，摇杆可随时接管', x + 17, y + 44, 10, COLORS.muted);
    const cellW = (w - 24 - (columns - 1) * 8) / columns;
    const targetNames = { seedling: '育苗台', plantingSpot: '种植穴', support: '支撑杆', water: '水源', nameplate: '名牌台', sun: '朝阳架', creek: '溪边架', slope: '坡上架', drain: '排水沟', shelter: '避雨棚', delivery: '交货点', clock: '天气牌' };
    targets.forEach((target, index) => button('field-go-' + target.id, '走到' + (targetNames[target.id] || target.name || target.label || '任务地点'), x + 12 + index % columns * (cellW + 8), y + 60 + Math.floor(index / columns) * rowH, cellW, compact ? 30 : 35, callback('onNavigate', target.id), { size: 10, disabled: view.busy }));
    const footerY = y + h - 38, footerW = (w - 32) / 2;
    button('field-help', '怎么玩 / 重开', x + 12, footerY, footerW, 28, callback('onHelp'), { size: 10, disabled: view.busy });
    button('field-report', '田间记录', x + 20 + footerW, footerY, footerW, 28, callback('onReport'), { size: 10, disabled: view.busy });
  }

  function drawMovement() {
    const bottom = height - safeBottom, joystickRect = getJoystickRect(), radius = joystickRect.radius;
    const x = joystickRect.x, y = joystickRect.y;
    circle(x, y, radius, 'rgba(245,250,234,0.18)', 'rgba(255,255,255,0.42)', 1.5);
    circle(x, y, radius * 0.66, null, 'rgba(255,255,255,0.22)');
    const joystick = view.joystick || {}, jx = clamp(joystick.x, -1, 1), jy = clamp(joystick.y, -1, 1);
    circle(x + jx * 22, y + jy * 22, 17, 'rgba(252,253,245,0.75)', 'rgba(255,255,255,0.4)');
    text(view.moving ? '在葡萄园里走走' : '拖动摇杆行走', x, y + radius + 20, 10, 'rgba(255,255,255,0.9)', 500, 'center');
    const nearest = view.nearest, interaction = nearest && nearest.interaction;
    const enabled = !!interaction && interaction.disabled !== true && interaction.enabled !== false && !view.busy;
    const buttonX = width - (width >= 740 ? 88 : 77), buttonY = y - 2, r = width >= 740 ? 42 : 43;
    circle(buttonX, buttonY, r + 5, 'rgba(250,252,241,0.23)');
    circle(buttonX, buttonY, r, enabled ? '#F4F7E9' : 'rgba(222,233,211,0.76)', 'rgba(255,255,255,0.68)', 1.5);
    leafIcon(buttonX, buttonY - 11, enabled ? COLORS.leaf : '#819377', 0.83);
    const label = typeof interaction === 'string' ? interaction : interaction && (interaction.label || interaction.name || interaction.title);
    text(short(label || '靠近照料', 70, 12, 600), buttonX, buttonY + 17, 12, enabled ? COLORS.ink : '#687E61', 600, 'center');
    if (view.interactionProgress != null) {
      ctx.beginPath(); ctx.arc(buttonX, buttonY, r + 2, -Math.PI / 2, -Math.PI / 2 + Math.PI * 2 * clamp(view.interactionProgress, 0, 1));
      ctx.strokeStyle = COLORS.gold; ctx.lineWidth = 4; ctx.stroke();
    }
    control('field-interact', label || '靠近任务地点后照料', buttonX - r, buttonY - r, r * 2, r * 2, callback('onInteract'), !enabled && !view.busy);
    text(view.busy ? '松开可中断' : '按住操作', buttonX, buttonY + r + 20, 10, 'rgba(255,255,255,0.9)', 500, 'center');
    if (nearest) {
      const distance = Number(nearest.distance), hint = nearest.label || nearest.name || '田间地点';
      const full = hint + (Number.isFinite(distance) ? ' · ' + distance.toFixed(1) + ' 米' : '');
      box(buttonX - 94, buttonY - r - 41, 188, 29, 14, 'rgba(25,55,36,0.68)');
      text(short(full, 164, 10), buttonX, buttonY - r - 26, 10, '#F6F9EE', 500, 'center');
    }
    if (width >= 740) text('W A S D / 方向键移动 · 按住 E 照料 · 拖动画面转动镜头', width / 2, bottom - 21, 10, 'rgba(255,255,255,0.74)', 400, 'center');
  }

  function drawReport() {
    const report = view.report || (view.state && view.state.report) || {}, objective = view.objective || {};
    const p = panel(448, 392);
    const failed = report.status === 'failed';
    panelHeading(p, failed ? 'FIELD CHAPTER / TRY AGAIN' : 'FIELD CHAPTER / COMPLETE', short(report.title || '这一章，种好了', p.w - 46, 23, 700), report.summary || report.text || (failed ? '订单超时，园子和已有资产仍然保留。' : '看看这次田间决策带来的变化。'), true);
    let entries = Array.isArray(report.metrics) ? report.metrics : [];
    if (!entries.length) {
      if (report.chapter === 'planting') entries = [{ label: '第一株葡萄', value: report.name }, { label: '当前经营币', value: number(report.coins) }];
      else if (report.chapter === 'harvest') entries = [{ label: '本季订单交付', value: (report.deliveredKg || 0) + ' / ' + (report.targetKg || 18) + ' kg' }, { label: '销售收入', value: number(report.revenue) + ' 币' }, { label: '本季支出', value: number(report.costs) + ' 币' }, { label: '库存保留', value: (report.inventoryKg || 0) + ' kg' }];
      else [['品质', report.quality], ['收获', report.harvestWeight != null ? (report.harvestWeight / 1000).toFixed(1) + ' kg' : report.harvest], ['收入', report.revenue], ['评分', report.score]].forEach(item => { if (item[1] != null) entries.push({ label: item[0], value: item[1] }); });
    }
    const startY = p.y + 122;
    if (entries.length) {
      const cellW = (p.w - 57) / 2;
      entries.slice(0, 4).forEach((entry, index) => {
        const x = p.x + 23 + (index % 2) * (cellW + 11), y = startY + Math.floor(index / 2) * 66;
        box(x, y, cellW, 56, 12, '#EDF2E3'); text(entry.label || entry.name, x + 13, y + 16, 10, COLORS.muted);
        const valueSize = report.chapter === 'planting' && index === 0 ? 12 : 18;
        text(short(entry.value, cellW - 26, valueSize, 600), x + 13, y + 37, valueSize, COLORS.ink, 600);
      });
    }
    const notes = report.notes || report.lessons || [objective.text || '每一次照料，都留下下一步种植的经验。'];
    let noteY = entries.length ? startY + Math.ceil(Math.min(entries.length, 4) / 2) * 66 + 10 : startY + 8;
    (Array.isArray(notes) ? notes : [notes]).slice(0, 3).forEach(note => {
      if (noteY > p.y + p.h - 105) return;
      circle(p.x + 27, noteY + 2, 2.5, COLORS.leaf);
      noteY += wrap(typeof note === 'string' ? note : note.text, p.x + 39, noteY, p.w - 63, 12, COLORS.muted, 19, 2) * 19 + 10;
    });
    const nextHarvest = report.chapter === 'harvest';
    if (report.status === 'active') button('field-report-continue', '回到葡萄园继续照料', p.x + 23, p.y + p.h - 64, p.w - 46, 44, callback('onCloseMenu'), { primary: true, disabled: view.busy });
    else button(nextHarvest ? 'field-retry' : 'field-next', nextHarvest ? '进入下一成熟采收季' : '走进下一章', p.x + 23, p.y + p.h - 64, p.w - 46, 44, callback(nextHarvest ? 'onRetry' : 'onNextChapter'), { primary: true, disabled: view.busy });
  }

  function drawDelivery() {
    const p = panel(427, 382), state = view.state || {}, rain = state.rainChallenge || {};
    panelHeading(p, 'FROM VINE TO TABLE', '把葡萄，送到餐桌', '采收每架花费 40 币。交货前，比较可售数量和收入。', true);
    const entries = [
      { id: 'contract', name: '履约交付', text: '只收本季 A/B 级果，累计 18 公斤完成订单。' },
      { id: 'wholesale', name: '市场批发', text: '接收所有等级；售出后不计入订单交付量。' }
    ];
    entries.forEach((entry, index) => {
      const cardH = Math.min(128, (p.h - 142) / 2 - 8), x = p.x + 23, y = p.y + 124 + index * (cardH + 11);
      const eligible = (state.inventory || []).filter(batch => entry.id === 'wholesale' || (batch.attempt === rain.attempt && (batch.grade === 'A' || batch.grade === 'B')));
      const wholesaleFactor = rain.clock >= 7 ? 0.6 : 0.8, prices = { A: 48, B: 32, C: 14 };
      let weight = 0, revenue = 0;
      eligible.forEach(batch => {
        weight += batch.weight;
        const price = entry.id === 'contract' ? prices[batch.grade] : Math.floor(prices[batch.grade] * wholesaleFactor * (batch.attempt < rain.attempt ? 0.6 : 1));
        revenue += batch.weight * price;
      });
      const unavailable = view.busy || !weight || entry.id === 'contract' && rain.status !== 'active';
      box(x, y, p.w - 46, cardH, 16, unavailable ? '#F1F3E8' : index === 0 ? '#E8EFDA' : '#EFF2E5', COLORS.stroke);
      text(entry.name, x + 16, y + 23, 15, COLORS.ink, 600);
      text(weight + ' kg / ' + number(revenue) + ' 币', x + p.w - 62, y + 23, 11, COLORS.leaf, 600, 'right');
      text(short(entry.text, p.w - 78, 10), x + 16, y + 47, 10, COLORS.muted);
      const priceText = entry.id === 'contract' ? 'A 48 / B 32 币每 kg' : 'A ' + Math.floor(48 * wholesaleFactor) + ' / B ' + Math.floor(32 * wholesaleFactor) + ' / C ' + Math.floor(14 * wholesaleFactor) + ' 币每 kg';
      text(priceText, x + 16, y + 67, 10, COLORS.muted);
      text(unavailable ? !weight ? '暂无可交付库存' : '本季订单已结束' : '选择这个渠道  →', x + 16, y + cardH - 15, 10, unavailable ? '#9D805A' : COLORS.leaf, 600);
      control('field-deliver-' + entry.id, '通过' + entry.name + '销售 ' + weight + ' 公斤，收入 ' + revenue + ' 金币', x, y, p.w - 46, cardH, callback('onAction', { type: 'deliver', channel: entry.id }), unavailable);
    });
  }

  function drawChapters() {
    const chapters = view.model && view.model.CHAPTERS || [{ id: 'planting', title: '亲手栽下第一株葡萄' }, { id: 'harvest', title: '成熟成园，雨前采收交货' }], p = panel(Math.min(530, 146 + chapters.length * 58), 374);
    panelHeading(p, 'YOUR FIELD JOURNAL', '这一季的田间日记', '完成当前任务，沿着生长季继续向前。', true);
    const current = view.state && view.state.chapter === 'harvest' ? 1 : Number(view.state && view.state.chapterIndex) || 0;
    const shown = Math.min(chapters.length, Math.floor((p.h - 130) / 57));
    chapters.slice(0, shown).forEach((chapter, index) => {
      const x = p.x + 23, y = p.y + 118 + index * 57, active = index === current;
      box(x, y, p.w - 46, 47, 11, active ? '#E5EFDA' : '#F2F3E9');
      text(String(index + 1).padStart(2, '0'), x + 14, y + 24, 12, active ? COLORS.leaf : COLORS.muted, 700);
      text(short(chapter.title || chapter.name || chapter.id, p.w - 122, 12), x + 44, y + 24, 12, COLORS.ink, 500);
      text(index < current ? '完成' : active ? '进行中' : '待开启', x + p.w - 62, y + 24, 10, active ? COLORS.leaf : COLORS.muted, 500, 'right');
    });
  }

  function drawHelp() {
    const p = panel(417, 374);
    panelHeading(p, 'A WALK THROUGH THE VINEYARD', '走进园子，亲手照料', '每一章都围绕一个真实的种植取舍。', true);
    const steps = [
      ['01', '走到田间', '拖动摇杆行走；拖动画面调整镜头。电脑可用方向键或 W A S D。'],
      ['02', '靠近，再照料', '靠近任务地点，按住右下按钮或 E 干活；松开会中断。'],
      ['03', '种好这一章', '按任务完成照料，看看收成和品质怎样变化。']
    ];
    let y = p.y + 123;
    const stepH = Math.min(77, (p.h - 168) / 3);
    steps.forEach(step => {
      text(step[0], p.x + 23, y + 3, 11, COLORS.leaf, 700); text(step[1], p.x + 56, y + 3, 14, COLORS.ink, 600);
      wrap(step[2], p.x + 56, y + 28, p.w - 80, 11, COLORS.muted, 18, 2); y += stepH;
    });
    button('field-reset-prompt', '重新开始田间日记', p.x + 23, p.y + p.h - 57, p.w - 46, 36, callback('onResetPrompt'), { size: 12 });
  }
  function drawReset() {
    const p = panel(285, 374);
    panelHeading(p, 'START A NEW FIELD JOURNAL', '重新走进这一季', '当前角色、章节和种植进度将重新开始。', !view.guard && !!view.state && !/存档/.test(view.error || ''));
    wrap('这一份田间日记会被清空。确认后，你可以重新选择园艺伙伴，从第一章开始。', p.x + 23, p.y + 128, p.w - 46, 12, COLORS.muted, 21, 3);
    button('field-reset-confirm', '确认重新开始', p.x + 23, p.y + p.h - 67, p.w - 46, 44, function () {
      if (typeof view.onResetConfirm === 'function') view.onResetConfirm();
      else if (typeof view.onReset === 'function') view.onReset();
      else if (typeof view.onAction === 'function') view.onAction({ type: 'reset' });
    }, { danger: true, disabled: view.busy });
  }
  function drawError() {
    if (!view.error) return;
    const w = Math.min(width - 32, 442), h = 100, x = (width - w) / 2;
    const y = getMenu() === 'reset' ? safeTop + 14 : Math.max(safeTop + 9, height - safeBottom - (modal ? 113 : 250));
    box(x, y, w, h, 17, '#FCF2E6', '#D9C3A4', true);
    text('这一步还没完成', x + 16, y + 22, 13, COLORS.danger, 600);
    wrap(view.error, x + 16, y + 46, w - 120, 11, COLORS.muted, 17, 2);
    button('field-retry', '重试', x + w - 88, y + 29, 71, 33, callback('onRetry'), { size: 12, disabled: view.busy });
    control('field-dismiss-error', '关闭提示', x + w - 32, y + 4, 25, 22, callback('onDismissError'));
    text('×', x + w - 18, y + 15, 17, COLORS.muted, 500, 'center');
  }

  function render(nextView) {
    if (disposed) return;
    view = nextView || {}; controls = []; modal = !!getMenu();
    if (typeof hooks.onControlsStart === 'function') hooks.onControlsStart();
    ctx.setTransform(1, 0, 0, 1, 0, 0); ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0); ctx.globalAlpha = 1; ctx.lineCap = 'round'; ctx.lineJoin = 'round';
    if (!modal) { drawTop(); drawGuide(); drawMovement(); }
    else {
      ctx.fillStyle = getMenu() === 'characters' ? 'rgba(12,32,19,0.16)' : 'rgba(12,32,19,0.36)'; ctx.fillRect(0, 0, width, height);
      switch (getMenu()) {
        case 'characters': drawCharacters(); break;
        case 'chapters': drawChapters(); break;
        case 'report': drawReport(); break;
        case 'delivery': drawDelivery(); break;
        case 'reset': drawReset(); break;
        default: drawHelp(); break;
      }
    }
    if (!modal && !view.guide && view.status && !view.error) {
      const y = height - safeBottom - 205, w = Math.min(width - 36, 324), x = (width - w) / 2;
      box(x, y, w, 34, 17, 'rgba(25,55,36,0.75)'); text(short(view.status, w - 26, 11), width / 2, y + 17, 11, '#F4F8EC', 500, 'center');
    }
    drawError();
  }
  function hit(x, y) {
    if (disposed) return null;
    for (let index = controls.length - 1; index >= 0; index -= 1) {
      const item = controls[index];
      if (x >= item.x && x <= item.x + item.width && y >= item.y && y <= item.y + item.height) {
        if (!item.disabled) item.onPress();
        return item.id;
      }
    }
    return modal ? 'field-menu' : null;
  }
  function dispose() {
    disposed = true; controls = []; view = {};
    if (typeof hooks.onControlsStart === 'function') hooks.onControlsStart();
    ctx.setTransform(1, 0, 0, 1, 0, 0); ctx.clearRect(0, 0, canvas.width, canvas.height);
  }
  resize(width, height, 1, 0, 0);
  return { resize, render, hit, getControls: () => controls.slice(), getJoystickRect, dispose };
}

module.exports = { createHUD };
