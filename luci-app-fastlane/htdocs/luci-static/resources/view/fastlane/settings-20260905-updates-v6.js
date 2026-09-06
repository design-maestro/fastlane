'use strict';
'require view';
'require fs';
'require ui';
'require dom';
'require poll';
'require uci';
'require fastlane.fastlane-20260906-v4 as fastlaneShell';

var binary = '/usr/bin/fastlane';
// Long-running release operations belong to the router, not this view.
var uninstaller = '/usr/libexec/fastlane-uninstall';
function trim(value) { return value == null ? '' : String(value).trim(); }
function normalizeDuration(value) { return trim(value).replace(/\s+/g, ''); }
function durationMilliseconds(value) {
	var normalized = normalizeDuration(value);
	var scales = { h: 3600000, m: 60000, s: 1000, ms: 1, us: 0.001, 'µs': 0.001, ns: 0.000001 };
	var pattern = /(\d+(?:\.\d+)?)(ms|µs|us|ns|h|m|s)/g;
	var match;
	var total = 0;
	var matched = false;
	while ((match = pattern.exec(normalized)) !== null) {
		total += (parseFloat(match[1]) || 0) * scales[match[2]];
		matched = true;
	}
	return { total: Math.round(total), matched: matched, normalized: normalized };
}
function durationParts(value, units) {
	var parts = {};
	for (var i = 0; i < units.length; i++) parts[units[i]] = 0;
	var parsed = durationMilliseconds(value);
	if (!parsed.matched && /^\d+$/.test(parsed.normalized) && units.length) {
		parts[units[units.length - 1]] = parseInt(parsed.normalized, 10) || 0;
		return parts;
	}
	var remaining = parsed.total;
	var scales = { h: 3600000, m: 60000, s: 1000, ms: 1 };
	for (var j = 0; j < units.length; j++) {
		var scale = scales[units[j]];
		parts[units[j]] = Math.floor(remaining / scale);
		remaining %= scale;
	}
	return parts;
}
function durationValue(parts, units) {
	return units.map(function(unit) { return String(parts[unit] || 0) + unit; }).join('');
}
function durationUnitName(unit) {
	return { h: _('hours'), m: _('minutes'), s: _('seconds'), ms: _('milliseconds') }[unit] || unit;
}

