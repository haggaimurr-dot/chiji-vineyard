'use strict';

const { getStage, getWeather } = require('./model');
const { RULES, ACTIONS } = require('./rules');

const W = 390, H = 844;
const C = {
  paper: '#F5F2E7', ink: '#234A38', muted: '#7C8775', line: '#DADFCF',
  green: '#83A866', leaf: '#648449', lightLeaf: '#A4BB74', purple: '#785282',
  card: '#FCFBF5', soft: '#E8EDDC', soil: '#B4956A', soilSide: '#8F724D',
  gold: '#B29458', danger: '#AC6652', disabled: '#DEE3D5'
};
const SANS = '"PingFang SC","Microsoft YaHei",sans-serif';
const SERIF = '"Songti SC","STSong","SimSun",serif';
const money = value => Math.round(Number(value) || 0).toLocaleString('en-US');
const kg = value => ((Number(value) || 0) / 1000).toFixed(1);
const pct = value => Math.round(Number(value) || 0);

function createRenderer(canvas, hooks) {
  hooks = hooks || {};
  const ctx = canvas.getContext('2d');
  let width = W, height = H, dpr = 1, scale = 1, offsetX = 0, offsetY = 0;
  let controls = [], currentView, allowControls = true;

  function resize(nextWidth, nextHeight, nextDpr, safeTop, safeBottom) {
    width = Math.max(1, Number(nextWidth) || W);
    height = Math.max(1, Number(nextHeight) || H);
    dpr = Math.max(1, Number(nextDpr) || 1);
    safeTop = Math.max(0, Number(safeTop) || 0);
    safeBottom = Math.max(0, Number(safeBottom) || 0);
    scale = Math.max(0.01, Math.min(width / W, Math.max(1, height - safeTop - safeBottom) / H));
    offsetX = (width - W * scale) / 2;
    offsetY = safeTop + (height - safeTop - safeBottom - H * scale) / 2;
    canvas.width = Math.round(width * dpr);
    canvas.height = Math.round(height * dpr);
  }

  function font(size, weight, serif) { ctx.font = `${weight || 400} ${size}px ${serif ? SERIF : SANS}`; }
  function text(value, x, y, size, color, weight, align, serif) {
    font(size || 12, weight, serif);
    ctx.fillStyle = color || C.ink;
    ctx.textAlign = align || 'left'; ctx.textBaseline = 'middle';
    ctx.fillText(String(value === undefined ? '' : value), x, y);
  }
  function rounded(x, y, w, h, radius, fill, stroke) {
    radius = Math.min(radius || 0, w / 2, h / 2);
    ctx.beginPath(); ctx.moveTo(x + radius, y);
    ctx.lineTo(x + w - radius, y); ctx.quadraticCurveTo(x + w, y, x + w, y + radius);
    ctx.lineTo(x + w, y + h - radius); ctx.quadraticCurveTo(x + w, y + h, x + w - radius, y + h);
    ctx.lineTo(x + radius, y + h); ctx.quadraticCurveTo(x, y + h, x, y + h - radius);
    ctx.lineTo(x, y + radius); ctx.quadraticCurveTo(x, y, x + radius, y); ctx.closePath();
    if (fill) { ctx.fillStyle = fill; ctx.fill(); }
    if (stroke) { ctx.strokeStyle = stroke; ctx.lineWidth = 1; ctx.stroke(); }
  }
  function line(x1, y1, x2, y2, color, thickness) {
    ctx.beginPath(); ctx.moveTo(x1, y1); ctx.lineTo(x2, y2);
    ctx.strokeStyle = color || C.line; ctx.lineWidth = thickness || 1; ctx.stroke();
  }
  function circle(x, y, r, fill, stroke) {
    ctx.beginPath(); ctx.arc(x, y, r, 0, Math.PI * 2);
    if (fill) { ctx.fillStyle = fill; ctx.fill(); }
    if (stroke) { ctx.strokeStyle = stroke; ctx.lineWidth = 1; ctx.stroke(); }
  }
  function ellipse(x, y, rx, ry, fill) {
    ctx.beginPath(); ctx.ellipse(x, y, rx, ry, 0, 0, Math.PI * 2); ctx.fillStyle = fill; ctx.fill();
  }
  function poly(points, fill, stroke, thickness) {
    ctx.beginPath(); ctx.moveTo(points[0][0], points[0][1]);
    for (let i = 1; i < points.length; i++) ctx.lineTo(points[i][0], points[i][1]);
    ctx.closePath();
    if (fill) { ctx.fillStyle = fill; ctx.fill(); }
    if (stroke) { ctx.strokeStyle = stroke; ctx.lineWidth = thickness || 1; ctx.stroke(); }
  }
  function wrap(value, x, y, maxWidth, size, color, lineHeight, maxLines, weight) {
    font(size || 12, weight);
    const paragraphs = String(value || '').split('\n'), lines = [];
    for (const paragraph of paragraphs) {
      let row = '';
      for (const char of Array.from(paragraph)) {
        if (row && ctx.measureText(row + char).width > maxWidth) { lines.push(row); row = char; }
        else row += char;
      }
      lines.push(row);
    }
    const limit = maxLines || lines.length;
    const output = lines.slice(0, limit);
    if (lines.length > limit && output.length) {
      let last = output[output.length - 1];
      while (last && ctx.measureText(last + '…').width > maxWidth) last = last.slice(0, -1);
      output[output.length - 1] = last + '…';
    }
    output.forEach((row, i) => text(row, x, y + i * (lineHeight || 18), size, color, weight));
    return output.length;
  }
  function short(value, maxWidth, size) {
    font(size || 12);
    let result = String(value || '');
    if (ctx.measureText(result).width <= maxWidth) return result;
    while (result && ctx.measureText(result + '…').width > maxWidth) result = result.slice(0, -1);
    return result + '…';
  }

  function control(id, label, x, y, w, h, onPress, disabled) {
    if (!allowControls) return;
    const entry = {
      id, label, x: offsetX + x * scale, y: offsetY + y * scale,
      width: w * scale, height: h * scale, disabled: !!disabled,
      onPress: typeof onPress === 'function' ? onPress : () => {},
    };
    entry.rect = { x: entry.x, y: entry.y, width: entry.width, height: entry.height };
    controls.push(entry);
    if (hooks.onControl) hooks.onControl(entry);
  }
  function button(id, label, x, y, w, h, onPress, options) {
    options = options || {};
    const disabled = !!options.disabled, primary = !!options.primary;
    const fill = disabled ? C.disabled : primary ? C.ink : options.fill || C.soft;
    rounded(x, y, w, h, options.radius || 11, fill, primary || disabled ? null : C.line);
    const ink = disabled ? '#9EA893' : primary ? '#FFFFFF' : C.ink;
    if (options.note) {
      text(label, x + w / 2, y + h * 0.36, options.size || 13, ink, 500, 'center');
      text(options.note, x + w / 2, y + h * 0.75, 9, disabled ? '#A2AB99' : primary ? '#C8D6BA' : C.muted, 400, 'center');
    } else text(label, x + w / 2, y + h / 2, options.size || 14, ink, 500, 'center');
    control(id, options.accessibilityLabel || label, x, y, w, h, onPress, disabled);
  }

  function grape(x, y, size, green) {
    const dark = green ? '#78964C' : C.purple, light = green ? '#B9CE7C' : '#A782B0';
    [[-0.37, -0.2], [0.35, -0.2], [-0.62, 0.4], [0.04, 0.43], [0.65, 0.4], [-0.27, 1.02], [0.36, 1.03], [0.04, 1.59]].forEach(([dx, dy], i) => {
      circle(x + dx * size, y + dy * size, size * 0.43, i % 3 === 0 ? light : dark);
      circle(x + dx * size - size * 0.1, y + dy * size - size * 0.1, size * 0.08, '#D5E0B1');
    });
    line(x, y - size * 0.48, x + size * 0.24, y - size * 1.03, C.ink, Math.max(1, size * 0.12));
    poly([[x + size * 0.15, y - size * 0.68], [x + size * 1.03, y - size * 1.04], [x + size * 0.87, y - size * 0.35]], C.leaf);
  }
  function weatherIcon(kind, x, y, size) {
    const rainy = kind === 'rain' || kind === 'shower';
    if (!rainy || kind === 'shower') {
      circle(x - (rainy ? 4 : 0), y - 3, size * 0.28, '#DDB563');
      for (let i = 0; i < 8; i++) {
        const a = i * Math.PI / 4;
        line(x + Math.cos(a) * size * 0.39, y - 3 + Math.sin(a) * size * 0.39,
          x + Math.cos(a) * size * 0.5, y - 3 + Math.sin(a) * size * 0.5, '#CBA253', 1.2);
      }
    }
    if (rainy) {
      circle(x - 6, y + 1, 6, '#A7B7AB'); circle(x + 2, y - 1, 8, '#A7B7AB'); circle(x + 9, y + 3, 5, '#A7B7AB');
      rounded(x - 9, y + 2, 22, 6, 3, '#A7B7AB');
      [-6, 2, 9].forEach(dx => line(x + dx, y + 11, x + dx - 2, y + 15, '#809CAC', 1.4));
    }
  }
  function tabIcon(type, x, y, active) {
    const ink = active ? C.ink : '#96A18C';
    if (type === 'farm') {
      poly([[x, y - 8], [x + 12, y - 2], [x, y + 4], [x - 12, y - 2]], null, ink, 1.4);
      line(x - 7, y - 4, x - 7, y - 12, ink, 1.2); line(x + 4, y + 1, x + 4, y - 8, ink, 1.2);
      circle(x - 7, y - 11, 3, ink); circle(x + 4, y - 7, 3, ink);
    } else if (type === 'inventory') {
      poly([[x - 11, y - 4], [x + 11, y - 4], [x + 8, y + 7], [x - 8, y + 7]], null, ink, 1.5);
      ctx.beginPath(); ctx.arc(x, y - 4, 7, Math.PI, 0); ctx.strokeStyle = ink; ctx.lineWidth = 1.5; ctx.stroke();
      line(x - 5, y - 1, x - 4, y + 4, ink); line(x + 5, y - 1, x + 4, y + 4, ink);
    } else if (type === 'facilities') {
      poly([[x - 12, y - 1], [x, y - 11], [x + 12, y - 1]], null, ink, 1.5);
      line(x - 9, y - 2, x - 9, y + 8, ink, 1.5); line(x + 9, y - 2, x + 9, y + 8, ink, 1.5);
      line(x - 9, y + 8, x + 9, y + 8, ink, 1.5); line(x, y + 8, x, y, ink, 1.2);
    } else {
      rounded(x - 10, y - 10, 20, 21, 3, null, ink);
      [-4, 1, 6].forEach(dy => line(x - 5, y + dy, x + 6, y + dy, ink));
      line(x - 7, y - 10, x - 7, y + 10, ink);
    }
  }
  function heading(title, note) {
    text(title, 22, 170, 17, C.ink, 500, 'left', true);
    if (note) text(note, 368, 171, 10, C.muted, 400, 'right');
  }
  function header(view, state) {
    grape(31, 34, 6, false);
    text('迟记葡萄园', 52, 33, 23, C.ink, 500, 'left', true);
    text('种一园葡萄，过一季日子。', 24, 62, 11, C.muted);
    text(view.demo ? '试玩版' : '云端进度', 365, 62, 9, C.muted, 400, 'right');
    circle(353, 35, 14, C.soft, C.line); text('?', 353, 36, 16, C.ink, 500, 'center');
    control('help', '查看种植指南', 337, 18, 33, 35, view.onHelp, false);
    line(22, 79, 368, 79, C.line);
    if (!state) return;
    const stage = getStage(state.day);
    const seed = ((state.seed || 0) ^ Math.imul(state.season - 1, 0x85EBCA6B)) >>> 0;
    const weather = getWeather(seed, state.day);
    text(`第 ${state.season} 季 · 第 ${state.day} 天`, 24, 100, 14, C.ink, 500);
    text(stage.name, 24, 124, 11, C.muted);
    rounded(108, 120, 46, 4, 2, '#DFE4D6'); rounded(108, 120, Math.max(3, 46 * state.day / RULES.seasonDays), 4, 2, C.green);
    weatherIcon(weather.kind, 207, 103, 27);
    text(weather.name, 207, 130, 10, C.muted, 400, 'center');
    text(`${weather.temp}°`, 228, 101, 12, C.muted);
    text(money(state.coins), 366, 101, 22, C.ink, 500, 'right');
    text('经营币', 366, 127, 10, C.muted, 400, 'right');
    line(22, 147, 368, 147, C.line);
  }
  function footer(view) {
    if (view.status) text(short(view.status, 346, 10), 195, 779, 10, C.muted, 400, 'center');
    line(18, 791, 372, 791, C.line);
    const tabs = [['farm', '葡萄园'], ['inventory', '果实仓'], ['facilities', '小设施'], ['rank', '排行榜'], ['journal', '种植手记']];
    tabs.forEach(([id, label], i) => {
      const x = 39 + i * 78, active = view.tab === id;
      if (active) rounded(x - 27, 797, 54, 38, 13, '#E3EBD9');
      tabIcon(id, x, 808, active); text(label, x, 829, 10, active ? C.ink : '#909B85', active ? 500 : 400, 'center');
      control(`tab-${id}`, label, i * 78 + 4, 795, 70, 47, () => view.onTab(id), false);
    });
  }

  function tree(x, y, size) {
    line(x, y, x, y - size * 0.8, '#817559', 3);
    ellipse(x - size * 0.22, y - size * 0.75, size * 0.5, size * 0.6, '#92A86A');
    ellipse(x + size * 0.18, y - size * 0.94, size * 0.48, size * 0.61, '#A5BA7C');
    ellipse(x + size * 0.13, y - size * 0.6, size * 0.44, size * 0.43, '#899F61');
  }
  function vine(x, y, growth, harvested, mature, phase) {
    line(x, y + 4, x, y - 20, '#786C51', 2);
    line(x - 4, y - 11, x + 4, y - 18, '#817559', 1.6);
    const drift = Math.sin(phase + x * 0.02) * 0.7;
    ellipse(x - 10 + drift, y - 17, 12 * growth, 7 * growth, '#6F874F');
    ellipse(x + 8 + drift, y - 20, 13 * growth, 8 * growth, C.leaf);
    ellipse(x + drift, y - 24, 14 * growth, 8 * growth, C.lightLeaf);
    ellipse(x + 13 + drift, y - 17, 8 * growth, 5 * growth, '#90AB62');
    if (!harvested && mature) grape(x + 8, y - 8, 3.2, true);
  }
  function drawPlot(plot, x, y, state, selected, phase) {
    if (selected) poly([[x, y - 34], [x + 77, y], [x, y + 37], [x - 77, y]], '#D7E1BD', C.ink, 1.7);
    ellipse(x, y + 28, 66, 13, 'rgba(88,91,59,0.10)');
    poly([[x - 70, y], [x, y + 31], [x, y + 43], [x - 70, y + 12]], '#9E8056');
    poly([[x + 70, y], [x, y + 31], [x, y + 43], [x + 70, y + 12]], '#8C724D');
    poly([[x, y - 31], [x + 70, y], [x, y + 31], [x - 70, y]], plot.moisture > 70 ? '#A38B61' : C.soil, '#C5AA7B');
    for (let i = -2; i <= 2; i++) line(x - 45 + i * 10, y - 6 - i * 5, x + 22 + i * 10, y + 24 - i * 5, '#BCA173', 1.2);
    const growth = state.day < 7 ? 0.62 : Math.max(0.75, Math.min(1.17, plot.canopy / 65));
    [[-33, -10], [-7, 1], [20, 13]].forEach(([dx, dy]) => vine(x + dx, y + dy, growth, plot.harvested, state.day >= 35, phase));
    line(x - 53, y - 24, x + 44, y + 20, '#839575', 1.1);
    line(x - 53, y - 24, x - 53, y + 1, '#8B785A', 2);
    line(x + 44, y + 20, x + 44, y + 38, '#8B785A', 2);
    if (state.facilities.shelter) {
      poly([[x - 68, y - 44], [x - 5, y - 69], [x + 67, y - 36], [x + 4, y - 10]], 'rgba(249,252,234,0.25)', '#B1BBA0');
      line(x - 68, y - 44, x - 68, y + 4, '#A0A68F'); line(x + 67, y - 36, x + 67, y + 8, '#A0A68F');
    }
    if (state.facilities.drip) line(x - 40, y + 8, x + 21, y + 34, '#70919A', 1.8);
    control(`plot-${plot.id}`, `选择${plot.name}${plot.harvested ? '，已采收' : ''}`, x - 49, y - 22, 98, 87,
      () => currentView.onSelectPlot(plot.id), !!currentView.busy);
  }
  function orchard(state, selected, elapsed) {
    rounded(16, 183, 358, 268, 22, '#E6ECCD');
    ctx.save();
    rounded(16, 183, 358, 268, 22); ctx.clip();
    poly([[16, 237], [68, 208], [117, 231], [184, 201], [252, 226], [330, 210], [374, 230], [374, 287], [16, 287]], '#D2DEB3');
    poly([[16, 253], [90, 230], [161, 256], [270, 229], [374, 254], [374, 287], [16, 287]], '#C4D3A2');
    ctx.beginPath(); ctx.moveTo(36, 461); ctx.bezierCurveTo(20, 398, 41, 345, 64, 307); ctx.bezierCurveTo(96, 257, 213, 243, 326, 238);
    ctx.strokeStyle = '#D6CFA5'; ctx.lineWidth = 23; ctx.stroke();
    tree(41, 264, 25); tree(348, 306, 21);
    rounded(301, 226, 39, 29, 2, '#F2E7CE'); poly([[296, 228], [319, 208], [345, 228]], '#9E7764');
    rounded(311, 238, 8, 17, 1, '#B69F7C'); rounded(324, 234, 8, 8, 1, '#849D8B');
    line(291, 255, 350, 255, '#BAAD87');
    // 绘制顺序跟随从后到前的地块，使棚架和葡萄有真实遮挡。
    const positions = [[186, 276], [267, 319], [105, 319], [186, 362]];
    state.plots.forEach((plot, i) => drawPlot(plot, positions[i][0], positions[i][1], state, plot.id === selected, elapsed / 1900));
    state.plots.forEach((plot, i) => {
      const [x, y] = positions[i], active = plot.id === selected;
      rounded(x - 33, y + 43, 66, 22, 11, active ? C.ink : '#F5F3E5');
      text(`${plot.id} · ${plot.name}`, x, y + 54, 9.5, active ? '#F7F7EB' : '#68785A', 500, 'center');
    });
    for (let i = 0; i < 6; i++) {
      const x = 292 + i * 12, y = 404 + i * 2;
      line(x, y, x, y + 14, '#A6A180', 2); if (i) line(x - 12, y + 5, x, y + 7, '#B5AF8D', 1.4);
    }
    [[60, 424], [330, 378], [278, 436], [30, 291]].forEach(([x, y]) => {
      line(x, y, x - 3, y - 6, '#99AC75'); line(x, y, x + 1, y - 8, '#99AC75'); line(x, y, x + 5, y - 5, '#99AC75');
    });
    text('点一点地块，看看葡萄的近况', 195, 435, 9, '#7C8E65', 400, 'center');
    ctx.restore();
  }

  function meter(label, value, x, color) {
    text(label, x, 520, 10, C.muted); text(`${pct(value)}%`, x + 97, 520, 11, C.ink, 500, 'right');
    rounded(x, 535, 97, 5, 2.5, '#E5E9DD');
    rounded(x, 535, Math.max(2, Math.min(97, 97 * value / 100)), 5, 2.5, color);
  }
  function advice(plot, state) {
    if (plot.harvested) return '这块地已经采收。去果实仓卖出葡萄，减少采后损耗。';
    if (plot.moisture < 28) return '土壤偏干，果粒生长会变慢。先补水，再留意下一周的天气。';
    if (plot.moisture > 88) return '土壤偏湿，葡萄也需要透气。别急着继续灌溉。';
    if (plot.canopy > 78) return '枝叶有些拥挤。适度整枝能通风，剪得过多也会影响糖度。';
    if (plot.load > plot.vigor * 0.95 && state.day >= 21 && state.day <= 55) return '留果偏多会拖慢成熟。适度疏果，让养分留给更好的果实。';
    if (state.day >= 56 && plot.brix >= RULES.minHarvestBrix) return '已经可以采收。再等一等或许更甜，也要留意病害与损耗。';
    return '水分、枝叶和留果量要互相配合。照料后推进时间，看看这一周的变化。';
  }
  function actionAvailability(type, plot, state, busy) {
    const rule = ACTIONS[type];
    if (busy) return { disabled: true, note: '正在处理' };
    if (plot.harvested) return { disabled: true, note: '已采收' };
    if (state.day < rule.start) return { disabled: true, note: `第 ${rule.start} 天开放` };
    if (state.day > rule.end) return { disabled: true, note: '本季窗口已过' };
    if (type !== 'harvest' && state.day - plot.lastActions[type] < rule.cooldown) {
      return { disabled: true, note: `${rule.cooldown - state.day + plot.lastActions[type]} 天后可做` };
    }
    if (type === 'harvest' && plot.brix < RULES.minHarvestBrix) return { disabled: true, note: `糖度需达 ${RULES.minHarvestBrix}°` };
    if (type === 'prune' && plot.canopy <= 24) return { disabled: true, note: '叶量已偏少' };
    if (type === 'thin' && plot.load <= 24) return { disabled: true, note: '留果已偏少' };
    if (state.coins < rule.cost) return { disabled: true, note: '经营币不足' };
    return { disabled: false, note: `${money(rule.cost)} 币` };
  }
  function farm(view, state, elapsed) {
    if (state.day === RULES.seasonDays) { seasonReport(view, state); return; }
    const selected = state.plots.find(plot => plot.id === view.selectedPlot) || state.plots[0];
    heading('我的小葡萄园', '鲜食葡萄 · 四块地，一季经营');
    orchard(state, selected.id, elapsed);
    rounded(16, 463, 358, 249, 18, C.card, C.line);
    text(selected.name, 31, 489, 18, C.ink, 500, 'left', true);
    text(`地块 ${selected.id}`, 117, 490, 10, C.muted);
    rounded(304, 476, 54, 22, 11, selected.harvested ? '#E8E2EC' : C.soft);
    text(selected.harvested ? '已采收' : '生长中', 331, 487, 10, selected.harvested ? C.purple : '#6A8255', 500, 'center');
    meter('土壤水分', selected.moisture, 31, selected.moisture < 28 || selected.moisture > 88 ? C.gold : '#88A5A3');
    meter('枝叶密度', selected.canopy, 146, C.green);
    meter('留果负载', selected.load, 260, C.purple);
    text(`糖度 ${selected.brix.toFixed(1)}°Bx`, 31, 560, 10.5, C.ink);
    text(`粒重 ${selected.berryWeight.toFixed(1)}g`, 146, 560, 10.5, C.ink);
    text(`损伤 ${pct(selected.damage)}%`, 260, 560, 10.5, selected.damage > 20 ? C.danger : C.ink);
    wrap(advice(selected, state), 31, 582, 326, 10, C.muted, 14, 2);
    [['water', '灌溉'], ['fertilize', '施肥'], ['prune', '修剪'], ['thin', '疏果'], ['protect', '巡护'], ['harvest', '采收']].forEach(([type, label], i) => {
      const available = actionAvailability(type, selected, state, view.busy);
      const x = 30 + (i % 3) * 113, y = 613 + Math.floor(i / 3) * 46;
      button(`action-${type}`, label, x, y, 103, 40,
        () => view.onAction({ type, plotId: selected.id }), {
          disabled: available.disabled, note: available.note, fill: type === 'harvest' ? '#EAE1ED' : C.soft,
          accessibilityLabel: `${selected.name}${label}，${available.note}`,
        });
    });
    const days = Math.min(RULES.advanceDays, RULES.seasonDays - state.day);
    button('advance', view.busy ? '正在照料葡萄园…' : `照料好了，过 ${days} 天  →`, 20, 723, 350, 46, view.onAdvance,
      { primary: true, disabled: view.busy, size: 14 });
  }

  function basket(x, y, size) {
    grape(x - size * 0.24, y - size * 0.15, size * 0.16, true);
    grape(x + size * 0.25, y - size * 0.08, size * 0.18, true);
    grape(x, y - size * 0.38, size * 0.19, true);
    poly([[x - size * 0.68, y], [x + size * 0.68, y], [x + size * 0.5, y + size * 0.5], [x - size * 0.5, y + size * 0.5]], '#C0A079', '#A78B64');
    for (let i = -3; i <= 3; i++) line(x + i * size * 0.13, y + 2, x + i * size * 0.11, y + size * 0.44, '#A78B64', 1.5);
    line(x - size * 0.6, y + size * 0.15, x + size * 0.6, y + size * 0.15, '#D3B48A', 2);
    line(x - size * 0.55, y + size * 0.33, x + size * 0.55, y + size * 0.33, '#D3B48A', 2);
  }
  function saleEstimate(state, channel) {
    return state.inventory.reduce((sum, batch) => sum + Math.floor(batch.weight * RULES.prices[channel][batch.grade] / 1000)
      - Math.ceil(batch.weight * RULES.channelCost[channel] / 1000), 0);
  }
  function inventory(view, state) {
    heading('把好果实，送到好地方', `现有 ${state.inventory.length} 批`);
    text('采收后的葡萄，也需要好好照料。', 22, 197, 11, C.muted);
    rounded(18, 218, 354, 96, 18, C.soft);
    basket(62, 264, 37);
    const weight = state.inventory.reduce((sum, batch) => sum + batch.weight, 0);
    text(kg(weight), 117, 250, 30, C.ink, 500); text('kg 库存', 117, 281, 11, C.muted);
    text(state.facilities.coldStorage ? '冷库保鲜中' : '常温存放', 351, 249, 12, state.facilities.coldStorage ? C.ink : C.gold, 500, 'right');
    text('鲜果会随时间损耗', 351, 277, 10, C.muted, 400, 'right');
    if (!state.inventory.length) {
      circle(195, 422, 70, '#ECEEDD'); basket(195, 425, 68);
      text('等这一季的第一筐葡萄', 195, 516, 18, C.ink, 400, 'center', true);
      text('果实成熟后，到葡萄园点“采收”。', 195, 548, 11, C.muted, 400, 'center');
    } else state.inventory.forEach((batch, i) => {
      const y = 328 + i * 72;
      rounded(18, y, 354, 62, 12, C.card, C.line);
      rounded(30, y + 13, 37, 35, 9, batch.grade === 'A' ? '#E4EAD6' : batch.grade === 'B' ? '#EBE3EE' : '#EFE6D6');
      text(batch.grade, 49, y + 31, 18, batch.grade === 'A' ? C.ink : C.purple, 500, 'center');
      const plot = state.plots.find(item => item.id === batch.plotId);
      text(plot ? plot.name : `地块 ${batch.plotId}`, 81, y + 22, 13, C.ink, 500);
      text(`糖度 ${batch.brix.toFixed(1)}° · 第 ${batch.harvestDay} 天采收`, 81, y + 43, 10, C.muted);
      text(`${kg(batch.weight)} kg`, 354, y + 30, 17, C.ink, 500, 'right');
    });
    [['wholesale', '批发售出'], ['retail', '零售售出']].forEach(([channel, label], i) => {
      const x = 18 + i * 183;
      rounded(x, 632, 171, 90, 15, weight ? (i ? '#E9E1EC' : '#E1E8D6') : '#E6E7DF', C.line);
      text(label, x + 16, 653, 15, weight ? C.ink : '#98A18E', 500);
      text(`预计净入 ${money(saleEstimate(state, channel))} 币`, x + 16, 682, 12, weight ? C.ink : '#98A18E');
      text(`渠道费 ${RULES.channelCost[channel]} 币/kg`, x + 16, 704, 9, C.muted);
      text('↗', x + 150, 653, 16, weight ? C.ink : '#98A18E', 400, 'right');
      control(`sell-${channel}`, `${label}，预计净入 ${money(saleEstimate(state, channel))} 经营币`, x, 632, 171, 90,
        () => view.onSell(channel), view.busy || !weight);
    });
    text('两种渠道都售出全部库存；经营币用于游戏内经营。', 195, 747, 10, C.muted, 400, 'center');
  }

  function facilityIcon(type, x, y) {
    circle(x, y, 32, '#EDF0E2');
    if (type === 'drip') {
      line(x - 24, y - 3, x + 20, y - 3, '#8B9A86', 4);
      [-13, 0, 13].forEach(dx => {
        line(x + dx, y - 4, x + dx, y + 10, '#899E98', 2);
        ctx.beginPath(); ctx.moveTo(x + dx, y + 12); ctx.quadraticCurveTo(x + dx - 5, y + 22, x + dx, y + 23); ctx.quadraticCurveTo(x + dx + 5, y + 22, x + dx, y + 12);
        ctx.fillStyle = '#94B5B8'; ctx.fill();
      });
      rounded(x - 28, y - 9, 8, 12, 2, '#869C77');
    } else if (type === 'shelter') {
      line(x - 23, y + 19, x - 23, y - 6, '#879A77', 2); line(x + 23, y + 19, x + 23, y - 6, '#879A77', 2);
      ctx.beginPath(); ctx.moveTo(x - 28, y - 5); ctx.quadraticCurveTo(x, y - 39, x + 28, y - 5);
      ctx.lineTo(x - 28, y - 5); ctx.fillStyle = '#D0DAB9'; ctx.fill(); ctx.strokeStyle = '#879A77'; ctx.lineWidth = 2; ctx.stroke();
      line(x, y - 24, x, y - 5, '#A0B38C', 1); grape(x, y + 7, 5, true);
    } else {
      poly([[x - 22, y - 11], [x + 5, y - 24], [x + 24, y - 14], [x - 3, y]], '#D0DCD2', '#8DA79B');
      poly([[x - 22, y - 11], [x - 3, y], [x - 3, y + 26], [x - 22, y + 15]], '#B4C7BE', '#8DA79B');
      poly([[x - 3, y], [x + 24, y - 14], [x + 24, y + 14], [x - 3, y + 26]], '#E0E9E0', '#8DA79B');
      line(x + 7, y + 5, x + 7, y + 12, '#7E9A8C', 2); text('✳', x + 15, y + 3, 14, '#7D9F96', 400, 'center');
    }
  }
  function facilities(view, state) {
    heading('给葡萄园，添一点帮手', '慢慢建设，认真经营');
    const descriptions = {
      drip: '稳定补水，减少干旱影响。运行时也需要维护费用。',
      shelter: '减少雨水带来的病害压力。棚内仍需留意水分和通风。',
      coldStorage: '让采后的葡萄多留一点新鲜。减少库存每日的损耗。'
    };
    ['drip', 'shelter', 'coldStorage'].forEach((type, i) => {
      const y = 207 + i * 166, rule = RULES.facilities[type], built = state.facilities[type];
      rounded(18, y, 354, 151, 18, C.card, C.line);
      facilityIcon(type, 63, y + 64);
      text(rule.name, 113, y + 27, 18, C.ink, 500, 'left', true);
      text(`${money(rule.cost)} 币建设 · ${rule.dailyCost} 币/天维护`, 113, y + 54, 10, C.gold);
      wrap(descriptions[type], 113, y + 79, 238, 10.5, C.muted, 17, 2);
      const insufficient = state.coins < rule.cost, closed = state.day === RULES.seasonDays;
      button(`upgrade-${type}`, built ? '已经建成  ✓' : closed ? '下一季再建设' : insufficient ? '经营币不足' : '建设这项设施  →',
        113, y + 110, 241, 29, () => view.onUpgrade(type),
        { primary: !built, disabled: view.busy || built || insufficient || closed, size: 11 });
    });
    wrap('设施会随时间扣除运行维护费。经营币不足时，今日设施暂停运行。', 28, 730, 334, 10, C.muted, 17, 2);
  }

  function rankBoard(view) {
    heading('果园排行榜', '按繁荣度（累计收益+金币）排名');
    const board = view.leaderboard;
    if (view.rankLoading) { text('正在加载排行…', 195, 400, 13, C.muted, 400, 'center'); return; }
    if (!board || !board.items || !board.items.length) { text('暂无排行数据，先去经营你的果园吧。', 195, 400, 13, C.muted, 400, 'center'); return; }
    if (board.myRank) text(`我的排名：第 ${board.myRank} 名 · 繁荣度 ${board.myScore}`, 22, 205, 12, C.ink, 500);
    board.items.slice(0, 12).forEach((entry, i) => {
      const y = 235 + i * 40;
      rounded(18, y, 354, 34, 10, entry.mine ? '#E2E9D5' : C.card, C.line);
      const medal = i === 0 ? '🥇' : i === 1 ? '🥈' : i === 2 ? '🥉' : `${i + 1}`;
      text(medal, 34, y + 22, 13, C.ink, 500);
      text(entry.nickname || '匿名果园', 80, y + 22, 12, C.ink, 400);
      text(`${entry.score}`, 330, y + 22, 12, C.ink, 500, 'right');
    });
    text('助力好友：为好友果园 +30 金币，每天最多助力 5 次。', 22, 730, 10, C.muted, 400);
    button('rank-refresh', '刷新排行', 20, 748, 170, 44, view.onRankRefresh, { size: 12 });
  }
  function journal(view, state) {
    heading('这一季的种植手记', '每一次照料，都留下经验');
    const stats = [['本季收入', state.totalRevenue, C.ink], ['本季支出', state.totalCosts, C.gold], ['经营盈余', state.totalRevenue - state.totalCosts, C.purple]];
    stats.forEach(([label, value, color], i) => {
      const x = 18 + i * 121;
      rounded(x, 207, 112, 88, 14, C.card, C.line);
      text(label, x + 14, 230, 10, C.muted); text(money(value), x + 14, 262, 20, color, 500); text('币', x + 95, 266, 9, C.muted, 400, 'right');
    });
    text('最近农事', 23, 326, 12, C.ink, 500); line(90, 326, 366, 326, C.line);
    const entries = state.history.slice(-6).reverse();
    if (!entries.length) {
      circle(195, 452, 65, '#E9EDDE'); tabIcon('journal', 195, 450, true);
      text('第一篇手记，等你来写。', 195, 549, 18, C.ink, 400, 'center', true);
      text('去照料一块地，记录就会留在这里。', 195, 582, 11, C.muted, 400, 'center');
    } else entries.forEach((entry, i) => {
      const y = 355 + i * 57;
      if (i < entries.length - 1) line(29, y + 7, 29, y + 50, '#DBE2D0');
      circle(29, y + 2, 3.5, entry.type === 'sell' ? C.purple : C.green);
      const amount = entry.revenue !== undefined ? `+${money(entry.revenue)}` : entry.cost !== undefined ? `−${money(entry.cost)}` : '';
      text(`第 ${entry.season} 季 · ${entry.day} 天`, 43, y, 9, C.muted);
      if (amount) text(amount, 367, y, 10, entry.revenue !== undefined ? C.ink : C.gold, 500, 'right');
      wrap(entry.text, 43, y + 21, 320, 10.5, C.ink, 15, 2);
    });
    button('journal-help', '读一读种植指南', 20, 723, 170, 46, view.onHelp, { size: 12 });
    button('reset', '重新开始经营', 200, 723, 170, 46, view.onReset, { size: 12, disabled: view.busy, fill: '#EDE6DC' });
  }
  function seasonReport(view, state) {
    const report = state.report || {};
    heading(`第 ${state.season} 季的收成`, '一季结束，经验留下');
    rounded(18, 198, 354, 160, 20, '#E2E9D5');
    basket(295, 278, 59);
    text('从枝头到果筐，', 39, 233, 21, C.ink, 400, 'left', true);
    text('你把这一季种完了。', 39, 265, 21, C.ink, 400, 'left', true);
    text(`本季盈余  ${money(report.profit === undefined ? state.totalRevenue - state.totalCosts : report.profit)} 币`, 39, 317, 13, C.ink, 500);
    rounded(18, 374, 354, 338, 18, C.card, C.line);
    text('一季复盘', 34, 399, 16, C.ink, 500, 'left', true);
    const items = [
      ['采收重量', `${kg(state.stats.harvestWeight)} kg`], ['售出重量', `${kg(state.stats.soldWeight)} kg`],
      ['平均糖度', `${Number(report.avgBrix || 0).toFixed(1)}°Bx`], ['最好等级', `${report.bestGrade || '—'} 级`]
    ];
    items.forEach(([label, value], i) => {
      const x = 35 + (i % 2) * 174, y = 435 + Math.floor(i / 2) * 65;
      text(label, x, y, 10, C.muted); text(value, x, y + 25, 20, i === 2 ? C.purple : C.ink, 500);
    });
    line(34, 552, 357, 552, C.line);
    const notes = report.notes || ['下一季继续观察水分、枝叶与留果量，让经验慢慢变成好收成。'];
    let cursor = 575;
    notes.slice(0, 3).forEach(note => {
      circle(38, cursor, 2.5, C.green);
      const rows = wrap(note, 49, cursor, 300, 10.5, C.muted, 16, 2);
      cursor += rows * 16 + 11;
    });
    if (state.inventory.length) {
      button('season-sell', '还有鲜果库存，先去销售  →', 20, 723, 350, 46, () => view.onTab('inventory'), { primary: true, disabled: view.busy });
    } else button('new-season', '带着经验，开启下一季  →', 20, 723, 350, 46, view.onNewSeason, { primary: true, disabled: view.busy });
  }

  function loading(view, elapsed) {
    circle(195, 330, 89, '#E8EDDA'); grape(195, 312, 28, true);
    text(view.error ? '葡萄园暂时没有打开' : '正在走进你的葡萄园…', 195, 463, 20, C.ink, 400, 'center', true);
    if (view.error) {
      wrap(view.error, 56, 504, 278, 12, C.muted, 20, 4);
      button('retry', '再试一次', 100, 622, 190, 46, view.onRetry, { primary: true, disabled: view.busy });
      if (view.demo) button('reset', '确认后重新开园', 100, 684, 190, 42, view.onReset, { size: 12, disabled: view.busy });
    } else {
      text('四块小田，等你慢慢种好。', 195, 505, 12, C.muted, 400, 'center');
      for (let i = 0; i < 3; i++) circle(181 + i * 14, 548, 3, Math.floor(elapsed / 380) % 3 === i ? C.green : '#D2DCC5');
    }
  }
  function errorBanner(view) {
    rounded(20, 155, 350, 68, 12, '#FAEBDF', '#E0BBA5');
    wrap(view.error, 34, 176, 234, 11, '#8A5747', 17, 2);
    button('retry', '重试', 280, 179, 53, 29, view.onRetry, { size: 11, disabled: view.busy, fill: '#F0D6C4' });
    text('×', 348, 169, 20, '#A27B65', 400, 'center');
    control('dismiss-error', '关闭提示', 337, 155, 30, 26, view.onDismissError, false);
  }
  function modal(view) {
    const m = view.modal;
    ctx.fillStyle = 'rgba(34,50,35,0.42)'; ctx.fillRect(0, 0, W, H);
    font(12);
    // 先计算行数，短确认保持紧凑，指南允许中文完整换行。
    const chars = Array.from(String(m.body || ''));
    let row = '', count = 1;
    chars.forEach(char => {
      if (char === '\n') { count++; row = ''; }
      else if (ctx.measureText(row + char).width > 292) { count++; row = char; }
      else row += char;
    });
    const lines = Math.min(23, count), h = Math.min(700, 133 + lines * 20), y = (H - h) / 2;
    rounded(24, y, 342, h, 22, C.paper);
    text(m.title || '葡萄园', 46, y + 34, 20, C.ink, 500, 'left', true);
    wrap(m.body, 46, y + 74, 298, 12, C.muted, 20, lines);
    allowControls = true;
    const by = y + h - 62;
    if (m.onCancel || m.cancelText) {
      button('modal-cancel', m.cancelText || '取消', 42, by, 142, 42, m.onCancel, { size: 13 });
      button('modal-confirm', m.confirmText || '确定', 197, by, 151, 42, m.onConfirm, { primary: true, size: 13, disabled: view.busy });
    } else button('modal-confirm', m.confirmText || '知道了', 42, by, 306, 42, m.onConfirm, { primary: true, size: 13, disabled: view.busy });
  }

  function render(view, elapsedMs) {
    currentView = view; controls = []; allowControls = !view.modal;
    if (hooks.onControlsStart) hooks.onControlsStart();
    const elapsed = Number(elapsedMs) || 0;
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.fillStyle = C.paper; ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.setTransform(dpr * scale, 0, 0, dpr * scale, offsetX * dpr, offsetY * dpr);
    ctx.lineCap = 'round'; ctx.lineJoin = 'round'; ctx.globalAlpha = 1;
    // 一点纸张颗粒，不需要位图素材或网络资源。
    ctx.fillStyle = 'rgba(103,106,68,0.025)';
    for (let i = 0; i < 150; i++) ctx.fillRect((i * 97 + 13) % W, (i * 149 + 23) % H, 1, 1);
    header(view, view.state);
    if (!view.state) loading(view, elapsed);
    else {
      if (view.tab === 'inventory') inventory(view, view.state);
      else if (view.tab === 'facilities') facilities(view, view.state);
      else if (view.tab === 'journal') journal(view, view.state);
      else if (view.tab === 'rank') rankBoard(view);
      else farm(view, view.state, elapsed);
      footer(view);
      if (view.error) errorBanner(view);
    }
    if (view.modal) modal(view);
  }
  function hit(x, y) {
    for (let i = controls.length - 1; i >= 0; i--) {
      const entry = controls[i];
      if (x >= entry.x && x <= entry.x + entry.width && y >= entry.y && y <= entry.y + entry.height) {
        if (!entry.disabled) entry.onPress();
        return entry.disabled ? null : entry.id;
      }
    }
    return null;
  }
  resize(W, H, 1, 0, 0);
  return { resize, render, hit, getControls: () => controls.slice() };
}

module.exports = { createRenderer };
