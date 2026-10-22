'use strict';
const { start } = require('../src/main');
window.vineyardGame = start();
window.addEventListener('pagehide', () => window.vineyardGame.destroy(), { once: true });
