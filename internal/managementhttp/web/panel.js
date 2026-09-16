'use strict';
const tr=window.fastlaneText;
const $ = id => document.getElementById(id);
let pendingImport = null, addMode = 'link';
let snapshot, awgs=[], probeResults={}, activeJob = false, sending = false, lastJob = null;
const pageControllers={};
const jobNames = {'health-check':tr('Проверяем серверы'),'refresh':tr('Обновляем подписки'),'connect-auto':tr('Выбираем сервер'),'connect-manual':tr('Подключаем сервер'),'disconnect':tr('Отключаем VPN'),'add-subscription':tr('Добавляем серверы'),'remove-subscription':tr('Удаляем источник'),'settings':tr('Сохраняем настройки'),'routing':tr('Применяем маршруты'),'awg-import':tr('Импортируем AWG'),'awg-check':tr('Проверяем AWG'),'awg-connect':tr('Подключаем AWG'),'awg-disconnect':tr('Отключаем AWG'),'awg-remove':tr('Удаляем AWG')};
const messages = {auth_required:tr('Войдите с ключом доступа.'),auth_rate_limited:tr('Слишком много попыток входа. Повторите через минуту.'),job_already_running:tr('Дождитесь завершения текущей операции.'),operation_failed:tr('Операция не выполнена. Проверьте данные и состояние подключения; подробности доступны в журнале службы.'),invalid_request:tr('Проверьте заполненные поля.'),feature_unavailable:tr('Эта функция недоступна в установленной службе.')};
messages.vpn_runtime_unavailable=tr('В локальном превью нет VPN-движка. Подключение проверяется на отдельном OpenWrt-стенде; состояние VPN не изменено.');
messages.operation_cancelled=tr('Проверка остановлена.');
Object.assign(messages,{unsafe_awg_directive:tr('В профиле есть команды запуска или остановки. Такие файлы не разрешены.'),unsupported_awg_version:tr('Поддерживаются профили AWG Legacy и 2.0.'),unsupported_awg_parameter:tr('В профиле есть неподдерживаемые параметры AWG.'),awg_import_failed:tr('Не удалось импортировать AWG. Проверьте ключи, адрес и единственный Peer; перед заменой отключите активный профиль.')});
let noticeTimer;
function notice(message, error = false) { clearTimeout(noticeTimer); $('notice').textContent = message; $('notice').classList.toggle('bad', error); if(message)noticeTimer=setTimeout(()=>{$('notice').textContent='';},error?6000:3000); }
async function api(path, method = 'GET', body) {
  const response = await fetch('/api/v1/' + path, {method, credentials:'same-origin', cache:'no-store', headers:body === undefined ? {} : {'Content-Type':'application/json'}, body:body === undefined ? undefined : JSON.stringify(body)});
  const value = await response.json();
  if (!response.ok) { if (response.status === 401) showLogin(); throw new Error(messages[value.error] || tr('Не удалось выполнить запрос (') + response.status + ').'); }
  return value;
}
function showLogin() { $('login').hidden = false; $('application').hidden = true; $('logout').hidden = true; snapshot = null; }
function setBusy() { document.querySelectorAll('[data-operation]').forEach(button => { button.disabled = activeJob || sending || button.dataset.unavailable === 'true'; }); }
async function operation(path, method = 'POST', body) {
  if (activeJob || sending) { notice(messages.job_already_running); return false; }
  sending = true; setBusy();
  try { const result = await api(path, method, body); if (result.running) { activeJob = true; $('job').textContent = jobNames[result.kind] || tr('Выполняем операцию…'); } notice(''); await reload(); return true; }
  catch (error) { notice(error.message,true); return false; }
  finally { sending = false; setBusy(); }
}
function element(tag, text, className) { const e = document.createElement(tag); if (text != null) e.textContent = text; if (className) e.className = className; return e; }
function list(value) { return (value || '').split(/[\n,]+/).map(v=>v.trim()).filter(Boolean); }
function date(value) { return value && !value.startsWith('0001-') ? new Date(value).toLocaleString() : tr('Нет данных'); }
function expired(sub) { return sub.expires_at && new Date(sub.expires_at).getTime() <= Date.now(); }
function hidden(node) { const settings = snapshot.status.settings, excluded = settings.auto_excluded_nodes || []; return excluded.includes(node.id) || excluded.includes(node.subscription_id+'/'+node.id) || (settings.auto_hide_keywords || []).some(word => (node.name+' '+(node.remark || '')).toLocaleLowerCase().includes(word.toLocaleLowerCase())); }
function keywordHidden(node) {return (snapshot.status.settings.auto_hide_keywords || []).some(word=>(node.name+' '+(node.remark||'')).toLocaleLowerCase().includes(word.toLocaleLowerCase()));}
const countryCache=new Map();
function countryCode(node) {
  const key=node.name+' '+(node.remark||'');if(countryCache.has(key))return countryCache.get(key);
  const code=detectCountryCode(node);countryCache.set(key,code);return code;
}
function detectCountryCode(node) {
  const text=node.name+' '+(node.remark||''),flag=text.match(/[\u{1F1E6}-\u{1F1FF}]{2}/u)?.[0];
  if(flag)return [...flag].map(c=>String.fromCharCode(c.codePointAt(0)-0x1f1e6+65)).join('');
  const normalized=' '+text.toLocaleLowerCase().replace(/[.,()|—·]/g,' ')+' ';
  for(const code of window.FastLaneCountries?.codes||[])for(const lang of ['ru','en']){
    const name=new Intl.DisplayNames([lang],{type:'region'}).of(code).toLocaleLowerCase();
    if(normalized.includes(' '+name+' ')||normalized.includes(' '+code.toLowerCase()+' '))return code;
  }
  return '';
}
function awgByID(id){return awgs.find(profile=>profile.id===id);}
function activeAWG(){const state=snapshot?.status?.state||{};return awgByID(state.active_awg_profile_id||state.active_node_id)||awgs.find(profile=>profile.active);}
function active(node) { if(node.kind==='awg') return !!awgByID(node.id)?.active; const state = snapshot.status.state; return state.connected && state.active_subscription_id === node.subscription_id && state.active_node_id === node.id; }
function latencyMS(health) {
  if (!health?.healthy) return null;
  const value=String(health.last_latency || ''), scales={h:3600000,m:60000,s:1000,ms:1,'µs':.001,us:.001,ns:.000001};
  let total=0, found=false;
  for (const match of value.matchAll(/([\d.]+)(ns|µs|us|ms|s|m|h)/g)) {total+=Number(match[1])*scales[match[2]];found=true;}
  return found?Math.round(total):null;
}
function pingText(health) { const value=latencyMS(health); return value===null?tr('Нет замера'):value+tr(' мс'); }
function pingClass(health) {const value=latencyMS(health);return value===null?'':value<=100?'fl-latency-good':value<=200?'fl-latency-mid':value<=1000?'fl-latency-slow':'fl-latency-critical';}
function awgNodes() {
  return awgs.filter(profile=>profile&&profile.state!=='absent').map(profile=>{
    const version=profile.profile?.version==='legacy'?'Legacy':'2.0';
    return {id:profile.id,subscription_id:'server-list',kind:'awg',name:profile.name || 'AmneziaWG',remark:tr('Экспериментально')+' · AWG '+version,protocol:'amneziawg',address:profile.profile?.endpoint || ''};
  });
}
function subscriptions() {
	const result=(snapshot.subscriptions || []).map(sub=>({...sub,nodes:[...(sub.nodes||[])]})),nodes=awgNodes();
	let serverList=result.find(sub=>sub.id==='server-list');
	if(nodes.length&&!serverList){serverList={id:'server-list',display_name:'Server List',node_count:0,nodes:[],source_type:'raw'};result.push(serverList);}
	if(serverList){serverList.nodes.push(...nodes);serverList.node_count=serverList.nodes.length;}
	return result;
}
function nodeHealth(node) {
	if(node.kind==='awg'){const profile=awgByID(node.id);return profile?.last_probe?{healthy:profile.last_probe.success,last_latency:profile.last_probe.latency_ms+'ms',last_checked_at:profile.last_probe.checked_at}:null;}
  const measured=probeResults[node.subscription_id+'/'+node.id],stored=snapshot.status.state.health?.[node.id];
  return measured&&(!stored||new Date(measured.checked_at)>new Date(stored.last_checked_at))?{healthy:measured.success,last_latency:measured.success?measured.latency_ms+'ms':'',last_checked_at:measured.checked_at}:stored;
}
function connectNode(node) {
  if(hidden(node)){notice(tr('Сервер скрыт правилом. Измените правила скрытия в настройках.'),true);return;}
	if(node.kind==='awg')return operation('awg/profiles/'+encodeURIComponent(node.id)+'/connect');
  return operation('connect','POST',{mode:'manual',subscription_id:node.subscription_id,node_id:node.id});
}
function renderSources() {
  const sources=subscriptions(),select=$('source'),old=select.value;
  const options=sources.map(sub=>({id:sub.id,label:sub.display_name || sub.id,meta:sub.last_error?tr('Ошибка обновления'):expired(sub)?tr('Подписка истекла'):tr('Обновлён: ')+date(sub.last_updated_at),count:sub.node_count}));
  if(sources.length!==1)options.unshift({id:'',label:tr('Все серверы'),meta:tr('Общий пул'),count:sources.reduce((n,s)=>n+(expired(s)?0:s.node_count),0)});
  const hiddenCount=sources.flatMap(s=>s.nodes||[]).filter(hidden).length;
  if(hiddenCount)options.push({id:'hidden',label:tr('Скрытые'),meta:tr('Исключены из автовыбора'),count:hiddenCount});
  const signature=JSON.stringify(options);
  if(select.dataset.signature!==signature){
    select.replaceChildren(...options.map(option=>new Option(option.label,option.id)));
    select.value=options.some(o=>o.id===old)?old:sources.length===1?sources[0].id:'';
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
  const hiddenOnly=$('source').value==='hidden',selected=hiddenOnly?'':$('source').value,query=$('search').value.trim().toLocaleLowerCase(),protocol=$('protocol-filter').value;
	const selectedSource=subscriptions().find(sub=>sub.id===selected);
	$('remove-source').hidden=!selected||!(selectedSource?.nodes||[]).some(node=>node.kind!=='awg');
  const rows=subscriptions().filter(sub=>selected?sub.id===selected:!expired(sub)).flatMap(sub=>(sub.nodes || []).map(node=>({sub,node})));
  const country=$('country-filter').value, countryOptions=[...new Set(rows.map(r=>countryCode(r.node)).filter(Boolean))].sort();
  const signature=countryOptions.join();if($('country-filter').dataset.signature!==signature){$('country-filter').replaceChildren(new Option(tr('Все страны'),''),...countryOptions.map(code=>new Option(window.FastLaneCountries.name(code),code)));$('country-filter').value=countryOptions.includes(country)?country:'';$('country-filter').dataset.signature=signature;}
  const protocols=[...new Set(rows.map(r=>r.node.protocol))].sort();if($('protocol-filter').dataset.signature!==protocols.join()){$('protocol-filter').replaceChildren(new Option(tr('Все протоколы'),''),...protocols.map(p=>new Option(p==='amneziawg'?'AmneziaWG':p.toUpperCase(),p)));$('protocol-filter').value=protocols.includes(protocol)?protocol:'';$('protocol-filter').dataset.signature=protocols.join();}
  const visible=rows.filter(({node})=>(hiddenOnly?hidden(node):!hidden(node)) && (!$('protocol-filter').value||node.protocol===$('protocol-filter').value) && (!$('country-filter').value||countryCode(node)===$('country-filter').value) && (node.name+' '+(node.remark || '')+' '+node.protocol).toLocaleLowerCase().includes(query));
  visible.sort((a,b)=>Number(active(b.node))-Number(active(a.node)) || ($('sort').value==='ping'?(latencyMS(nodeHealth(a.node))??Infinity)-(latencyMS(nodeHealth(b.node))??Infinity):$('sort').value==='source'?(a.sub.display_name||'').localeCompare(b.sub.display_name||''):0) || a.node.name.localeCompare(b.node.name));
  const root=$('server-list');
  if(!visible.length){if(!root.querySelector('.fl-empty'))root.replaceChildren(element('div','','fl-empty'));root.firstChild.textContent=rows.length?tr('Нет серверов по выбранным фильтрам.'):tr('Добавьте подписку или файл, чтобы подключить VPN.');return;}
  let table=root.querySelector('table');
  if(!table){table=element('table','');const head=element('thead'),hr=element('tr');[tr('Сервер'),tr('Источник'),tr('Протокол'),tr('Пинг (GET)'),tr('Статус'),''].forEach(text=>hr.append(element('th',text)));head.append(hr);table.append(head,element('tbody'));root.replaceChildren(table);}
  table.className='fl-table '+(selected?'fl-table-single':'fl-table-all');table.querySelector('th:nth-child(2)').hidden=!!selected;
  const body=table.querySelector('tbody'),existing=new Map(Array.from(body.children,row=>[row.dataset.key,row]));
  visible.forEach(({sub,node},index)=>{
    const profile=node.kind==='awg'?awgByID(node.id):null,key=sub.id+'/'+node.id,health=nodeHealth(node),isActive=active(node),isHidden=hidden(node),unavailable=expired(sub)||isHidden||(node.kind==='awg'&&['incompatible','invalid'].includes(profile?.state));
    let row=existing.get(key);
    if(!row){
      row=element('tr');row.dataset.key=key;
      const title=element('td'),server=element('div',null,'fl-server'),mark=element('div',null,'fl-server-mark'),text=element('div',null,'fl-server-text');text.append(element('div',null,'fl-server-name'),element('div',null,'fl-server-address'));server.append(mark,text);title.append(server);
      const source=element('td',null,'fl-meta-cell fl-meta-source'),protocolCell=element('td',null,'fl-meta-cell');protocolCell.append(element('span',null,'fl-protocol'));
      const ping=element('td',null,'fl-meta-cell');ping.append(element('span',null,'fl-latency'));
      const state=element('td',null,'fl-meta-cell fl-meta-status');state.append(element('span',null,'fl-node-status'));
      source.dataset.label=tr('Источник');protocolCell.dataset.label=tr('Протокол');ping.dataset.label=tr('Пинг (GET)');state.dataset.label=tr('Статус');
      const actions=element('td',null,'fl-actions-cell'),more=element('div',null,'fl-more'),toggle=element('button',null,'fl-more-toggle'),menu=element('div',null,'fl-more-menu');
      toggle.type='button';toggle.setAttribute('aria-label',tr('Действия сервера'));toggle.setAttribute('aria-expanded','false');menu.hidden=true;
      toggle.onclick=event=>{event.stopPropagation();const opening=menu.hidden;document.querySelectorAll('.fl-more-menu').forEach(e=>e.hidden=true);document.querySelectorAll('.fl-more-toggle').forEach(e=>e.setAttribute('aria-expanded','false'));menu.hidden=!opening;toggle.setAttribute('aria-expanded',String(opening));};
      const connect=element('button',tr('Подключить'),'fl-button');connect.dataset.operation='';connect.dataset.action='connect';connect.onclick=()=>{menu.hidden=true;connectNode(row.flNode);};menu.append(connect);
      if(node.kind==='awg'){
        const check=element('button',tr('Проверить пинг (GET)'),'fl-button');check.dataset.operation='';check.dataset.action='check';check.onclick=()=>{menu.hidden=true;operation('awg/profiles/'+encodeURIComponent(row.flNode.id)+'/check');};
        const hide=element('button',tr('Скрыть'),'fl-button fl-button-warning');hide.dataset.operation='';hide.dataset.action='hide';hide.onclick=()=>{menu.hidden=true;if(keywordHidden(row.flNode)){location.hash='settings';return;}operation('vpn/hidden','POST',{subscription_id:'server-list',node_id:row.flNode.id,hidden:!hidden(row.flNode)});};
        const remove=element('button',tr('Удалить профиль'),'fl-button fl-button-danger');remove.dataset.operation='';remove.onclick=()=>confirmAction(tr('Удалить профиль AWG? Если он активен, интернет пойдёт напрямую.'),()=>operation('awg/profiles/'+encodeURIComponent(row.flNode.id),'DELETE'));
        const version=profile?.profile?.version==='legacy'?'Legacy':'2.0';
        menu.append(check,hide,remove,element('span','AWG '+version+' · '+tr('Экспериментально. Участвует в общей GET-проверке и автовыборе.'),'fl-more-note'),element('span','','fl-more-note awg-probe-detail'));
      }else{
        const check=element('button',tr('Проверить пинг (GET)'),'fl-button');check.dataset.operation='';check.dataset.action='check';check.onclick=()=>{menu.hidden=true;operation('vpn/check','POST',{subscription_id:row.flNode.subscription_id,node_id:row.flNode.id});};
        const hide=element('button',tr('Скрыть'),'fl-button fl-button-warning');hide.dataset.operation='';hide.dataset.action='hide';hide.onclick=()=>{menu.hidden=true;if(keywordHidden(row.flNode)){location.hash='settings';return;}operation('vpn/hidden','POST',{subscription_id:row.flNode.subscription_id,node_id:row.flNode.id,hidden:!hidden(row.flNode)});};
        menu.append(check,hide);
        if(sub.id==='server-list'){const remove=element('button',tr('Удалить сервер'),'fl-button fl-button-danger');remove.dataset.operation='';remove.onclick=()=>confirmAction(tr('Удалить сервер? Если он активен, VPN отключится.'),()=>operation('subscriptions/'+encodeURIComponent(row.flNode.subscription_id)+'/nodes/'+encodeURIComponent(row.flNode.id),'DELETE'));menu.append(remove);}
      }
      menu.onclick=event=>event.stopPropagation();more.append(toggle,menu);actions.append(more);row.append(title,source,protocolCell,ping,state,actions);
      row.onclick=event=>{if(!event.target.closest('button')&&!row.flUnavailable)connectNode(row.flNode);};
      row.onkeydown=event=>{if(event.target===row&&(event.key==='Enter'||event.key===' ')){event.preventDefault();if(!row.flUnavailable)connectNode(row.flNode);}};
    }
    existing.delete(key);row.flNode=node;row.flUnavailable=unavailable;row.tabIndex=unavailable?-1:0;row.className=(isActive?'fl-active-row ':'')+(isHidden?'fl-hidden-row ':'');row.setAttribute('aria-label',node.name+(node.kind==='awg'?tr(' · AmneziaWG, экспериментально'):''));
    const cells=row.children,mark=row.querySelector('.fl-server-mark'),flag=node.name.match(/[\u{1F1E6}-\u{1F1FF}]{2}/u)?.[0];
    mark.replaceChildren(flag?document.createTextNode(flag):window.fastlaneIcon(isActive?'bolt':'server'));mark.classList.toggle('fl-server-flag-glyph',!!flag);
    row.querySelector('.fl-server-name').textContent=node.name || node.address;row.querySelector('.fl-server-address').textContent=node.kind==='awg'?tr('Экспериментально · ')+node.address:(node.remark!==node.name?node.remark:node.address);
    cells[1].textContent=sub.display_name || sub.id;cells[1].hidden=!!selected;cells[2].firstChild.textContent=node.kind==='awg'?'AmneziaWG':node.protocol.toUpperCase();
    cells[3].firstChild.textContent=pingText(health);cells[3].firstChild.className='fl-latency '+pingClass(health);
    const stateLabel=expired(sub)?tr('Истекла'):isHidden?tr('Скрыт'):isActive?tr('Активен'):node.kind==='awg'&&profile?.state==='incompatible'?tr('Несовместим'):node.kind==='awg'&&profile?.state==='invalid'?tr('Ошибка профиля'):health?.healthy?(latencyMS(health)>1000?tr('Медленный'):tr('Готов')):health?.last_checked_at?tr('Недоступен'):tr('Не проверен');
    cells[4].firstChild.textContent=stateLabel;cells[4].firstChild.className='fl-node-status '+(isActive?'fl-node-status-active':stateLabel===tr('Недоступен')?'bad':'');
    row.querySelector('[data-action=connect]').dataset.unavailable=String(unavailable||(isActive&&snapshot.status.state.mode==='manual'));
    row.querySelector('[data-action=connect]').textContent=isActive&&snapshot.status.state.mode==='manual'?tr('Закреплён'):tr('Подключить');
    if(node.kind!=='awg'){row.querySelector('[data-action=check]').dataset.unavailable=String(unavailable);const hide=row.querySelector('[data-action=hide]');hide.textContent=keywordHidden(node)?tr('Изменить правила скрытия'):isHidden?tr('Восстановить'):tr('Скрыть');hide.dataset.unavailable=String(false);}
    if(node.kind==='awg'){row.querySelector('[data-action=check]').dataset.unavailable=String(unavailable);const hide=row.querySelector('[data-action=hide]');hide.textContent=keywordHidden(node)?tr('Изменить правила скрытия'):isHidden?tr('Восстановить'):tr('Скрыть');row.querySelector('.awg-probe-detail').textContent=tr('Интерфейс: ')+(profile?.interface?.up?tr('поднят'):tr('не поднят'))+'. HTTPS: '+(profile?.last_probe?(profile.last_probe.success?tr('прошёл'):tr('не прошёл'))+' · '+date(profile.last_probe.checked_at):tr('не проверен'));}
    if(body.children[index]!==row){const focus=document.activeElement;body.insertBefore(row,body.children[index]||null);if(row.contains(focus))focus.focus({preventScroll:true});}
  });
  existing.forEach(row=>row.remove());setBusy();
}
function fill(form, values) { for (const [name,value] of Object.entries(values)) { if (form.elements[name]) form.elements[name].value=value ?? ''; } }
function render() {
  const status=snapshot.status, state=status.state, settings=status.settings, connected=state.connected && state.operational_mode!=='direct';
	const awg=activeAWG();
  const recovering=state.operational_mode==='recovering';
  $('connection-title').textContent=recovering?tr('Восстанавливаем VPN'):connected?tr('VPN подключён'):state.mode==='disconnected'?tr('VPN отключён — интернет напрямую'):tr('VPN недоступен — интернет напрямую');
  $('connection-title').parentElement.className='fl-status-cell fl-status-main fl-status-main-'+(recovering?'recovering':connected?'vpn':'direct');
  $('connection-dot').className='fl-dot '+(connected?'fl-dot-on':recovering?'fl-dot-recovering':'');
  $('connection-detail').textContent=connected?(state.active_node_name || status.active_node?.name || (state.active_connection_kind==='amneziawg'?awg?.name:tr('Нет в подписке'))):'—';
	$('connection-source').textContent=connected?(state.active_connection_kind==='amneziawg'?'Server List':status.active_subscription?.display_name || '—'):'—';
  $('selection-mode').textContent=state.mode==='auto'?tr('Авто'):state.mode==='manual'?tr('Вручную'):tr('Отключено');
  $('auto').setAttribute('aria-pressed',String(state.mode==='auto'));$('manual').setAttribute('aria-pressed',String(state.mode==='manual'));
  $('disconnect').dataset.unavailable=String(state.mode==='disconnected');
	$('connection-ping').textContent=connected?pingText(state.active_connection_kind==='amneziawg'?nodeHealth({kind:'awg',id:awg?.id}):state.health?.[state.active_node_id]):'—';
	$('connection-ping').className='fl-status-cell-value '+pingClass(state.active_connection_kind==='amneziawg'&&awg?nodeHealth({kind:'awg',id:awg.id}):state.health?.[state.active_node_id]);
  const job=snapshot.job; activeJob=job.running;
  $('cancel-check').hidden=!job.running||!['health-check','connect-auto'].includes(job.kind);
  $('job').textContent=job.running?(jobNames[job.kind] || tr('Выполняем операцию…'))+tr('. Можно закрыть вкладку.'):'';
  if (lastJob===null) lastJob=job.running?job.sequence-1:job.sequence;
  if (job.sequence && !job.running && lastJob!==job.sequence) { if(pendingImport!==job.kind)notice(job.succeeded?tr('Операция завершена.'):messages[job.error] || messages.operation_failed,!job.succeeded&&!job.cancelled);else notice(''); lastJob=job.sequence; }
  if(pendingImport && !job.running && job.kind===pendingImport){
    if(job.succeeded){$('add-dialog').close();$('add-form').reset();$('add-file-name').textContent='';$('source').value='';$('search').value='';$('protocol-filter').value='';}
    else $('add-error').textContent=messages[job.error] || messages.operation_failed;
    pendingImport=null;
  }
  renderServers();
  const shared={api,operation,notice,confirmAction,getSnapshot:()=>snapshot};
  for(const [id,module] of [['routing',window.FastLaneRouting],['settings',window.FastLaneSettings],['diagnostics',window.FastLaneDiagnostics]]){
    if(!module)continue;
    if(!pageControllers[id]){pageControllers[id]=module.mount($(id),shared);Promise.resolve(pageControllers[id]?.ready).catch(error=>notice(error.message,true));}
    Promise.resolve(pageControllers[id]?.refresh?.(snapshot)).catch(error=>notice(error.message,true));
  }
  setBusy();
}
async function reload() {
  try {
    const current=await api('state');
	try{awgs=await api('awg/profiles');}catch(error){notice(tr('Не удалось обновить состояние AWG: ')+error.message,true);awgs=[];}
    try{probeResults=await api('vpn/probes')||{};}catch{probeResults={};}
    snapshot=current;$('application').hidden=false;$('login').hidden=true;$('logout').hidden=false;render();
  } catch(error) {if(snapshot){notice(tr('Нет связи с роутером. Показано последнее полученное состояние.'),true);document.querySelectorAll('[data-operation]').forEach(b=>b.disabled=true);}else if($('login').hidden)notice(error.message,true);}
}
async function poll() { await reload(); window.setTimeout(poll,3000); }
function navigate() { const id=location.hash.slice(1); const page=['vpn','routing','diagnostics','settings'].includes(id)?id:'vpn'; $('vpn-status').hidden=page!=='vpn';document.querySelectorAll('.page').forEach(e=>e.hidden=e.id!==page); document.querySelectorAll('nav a').forEach(a=>{a.classList.toggle('fl-nav-link-active',a.hash==='#'+page);if(a.hash==='#'+page)a.setAttribute('aria-current','page');else a.removeAttribute('aria-current');}); requestAnimationFrame(()=>window.scrollTo(0,0)); }
function confirmAction(text, action) { const dialog=$('confirm'); $('confirm-text').textContent=text; dialog.returnValue='cancel'; dialog.addEventListener('close',()=>{if(dialog.returnValue==='confirm')action();},{once:true}); dialog.showModal(); }
$('login-form').addEventListener('submit',async event=>{event.preventDefault();const field=event.target.elements.token;try{await api('session','POST',{token:field.value});field.value='';notice('');await reload();}catch(error){notice(error.message,true);}});
$('logout').addEventListener('click',async()=>{try{await api('session','DELETE');showLogin();notice(tr('Вы вышли из панели.'));}catch(error){notice(error.message,true);}});
$('auto').addEventListener('click',()=>operation('connect','POST',{mode:'auto',subscription_id:'all'}));
$('disconnect').addEventListener('click',()=>confirmAction(tr('Отключить VPN? Трафик будет идти напрямую.'),()=>operation('disconnect')));
$('refresh').addEventListener('click',()=>{const source=$('source').value;return source&&source!=='hidden'?operation('vpn/refresh','POST',{subscription_id:source}):operation('jobs/refresh');});
$('check').addEventListener('click',()=>{const source=$('source').value;return operation('jobs/health-check','POST',{subscription_id:source==='hidden'?'all':source||'all'});});
$('cancel-check').onclick=async()=>{try{await api('jobs/cancel-check','POST',{sequence:snapshot.job.sequence});await reload();}catch(error){notice(error.message,true);}};
['search','source','country-filter','protocol-filter','sort'].forEach(id=>$(id).addEventListener(id==='search'?'input':'change',renderServers));
$('manual').onclick=()=>{location.hash='vpn';$('server-list').querySelector('tr[tabindex="0"]')?.focus();notice(tr('Выберите сервер в списке для ручного подключения.'));};
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
    if(addMode==='file'&&!file)throw Error(tr('Выберите файл.'));
    if(file?.size>5*1024*1024)throw Error(tr('Файл должен быть меньше 5 МБ.'));
    const raw=file?await file.text():form.elements.source.value.trim();if(!raw)throw Error(tr('Укажите ссылку или конфигурацию.'));
    const isAWG=!!file&&(/\.conf$/i.test(file.name)||(/^\s*\[Interface\]\s*$/im.test(raw)&&/^\s*\[Peer\]\s*$/im.test(raw)));
    if(isAWG&&file.size>1024*1024)throw Error(tr('Профиль AWG должен быть меньше 1 МБ.'));
    const send=async()=>{
      pendingImport=isAWG?'awg-import':'add-subscription';
      const payload=isAWG?{name:form.elements.name.value.trim()||file.name.replace(/\.conf$/i,''),config:raw}:{name:form.elements.name.value,file_name:file?.name || ''};
      if(!isAWG){if(!file&&/^https?:\/\//i.test(raw))payload.url=raw;else payload.raw=raw;}
      if(!await operation(isAWG?'awg':'subscriptions','POST',payload)){pendingImport=null;$('add-error').textContent=$('notice').textContent || tr('Не удалось отправить файл.');}
    };
	await send();
  }catch(error){$('add-error').textContent=error.message;}
});
$('remove-source').onclick=()=>{const id=$('source').value;if(id)confirmAction(tr('Удалить этот источник и все его серверы?'),()=>operation('subscriptions/'+encodeURIComponent(id),'DELETE'));};
document.addEventListener('click',event=>{if(!event.target.closest('.fl-more')){document.querySelectorAll('.fl-more-menu').forEach(e=>e.hidden=true);document.querySelectorAll('.fl-more-toggle').forEach(e=>e.setAttribute('aria-expanded','false'));}});
document.addEventListener('keydown',event=>{if(event.key==='Escape'){document.querySelectorAll('.fl-more-menu').forEach(e=>e.hidden=true);document.querySelectorAll('.fl-more-toggle').forEach(e=>e.setAttribute('aria-expanded','false'));}});
for(const [id,icon] of [['add-toggle','plus'],['refresh','refresh'],['check','bolt'],['remove-source','trash']])$(id).prepend(window.fastlaneIcon(icon));
window.addEventListener('hashchange',navigate);navigate();poll();
