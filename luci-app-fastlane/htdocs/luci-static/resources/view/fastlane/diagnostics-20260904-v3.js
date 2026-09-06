'use strict';
'require view';
'require fs';
'require ui';
'require fastlane.fastlane-20260904-v3 as fastlaneShell';

var binary = '/usr/bin/fastlane';

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

function execJSON(args) {
	return fs.exec(binary, args).then(function(result) {
		if (result.code !== 0)
			throw new Error(trim(result.stderr || result.stdout) || _('The command failed.'));
		return JSON.parse(trim(result.stdout));
	});
}

var css = `
.fastlane-diagnostics{color:var(--fl-text);min-height:620px}.fastlane-diagnostics *{box-sizing:border-box}.fld-head{display:flex;align-items:flex-start;justify-content:space-between;gap:18px;margin-bottom:18px}.fld-head h1{font-size:27px!important;letter-spacing:-.025em!important}.fld-head p{margin-top:5px;color:var(--fl-muted);font-size:14px}.fld-button{min-height:42px;border:1px solid var(--fl-line-strong);background:var(--fl-panel-2);color:var(--fl-text);border-radius:10px;padding:9px 14px;font:inherit;font-weight:700;cursor:pointer}.fld-button:hover{border-color:var(--fl-green-dim);color:var(--fl-green)}.fld-button:focus-visible,.fld-advanced summary:focus-visible{outline:2px solid var(--fl-green);outline-offset:3px}.fld-overview{border:1px solid var(--fl-line);border-radius:12px;background:var(--fl-panel);overflow:hidden}.fld-row{display:grid;grid-template-columns:minmax(150px,.8fr) minmax(180px,1fr) minmax(220px,1.5fr);align-items:center;gap:18px;min-height:64px;padding:11px 18px;border-bottom:1px solid var(--fl-line)}.fld-row:last-child{border-bottom:0}.fld-label{color:var(--fl-muted);font-size:13px}.fld-value{font-size:16px;font-weight:720;color:var(--fl-text)}.fld-note{color:var(--fl-muted);font-size:13px}.fld-state{display:inline-flex;align-items:center;gap:8px}.fld-state:before{content:"";width:9px;height:9px;border-radius:50%;background:var(--fl-subtle);box-shadow:0 0 0 4px rgba(117,111,105,.09)}.fld-ok{color:var(--fl-green)}.fld-ok:before{background:var(--fl-green);box-shadow:0 0 0 4px rgba(84,223,145,.1)}.fld-warn{color:var(--fl-amber)}.fld-warn:before{background:var(--fl-amber);box-shadow:0 0 0 4px rgba(255,197,40,.09)}.fld-problem{margin-top:14px;border:1px solid rgba(255,85,95,.42);border-radius:12px;background:rgba(255,85,95,.07);padding:15px 17px}.fld-problem-title{color:#ff9ba2;font-size:15px;font-weight:750}.fld-problem p{margin-top:4px;color:var(--fl-muted);font-size:13px}.fld-advanced{margin-top:16px;border:1px solid var(--fl-line);border-radius:12px;background:var(--fl-panel);overflow:hidden}.fld-advanced summary{display:flex;align-items:center;justify-content:space-between;min-height:56px;padding:10px 18px;cursor:pointer;font-weight:720;color:var(--fl-text);list-style:none}.fld-advanced summary::-webkit-details-marker{display:none}.fld-advanced summary:after{content:"+";color:var(--fl-green);font-size:20px;font-weight:650}.fld-advanced[open] summary:after{content:"−"}.fld-technical{border-top:1px solid var(--fl-line);padding:2px 18px 14px}.fld-tech-row{display:grid;grid-template-columns:minmax(170px,.8fr) minmax(0,1.8fr);gap:18px;padding:11px 0;border-bottom:1px solid rgba(26,43,49,.72)}.fld-tech-row:last-child{border-bottom:0}.fld-tech-key{color:var(--fl-muted);font-size:12px}.fld-tech-value{color:var(--fl-text);font-size:13px;overflow-wrap:anywhere}.fastlane-diagnostics-actions{display:flex;gap:8px}.fastlane-diagnostics-advanced{display:block}@media(max-width:760px){.fld-head{align-items:stretch}.fld-head p{max-width:32ch}.fld-row{grid-template-columns:1fr 1fr;gap:4px 12px;padding:13px 14px}.fld-note{grid-column:1/-1;padding-left:0}.fld-technical{padding-left:14px;padding-right:14px}.fld-tech-row{grid-template-columns:1fr;gap:4px}}
`;

return view.extend({
	load: function() {
		return Promise.all([
			execJSON([ '--json', 'diagnostics' ]).then(function(value) { return { value: value }; }).catch(function(error) { return { error: error }; }),
			execJSON([ '--json', 'list', 'subscriptions' ]).then(function(value) { return { value: value }; }).catch(function() { return { value: [] }; })
		]);
	},

	handleRefresh: function(ev) {
		if (ev) ev.preventDefault();
		window.location.reload();
	},

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

		var content = E('div', { id: 'fastlane-diagnostics-root', class: 'fastlane-diagnostics' }, [
			E('style', {}, [ css ]),
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

		return E('div', { class: 'fastlane-root' }, [
			fastlaneShell.renderStyles(),
			fastlaneShell.renderHeader('diagnostics'),
			E('main', { class: 'fl-shell' }, [ content ])
		]);
	},

	handleSaveApply: null,
	handleSave: null,
	handleReset: null
});