var css = `
.fastlane-settings{--panel:var(--fl-panel);--line:var(--fl-line);--text:var(--fl-text);--muted:var(--fl-muted);--blue:var(--fl-green-dim);background:transparent;color:var(--text);min-height:680px;font-family:inherit}.fastlane-settings *{box-sizing:border-box}.fls-head{display:flex;justify-content:space-between;align-items:center;gap:16px;margin-bottom:18px}.fls-head h2{margin:0;font-size:24px}.fls-head p{margin:4px 0 0;color:var(--muted)}.fls-grid{display:grid;grid-template-columns:1fr 1fr;gap:14px}.fls-card{background:var(--panel);border:1px solid var(--line);border-radius:18px;padding:18px}.fls-card h3{margin:0 0 5px;font-size:17px}.fls-card>p{margin:0 0 16px;color:var(--muted);font-size:12px;line-height:1.45}.fls-fields{display:grid;gap:13px}.fls-field{display:grid;grid-template-columns:minmax(180px,1fr) minmax(170px,240px);gap:16px;align-items:center}.fls-field-toggle{align-items:center}.fls-field label{font-weight:700}.fls-hint{display:block;color:var(--muted);font-weight:400;font-size:11px;margin-top:3px}.fls-input{width:100%;height:44px!important;border:1px solid var(--line)!important;background:#050d10!important;color:var(--text)!important;border-radius:11px!important;padding:9px 11px!important}.fls-select{appearance:auto}.fls-duration{display:flex;align-items:center;gap:12px;width:100%;height:44px;border:1px solid var(--line);background:#050d10;color:var(--text);border-radius:11px;padding:9px 11px;cursor:text}.fls-duration-segment{display:inline-flex;align-items:baseline;gap:3px}.fls-duration-input{position:static!important;top:auto!important;left:auto!important;width:4ch!important;height:auto!important;margin:0!important;border:0!important;border-radius:0!important;padding:0!important;background:transparent!important;color:var(--text)!important;font:inherit!important;font-variant-numeric:tabular-nums;text-align:right;outline:0!important;box-shadow:none!important}.fls-duration-unit{color:var(--muted);font-weight:600;pointer-events:none}.fls-toggle{display:inline-flex;align-items:center;justify-content:flex-end;gap:10px;min-height:22px;white-space:nowrap;line-height:22px}.fls-field-toggle>.fls-toggle{min-height:22px;padding-top:0}.fls-toggle input{position:static!important;top:auto!important;left:auto!important;width:22px!important;height:22px!important;margin:0!important;flex:0 0 auto;vertical-align:middle!important}.fls-toggle span{display:block;line-height:22px}.fls-actions{display:flex;gap:8px;flex-wrap:wrap}.fls-button{display:inline-flex;align-items:center;justify-content:center;min-height:44px;border:1px solid var(--line);background:#0d1c21;color:var(--text);border-radius:11px;padding:10px 14px;font-weight:750;cursor:pointer;text-decoration:none}.fls-button:focus-visible,.fls-input:focus-visible,.fls-toggle input:focus-visible{outline:2px solid var(--fl-green);outline-offset:3px}.fls-duration:focus-within{border-color:var(--fl-green);outline:2px solid var(--fl-green);outline-offset:3px}.fls-button:disabled{opacity:.48;cursor:not-allowed}.fls-primary{background:var(--blue);border-color:var(--blue)}.fls-danger{color:var(--fl-red);border-color:color-mix(in srgb,var(--fl-red) 55%,var(--line))}.fls-manage{grid-column:1/-1;display:flex;align-items:center;justify-content:space-between;gap:18px}.fls-manage-copy{min-width:0}.fls-manage-copy p{margin:5px 0 0;color:var(--muted);font-size:12px;line-height:1.45}.fls-manage-actions{display:flex;gap:10px;flex:0 0 auto}.fls-geo{display:grid;gap:12px}.fls-geo-status{border:1px solid var(--fl-line-strong);border-radius:13px;padding:14px;color:var(--muted);font-size:12px;line-height:1.5}.fls-ready{color:var(--fl-green);border-color:#245c4a}.fls-geo-actions{display:flex;gap:10px;align-items:center;justify-content:space-between;flex-wrap:wrap}@media(max-width:850px){.fls-head{align-items:flex-start;flex-direction:column}.fls-grid{grid-template-columns:1fr}.fls-field{grid-template-columns:1fr}.fls-toggle{justify-content:flex-start}.fls-manage{align-items:stretch;flex-direction:column}.fls-manage-actions{flex-direction:column}.fls-manage-actions .fls-button{width:100%}}
`;

css += '.fls-update{align-items:flex-start}.fls-update .fls-manage-copy{flex:1}.fls-update-actions{flex-wrap:wrap;justify-content:flex-end;max-width:52%}.fls-update .fls-primary{color:var(--fl-bg)}@media(max-width:850px){.fls-update{align-items:stretch}.fls-update-actions{width:100%;max-width:none;justify-content:flex-start}}';

