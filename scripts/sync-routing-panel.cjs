// Mechanical LuCI port. Run --check in CI; no LuCI, eval, or inline styles at runtime.
// Only the five owned routing files are written by this task.
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.resolve(__dirname, '..');
const sourcePath = 'luci-app-fastlane/htdocs/luci-static/resources/view/fastlane/routing-20260906-v5.js';
const source = fs.readFileSync(path.join(root, sourcePath), 'utf8');
const po = fs.readFileSync(path.join(root, 'luci-app-fastlane/po/ru/fastlane.po'), 'utf8');
const dictionary = {};
for (const block of po.split(/\n\s*\n/)) {
  let key = '', value = '', field = '';
  for (const line of block.split('\n')) {
    const start = line.match(/^(msgid|msgstr) (".*")$/);
    if (start) { field = start[1]; if (field === 'msgid') key = JSON.parse(start[2]); else value = JSON.parse(start[2]); }
    else if (/^"/.test(line)) { if (field === 'msgid') key += JSON.parse(line); else if (field === 'msgstr') value += JSON.parse(line); }
  }
  if (key && value) dictionary[key] = value;
}
const strings = [...source.matchAll(/_\(('(?:[^'\\]|\\.)*')\)/g)].map(m => vm.runInNewContext(m[1]));
const translations = Object.fromEntries([...new Set(strings)].map(key => [key, dictionary[key] || key]));
const missing = Object.keys(translations).filter(key => !dictionary[key]);
if (missing.length) throw Error('Missing existing Russian translations: '+missing.join(', '));
const helpers = source.slice(source.indexOf('function trim('), source.indexOf('\nvar css ='));
const cssTemplate = source.match(/var css = (`[\s\S]*?`);/)[1];
if (cssTemplate.includes('${')) throw Error('Expected static CSS');
const css = vm.runInNewContext(cssTemplate);
const methodNames = ['customRules','bypassSettings','isRuleActive','handleRuleOpen','renderRuleValues','renderBypassRules','renderImportPreview','renderContent'];
const starts = [...source.matchAll(/^\t(\w+): /gm)];
const methods = methodNames.map(name => {
  const index = starts.findIndex(m => m[1] === name);
  if (index < 0) throw Error('Missing donor method '+name);
  return source.slice(starts[index].index, starts[index+1].index).trim();
}).join('\n');

// This function is serialized into the generated browser asset. Keep all
// runtime adapters here; donor renderer functions above remain byte-for-byte.
function installRoutingPanel(translations, makeScreen) {
  'use strict';
  let instance;
  const text = value => String(value == null ? '' : value).trim();
  const words = value => text(value).split(/[\s,;]+/).filter(Boolean);
  const _ = message => (document.documentElement.lang || 'en').toLowerCase().startsWith('ru') ? (translations[message] || message) : message;
  const errors = {
    geo_runtime_unavailable:'GeoIP/GeoSite доступны только со штатной службой OpenWrt. В локальном прототипе обновление недоступно.',
    geo_status_failed:'Не удалось прочитать состояние GeoIP/GeoSite.',
    geo_update_failed:'GeoIP/GeoSite не прошли проверку. Правило не включено.',
    geo_update_timeout:'Обновление ещё не завершилось. Проверьте состояние позже.',
    routing_group_exists:'Исключение с таким именем уже существует. Откройте его для редактирования.',
    routing_group_not_editable:'Исключение не найдено или доступно только для чтения.',
    invalid_routing_group:'Проверьте домены и IPv4-адреса, подсети или диапазоны.',
    routing_group_saved_activation_failed:'Группа сохранена, но применение не завершилось. Проверьте её состояние перед повтором.',
    routing_group_delete_failed:'Не удалось удалить группу. Возможно, она используется в других правилах или сохранённых настройках.',
    routing_groups_unavailable:'Список исключений недоступен. Повторите загрузку.',
    happ_atomic_apply_unavailable:translations['The link is valid. Partial application is disabled until Fast Lane can apply custom direct, proxy, and block rules atomically.'],
    operation_failed:'Операция не выполнена. Проверьте данные и состояние службы.'
  };
  const errorText = value => (document.documentElement.lang || '').startsWith('ru') ? (errors[value] || value) : ({geo_runtime_unavailable:'Geo databases require the OpenWrt runtime. Updates are unavailable on this local prototype.',operation_failed:'Operation failed. Check the data and service status.'}[value] || value);
  function E(tag, attributes = {}, children = []) {
    const element = document.createElement(tag);
    for (const [key, value] of Object.entries(attributes)) {
      if (value == null) continue;
      if (typeof value === 'function') element.addEventListener(key, value);
      else if (['checked','selected','disabled'].includes(key)) element[key] = Boolean(value);
      else if (key === 'value') element.value = value;
      else element.setAttribute(key, value);
    }
    for (const child of (Array.isArray(children) ? children : [children]).flat(Infinity)) {
      if (child != null) element.append(child instanceof Node ? child : document.createTextNode(String(child)));
    }
    return element;
  }
  const L = {bind:(fn, self, ...args) => fn.bind(self, ...args)};
  function mount(container, shared) {
    if (!container || !shared || typeof shared.api !== 'function' || typeof shared.confirmAction !== 'function' || !window.FastLaneCountries) throw Error('Routing requires container, shared API and legacy-countries.js');
    if (instance) instance.destroy();
    let stopped = false, modal = null, requestVersion = 0, fingerprint = '', pending = false, latestJob = null, needsRetry = false;
    const countries = {
      name:code => window.FastLaneCountries.name(code),
      options:selected => window.FastLaneCountries.codes.map(code => E('option', {value:code, selected:code===selected}, [window.FastLaneCountries.name(code)+' ('+code+')'])).sort((a,b) => a.textContent.localeCompare(b.textContent,document.documentElement.lang || 'en'))
    };
    const ui = {
      createHandlerFn:(self, method, ...args) => event => self[method](...args,event),
      showModal:(title, children) => {
        ui.hideModal();
        modal = E('dialog', {class:'modal fl-dialog', 'aria-label':title}, [E('h4',{},[title]), ...children]);
        const opened=modal;
        container.append(modal);
        modal.addEventListener('close', () => { if(modal===opened) modal=null; opened.remove(); });
        modal.showModal();
      },
      hideModal:() => { if (modal) { const old=modal; modal=null; old.close(); old.remove(); } }
    };
    const screen = makeScreen(E, _, L, ui, countries);
    const page = E('div',{class:'flr-page'});
    container.replaceChildren(page);
    const notice = (message, failed=false) => { if(!stopped) shared.notice(message, failed); };
    screen.settings = {};
    screen.services = [];
    screen.geodata = {ready:false};
    screen.busy = true;
    screen.progress = _('Preparing routing…');
    screen.renderAgain = () => {
      if (stopped) return;
      const expanded = page.querySelector('.flr-advanced')?.open;
      const oldInput = page.querySelector('.flr-import input');
      const focused = document.activeElement === oldInput;
      const selection = focused ? [oldInput.selectionStart,oldInput.selectionEnd] : null;
      page.replaceChildren(...screen.renderContent());
      const advanced = page.querySelector('.flr-advanced');
      advanced.open = !!expanded;
      if(focused) { const input = page.querySelector('.flr-import input'); input.focus(); input.setSelectionRange(...selection); }
      const progress = page.querySelector('.flr-progress');
      if(screen.geodata.error && !screen.progress) progress.textContent = errorText(screen.geodata.error);
      // Validation preview must remain visible when rendered after a request.
      if(screen.importProfile || screen.importError) advanced.open = true;
      const input = page.querySelector('.flr-import input');
      input.setAttribute('aria-label','HAPP');
      input.addEventListener('input', () => {
        screen.importProfile=null; screen.importError='';
        page.querySelector('.flr-preview')?.remove();
      });
      page.querySelectorAll('.flr-rule').forEach(article => {
        article.querySelector('input').setAttribute('aria-label', article.querySelector('h3').textContent);
      });
    };
    function acceptJob(job) { latestJob=job || latestJob; }
    async function load() {
      const version = ++requestVersion;
      try {
        const data = await shared.api('routing/state');
        if(stopped || version !== requestVersion) return;
        needsRetry=false;
        acceptJob(data.job);
        const next = JSON.stringify([data.country_routing,data.firewall,data.services,data.geo,data.rules_error,!!data.job?.running]);
        screen.settings = {country_routing:data.country_routing,firewall:data.firewall};
        screen.services = data.services || [];
        screen.geodata = data.geo || {ready:false};
        screen.rulesError = data.rules_error ? errorText(data.rules_error) : '';
        screen.countryCode = data.country_routing?.country_code || ({ru:'RU',zh:'CN',fa:'IR'}[(document.documentElement.lang || navigator.language || '').toLowerCase().split('-')[0]] || '');
        if(!pending) { screen.busy=!!data.job?.running; screen.progress=data.geo?.updating ? _('Downloading and validating GeoIP and GeoSite…') : ''; }
        if(next!==fingerprint) { fingerprint=next; screen.renderAgain(); }
      } catch(error) {
        if(stopped || version!==requestVersion) return;
        needsRetry=true;
        screen.busy=true;
        screen.progress=error.message;
        screen.renderAgain();
      }
    }
    // Queue through api to retain the exact job sequence. operation() only
    // returns acceptance, so it cannot prove completion. No mutation is chained
    // in the browser: country preparation and group activation are server jobs.
    async function run(path, body, success) {
      if(pending || screen.busy) return false;
      pending=true; screen.busy=true;
      screen.progress=path==='geo/update' ? _('Downloading and validating GeoIP and GeoSite…') : _('Preparing routing…');
      screen.renderAgain();
      try {
        const job = await shared.api('routing/'+path,'POST',body);
        if(!job || !Number.isSafeInteger(job.sequence)) throw Error(errorText('operation_failed'));
        const end=Date.now()+16*60*1000;
        let observed=job;
        while(observed.running && Date.now()<end && !stopped) {
          await new Promise(resolve => setTimeout(resolve,750));
          if(stopped) return false;
          const state = await shared.api('state');
          acceptJob(state.job);
          observed=latestJob;
          if(!observed || observed.sequence!==job.sequence) throw Error(errorText('operation_failed'));
        }
        if(stopped) return false;
        if(observed.running) throw Error(errorText('geo_update_timeout'));
        if(!observed.succeeded) throw Error(errorText(observed.error || 'operation_failed'));
        if(success) notice(success);
        return true;
      } finally {
        pending=false; screen.busy=false; screen.progress='';
        await load();
        screen.renderAgain();
      }
    }
    async function action(path, body, success, failure) {
      try { return await run(path,body,success); }
      catch(error) { notice(failure+' '+error.message,true); return false; }
    }
    screen.handleToggle = event => {
      event?.preventDefault();
      const enabled=!(screen.settings.country_routing?.enabled && screen.settings.firewall?.enabled);
      return action('country',{country:screen.countryCode,enabled},enabled ? _('Local-country routing is on. LAN and the selected country go directly.') : _('Local-country routing is off.'),_('Could not change routing.'));
    };
    screen.handleCountryChange = event => {
      if(!event.target.value) return;
      return action('country',{country:event.target.value},_('Country saved.'),_('Could not save the country.'));
    };
    screen.handleGeoUpdate = () => action('geo/update',{},_('GeoIP and GeoSite are up to date.'),_('Could not update GeoIP and GeoSite.'));
    screen.handleRuleToggle = (name,event) => action('groups/toggle',{name,enabled:event.target.checked},event.target.checked ? _('Exclusion enabled.') : _('Exclusion disabled.'),_('Could not change the exclusion.'));
    screen.handleRuleDelete = (name,event) => {
      event?.preventDefault();
      if(screen.busy) return;
      return shared.confirmAction(_('Delete this exclusion? Traffic matching it will follow the regular VPN route.'), () => action('groups/delete',{name},_('Exclusion deleted.'),_('Could not delete the exclusion.')));
    };
    screen.handleRuleSubmit = async (rule,nameInput,domainsInput,cidrsInput,errorBox,submit,event) => {
      event?.preventDefault();
      const name=text(rule?.name || nameInput.value).toLowerCase();
      if(!/^[a-z][a-z0-9-]*$/.test(name)) { errorBox.textContent=_('Use lowercase Latin letters, digits, and hyphens; start with a letter.'); nameInput.focus(); return; }
      const domains=words(domainsInput.value), cidrs=words(cidrsInput.value);
      if(!domains.length && !cidrs.length && !rule?.services?.length) { errorBox.textContent=_('Add at least one domain or IP address.'); domainsInput.focus(); return; }
      submit.disabled=true;
      submit.textContent=rule ? _('Saving…') : _('Adding…');
      errorBox.textContent='';
      try {
        if(await run(rule ? 'groups/edit' : 'groups',{name,domains,cidrs},rule ? _('Exclusion saved.') : _('Exclusion added and enabled.'))) ui.hideModal();
      } catch(error) { errorBox.textContent=error.message; }
      finally { submit.disabled=false; submit.textContent=rule ? _('Save') : _('Add'); }
    };
    screen.handleImportCheck = async event => {
      event?.preventDefault();
      const link=screen.importValue || '';
      try {
        const profile=await shared.api('routing/happ/preview','POST',{link});
        if(stopped || screen.importValue!==link) return;
        screen.importProfile=profile; screen.importError=''; screen.renderAgain();
      } catch(error) { if(stopped || screen.importValue!==link) return; screen.importProfile=null; screen.importError=error.message; screen.renderAgain(); notice(_('Could not read the HAPP profile.'),true); }
    };
    screen.renderAgain();
    let refreshKey='';
    instance = {
      ready:load(),
      refresh: snapshot => {
        if(stopped || !snapshot) return;
        acceptJob(snapshot.job);
        const key=JSON.stringify([snapshot.status?.settings?.country_routing,snapshot.status?.settings?.firewall,snapshot.job]);
        if(key!==refreshKey || needsRetry) { refreshKey=key; void load(); }
      },
      destroy:() => { stopped=true; requestVersion++; ui.hideModal(); container.replaceChildren(); /* An accepted job stays on the server. */ }
    };
    return instance;
  }
  window.FastLaneRouting = {mount,refresh:snapshot=>instance?.refresh(snapshot),destroy:()=>{instance?.destroy();instance=null;}};
}

const output = '// Generated by scripts/sync-routing-panel.cjs from '+sourcePath+'.\n'+
  '// mount(container,{api,operation,notice,confirmAction,getSnapshot}) -> {ready,refresh,destroy}; root may call global refresh(snapshot).\n'+
  '('+installRoutingPanel.toString()+')('+JSON.stringify(translations,null,2)+', function(E, _, L, ui, countries) {\n'+helpers+'\nreturn {\n'+methods+'\n};\n});\n';
const files = new Map([
  ['internal/managementhttp/web/routing-panel.js',output],
  ['internal/managementhttp/web/routing-panel.css','/* Generated from '+sourcePath+'; shared fl-dialog tokens remain in legacy.css. */\n'+css+'\n/* Native dialog host and intrinsic-width fixes; source component styling above is unchanged. */\n.flr-page{min-width:0}.flr-page .flr-flow>*{min-width:0}.flr-page .flr-step-value,.flr-page .flr-rule-title{overflow-wrap:anywhere}.flr-page .flr-actions{flex-wrap:wrap}.flr-page .flr-import input{min-width:0}\n']
]);
for (const [name, content] of files) {
  const target=path.join(root,name);
  if(process.argv.includes('--check')) { if(!fs.existsSync(target)||fs.readFileSync(target,'utf8')!==content) throw Error('Run node scripts/sync-routing-panel.cjs: '+name); }
  else fs.writeFileSync(target,content);
}
