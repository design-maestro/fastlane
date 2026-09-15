'use strict';
const $ = id => document.getElementById(id);
let pendingImport = null, addMode = 'link';
let snapshot, awg, activeJob = false, sending = false, lastJob = null, routingDirty = false, settingsDirty = false, hideDirty = false, dnsDirty = false;
const jobNames = {'health-check':'Проверяем серверы','refresh':'Обновляем подписки','connect-auto':'Выбираем сервер','connect-manual':'Подключаем сервер','disconnect':'Отключаем VPN','add-subscription':'Добавляем серверы','remove-subscription':'Удаляем источник','settings':'Сохраняем настройки','routing':'Применяем маршруты','awg-import':'Импортируем AWG','awg-check':'Проверяем AWG','awg-connect':'Подключаем AWG','awg-disconnect':'Отключаем AWG','awg-remove':'Удаляем AWG'};
const messages = {auth_required:'Войдите с ключом доступа.',auth_rate_limited:'Слишком много попыток входа. Повторите через минуту.',job_already_running:'Дождитесь завершения текущей операции.',operation_failed:'Операция не выполнена. Проверьте данные и состояние подключения; подробности доступны в журнале службы.',invalid_request:'Проверьте заполненные поля.',feature_unavailable:'Эта функция недоступна в установленной службе.'};
Object.assign(messages,{unsafe_awg_directive:'В профиле есть команды запуска или остановки. Такие файлы не разрешены.',unsupported_awg_version:'Поддерживается только AWG 2.0. Экспортируйте профиль этой версии.',unsupported_awg_parameter:'В профиле есть неподдерживаемые параметры AWG.',awg_import_failed:'Не удалось импортировать AWG. Проверьте ключи, адрес и единственный Peer; перед заменой отключите активный профиль.'});
let noticeTimer;
function notice(message, error = false) { clearTimeout(noticeTimer); $('notice').textContent = message; $('notice').classList.toggle('bad', error); if(message)noticeTimer=setTimeout(()=>{$('notice').textContent='';},error?6000:3000); }
async function api(path, method = 'GET', body) {
  const response = await fetch('/api/v1/' + path, {method, credentials:'same-origin', cache:'no-store', headers:body === undefined ? {} : {'Content-Type':'application/json'}, body:body === undefined ? undefined : JSON.stringify(body)});
  const value = await response.json();
  if (!response.ok) { if (response.status === 401) showLogin(); throw new Error(messages[value.error] || 'Не удалось выполнить запрос (' + response.status + ').'); }
  return value;
}
function showLogin() { $('login').hidden = false; $('application').hidden = true; $('logout').hidden = true; snapshot = null; }
function setBusy() { document.querySelectorAll('[data-operation]').forEach(button => { button.disabled = activeJob || sending || button.dataset.unavailable === 'true'; }); }
async function operation(path, method = 'POST', body) {
  if (activeJob || sending) { notice(messages.job_already_running); return false; }
  sending = true; setBusy();
  try { const result = await api(path, method, body); if (result.running) { activeJob = true; $('job').textContent = jobNames[result.kind] || 'Выполняем операцию…'; } notice(''); await reload(); return true; }
  catch (error) { notice(error.message,true); return false; }
  finally { sending = false; setBusy(); }
}
function element(tag, text, className) { const e = document.createElement(tag); if (text != null) e.textContent = text; if (className) e.className = className; return e; }
function list(value) { return (value || '').split(/[\n,]+/).map(v=>v.trim()).filter(Boolean); }
function date(value) { return value && !value.startsWith('0001-') ? new Date(value).toLocaleString() : 'Нет данных'; }
function expired(sub) { return sub.expires_at && new Date(sub.expires_at).getTime() <= Date.now(); }
function hidden(node) { const settings = snapshot.status.settings, excluded = settings.auto_excluded_nodes || []; return excluded.includes(node.id) || excluded.includes(node.subscription_id+'/'+node.id) || (settings.auto_hide_keywords || []).some(word => (node.name+' '+(node.remark || '')).toLocaleLowerCase().includes(word.toLocaleLowerCase())); }
function active(node) { if(node.kind==='awg') return !!awg?.active; const state = snapshot.status.state; return state.connected && state.active_subscription_id === node.subscription_id && state.active_node_id === node.id; }
function latencyMS(health) {
  if (!health?.healthy) return null;
  const value=String(health.last_latency || ''), scales={h:3600000,m:60000,s:1000,ms:1,'µs':.001,us:.001,ns:.000001};
  let total=0, found=false;
  for (const match of value.matchAll(/([\d.]+)(ns|µs|us|ms|s|m|h)/g)) {total+=Number(match[1])*scales[match[2]];found=true;}
  return found?Math.round(total):null;
}
function pingText(health) { const value=latencyMS(health); return value===null?'Нет замера':value+' мс'; }
function pingClass(health) {const value=latencyMS(health);return value===null?'':value<=100?'fl-latency-good':value<=200?'fl-latency-mid':value<=1000?'fl-latency-slow':'fl-latency-critical';}
function awgNode() {
  return awg && awg.state!=='absent' ? {id:'awg-profile',subscription_id:'local-awg',kind:'awg',name:awg.name || 'AmneziaWG',remark:'Экспериментально · AWG 2.0',protocol:'amneziawg',address:awg.profile?.endpoint || ''} : null;
}
function subscriptions() {
  const result=[...(snapshot.subscriptions || [])],node=awgNode();
  if(node)result.push({id:'local-awg',display_name:'Файл AWG',node_count:1,nodes:[node],source_type:'file',last_updated_at:awg.last_probe?.checked_at});
  return result;
}
function nodeHealth(node) {
  if(node.kind==='awg')return awg?.last_probe?{healthy:awg.last_probe.success,last_latency:awg.last_probe.latency_ms+'ms',last_checked_at:awg.last_probe.checked_at}:null;
  return snapshot.status.state.health?.[node.id];
}
function connectNode(node) {
  if(hidden(node)){notice('Сервер скрыт правилом. Измените правила скрытия в настройках.',true);return;}
  if(node.kind==='awg')return operation('awg/connect');
  return operation('connect','POST',{mode:'manual',subscription_id:node.subscription_id,node_id:node.id});
}
function renderSources() {
  const sources=subscriptions(),select=$('source'),old=select.value;
  const options=sources.map(sub=>({id:sub.id,label:sub.display_name || sub.id,meta:sub.id==='local-awg'?'Файл · экспериментально':sub.last_error?'Ошибка обновления':expired(sub)?'Подписка истекла':'Обновлён: '+date(sub.last_updated_at),count:sub.node_count}));
  if(sources.length!==1)options.unshift({id:'',label:'Все серверы',meta:'Общий пул',count:sources.reduce((n,s)=>n+(expired(s)?0:s.node_count),0)});
  const signature=JSON.stringify(options);
  if(select.dataset.signature!==signature){
    select.replaceChildren(...options.map(option=>new Option(option.label,option.id)));
    select.value=sources.length===1?sources[0].id:options.some(o=>o.id===old)?old:'';
    select.dataset.signature=signature;
    const tabs=$('source-tabs'),focused=document.activeElement?.dataset.source;tabs.replaceChildren();
    options.forEach(option=>{const button=element('button',null,'fl-tab');button.dataset.source=option.id;button.type='button';const title=element('span',null,'fl-tab-top');title.append(element('span',option.label),element('span',option.count,'fl-count'));button.append(title,element('span',option.meta,'fl-tab-meta'));button.onclick=()=>{select.value=option.id;renderServers();};tabs.append(button);});
    if(focused!==undefined)Array.from(tabs.children).find(button=>button.dataset.source===focused)?.focus({preventScroll:true});
  }
  document.querySelectorAll('#source-tabs button').forEach(button=>{button.classList.toggle('fl-tab-active',button.dataset.source===select.value);button.setAttribute('aria-pressed',String(button.dataset.source===select.value));});
}
function renderServers() {
  if(!snapshot)return;
  renderSources();
  const selected=$('source').value,query=$('search').value.trim().toLocaleLowerCase(),protocol=$('protocol-filter').value;
  $('remove-source').hidden=!selected;
  const rows=subscriptions().filter(sub=>selected?sub.id===selected:!expired(sub)).flatMap(sub=>(sub.nodes || []).map(node=>({sub,node})));
  const visible=rows.filter(({node})=>($('show-hidden').checked || !hidden(node)) && (!protocol || node.protocol===protocol) && (node.name+' '+(node.remark || '')+' '+node.protocol).toLocaleLowerCase().includes(query));
  visible.sort((a,b)=>Number(active(b.node))-Number(active(a.node)) || ($('sort').value==='ping'?(latencyMS(nodeHealth(a.node))??Infinity)-(latencyMS(nodeHealth(b.node))??Infinity):0) || a.node.name.localeCompare(b.node.name));
  const root=$('server-list');
  if(!visible.length){if(!root.querySelector('.fl-empty'))root.replaceChildren(element('div','','fl-empty'));root.firstChild.textContent=rows.length?'Нет серверов по выбранным фильтрам.':'Добавьте подписку или файл, чтобы подключить VPN.';return;}
  let table=root.querySelector('table');
  if(!table){table=element('table','');const head=element('thead'),hr=element('tr');['Сервер','Источник','Протокол','Пинг (GET)','Статус',''].forEach(text=>hr.append(element('th',text)));head.append(hr);table.append(head,element('tbody'));root.replaceChildren(table);}
  table.className='fl-table '+(selected?'fl-table-single':'fl-table-all');table.querySelector('th:nth-child(2)').hidden=!!selected;
  const body=table.querySelector('tbody'),existing=new Map(Array.from(body.children,row=>[row.dataset.key,row]));
  visible.forEach(({sub,node},index)=>{
    const key=sub.id+'/'+node.id,health=nodeHealth(node),isActive=active(node),isHidden=hidden(node),unavailable=expired(sub)||isHidden||(node.kind==='awg'&&['incompatible','invalid'].includes(awg.state));
    let row=existing.get(key);
    if(!row){
      row=element('tr');row.dataset.key=key;
      const title=element('td'),server=element('div',null,'fl-server'),mark=element('div',null,'fl-server-mark'),text=element('div',null,'fl-server-text');text.append(element('div',null,'fl-server-name'),element('div',null,'fl-server-address'));server.append(mark,text);title.append(server);
      const source=element('td',null,'fl-meta-cell fl-meta-source'),protocolCell=element('td',null,'fl-meta-cell');protocolCell.append(element('span',null,'fl-protocol'));
      const ping=element('td',null,'fl-meta-cell');ping.append(element('span',null,'fl-latency'));
      const state=element('td',null,'fl-meta-cell fl-meta-status');state.append(element('span',null,'fl-node-status'));
      const actions=element('td',null,'fl-actions-cell'),more=element('div',null,'fl-more'),toggle=element('button',null,'fl-more-toggle'),menu=element('div',null,'fl-more-menu');
      toggle.type='button';toggle.setAttribute('aria-label','Действия сервера');toggle.setAttribute('aria-expanded','false');menu.hidden=true;
      toggle.onclick=event=>{event.stopPropagation();const opening=menu.hidden;document.querySelectorAll('.fl-more-menu').forEach(e=>e.hidden=true);document.querySelectorAll('.fl-more-toggle').forEach(e=>e.setAttribute('aria-expanded','false'));menu.hidden=!opening;toggle.setAttribute('aria-expanded',String(opening));};
      const connect=element('button','Подключить','fl-button');connect.dataset.operation='';connect.dataset.action='connect';connect.onclick=()=>{menu.hidden=true;connectNode(row.flNode);};menu.append(connect);
      if(node.kind==='awg'){
        const check=element('button','Проверить пинг (GET)','fl-button');check.dataset.operation='';check.dataset.action='check';check.onclick=()=>{menu.hidden=true;operation('awg/check');};
        const remove=element('button','Удалить профиль','fl-button fl-button-danger');remove.dataset.operation='';remove.onclick=()=>confirmAction('Удалить профиль AWG? Если он активен, интернет пойдёт напрямую.',()=>operation('awg','DELETE'));
        menu.append(check,remove,element('span','AWG 2.0 · экспериментально. Не участвует в автовыборе.','fl-more-note'),element('span','','fl-more-note awg-probe-detail'));
      }
      menu.onclick=event=>event.stopPropagation();more.append(toggle,menu);actions.append(more);row.append(title,source,protocolCell,ping,state,actions);
      row.onclick=event=>{if(!event.target.closest('button')&&!row.flUnavailable)connectNode(row.flNode);};
      row.onkeydown=event=>{if(event.target===row&&(event.key==='Enter'||event.key===' ')){event.preventDefault();if(!row.flUnavailable)connectNode(row.flNode);}};
    }
    existing.delete(key);row.flNode=node;row.flUnavailable=unavailable;row.tabIndex=unavailable?-1:0;row.className=(isActive?'fl-active-row ':'')+(isHidden?'fl-hidden-row ':'');row.setAttribute('aria-label',node.name+(node.kind==='awg'?' · AmneziaWG, экспериментально':''));
    const cells=row.children,mark=row.querySelector('.fl-server-mark'),flag=node.name.match(/[\u{1F1E6}-\u{1F1FF}]{2}/u)?.[0];
    mark.replaceChildren(flag?document.createTextNode(flag):window.fastlaneIcon(isActive?'bolt':'server'));mark.classList.toggle('fl-server-flag-glyph',!!flag);
    row.querySelector('.fl-server-name').textContent=node.name || node.address;row.querySelector('.fl-server-address').textContent=node.kind==='awg'?'Экспериментально · '+node.address:(node.remark!==node.name?node.remark:node.address);
    cells[1].textContent=sub.display_name || sub.id;cells[1].hidden=!!selected;cells[2].firstChild.textContent=node.kind==='awg'?'AmneziaWG':node.protocol.toUpperCase();
    cells[3].firstChild.textContent=pingText(health);cells[3].firstChild.className='fl-latency '+pingClass(health);
    const stateLabel=expired(sub)?'Истекла':isHidden?'Скрыт':isActive?'Активен':node.kind==='awg'&&awg.state==='incompatible'?'Несовместим':node.kind==='awg'&&awg.state==='invalid'?'Ошибка профиля':health?.healthy?(latencyMS(health)>1000?'Медленный':'Готов'):health?.last_checked_at?'Недоступен':'Не проверен';
    cells[4].firstChild.textContent=stateLabel;cells[4].firstChild.className='fl-node-status '+(isActive?'fl-node-status-active':stateLabel==='Недоступен'?'bad':'');
    row.querySelector('[data-action=connect]').dataset.unavailable=String(unavailable||isActive);
    if(node.kind==='awg'){row.querySelector('[data-action=check]').dataset.unavailable=String(unavailable);row.querySelector('.awg-probe-detail').textContent='Интерфейс: '+(awg.interface?.up?'поднят':'не поднят')+'. HTTPS: '+(awg.last_probe?(awg.last_probe.success?'прошёл':'не прошёл')+' · '+date(awg.last_probe.checked_at):'не проверен');}
    if(body.children[index]!==row){const focus=document.activeElement;body.insertBefore(row,body.children[index]||null);if(row.contains(focus))focus.focus({preventScroll:true});}
  });
  existing.forEach(row=>row.remove());setBusy();
}
function fill(form, values) { for (const [name,value] of Object.entries(values)) { if (form.elements[name]) form.elements[name].value=value ?? ''; } }
function render() {
  const status=snapshot.status, state=status.state, settings=status.settings, connected=state.connected && state.operational_mode!=='direct';
  const recovering=state.operational_mode==='recovering';
  $('connection-title').textContent=recovering?'Восстанавливаем VPN':connected?'VPN подключён':state.mode==='disconnected'?'VPN отключён — интернет напрямую':'VPN недоступен — интернет напрямую';
  $('connection-title').parentElement.className='fl-status-cell fl-status-main fl-status-main-'+(recovering?'recovering':connected?'vpn':'direct');
  $('connection-dot').className='fl-dot '+(connected?'fl-dot-on':recovering?'fl-dot-recovering':'');
  $('connection-detail').textContent=connected?(state.active_node_name || status.active_node?.name || (state.active_connection_kind==='amneziawg'?awg?.name:'Нет в подписке')):'—';
  $('connection-source').textContent=connected?(state.active_connection_kind==='amneziawg'?'Файл AWG':status.active_subscription?.display_name || '—'):'—';
  $('selection-mode').textContent=state.mode==='auto'?'Авто':state.mode==='manual'?'Вручную':'Отключено';
  $('auto').setAttribute('aria-pressed',String(state.mode==='auto'));$('manual').setAttribute('aria-pressed',String(state.mode==='manual'));
  $('disconnect').dataset.unavailable=String(state.mode==='disconnected');
  $('connection-ping').textContent=connected?pingText(state.active_connection_kind==='amneziawg'?nodeHealth({kind:'awg'}):state.health?.[state.active_node_id]):'—';
  $('connection-ping').className='fl-status-cell-value '+pingClass(state.active_connection_kind==='amneziawg'&&awg?nodeHealth({kind:'awg'}):state.health?.[state.active_node_id]);
  const job=snapshot.job; activeJob=job.running;
  $('job').textContent=job.running?(jobNames[job.kind] || 'Выполняем операцию…')+'. Можно закрыть вкладку.':'';
  if (lastJob===null) lastJob=job.running?job.sequence-1:job.sequence;
  if (job.sequence && !job.running && lastJob!==job.sequence) { if(pendingImport!==job.kind)notice(job.succeeded?'Операция завершена.':messages[job.error] || messages.operation_failed,!job.succeeded);else notice(''); lastJob=job.sequence; }
  if(pendingImport && !job.running && job.kind===pendingImport){
    if(job.succeeded){$('add-dialog').close();$('add-form').reset();$('add-file-name').textContent='';$('source').value='';$('search').value='';$('protocol-filter').value='';}
    else $('add-error').textContent=messages[job.error] || messages.operation_failed;
    pendingImport=null;
  }
  renderServers();
  const firewall=settings.firewall || {}, split=firewall.split || {}, selectors=v=>[...(v?.services || []),...(v?.domains || []),...(v?.cidrs || [])].join('\n');
  if (!routingDirty) {
    const supported = ['disabled','split'].includes(firewall.mode);
    const mode = !firewall.enabled || firewall.mode==='disabled' ? 'disabled' : supported ? (split.default_action==='proxy'?'bypass':'split') : '';
    fill($('routing-form'),{mode,proxy:selectors(split.proxy),bypass:selectors(split.bypass),excluded:(split.excluded_sources || []).join('\n')});
    $('routing-legacy').hidden=supported;
  }
  if (!settingsDirty) fill($('settings-form'),{'refresh-interval':settings.refresh_interval,'health-check-interval':settings.health_check_interval});
  if (!hideDirty) fill($('hide-form'),{'auto.hide-keywords':(settings.auto_hide_keywords || []).join('\n')});
  if (!dnsDirty) fill($('dns-form'),{'dns.mode':settings.dns?.mode,'dns.transport':settings.dns?.transport,'dns.servers':(settings.dns?.servers || []).join('\n')});
  const diagnostic=$('diagnostic-state'); diagnostic.replaceChildren();
  [['Режим', $('connection-title').textContent],['Сервер',state.active_node_name || status.active_node?.name || 'Не выбран'],['Последнее переключение',date(state.last_switch_at)],['Причина',state.last_switch_reason || 'Нет данных'],['Последняя ошибка',state.last_failure_reason || 'Нет'],['Текущая операция',job.running?(jobNames[job.kind] || job.kind):'Нет'],['Выбранный выход',state.selected_outbound_tag || 'Нет']].forEach(([key,value])=>diagnostic.append(element('dt',key),element('dd',value)));
  const reserves=$('reserves'); reserves.replaceChildren(); const entries=(state.runtime_outbounds || []).filter(out=>out.role==='reserve');
  if (!entries.length) reserves.append(element('p','Проверенных резервов пока нет.'));
  entries.forEach(out=>reserves.append(element('p',(out.node_id || out.tag)+' · Проверен: '+date(out.verified_at))));
  setBusy();
}
async function reload() {
  try {
    const current=await api('state');
    try{awg=await api('awg');}catch(error){notice('Не удалось обновить состояние AWG: '+error.message,true);awg=null;}
    snapshot=current;$('application').hidden=false;$('login').hidden=true;$('logout').hidden=false;render();
  } catch(error) {if(snapshot){notice('Нет связи с роутером. Показано последнее полученное состояние.',true);document.querySelectorAll('[data-operation]').forEach(b=>b.disabled=true);}else if($('login').hidden)notice(error.message,true);}
}
async function poll() { await reload(); window.setTimeout(poll,3000); }
function navigate() { const id=location.hash.slice(1); const page=['vpn','routing','diagnostics','settings'].includes(id)?id:'vpn'; document.querySelectorAll('.page').forEach(e=>e.hidden=e.id!==page); document.querySelectorAll('nav a').forEach(a=>{a.classList.toggle('fl-nav-link-active',a.hash==='#'+page);if(a.hash==='#'+page)a.setAttribute('aria-current','page');else a.removeAttribute('aria-current');}); }
function confirmAction(text, action) { const dialog=$('confirm'); $('confirm-text').textContent=text; dialog.returnValue='cancel'; dialog.addEventListener('close',()=>{if(dialog.returnValue==='confirm')action();},{once:true}); dialog.showModal(); }
$('login-form').addEventListener('submit',async event=>{event.preventDefault();const field=event.target.elements.token;try{await api('session','POST',{token:field.value});field.value='';notice('');await reload();}catch(error){notice(error.message,true);}});
$('logout').addEventListener('click',async()=>{try{await api('session','DELETE');showLogin();notice('Вы вышли из панели.');}catch(error){notice(error.message,true);}});
$('auto').addEventListener('click',()=>operation('connect','POST',{mode:'auto',subscription_id:'all'}));
$('disconnect').addEventListener('click',()=>confirmAction('Отключить VPN? Трафик будет идти напрямую.',()=>operation('disconnect')));
$('refresh').addEventListener('click',()=>operation('jobs/refresh'));
$('check').addEventListener('click',()=>operation('jobs/health-check'));
['search','source','show-hidden','protocol-filter','sort'].forEach(id=>$(id).addEventListener(id==='search'?'input':'change',renderServers));
$('manual').onclick=()=>{location.hash='vpn';$('server-list').querySelector('tr[tabindex="0"]')?.focus();notice('Выберите сервер в списке для ручного подключения.');};
function setAddMode(mode) {
  addMode=mode;
  for(const value of ['link','file']){$('add-'+value+'-pane').hidden=value!==mode;$('add-'+value+'-tab').classList.toggle('fl-add-mode-button-active',value===mode);$('add-'+value+'-tab').setAttribute('aria-pressed',String(value===mode));}
}
$('add-toggle').onclick=()=>{$('add-error').textContent='';$('add-dialog').showModal();};
$('add-cancel').onclick=()=>{$('add-dialog').close();};
$('add-link-tab').onclick=()=>setAddMode('link');$('add-file-tab').onclick=()=>setAddMode('file');
$('add-form').elements.file.onchange=event=>{$('add-file-name').textContent=event.target.files[0]?.name || '';};
$('add-form').addEventListener('submit',async event=>{
  event.preventDefault();const form=event.target,file=addMode==='file'?form.elements.file.files[0]:null;
  $('add-error').textContent='';
  try{
    if(addMode==='file'&&!file)throw Error('Выберите файл.');
    if(file?.size>5*1024*1024)throw Error('Файл должен быть меньше 5 МБ.');
    const raw=file?await file.text():form.elements.source.value.trim();if(!raw)throw Error('Укажите ссылку или конфигурацию.');
    const isAWG=!!file&&(/\.conf$/i.test(file.name)||(/^\s*\[Interface\]\s*$/im.test(raw)&&/^\s*\[Peer\]\s*$/im.test(raw)));
    if(isAWG&&file.size>1024*1024)throw Error('Профиль AWG должен быть меньше 1 МБ.');
    if(isAWG&&awg?.active)throw Error('Сначала отключите активный AWG, затем замените профиль.');
    const send=async()=>{
      pendingImport=isAWG?'awg-import':'add-subscription';
      const payload=isAWG?{name:form.elements.name.value.trim()||file.name.replace(/\.conf$/i,''),config:raw}:{name:form.elements.name.value,file_name:file?.name || ''};
      if(!isAWG){if(!file&&/^https?:\/\//i.test(raw))payload.url=raw;else payload.raw=raw;}
      if(!await operation(isAWG?'awg':'subscriptions','POST',payload)){pendingImport=null;$('add-error').textContent=$('notice').textContent || 'Не удалось отправить файл.';}
    };
    if(isAWG&&awgNode())confirmAction('Уже есть профиль AWG. Заменить его новым файлом?',send);else await send();
  }catch(error){$('add-error').textContent=error.message;}
});
$('remove-source').onclick=()=>{const id=$('source').value;if(id)confirmAction(id==='local-awg'?'Удалить профиль AWG? Активный туннель будет отключён.':'Удалить этот источник и все его серверы?',()=>operation(id==='local-awg'?'awg':'subscriptions/'+encodeURIComponent(id),'DELETE'));};
document.addEventListener('click',event=>{if(!event.target.closest('.fl-more')){document.querySelectorAll('.fl-more-menu').forEach(e=>e.hidden=true);document.querySelectorAll('.fl-more-toggle').forEach(e=>e.setAttribute('aria-expanded','false'));}});
document.addEventListener('keydown',event=>{if(event.key==='Escape'){document.querySelectorAll('.fl-more-menu').forEach(e=>e.hidden=true);document.querySelectorAll('.fl-more-toggle').forEach(e=>e.setAttribute('aria-expanded','false'));}});
$('routing-form').addEventListener('input',()=>routingDirty=true);
$('routing-form').addEventListener('submit',event=>{event.preventDefault();const form=event.target,input={mode:form.elements.mode.value,proxy:list(form.elements.proxy.value),bypass:list(form.elements.bypass.value),excluded:list(form.elements.excluded.value)};confirmAction('Применить новые маршруты к трафику домашней сети?',async()=>{if(await operation('routing','POST',input))routingDirty=false;});});
$('settings-form').addEventListener('input',()=>settingsDirty=true);
$('settings-form').addEventListener('submit',async event=>{event.preventDefault();if(await operation('jobs/settings','POST',Object.fromEntries(new FormData(event.target))))settingsDirty=false;});
$('hide-form').addEventListener('input',()=>hideDirty=true);
$('hide-form').addEventListener('submit',event=>{event.preventDefault();const keywords=list(event.target.elements['auto.hide-keywords'].value);confirmAction('Применить скрытие? Если активный сервер совпадёт, Fast Lane выберет замену.',async()=>{if(await operation('hide-keywords','POST',{keywords}))hideDirty=false;});});
$('dns-form').addEventListener('input',()=>dnsDirty=true);
$('dns-form').addEventListener('submit',event=>{event.preventDefault();const form=event.target,input={mode:form.elements['dns.mode'].value,transport:form.elements['dns.transport'].value,servers:list(form.elements['dns.servers'].value)};confirmAction('Сохранить DNS? Это может потребовать перезапуска сетевого движка.',async()=>{if(await operation('dns','POST',input))dnsDirty=false;});});
for(const [id,icon] of [['add-toggle','plus'],['refresh','refresh'],['check','bolt'],['remove-source','trash']])$(id).prepend(window.fastlaneIcon(icon));
window.addEventListener('hashchange',navigate);navigate();poll();