return view.extend({
	load: function() {
		return Promise.all([ this.execJSON([ '--json', 'settings', 'get' ]), uci.load('luci') ]).then(L.bind(function(result) {
			var data = result[0];
			var settings = data || {};
			this.settings = settings;
			this.draft = Object.assign({}, this.settings);
			this.interfaceLanguage = uci.get('luci', 'main', 'lang') || 'auto';
			return this.refreshUpdate().then(function() { return data; });
		}, this));
	},

	handleLanguageChange: function(ev) {
		var language = trim(ev && ev.target && ev.target.value) || 'auto';
		this.interfaceLanguage = language;
		uci.set('luci', 'main', 'lang', language);
		return uci.save().then(function() { return uci.apply(); }).then(function() { window.location.reload(); }).catch(function(err) {
			fastlaneShell.showToast(_('Could not change the interface language.'), 'error', err.message || String(err));
		});
	},

	execJSON: function(args) {
		return fs.exec(binary, args).then(function(result) {
			if (result.code !== 0) throw new Error(trim(result.stderr || result.stdout));
			return JSON.parse(trim(result.stdout));
		});
	},

	updateBusy: function() {
		return this.updateState && (this.updateState.status === 'checking' || this.updateState.status === 'installing');
	},

	refreshUpdate: function() {
		return this.execJSON([ '--json', 'update', 'status' ]).then(L.bind(function(state) {
			this.updateState = state;
			this.updateTransportError = '';
			this.syncUpdate();
		}, this)).catch(L.bind(function() {
			this.updateTransportError = _('Could not read update status. Try again; during installation, wait for the connection to return.');
			this.syncUpdate();
		}, this));
	},

	syncUpdate: function() {
		if (this.updateBox) dom.content(this.updateBox, this.renderUpdateContents());
	},

	handleUpdateCheck: function(ev) {
		if (ev) ev.preventDefault();
		if (this.saving || this.updateRequest || this.updateBusy()) return Promise.resolve();
		this.updateRequest = true;
		this.syncUpdate();
		return this.execJSON([ '--json', 'update', 'check' ]).then(L.bind(function(state) {
			this.updateState = state;
			this.updateTransportError = '';
		}, this)).catch(L.bind(function() {
			this.updateTransportError = _('Could not start the update check. Try again.');
		}, this)).finally(L.bind(function() { this.updateRequest = false; this.syncUpdate(); }, this));
	},

	handleUpdateInstall: function(ev) {
		if (ev) ev.preventDefault();
		var state = this.updateState || {};
		if (this.saving || this.updateRequest || this.updateBusy() || state.status !== 'available' || !state.candidate) return Promise.resolve();
		if (!window.confirm(_('Install Fast Lane') + ' ' + state.candidate.version + '? ' + _('Saved settings and subscriptions will remain. VPN may reconnect briefly. Do not turn off the router until installation finishes.'))) return Promise.resolve();
		this.updateRequest = true;
		this.syncUpdate();
		return this.execJSON([ '--json', 'update', 'install', '--release', String(state.candidate.id) ]).then(L.bind(function(result) {
			this.updateState = result;
			this.updateTransportError = '';
		}, this)).catch(L.bind(function() {
			this.updateTransportError = _('Could not confirm installation. Check the current status before trying again.');
			return this.refreshUpdate();
		}, this)).finally(L.bind(function() { this.updateRequest = false; this.syncUpdate(); }, this));
	},

	renderUpdateContents: function() {
		var state = this.updateState || {};
		var candidate = state.candidate;
		var blocked = this.updateRequest || this.updateBusy();
		var actions = [ E('button', { class: 'fls-button', disabled: blocked ? 'disabled' : null, click: ui.createHandlerFn(this, 'handleUpdateCheck') }, [ this.updateRequest || state.status === 'checking' ? _('Checking…') : _('Check for updates') ]) ];
		if (state.status === 'available' && candidate) actions.push(E('button', { class: 'fls-button fls-primary', disabled: blocked ? 'disabled' : null, click: ui.createHandlerFn(this, 'handleUpdateInstall') }, [ _('Install') + ' ' + candidate.version ]));
		if (candidate && candidate.page && /^https:\/\/github\.com\/design-maestro\/fastlane\/releases\/tag\/v[0-9]+\.[0-9]+\.[0-9]+$/.test(candidate.page)) actions.push(E('a', { class: 'fls-button', href: candidate.page, target: '_blank', rel: 'noopener noreferrer' }, [ _('What is new') ]));
		if (state.status === 'updated') actions.push(E('a', { class: 'fls-button fls-primary', href: L.url('admin/services/fastlane/settings') }, [ _('Reload page') ]));
		return [
			E('div', { class: 'fls-manage-copy' }, [
				E('h3', {}, [ _('Fast Lane update') ]),
				E('p', {}, [ _('Installed version:') + ' ' + (state.current_version || _('unknown')) + '. ' + _('Only stable GitHub releases are used, and installation always requires confirmation.') ]),
				E('p', { role: 'status', 'aria-live': 'polite' }, [ this.updateTransportError || state.message || _('Check whether a new version is available.') ]),
				this.updateBusy() ? E('p', {}, [ _('You can close the admin panel; the task will continue on the router.') ]) : ''
			]),
			E('div', { class: 'fls-manage-actions fls-update-actions' }, actions)
		];
	},

	handleInput: function(key, ev) {
		this.draft[key] = ev && ev.target ? ev.target.value : '';
	},

	handleBool: function(key, ev) {
		this.draft[key] = !!(ev && ev.target && ev.target.checked);
		var label = ev && ev.target && ev.target.parentNode ? ev.target.parentNode.querySelector('[data-toggle-label]') : null;
		if (label) label.textContent = this.draft[key] ? _('On') : _('Off');
	},

	setSaving: function(saving) {
		this.saving = !!saving;
		if (!this.settingsRoot || !this.settingsRoot.querySelectorAll) return;
		var controls = this.settingsRoot.querySelectorAll('input, button');
		for (var i = 0; i < controls.length; i++) controls[i].disabled = this.saving;
	},

	syncSettingsControls: function() {
		if (!this.settingsRoot || !this.settingsRoot.querySelectorAll) return;
		var controls = this.settingsRoot.querySelectorAll('[data-setting-key]');
		for (var i = 0; i < controls.length; i++) {
			var control = controls[i];
			var key = control.getAttribute('data-setting-key');
			var unit = control.getAttribute('data-duration-unit');
			if (control.type === 'checkbox') {
				control.checked = !!this.draft[key];
				var label = control.parentNode ? control.parentNode.querySelector('[data-toggle-label]') : null;
				if (label) label.textContent = control.checked ? _('On') : _('Off');
			} else if (unit) {
				var units = trim(control.getAttribute('data-duration-units')).split(',');
				control.value = String(durationParts(this.draft[key], units)[unit] || 0);
			} else {
				control.value = this.draft[key] == null ? '' : String(this.draft[key]);
			}
		}
	},

	handleDurationInput: function(key, units, unit, ev) {
		var input = ev && ev.target;
		var digits = input ? String(input.value).replace(/\D/g, '') : '';
		if (input) input.value = digits;
		var parts = durationParts(this.draft[key], units);
		parts[unit] = digits === '' ? 0 : parseInt(digits, 10) || 0;
		this.draft[key] = durationValue(parts, units);
	},

	handleUninstall: function(ev) {
		if (ev) ev.preventDefault();
		if (this.updateRequest || this.updateBusy()) {
			fastlaneShell.showToast(_('Wait for the update to finish before removing Fast Lane.'), 'error');
			return Promise.resolve();
		}
		if (!window.confirm(_('Remove Fast Lane from the router? Settings and subscriptions will also be removed.'))) return Promise.resolve();
		this.setSaving(true);
		return fs.exec(uninstaller, [ '--confirm' ]).then(function(result) {
			if (result.code !== 0) throw new Error(trim(result.stderr || result.stdout));
			window.location.href = L.url('admin/system/package-manager');
		}).catch(L.bind(function(err) {
			this.setSaving(false);
			fastlaneShell.showToast(_('Could not remove Fast Lane.'), 'error', err.message || String(err));
		}, this));
	},

	handleSaveSettings: function(ev) {
		if (ev) ev.preventDefault();
		if (this.updateRequest || (this.updateState && this.updateState.status === 'installing')) {
			fastlaneShell.showToast(_('Wait for the installation to finish before saving settings.'), 'error');
			return Promise.resolve();
		}
		var mappings = [
			[ 'refresh_interval', 'refresh-interval' ],
			[ 'health_check_interval', 'health-check-interval' ],
			[ 'url_test_url', 'url-test-url' ],
			[ 'url_test_timeout', 'url-test-timeout' ],
			[ 'switch_cooldown', 'switch-cooldown' ],
			[ 'latency_threshold', 'latency-threshold' ],
			[ 'strict_egress_check', 'strict-egress-check' ]
		];
		var patch = {};
		var durationSettings = { refresh_interval: true, health_check_interval: true, url_test_timeout: true, switch_cooldown: true, latency_threshold: true };
		for (var i = 0; i < mappings.length; i++) {
			var key = mappings[i][0];
			var value = durationSettings[key] ? normalizeDuration(this.draft[key]) : String(this.draft[key]);
			var current = durationSettings[key] ? normalizeDuration(this.settings[key]) : String(this.settings[key]);
			if (value === current) continue;
			patch[mappings[i][1]] = value;
		}
		if (!Object.keys(patch).length) {
			fastlaneShell.showToast(_('No settings changed.'), 'info');
			return Promise.resolve();
		}
		this.setSaving(true);
		return this.execJSON([ '--json', 'settings', 'patch', JSON.stringify(patch) ]).then(L.bind(function(settings) {
			this.settings = settings || Object.assign({}, this.draft);
			this.draft = Object.assign({}, this.settings);
			this.syncSettingsControls();
			fastlaneShell.showToast(_('Fast Lane settings saved.'), 'success');
		}, this)).catch(function(err) {
			fastlaneShell.showToast(_('Could not save settings.'), 'error', err.message || String(err));
		}).then(L.bind(function() {
			this.setSaving(false);
		}, this));
	},

	field: function(key, title, hint, type) {
		return E('div', { class: 'fls-field' }, [
			E('label', {}, [ title, E('span', { class: 'fls-hint' }, [ hint ]) ]),
			E('input', { class: 'fls-input', type: type || 'text', 'data-setting-key': key, value: this.draft[key] == null ? '' : String(this.draft[key]), input: L.bind(this.handleInput, this, key) })
		]);
	},

	durationField: function(key, title, hint, units) {
		var parts = durationParts(this.draft[key], units);
		return E('div', { class: 'fls-field' }, [
			E('label', {}, [ title, E('span', { class: 'fls-hint' }, [ hint ]) ]),
			E('div', { class: 'fls-duration', role: 'group', 'aria-label': title }, units.map(L.bind(function(unit) {
				return E('span', { class: 'fls-duration-segment' }, [
					E('input', { class: 'fls-duration-input', type: 'text', inputmode: 'numeric', pattern: '[0-9]*', 'data-setting-key': key, 'data-duration-unit': unit, 'data-duration-units': units.join(','), value: String(parts[unit]), 'aria-label': title + ': ' + durationUnitName(unit), input: L.bind(this.handleDurationInput, this, key, units, unit) }),
					E('span', { class: 'fls-duration-unit', 'aria-hidden': 'true' }, [ unit ])
				]);
			}, this)))
		]);
	},

	render: function(data) {
		this.settings = this.settings || data || {};
		this.draft = this.draft || Object.assign({}, this.settings);
		this.updateBox = E('section', { class: 'fls-card fls-manage fls-update' }, this.renderUpdateContents());
		if (!this.updatePoll) {
			this.updatePoll = L.bind(function() { return this.updateBusy() ? this.refreshUpdate() : Promise.resolve(); }, this);
			poll.add(this.updatePoll, 3);
			poll.start();
		}
		var settingsContent = E('div', { class: 'fastlane-settings' }, [
			E('style', {}, [ css ]),
			E('div', { class: 'fls-head' }, [
				E('div', {}, [ E('h2', {}, [ _('Fast Lane settings') ]), E('p', {}, [ _('Only parameters that affect VPN selection and stability.') ]) ]),
				E('div', { class: 'fls-actions' }, [ E('button', { class: 'fls-button fls-primary', click: ui.createHandlerFn(this, 'handleSaveSettings') }, [ _('Save') ]) ])
			]),
			E('div', { class: 'fls-grid' }, [
				E('section', { class: 'fls-card' }, [ E('h3', {}, [ _('Subscriptions and checks') ]), E('p', {}, [ _('Background updates run automatically; every action can also be started manually on the VPN page.') ]), E('div', { class: 'fls-fields' }, [
					this.durationField('refresh_interval', _('Subscription update'), _('Background update interval'), [ 'h', 'm', 's' ]),
					this.durationField('health_check_interval', _('Automatic server check'), _('0 s disables automatic checks'), [ 's' ]),
					this.field('url_test_url', _('URL test address'), _('HTTPS page with a fast 204 response'), 'url'),
					this.durationField('url_test_timeout', _('URL test timeout'), _('Maximum wait time'), [ 's' ])
				]) ]),
				E('section', { class: 'fls-card' }, [ E('h3', {}, [ _('Automatic selection') ]), E('p', {}, [ _('Fast Lane avoids reconnecting for insignificant latency differences.') ]), E('div', { class: 'fls-fields' }, [
					this.durationField('switch_cooldown', _('Pause between switches'), _('Prevents constant server hopping'), [ 'm', 's' ]),
					this.durationField('latency_threshold', _('Minimum improvement'), _('How much faster a new server must be'), [ 'ms' ]),
					E('div', { class: 'fls-field fls-field-toggle' }, [ E('label', {}, [ _('Strict internet check'), E('span', { class: 'fls-hint' }, [ _('Restore the previous server if HTTPS does not work after connecting') ]) ]), E('label', { class: 'fls-toggle' }, [ E('input', { type: 'checkbox', 'data-setting-key': 'strict_egress_check', checked: this.draft.strict_egress_check ? 'checked' : null, change: L.bind(this.handleBool, this, 'strict_egress_check') }), E('span', { 'data-toggle-label': 'strict_egress_check' }, [ this.draft.strict_egress_check ? _('On') : _('Off') ]) ]) ])
				]) ]),
				E('section', { class: 'fls-card fls-manage' }, [
					E('div', { class: 'fls-manage-copy' }, [ E('h3', {}, [ _('Interface language') ]), E('p', {}, [ _('Automatic follows the LuCI language. Other languages fall back to English.') ]) ]),
					E('select', { class: 'fls-input fls-select', change: L.bind(this.handleLanguageChange, this) }, [
						E('option', { value: 'auto', selected: this.interfaceLanguage === 'auto' ? 'selected' : null }, [ _('Automatic') ]),
						E('option', { value: 'en', selected: this.interfaceLanguage === 'en' ? 'selected' : null }, [ 'English' ]),
						E('option', { value: 'ru', selected: this.interfaceLanguage === 'ru' ? 'selected' : null }, [ 'Русский' ])
					])
				]),
				this.updateBox,
				E('section', { class: 'fls-card fls-manage' }, [
					E('div', { class: 'fls-manage-copy' }, [ E('h3', {}, [ _('Application management') ]), E('p', {}, [ _('Remove other services through the FriendlyWrt package manager. Fast Lane can be removed here.') ]) ]),
					E('div', { class: 'fls-manage-actions' }, [
						E('a', { class: 'fls-button', href: L.url('admin/system/package-manager') }, [ _('FriendlyWrt package manager') ]),
						E('button', { class: 'fls-button fls-danger', click: ui.createHandlerFn(this, 'handleUninstall') }, [ _('Remove Fast Lane') ])
					])
				])
			])
		]);

		this.settingsRoot = settingsContent;
		return E('div', { class: 'fastlane-root' }, [
			fastlaneShell.renderStyles(),
			fastlaneShell.renderHeader('settings'),
			E('main', { class: 'fl-shell' }, [ settingsContent ])
		]);
	},

	handleSaveApply: null,
	handleSave: null,
	handleReset: null
});
