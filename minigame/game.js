'use strict';
const config = require('./src/config');
// The new field experience currently has its own local demo save. The existing
// official service stays available when demo mode is disabled.
if (config.demo && config.experience === 'field') require('./field/field-wechat').start();
else require('./src/main').start();
