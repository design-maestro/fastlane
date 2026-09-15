/* Rendered from diagnostics-20260904-v3.js; requires settings-panel.js for
 * the shared DOM/translation helpers. mount uses the same root contract and
 * returns {refresh(snapshot),ready}. No router command runs in the browser. */
(function(){
'use strict';
const {E,_,local,ui,L} = window.FastLaneSettings._ui;
function trim(value) {
	return value == null ? '' : String(value).trim();
}

function safe(value, fallback) {
	var text = trim(value);
	return text === '' || text === '0001-01-01T00:00:00Z' ? (fallback || '—') : text;
}

function activeNodeDisplayName(node, fallbackID) {
	var values = [
		node && node.name,
		node && node.remark,
		node && node.address,
		node && node.id,
		fallbackID
	];
	for (var i = 0; i < values.length; i++) {
		var value = trim(values[i]);
		if (value !== '')
			return value;
	}
	return '';
}

function mount(container,shared) {
  const fastlaneShell={showToast:(message,tone)=>shared.notice(message,tone==='error')};
  let loading=false, latest, disposed=false;
  const view={

	statusRow: function(label, value, note, tone) {
		return E('div', { class: 'fld-row' }, [
			E('div', { class: 'fld-label' }, [ label ]),
			E('div', { class: 'fld-value fld-state ' + (tone || '') }, [ value ]),
			E('div', { class: 'fld-note' }, [ note ])
		]);
	},


	techRow: function(label, value) {
		return E('div', { class: 'fld-tech-row' }, [
			E('div', { class: 'fld-tech-key' }, [ label ]),
			E('div', { class: 'fld-tech-value' }, [ safe(value) ])
		]);
	},


	activeSubscriptionName: function(raw, id) {
		var items = Array.isArray(raw) ? raw : ((raw && raw.subscriptions) || []);
		for (var i = 0; i < items.length; i++) {
			if (trim(items[i].id) === trim(id))
				return safe(items[i].provider_name || items[i].display_name || items[i].name || items[i].id);
		}
		return trim(id) === '' ? _('Not selected') : id;
	},


	render: function(data) {
		var diagnosticsResult = data && data[0] || {};
		var subscriptionsResult = data && data[1] || {};
		var snapshot = diagnosticsResult.value || {};
		var status = snapshot.status || {};
		var state = status.state || {};
		var settings = status.settings || {};
		var runtime = snapshot.runtime || {};
		var dns = snapshot.dns || {};
		var ipv6 = snapshot.ipv6 || {};
		var files = snapshot.files || {};
		var connected = !!state.connected;
		var serviceRunning = !!runtime.running;
		var dnsActive = !!dns.active;
		var subscription = this.activeSubscriptionName(subscriptionsResult.value, state.active_subscription_id);
		var activeNode = activeNodeDisplayName(status.active_node, state.active_node_id);
		var modeText = connected ? (settings.auto_mode ? _('Automatic') : _('Manual')) : _('Disconnected');
		var problem = diagnosticsResult.error || trim(state.last_failure_reason || state.last_transport_failure_reason);
		if (problem && !this.problemNotified) {
			fastlaneShell.showToast(
				diagnosticsResult.error ? _('Could not read Fast Lane status.') : _('The last connection failed.'),
				'error',
				diagnosticsResult.error ? diagnosticsResult.error.message : problem
			);
			this.problemNotified = true;
		}
		var fileNames = Object.keys(files).filter(function(key) { return key.indexOf('zapret') !== 0; });
		var readyFiles = fileNames.filter(function(key) { return files[key] && files[key].exists; }).length;
		var technicalRows = [
			this.techRow(_('Fast Lane service'), safe(runtime.service_state, serviceRunning ? _('Running') : _('Stopped'))),
			this.techRow(_('VPN configuration'), safe(runtime.config_path)),
			this.techRow(_('Local DNS'), dns.available ? safe(dns.local_dns_listen) + ':' + safe(dns.local_dns_port) : _('Unavailable')),
			this.techRow(_('System DNS'), (dns.system_resolvers || []).join(', ') || _('Not detected')),
			this.techRow('IPv6', ipv6.available ? (ipv6.runtime_disabled ? _('Disabled to prevent leaks') : _('Enabled')) : _('Unsupported')),
			this.techRow(_('Service files'), readyFiles + ' / ' + fileNames.length + ' ' + _('available'))
		];
		(state.runtime_outbounds || []).forEach(L.bind(function(outbound, index) {
			if (!outbound || (outbound.role !== 'reserve' && outbound.role !== 'candidate'))
				return;
			var label = outbound.role === 'reserve' ? _('Reserve') : _('Reserve candidate');
			var value = safe(outbound.subscription_id) + ' / ' + safe(outbound.node_id) +
				' · ' + _('score') + ' ' + Number(outbound.score || 0).toFixed(0) +
				' · ' + Number(outbound.samples || 0) + ' ' + _('samples') +
				' · ' + safe(outbound.selection_reason);
			technicalRows.push(this.techRow(label + ' ' + (index + 1), value));
		}, this));

		var content = E('div', { id: 'fastlane-diagnostics-root', class: 'fastlane-diagnostics' }, [
			E('div', { class: 'fld-head' }, [
				E('div', {}, [ E('h1', {}, [ _('Diagnostics') ]), E('p', {}, [ _('Current Fast Lane and connection status.') ]) ]),
				E('div', { class: 'fastlane-diagnostics-actions' }, [ E('button', { class: 'fld-button', click: ui.createHandlerFn(this, 'handleRefresh') }, [ _('Refresh') ]) ])
			]),
			E('section', { class: 'fld-overview', 'aria-label': _('Fast Lane status') }, [
				this.statusRow('VPN', connected ? _('Connected') : _('Disconnected'), connected ? _('Traffic goes through the selected server.') : _('Internet traffic is direct.'), connected ? 'fld-ok' : ''),
				this.statusRow(_('VPN service'), serviceRunning ? _('Running') : _('Stopped'), serviceRunning ? _('The service process is running.') : _('It starts when a server is connected.'), serviceRunning ? 'fld-ok' : ''),
				this.statusRow(_('Subscription'), subscription, activeNode !== '' ? _('Server:') + ' ' + activeNode : _('No server selected yet.'), trim(state.active_subscription_id) ? 'fld-ok' : ''),
				this.statusRow('DNS', dnsActive ? _('Running') : _('Inactive'), dnsActive ? _('DNS requests are handled by Fast Lane.') : _('It starts together with VPN.'), dnsActive ? 'fld-ok' : ''),
				this.statusRow(_('Mode'), modeText, connected ? _('Server selection method.') : _('No active connection.'), connected ? 'fld-ok' : '')
			]),
			E('details', { class: 'fastlane-diagnostics-advanced fld-advanced' }, [
				E('summary', {}, [ _('Technical details') ]),
				E('div', { class: 'fld-technical' }, technicalRows)
			])
		]);

		return content;
	},

    handleRefresh(ev){ ev?.preventDefault(); return refresh(); }
  };
  function paint() {
    if (!latest) return;
    const open=container.querySelector('details')?.open || false;
    const data=latest.data, snapshot=latest.snapshot, absent=data.unavailable || {};
    const content=view.render([{value:data},{value:snapshot?.subscriptions || []}]);
    content.querySelector('details').open=open;
    const rows=content.querySelectorAll('.fld-row');
    for(const [index,key] of [[1,'runtime'],[3,'dns']]) {
      if (!absent[key]) continue;
      rows[index].querySelector('.fld-value').textContent=local('Unavailable','Недоступно');
      rows[index].querySelector('.fld-value').classList.remove('fld-ok');
      rows[index].querySelector('.fld-note').textContent=absent[key]==='unsupported_runtime' ? local('Runtime adapter is not installed on this host.','На этом хосте нет адаптера runtime.') : local('Could not read runtime status.','Не удалось прочитать фактическое состояние.');
    }
    const technical=content.querySelectorAll('.fld-tech-row');
    if(absent.runtime) technical[0].querySelector('.fld-tech-value').textContent=local('Unavailable','Недоступно');
    if(absent.files) technical[5].querySelector('.fld-tech-value').textContent=local('File inspection adapter unavailable.','Адаптер проверки файлов недоступен.');
    const problem=data.status?.state?.last_failure_reason || data.status?.state?.last_transport_failure_reason;
    if(problem) content.querySelector('.fld-overview').after(E('div',{class:'fld-problem',role:'status'},[
      E('div',{class:'fld-problem-title'},[_('The last connection failed.')]),E('p',{},[problem])
    ]));
    container.replaceChildren(content);
  }
  async function refresh(snapshot) {
    if (loading || disposed) return;
    loading=true;
    try {
      const data=await shared.api('diagnostics-panel');
      latest={data,snapshot:snapshot || shared.getSnapshot()};
      paint();
    } catch (error) {
      const content=container.querySelector('.fastlane-diagnostics');
      const text=local('Could not refresh diagnostics. Previously received information may be outdated.','Не удалось обновить диагностику. Ранее полученные сведения могли устареть.');
      if(content) {
        let error=content.querySelector('[data-read-error]');
        if(!error){error=E('p',{role:'alert','data-read-error':'true'});content.prepend(error);}
        error.textContent=text;
      } else container.replaceChildren(E('section',{class:'fastlane-diagnostics'},[
        E('p',{role:'alert'},[local('Could not read Fast Lane status.','Не удалось прочитать состояние Fast Lane.')]),
        E('button',{class:'fld-button',click:()=>refresh()},[_('Refresh')])
      ]));
    } finally { loading=false; }
  }
  const onLanguage=()=>paint();
  window.addEventListener('fastlane:languagechange',onLanguage);
  container.replaceChildren(E('p',{role:'status'},[local('Loading diagnostics…','Загрузка диагностики…')]));
  return {refresh,ready:refresh(),destroy(){disposed=true;window.removeEventListener('fastlane:languagechange',onLanguage);}};
}
window.FastLaneDiagnostics={mount};
})();
