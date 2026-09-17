// LuCI owns these screen styles; legacy.css owns the shared tokens. This script
// emits an apply_patch payload (or checks drift), and never evaluates browser JS.
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.resolve(__dirname, '..');
let patch = '*** Begin Patch\n';
for (const [name, source] of [
  ['settings', 'settings-20260917-v9.js'],
  ['diagnostics', 'diagnostics-20260904-v3.js']
]) {
  const input = 'luci-app-fastlane/htdocs/luci-static/resources/view/fastlane/' + source;
  const text = fs.readFileSync(path.join(root,input),'utf8');
  const literal = text.match(/var css = (`[\s\S]*?`);/);
  if (!literal || literal[1].includes('${')) throw Error('Expected static CSS');
  let css = vm.runInNewContext(literal[1]);
  for (const match of text.matchAll(/^css \+= ('[^\n]*');$/gm)) css += vm.runInNewContext(match[1]);
  // Scope inherited panel styles and keep the original layout responsive at
  // intermediate widths where the standalone shell differs from LuCI.
  const adaptation = name === 'settings'
    ? '\n.fastlane-settings .fls-head>div{min-width:0;max-width:100%}.fastlane-settings .fls-head h2{overflow-wrap:anywhere}.fastlane-settings .fls-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.fastlane-settings .fls-field{grid-template-columns:minmax(0,1fr) minmax(0,240px)}.fastlane-settings .fls-card,.fastlane-settings .fls-field>*{min-width:0}.fastlane-settings .fls-button{font:inherit}.fastlane-settings .fls-duration{gap:6px}.fastlane-settings .fls-duration-segment{min-width:0;flex:1 1 0}.fastlane-settings .fls-duration-input{min-width:0!important;flex:1 1 0;max-width:4ch!important}.fastlane-settings .fls-field label,.fastlane-settings .fls-card h3,.fastlane-settings .fls-manage-copy{overflow-wrap:anywhere}.fastlane-settings .fls-button{min-width:0;max-width:100%;white-space:normal;overflow-wrap:anywhere}.fastlane-settings .fls-select{max-width:260px}.fastlane-settings .fls-input{height:48px!important}.fastlane-settings .fls-duration{height:48px}@media(max-width:1000px){.fastlane-settings .fls-grid{grid-template-columns:minmax(0,1fr)}}@media(max-width:850px){.fastlane-settings .fls-field{grid-template-columns:minmax(0,1fr)}}\n'
    : '\n.fastlane-diagnostics .fld-row>*{min-width:0;overflow-wrap:anywhere}.fastlane-diagnostics .fld-head>div{min-width:0;max-width:100%}.fastlane-diagnostics .fld-head h1{margin:0;overflow-wrap:anywhere}.fastlane-diagnostics .fld-head p{margin-bottom:0}.fastlane-diagnostics .fld-button{min-width:0;max-width:100%;white-space:normal;overflow-wrap:anywhere}.fastlane-diagnostics .fastlane-diagnostics-actions{min-width:0;flex-wrap:wrap}.fastlane-diagnostics button:disabled{opacity:.5}@media(max-width:760px){.fastlane-diagnostics .fld-head{flex-wrap:wrap}}\n';
  const content = '/* Source: '+input+'; generated via scripts/sync-settings-panel.cjs. Shared tokens: legacy.css. */\n'+css+adaptation;
  const target = path.join(root,'internal/managementhttp/web/'+name+'-panel.css');
  if (process.argv.includes('--check')) {
    if (!fs.existsSync(target) || fs.readFileSync(target,'utf8') !== content) throw Error('CSS source drift: '+target);
  } else {
    if (fs.existsSync(target)) {
      patch += '*** Update File: '+target+'\n@@\n'+fs.readFileSync(target,'utf8').trimEnd().split('\n').map(line=>'-'+line).join('\n')+'\n';
    } else patch += '*** Add File: '+target+'\n';
    patch += content.trimEnd().split('\n').map(line=>'+'+line).join('\n')+'\n';
  }
}
if (!process.argv.includes('--check')) process.stdout.write(patch+'*** End Patch\n');
