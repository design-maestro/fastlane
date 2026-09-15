'use strict';
const $ = id => document.getElementById(id);
let snapshot, awg, activeJob = false, sending = false, lastJob = null, routingDirty = false, settingsDirty = false, hideDirty = false, dnsDirty = false;
const jobNames = {'health-check':'Проверяем серверы','refresh':'Обновляем подписки','connect-auto':'Выбираем сервер','connect-manual':'Подключаем сервер','disconnect':'Отключаем VPN','add-subscription':'Добавляем серверы','remove-subscription':'Удаляем источник','settings':'Сохраняем настройки','routing':'Применяем маршруты','awg-import':'Импортируем AWG','awg-check':'Проверяем AWG','awg-connect':'Подключаем AWG','awg-disconnect':'Отключаем AWG','awg-remove':'Удаляем AWG'};
const messages = {auth_required:'Войдите с ключом доступа.',auth_rate_limited:'Слишком много попыток входа. Повторите через минуту.',job_already_running:'Дождитесь завершения текущей операции.',operation_failed:'Операция не выполнена. Проверьте данные и состояние подключения; подробности доступны в журнале службы.',invalid_request:'Проверьте заполненные поля.',feature_unavailable:'Эта функция недоступна в установленной службе.'};
Object.assign(messages,{unsafe_awg_directive:'В профиле есть команды запуска или остановки. Такие файлы не разрешены.',unsupported_awg_version:'Поддерживается только AWG 2.0. Экспортируйте профиль этой версии.',unsupported_awg_parameter:'В профиле есть неподдерживаемые параметры AWG.',awg_import_failed:'Не удалось импортировать AWG. Проверьте ключи, адрес и единственный Peer; перед заменой отключите активный профиль.'});
let noticeTimer;
function notice(message, error = false) { clearTimeout(noticeTimer); $('notice').textContent = message; $('notice').classList.toggle('bad', error); if(message && !error)noticeTimer=setTimeout(()=>{$('notice').textContent='';},4500); }
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
function active(node) { const state = snapshot.status.state; return state.connected && state.active_subscription_id === node.subscription_id && state.active_node_id === node.id; }
function latencyMS(health) {
  if (!health?.healthy) return null;
  const value=String(health.last_latency || ''), scales={h:3600000,m:60000,s:1000,ms:1,'µs':.001,us:.001,ns:.000001};
  let total=0, found=false;
  for (const match of value.matchAll(/([\d.]+)(ns|µs|us|ms|s|m|h)/g)) {total+=Number(match[1])*scales[match[2]];found=true;}
  return found?Math.round(total):null;
}
function pingText(health) { const value=latencyMS(health); return value===null?'Нет замера':value+' мс'; }
function pingClass(health) {const value=latencyMS(health);return value===null?'':value<=100?'good':value<=200?'ping-amber':value<=1000?'ping-orange':'bad';}
function renderServers() {
  if (!snapshot) return;
  const query=$('search').value.trim().toLocaleLowerCase(), selected=$('source').value, subscriptions=snapshot.subscriptions || [];
  $('remove-source').hidden=!selected;
  const source=subscriptions.find(sub=>sub.id===selected);
  $('source-detail').textContent=source?[(source.last_error?'Ошибка обновления':expired(source)?'Подписка истекла':source.node_count+' серверов'),'Обновлён: '+date(source.last_updated_at)].join(' · '):'';
  const rows=subscriptions.filter(sub=>selected?sub.id===selected:!expired(sub)).flatMap(sub=>(sub.nodes || []).map(node=>({sub,node})));
  const visible=rows.filter(({node})=>($('show-hidden').checked || !hidden(node)) && (node.name+' '+(node.remark || '')+' '+node.protocol).toLocaleLowerCase().includes(query));
  visible.sort((a,b)=>Number(active(b.node))-Number(active(a.node)) || a.node.name.localeCompare(b.node.name));
  const root=$('server-list');
  if(!visible.length){if(!root.querySelector('.empty'))root.replaceChildren(element('div','','empty'));root.firstChild.textContent=rows.length?'Нет серверов по выбранным фильтрам.':'Добавьте подписку или файл, чтобы подключить VPN.';return;}
  let table=root.querySelector('table');
  if(!table){table=element('table');const head=element('thead'),hr=element('tr');['Сервер','Источник','Пинг (GET)','Состояние','Действие'].forEach(text=>hr.append(element('th',text)));head.append(hr);table.append(head,element('tbody'));root.replaceChildren(table);}
  table.classList.toggle('single-source',!!selected);
  table.querySelector('th:nth-child(2)').hidden=!!selected;
  const body=table.querySelector('tbody'),existing=new Map(Array.from(body.children,row=>[row.dataset.key,row]));
  visible.forEach(({sub,node},index)=>{
    const key=sub.id+'/'+node.id,health=snapshot.status.state.health?.[node.id];
    let row=existing.get(key);
    if(!row){row=element('tr');row.dataset.key=key;const title=element('td');title.append(element('strong'),element('small'));const action=element('td'),button=element('button');button.dataset.operation='';button.addEventListener('click',()=>operation('connect','POST',{mode:'manual',subscription_id:sub.id,node_id:node.id}));action.append(button);row.append(title,element('td'),element('td'),element('td'),action);}
    existing.delete(key);row.className=active(node)?'active':'';
    const cells=row.children;cells[0].firstChild.textContent=node.name || node.address;cells[0].lastChild.textContent=[node.remark!==node.name?node.remark:'',node.protocol?.toUpperCase()].filter(Boolean).join(' · ');
    cells[1].textContent=sub.display_name || sub.provider_name || sub.id;cells[1].hidden=!!selected;
    cells[2].textContent='GET: '+pingText(health);cells[2].className='latency '+pingClass(health);
    cells[3].textContent=expired(sub)?'Подписка истекла':hidden(node)?'Скрыт':active(node)?'Активен':health?.healthy?(latencyMS(health)>1000?'Медленный':'Проверен'):health?.last_checked_at && !health.last_checked_at.startsWith('0001-')?'Недоступен':'Не проверен';
    cells[3].className=active(node)?'good':'';
    const button=cells[4].firstChild;button.textContent=active(node)?'Выбран':'Подключить';button.dataset.unavailable=String(expired(sub)||hidden(node)||active(node));
    if(body.children[index]!==row){const focus=document.activeElement;body.insertBefore(row,body.children[index] || null);if(row.contains(focus))focus.focus({preventScroll:true});}
  });
  existing.forEach(row=>row.remove());setBusy();
}
function fill(form, values) { for (const [name,value] of Object.entries(values)) { if (form.elements[name]) form.elements[name].value=value ?? ''; } }
function render() {
  const status=snapshot.status, state=status.state, settings=status.settings, connected=state.connected && state.operational_mode!=='direct';
  const recovering=state.operational_mode==='recovering';
  $('connection-title').textContent = recovering?'Восстанавливаем подключение':connected?'VPN подключён':state.mode==='disconnected'?'VPN отключён — интернет напрямую':'VPN недоступен — интернет напрямую';
  $('connection-title').className=connected?'good':'';
  $('connection-detail').textContent=connected ? [state.active_node_name || status.active_node?.name || (state.active_connection_kind==='amneziawg'?'AmneziaWG':'Сервер отсутствует в подписке'),status.active_subscription?.display_name].filter(Boolean).join(' · ') : 'Соединение с VPN сейчас не используется.';
  $('selection-mode').textContent=state.mode==='auto'?'Авто':state.mode==='manual'?'Вручную':'Отключено';
  $('disconnect').dataset.unavailable=String(state.mode==='disconnected');
  $('disconnect').hidden=state.mode==='disconnected';
  const activeHealth=state.health?.[state.active_node_id];
  $('connection-ping').textContent=connected?'Пинг (GET): '+pingText(activeHealth):'';
  $('connection-ping').className=pingClass(activeHealth);
  const job=snapshot.job; activeJob=job.running;
  $('job').textContent=job.running?(jobNames[job.kind] || 'Выполняем операцию…')+'. Можно закрыть вкладку.':'';
  if (lastJob===null) lastJob=job.running?job.sequence-1:job.sequence;
  if (job.sequence && !job.running && lastJob!==job.sequence) { notice(job.succeeded?'Операция завершена.':messages[job.error] || messages.operation_failed,!job.succeeded); lastJob=job.sequence; }
  const source=$('source'), old=source.value, subscriptions=snapshot.subscriptions || [];
  const options=subscriptions.map(sub=>({id:sub.id,label:(sub.display_name || sub.id)+' · '+sub.node_count+(expired(sub)?' · истекла':'')}));
  if(subscriptions.length!==1)options.unshift({id:'',label:subscriptions.length?'Все серверы':'Нет источников'});
  const signature=JSON.stringify(options);
  if(source.dataset.signature!==signature){source.replaceChildren(...options.map(option=>new Option(option.label,option.id)));source.value=subscriptions.length===1?subscriptions[0].id:old;source.dataset.signature=signature;}
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
function renderAWG() {
  const states={absent:'Профиль не добавлен',imported:'Импортирован',prepared:'Интерфейс поднят',connected:'VPN подключён',direct:'Интернет напрямую',probe_failed:'Проверка не пройдена',incompatible:'Нет совместимого модуля',invalid:'Ошибка профиля'};
  $('awg-state').textContent=states[awg.state] || 'Состояние неизвестно';
  $('awg-detail').textContent=[awg.name,awg.message,awg.last_probe?'Проверка: '+(awg.last_probe.success?'успешна':'не пройдена')+' · '+date(awg.last_probe.checked_at):'Интернет через AWG ещё не проверен'].filter(Boolean).join(' · ');
  $('awg-actions').hidden=awg.state==='absent';
  $('awg-form').hidden=!!awg.active;
  $('awg-disconnect').hidden=!awg.active;
  for(const id of ['awg-check','awg-connect']) $(id).dataset.unavailable=String(awg.state==='incompatible'||awg.state==='invalid');
  $('awg-connect').hidden=!!awg.active; setBusy();
}
async function reload() {
  try { snapshot=await api('state'); $('application').hidden=false; $('login').hidden=true; $('logout').hidden=false; render(); try{awg=await api('awg');renderAWG();}catch(error){$('awg-detail').textContent=error.message;} }
  catch(error) { if (snapshot) { notice('Нет связи с роутером. Показано последнее полученное состояние.',true); document.querySelectorAll('[data-operation]').forEach(b=>b.disabled=true); } else if ($('login').hidden) {notice(error.message,true);} }
}
async function poll() { await reload(); window.setTimeout(poll,3000); }
function navigate() { const id=location.hash.slice(1); const page=['vpn','routing','diagnostics','settings'].includes(id)?id:'vpn'; document.querySelectorAll('.page').forEach(e=>e.hidden=e.id!==page); document.querySelectorAll('nav a').forEach(a=>{if(a.hash==='#'+page)a.setAttribute('aria-current','page');else a.removeAttribute('aria-current');}); }
function confirmAction(text, action) { const dialog=$('confirm'); $('confirm-text').textContent=text; dialog.returnValue='cancel'; dialog.addEventListener('close',()=>{if(dialog.returnValue==='confirm')action();},{once:true}); dialog.showModal(); }
$('login-form').addEventListener('submit',async event=>{event.preventDefault();const field=event.target.elements.token;try{await api('session','POST',{token:field.value});field.value='';notice('');await reload();}catch(error){notice(error.message,true);}});
$('logout').addEventListener('click',async()=>{try{await api('session','DELETE');showLogin();notice('Вы вышли из панели.');}catch(error){notice(error.message,true);}});
$('auto').addEventListener('click',()=>operation('connect','POST',{mode:'auto',subscription_id:'all'}));
$('disconnect').addEventListener('click',()=>confirmAction('Отключить VPN? Трафик будет идти напрямую.',()=>operation('disconnect')));
$('refresh').addEventListener('click',()=>operation('jobs/refresh'));
$('check').addEventListener('click',()=>operation('jobs/health-check'));
['search','source','show-hidden'].forEach(id=>$(id).addEventListener(id==='search'?'input':'change',renderServers));
$('add-toggle').addEventListener('click',()=>{$('add-form').hidden=false;$('add-form').elements.name.focus();});
$('add-cancel').addEventListener('click',()=>{$('add-form').hidden=true;});
$('add-form').addEventListener('submit',async event=>{event.preventDefault();const form=event.target, file=form.elements.file.files[0];try{if(file?.size>5*1024*1024)throw new Error('Файл должен быть меньше 5 МБ.');const source=file?await file.text():form.elements.source.value.trim();if(!source)throw new Error('Укажите ссылку или выберите файл.');const input={name:form.elements.name.value,file_name:file?.name || ''};if(!file && /^https?:\/\//i.test(source))input.url=source;else input.raw=source;if(await operation('subscriptions','POST',input)){form.reset();form.hidden=true;}}catch(error){notice(error.message,true);}});
$('remove-source').addEventListener('click',()=>{const id=$('source').value;if(id)confirmAction('Удалить этот источник и все его серверы?',()=>operation('subscriptions/'+encodeURIComponent(id),'DELETE'));});
$('awg-form').addEventListener('submit',async event=>{event.preventDefault();const form=event.target,file=form.elements.file.files[0];try{if(!file || file.size>1024*1024)throw new Error('Выберите .conf размером до 1 МБ.');if(await operation('awg','POST',{name:form.elements.name.value,config:await file.text()}))form.reset();}catch(error){notice(error.message,true);}});
for(const action of ['connect','check']) $('awg-'+action).addEventListener('click',()=>operation('awg/'+action));
$('awg-disconnect').addEventListener('click',()=>confirmAction('Отключить AWG и направить интернет напрямую?',()=>operation('awg/disconnect')));
$('awg-remove').addEventListener('click',()=>confirmAction('Удалить профиль AWG? Если он активен, интернет пойдёт напрямую.',()=>operation('awg','DELETE')));
$('routing-form').addEventListener('input',()=>routingDirty=true);
$('routing-form').addEventListener('submit',event=>{event.preventDefault();const form=event.target,input={mode:form.elements.mode.value,proxy:list(form.elements.proxy.value),bypass:list(form.elements.bypass.value),excluded:list(form.elements.excluded.value)};confirmAction('Применить новые маршруты к трафику домашней сети?',async()=>{if(await operation('routing','POST',input))routingDirty=false;});});
$('settings-form').addEventListener('input',()=>settingsDirty=true);
$('settings-form').addEventListener('submit',async event=>{event.preventDefault();if(await operation('jobs/settings','POST',Object.fromEntries(new FormData(event.target))))settingsDirty=false;});
$('hide-form').addEventListener('input',()=>hideDirty=true);
$('hide-form').addEventListener('submit',event=>{event.preventDefault();const keywords=list(event.target.elements['auto.hide-keywords'].value);confirmAction('Применить скрытие? Если активный сервер совпадёт, Fast Lane выберет замену.',async()=>{if(await operation('hide-keywords','POST',{keywords}))hideDirty=false;});});
$('dns-form').addEventListener('input',()=>dnsDirty=true);
$('dns-form').addEventListener('submit',event=>{event.preventDefault();const form=event.target,input={mode:form.elements['dns.mode'].value,transport:form.elements['dns.transport'].value,servers:list(form.elements['dns.servers'].value)};confirmAction('Сохранить DNS? Это может потребовать перезапуска сетевого движка.',async()=>{if(await operation('dns','POST',input))dnsDirty=false;});});
window.addEventListener('hashchange',navigate);navigate();poll();
