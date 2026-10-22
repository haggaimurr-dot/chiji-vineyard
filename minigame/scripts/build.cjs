'use strict';
const esbuild = require('esbuild');
const path = require('node:path');
const fs = require('node:fs');
const root = path.resolve(__dirname, '..');
const sourceFiles = ['game.js', 'src/main.js', 'src/platform.js', 'src/service.js', 'src/model.js', 'src/rules.js', 'src/renderer.js',
  'field/main.js', 'field/model.js', 'field/platform.js', 'field/navigation.js', 'field/world.js', 'field/hud.js', 'field/wechat-entry.js'];
for (const file of sourceFiles) {
  if (!fs.existsSync(path.join(root, file))) throw new Error('Missing game file: ' + file);
  new (require('node:vm').Script)(fs.readFileSync(path.join(root, file), 'utf8'), { filename: file });
}
esbuild.buildSync({
  entryPoints: [path.join(root, 'preview/entry.js')],
  outfile: path.join(root, 'preview/game-preview.js'),
  bundle: true, platform: 'browser', format: 'iife', target: 'es2020',
  minify: false, sourcemap: false, legalComments: 'none'
});
esbuild.buildSync({
  entryPoints: [path.join(root, 'preview/field-entry.js')],
  outfile: path.join(root, 'preview/field-preview.js'),
  bundle: true, platform: 'browser', format: 'iife', target: 'es2020',
  minify: true, sourcemap: false, legalComments: 'none'
});
esbuild.buildSync({
  entryPoints: [path.join(root, 'field/wechat-entry.js')],
  outfile: path.join(root, 'field/field-wechat.js'),
  bundle: true, platform: 'neutral', format: 'cjs', target: 'es2019',
  minify: true, sourcemap: false, legalComments: 'none'
});
fs.copyFileSync(path.resolve(path.dirname(require.resolve('three')), '../LICENSE'), path.join(root, 'field/THREE-LICENSE.txt'));
console.log('WeChat native field bundle and both shared browser previews built.');
