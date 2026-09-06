'use strict';
'require view';
'require fs';
'require ui';
'require dom';
'require fastlane.fastlane-20260906-v4 as fastlaneShell';
'require fastlane.countries as countries';

var binary = '/usr/bin/fastlane';
var geodataHelper = '/usr/libexec/fastlane-geodata';
function trim(value) { return value == null ? '' : String(value).trim(); }
function list(value) { return Array.isArray(value) ? value.filter(function(item) { return trim(item) !== ''; }) : []; }
function tokens(value) { return trim(value).split(/[\s,;]+/).map(trim).filter(Boolean); }
function titleCase(value) {
	value = trim(value).replace(/-/g, ' ');
	return value ? value.charAt(0).toUpperCase() + value.slice(1) : value;
}

var css = `
.flr-page{min-height:680px;color:var(--fl-text)}.flr-head{display:flex;align-items:flex-end;justify-content:space-between;gap:24px;margin-bottom:24px}.flr-head h1{font-size:30px!important;letter-spacing:-.025em!important}.flr-head p{max-width:68ch;margin-top:6px;color:var(--fl-muted);font-size:14px}
.flr-control{display:grid;grid-template-columns:minmax(0,1fr) minmax(260px,360px);gap:30px;align-items:center;padding:28px 30px;border:1px solid var(--fl-line);border-radius:14px;background:var(--fl-panel)}.flr-control h2{font-size:22px!important}.flr-control p{max-width:64ch;margin-top:7px;color:var(--fl-muted);line-height:1.55}.flr-control-actions{display:grid;gap:12px}.flr-country-label{display:grid;gap:9px;color:var(--fl-muted);font-size:12px;font-weight:700}.flr-select{display:block;width:100%;height:52px!important;min-height:52px!important;margin:0!important;padding:0 42px 0 16px!important;border:1px solid var(--fl-line-strong)!important;border-radius:11px!important;background:#050d10!important;color:var(--fl-text)!important;font:inherit!important;font-size:15px!important;line-height:1.4!important;box-shadow:none!important}.flr-switch{position:relative;display:flex;align-items:center;gap:12px;min-height:48px;cursor:pointer}.flr-switch input{position:absolute;opacity:0;pointer-events:none}.flr-switch-track{position:relative;width:58px;height:32px;border:1px solid var(--fl-line-strong);border-radius:18px;background:#101c20;transition:background-color .18s ease,border-color .18s ease}.flr-switch-track:after{content:"";position:absolute;top:4px;left:4px;width:22px;height:22px;border-radius:50%;background:#8e8983;transition:transform .18s cubic-bezier(.2,.8,.2,1),background-color .18s ease}.flr-switch input:checked+.flr-switch-track{border-color:#4fc486;background:#1d6d4b}.flr-switch input:checked+.flr-switch-track:after{transform:translateX(26px);background:#effff5}.flr-switch input:focus-visible+.flr-switch-track{outline:2px solid var(--fl-green);outline-offset:3px}.flr-switch input:disabled+.flr-switch-track{opacity:.5;cursor:wait}.flr-switch-label{color:var(--fl-text);font-weight:700}
.flr-flow{display:grid;grid-template-columns:1fr auto 1fr auto 1fr;align-items:stretch;margin-top:16px;border:1px solid var(--fl-line);border-radius:14px;background:#050d10;overflow:hidden}.flr-step{min-height:122px;padding:22px 24px}.flr-step-label{color:var(--fl-subtle);font-size:12px;font-weight:700;text-transform:uppercase;letter-spacing:.08em}.flr-step-value{margin-top:9px;color:var(--fl-text);font-size:18px;font-weight:700}.flr-step-note{margin-top:4px;color:var(--fl-muted);font-size:12px}.flr-arrow{display:grid;place-items:center;color:var(--fl-subtle);font-size:22px}.flr-ready{color:var(--fl-green)}.flr-warn{color:var(--fl-amber)}
.flr-actions{display:flex;align-items:center;gap:10px;margin-top:16px}.flr-button{min-height:44px;padding:10px 15px;border:1px solid var(--fl-line-strong)!important;border-radius:9px!important;background:#0d1c21!important;color:var(--fl-text)!important;font:inherit;font-weight:700;cursor:pointer}.flr-button:hover{border-color:#426068!important;background:#102329!important}.flr-button:focus-visible,.flr-input:focus-visible,.flr-select:focus-visible{outline:2px solid var(--fl-green);outline-offset:3px}.flr-button:disabled{opacity:.5;cursor:wait}.flr-button-primary{border-color:#4dbb82!important;background:#43ad77!important;color:#f6fff9!important}.flr-button-primary:hover{background:#50c187!important}.flr-progress{color:var(--fl-muted);font-size:13px}
.flr-bypass{margin-top:22px;border:1px solid var(--fl-line);border-radius:14px;background:var(--fl-panel);overflow:hidden}.flr-bypass-head{display:flex;align-items:center;justify-content:space-between;gap:24px;padding:24px 28px}.flr-bypass-head h2{font-size:22px!important}.flr-bypass-head p{max-width:72ch;margin-top:6px;color:var(--fl-muted);font-size:13px;line-height:1.5}.flr-rule{border-top:1px solid var(--fl-line);padding:20px 28px;background:#050d10}.flr-rule-active{box-shadow:inset 3px 0 0 var(--fl-green)}.flr-rule-head{display:flex;align-items:center;gap:18px}.flr-rule-title{min-width:0;flex:1}.flr-rule-title h3{font-size:17px!important}.flr-rule-meta{margin-top:3px;color:var(--fl-muted);font-size:12px}.flr-rule-actions{display:flex;align-items:center;gap:8px}.flr-rule-actions .flr-button{min-height:38px;padding:8px 12px;font-size:13px}.flr-button-danger{border-color:rgba(255,85,95,.35)!important;background:rgba(255,85,95,.04)!important;color:#ff737d!important}.flr-button-danger:hover{border-color:#ff737d!important;background:rgba(255,85,95,.1)!important}.flr-rule-grid{display:grid;grid-template-columns:1fr 1fr;gap:22px;margin-top:16px}.flr-rule-column{min-width:0}.flr-rule-column-title{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:8px;color:var(--fl-muted);font-size:12px;font-weight:700}.flr-rule-values{display:flex;flex-wrap:wrap;gap:7px}.flr-rule-value{max-width:100%;padding:6px 9px;border:1px solid var(--fl-line);border-radius:7px;background:#091519;color:var(--fl-text);font:12px/1.35 ui-monospace,SFMono-Regular,Menlo,monospace;overflow-wrap:anywhere}.flr-rule-empty{color:var(--fl-subtle);font-size:12px}.flr-bypass-empty{border-top:1px solid var(--fl-line);padding:30px 28px;color:var(--fl-muted);font-size:13px}.flr-bypass-error{border-top:1px solid rgba(255,85,95,.25);padding:15px 28px;background:rgba(255,85,95,.05);color:#ffb0b5;font-size:13px}
.flr-rule-switch{position:relative;display:flex;align-items:center;gap:9px;cursor:pointer}.flr-rule-switch input{position:absolute;opacity:0;pointer-events:none}.flr-rule-switch-track{position:relative;width:46px;height:26px;border:1px solid var(--fl-line-strong);border-radius:14px;background:#101c20}.flr-rule-switch-track:after{content:"";position:absolute;top:3px;left:3px;width:18px;height:18px;border-radius:50%;background:#8e8983;transition:transform .18s ease,background-color .18s ease}.flr-rule-switch input:checked+.flr-rule-switch-track{border-color:#4fc486;background:#1d6d4b}.flr-rule-switch input:checked+.flr-rule-switch-track:after{transform:translateX(20px);background:#effff5}.flr-rule-switch input:focus-visible+.flr-rule-switch-track{outline:2px solid var(--fl-green);outline-offset:3px}.flr-rule-switch-label{min-width:28px;color:var(--fl-muted);font-size:12px;font-weight:700}.flr-rule-active .flr-rule-switch-label{color:var(--fl-green)}
.flr-rule-form{display:grid;gap:16px;min-width:min(610px,80vw);padding:10px 24px 20px;color:var(--fl-text)}.flr-form-field{display:grid;gap:7px}.flr-form-label{font-size:14px;font-weight:700}.flr-form-help{color:var(--fl-muted);font-size:12px;line-height:1.45}.flr-rule-form input,.flr-rule-form textarea{position:static!important;width:100%!important;margin:0!important;border:1px solid var(--fl-line-strong)!important;border-radius:11px!important;background:#030b0e!important;color:var(--fl-text)!important;padding:12px 14px!important;font:inherit!important;line-height:1.45!important;box-shadow:none!important}.flr-rule-form input{height:48px!important}.flr-rule-form textarea{min-height:126px!important;resize:vertical}.flr-rule-form input:disabled{opacity:.65}.flr-rule-form input:focus-visible,.flr-rule-form textarea:focus-visible{border-color:var(--fl-green)!important;outline:2px solid var(--fl-green)!important;outline-offset:2px}.flr-form-error{min-height:0;padding:10px 12px;border:1px solid rgba(255,85,95,.3);border-radius:9px;background:rgba(255,85,95,.06);color:#ffb0b5;font-size:12px}.flr-form-error:empty{display:none}
body:has(.fastlane-routing-modal) #modal_overlay{position:fixed!important;inset:0!important;display:grid!important;place-items:center!important;padding:24px!important;background:rgba(0,6,8,.78)!important;overflow-y:auto!important}.fastlane-routing-modal{position:relative!important;inset:auto!important;width:min(660px,calc(100vw - 32px))!important;max-width:660px!important;max-height:calc(100vh - 48px)!important;margin:auto!important;padding:0!important;border:1px solid var(--fl-line-strong)!important;border-radius:16px!important;background:#071115!important;color:#ddd4ca!important;box-shadow:0 24px 70px rgba(0,0,0,.56)!important;overflow:auto!important;font-family:"Avenir Next",ui-sans-serif,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif!important}.fastlane-routing-modal h4{margin:0!important;padding:24px 24px 8px!important;border:0!important;background:transparent!important;color:#ddd4ca!important;font-size:22px!important;font-weight:750!important;letter-spacing:-.02em!important}.fastlane-routing-modal .right{display:flex!important;justify-content:flex-end!important;gap:10px!important;margin:0!important;padding:16px 24px 24px!important;border:0!important;border-top:1px solid var(--fl-line)!important;background:#071115!important}.fastlane-routing-modal .flr-modal-button{min-width:112px!important;min-height:44px!important;margin:0!important;border:1px solid var(--fl-line-strong)!important;border-radius:10px!important;padding:10px 16px!important;background:#0d1c21!important;color:var(--fl-text)!important;font:inherit!important;font-size:14px!important;font-weight:700!important;line-height:1!important;box-shadow:none!important;cursor:pointer}.fastlane-routing-modal .flr-modal-button:hover{border-color:#426068!important;background:#10252b!important}.fastlane-routing-modal .flr-modal-primary{border-color:#45a974!important;background:#45a974!important;color:#04110a!important}.fastlane-routing-modal .flr-modal-primary:hover{border-color:#54df91!important;background:#54df91!important}.fastlane-routing-modal .flr-modal-button:disabled{opacity:.5;cursor:wait}.fastlane-routing-modal .flr-modal-button:focus-visible{outline:2px solid var(--fl-green)!important;outline-offset:3px!important}body:not(.modal-overlay-active) #modal_overlay:has(.fastlane-routing-modal){display:none!important}
.flr-advanced{margin-top:22px;border-top:1px solid var(--fl-line);padding-top:18px}.flr-advanced summary{display:flex;align-items:center;justify-content:space-between;min-height:44px;color:var(--fl-text);font-weight:700;cursor:pointer;list-style:none}.flr-advanced summary::-webkit-details-marker{display:none}.flr-advanced summary:after{content:"+";color:var(--fl-green);font-size:20px}.flr-advanced[open] summary:after{content:"−"}.flr-advanced-copy{max-width:72ch;margin:2px 0 14px;color:var(--fl-muted);font-size:13px;line-height:1.5}.flr-import{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:18px;align-items:stretch}.flr-import .flr-button{min-width:176px}.flr-input{width:100%;min-height:46px!important;padding:10px 13px!important;border:1px solid var(--fl-line-strong)!important;border-radius:9px!important;background:#050d10!important;color:var(--fl-text)!important;font:inherit!important}.flr-input::placeholder{color:var(--fl-subtle)}.flr-preview{margin-top:12px;padding:14px 16px;border:1px solid var(--fl-line);border-radius:10px;background:#050d10;color:var(--fl-muted);font-size:13px;line-height:1.55}.flr-preview strong{color:var(--fl-text)}
@media(max-width:900px){.flr-head{align-items:flex-start;flex-direction:column}.flr-control{grid-template-columns:1fr}.flr-flow{grid-template-columns:1fr}.flr-arrow{height:28px;transform:rotate(90deg)}.flr-step{min-height:0}.flr-import{grid-template-columns:1fr}.flr-bypass-head,.flr-rule-head{align-items:stretch;flex-direction:column}.flr-bypass-head .flr-button{width:100%}.flr-rule-actions{display:grid;grid-template-columns:1fr 1fr}.flr-rule-actions .flr-button{width:100%}.flr-rule-grid{grid-template-columns:1fr}.flr-rule-switch{align-self:flex-start}.flr-button{width:100%}}@media(max-width:560px){body:has(.fastlane-routing-modal) #modal_overlay{place-items:end center!important;padding:12px!important}.fastlane-routing-modal{width:calc(100vw - 24px)!important;max-height:calc(100vh - 24px)!important;border-radius:14px!important}.fastlane-routing-modal h4{padding:20px 18px 6px!important;font-size:20px!important}.flr-bypass-head,.flr-rule,.flr-bypass-empty,.flr-bypass-error{padding-left:18px;padding-right:18px}.fastlane-routing-modal .right{padding:14px 18px 18px!important}.flr-rule-form{min-width:0;padding:10px 18px 16px}.fastlane-routing-modal .flr-modal-button{flex:1;min-width:0!important}}@media(prefers-reduced-motion:reduce){.flr-switch-track,.flr-switch-track:after,.flr-rule-switch-track:after{transition:none}}
`;

function decodeHappLink(raw) {
	var prefix = 'happ://routing/onadd/';
	var value = trim(raw);
	if (value.indexOf(prefix) !== 0) throw new Error(_('The link must start with happ://routing/onadd/.'));
	var encoded = value.slice(prefix.length).replace(/-/g, '+').replace(/_/g, '/');
	while (encoded.length % 4) encoded += '=';
	var bytes = atob(encoded);
	var escaped = '';
	for (var i = 0; i < bytes.length; i++) escaped += '%' + ('0' + bytes.charCodeAt(i).toString(16)).slice(-2);
	var profile = JSON.parse(decodeURIComponent(escaped));
	if (!profile || typeof profile !== 'object') throw new Error(_('The link does not contain a routing profile.'));
	return profile;
}

function suggestedCountry() {
	var locale = ((document.documentElement && document.documentElement.lang) || (window.navigator && window.navigator.language) || '').toLowerCase();
	if (locale.indexOf('ru') === 0) return 'RU';
	if (locale.indexOf('zh') === 0) return 'CN';
	if (locale.indexOf('fa') === 0) return 'IR';
	return '';
}

return view.extend({
	load: function() {
		return Promise.all([
			this.execJSON([ '--json', 'settings', 'get' ]),
			fs.exec(geodataHelper, [ 'status' ]).then(function(result) {
				if (result.code !== 0) return { ready: false, error: trim(result.stderr || result.stdout) };
				try { return JSON.parse(trim(result.stdout)); }
				catch (err) { return { ready: false, error: _('Could not read Geo database status.') }; }
			}),
			this.execJSON([ '--json', 'services', 'list' ]).then(function(value) {
				return { value: Array.isArray(value) ? value : [] };
			}).catch(function(err) {
				return { value: [], error: err.message || String(err) };
			})
		]).then(L.bind(function(data) {
			this.settings = data[0] || {};
			this.countryCode = trim(this.settings.country_routing && this.settings.country_routing.country_code) || suggestedCountry();
			this.geodata = data[1] || { ready: false };
			this.services = data[2] && data[2].value || [];
			this.rulesError = data[2] && data[2].error || '';
			this.busy = false;
			this.ruleBusy = '';
			this.progress = '';
			return data;
		}, this));
	},

	execJSON: function(args) {
		return fs.exec(binary, args).then(function(result) {
			if (result.code !== 0) throw new Error(trim(result.stderr || result.stdout));
			return JSON.parse(trim(result.stdout));
		});
	},

	execText: function(args) {
		return fs.exec(binary, args).then(function(result) {
			if (result.code !== 0) throw new Error(trim(result.stderr || result.stdout));
			return trim(result.stdout);
		});
	},

	renderAgain: function() {
		var root = document.querySelector('.flr-page');
		if (root) dom.content(root, this.renderContent());
	},

	geodataPollInterval: 3000,
	geodataPollAttempts: 300,

	parseGeoStatus: function(result) {
		if (result.code !== 0) throw new Error(trim(result.stderr || result.stdout));
		try { return JSON.parse(trim(result.stdout)); }
		catch (err) { throw new Error(_('Could not read Geo database status.')); }
	},

	readGeoStatus: function() { return fs.exec(geodataHelper, [ 'status' ]).then(L.bind(this.parseGeoStatus, this)); },

	finishGeoUpdate: function(status) {
		if (status && status.last_result === 'error') throw new Error(trim(status.message) || _('GeoIP and GeoSite update failed.'));
		if (!status || status.ready !== true || status.last_result !== 'ok') throw new Error(trim(status && status.message) || _('GeoIP and GeoSite did not pass validation.'));
		return status;
	},

	waitForGeoUpdate: function(attempt) {
		if (attempt >= this.geodataPollAttempts) return Promise.reject(new Error(_('The update did not finish in time. It may still be running in the background.')));
		return new Promise(L.bind(function(resolve) { window.setTimeout(resolve, this.geodataPollInterval); }, this)).then(L.bind(function() {
			return this.readGeoStatus();
		}, this)).then(L.bind(function(status) {
			this.geodata = status;
			this.renderAgain();
			if (status.updating === true || status.last_result === 'updating') return this.waitForGeoUpdate(attempt + 1);
			return this.finishGeoUpdate(status);
		}, this));
	},

	startGeoUpdate: function() {
		return fs.exec(geodataHelper, [ 'start' ]).then(L.bind(function(result) {
			var status = this.parseGeoStatus(result);
			this.geodata = status;
			this.renderAgain();
			if (status.updating === true || status.last_result === 'updating') return this.waitForGeoUpdate(0);
			return this.finishGeoUpdate(status);
		}, this));
	},

	handleToggle: function(ev) {
		if (ev) ev.preventDefault();
		if (this.busy) return Promise.resolve();
		var firewall = this.settings && this.settings.firewall || {};
		var countryRouting = this.settings && this.settings.country_routing || {};
		var enable = !(countryRouting.enabled === true && firewall.enabled === true);
		if (enable && !this.countryCode) {
			fastlaneShell.showToast(_('Choose a country first.'), 'error');
			return Promise.resolve();
		}
		this.busy = true;
		this.progress = enable ? _('Preparing routing…') : _('Turning the rule off…');
		this.renderAgain();
		var prepare = Promise.resolve();
		if (enable && !this.geodata.ready) {
			this.progress = _('Downloading and validating GeoIP and GeoSite…');
			this.renderAgain();
			prepare = this.startGeoUpdate().then(L.bind(function(status) { this.geodata = status; }, this));
		}
		return prepare.then(L.bind(function() {
			this.progress = enable ? _('Turning local-country routing on…') : _('Turning the rule off…');
			this.renderAgain();
			return this.execJSON([ '--json', 'settings', 'patch', JSON.stringify({ country_direct: enable, direct_country: this.countryCode }) ]);
		}, this)).then(L.bind(function(settings) {
			this.settings = settings || this.settings;
			this.busy = false;
			this.progress = '';
			this.renderAgain();
			fastlaneShell.showToast(enable ? _('Local-country routing is on. LAN and the selected country go directly.') : _('Local-country routing is off.'), 'success');
		}, this)).catch(L.bind(function(err) {
			this.busy = false;
			this.progress = '';
			this.renderAgain();
			fastlaneShell.showToast(_('Could not change routing.'), 'error', err.message || String(err));
		}, this));
	},

	handleCountryChange: function(ev) {
		if (this.busy) return Promise.resolve();
		var previous = this.countryCode;
		this.countryCode = trim(ev && ev.target && ev.target.value).toUpperCase();
		if (!this.countryCode) return Promise.resolve();
		var enabled = this.settings && this.settings.country_routing && this.settings.country_routing.enabled === true;
		this.busy = true;
		this.progress = enabled ? _('Applying the selected country…') : _('Saving the selected country…');
		this.renderAgain();
		return this.execJSON([ '--json', 'settings', 'patch', JSON.stringify({ direct_country: this.countryCode }) ]).then(L.bind(function(settings) {
			this.settings = settings || this.settings;
			this.busy = false;
			this.progress = '';
			this.renderAgain();
			fastlaneShell.showToast(_('Country saved.'), 'success');
		}, this)).catch(L.bind(function(err) {
			this.countryCode = previous;
			this.busy = false;
			this.progress = '';
			this.renderAgain();
			fastlaneShell.showToast(_('Could not save the country.'), 'error', err.message || String(err));
		}, this));
	},

	handleGeoUpdate: function(ev) {
		if (ev) ev.preventDefault();
		if (this.busy) return Promise.resolve();
		this.busy = true;
		this.progress = _('Downloading and validating GeoIP and GeoSite…');
		this.renderAgain();
		return this.startGeoUpdate().then(L.bind(function(status) {
			this.geodata = status;
			this.busy = false;
			this.progress = '';
			this.renderAgain();
			fastlaneShell.showToast(_('GeoIP and GeoSite are up to date.'), 'success');
		}, this)).catch(L.bind(function(err) {
			this.busy = false;
			this.progress = '';
			this.renderAgain();
			fastlaneShell.showToast(_('Could not update GeoIP and GeoSite.'), 'error', err.message || String(err));
		}, this));
	},

	customRules: function() {
		var services = Array.isArray(this.services) ? this.services : [];
		return services.filter(function(service) {
			return service && service.readonly !== true && trim(service.source) === 'custom';
		}).sort(function(a, b) { return trim(a.name).localeCompare(trim(b.name)); });
	},

	bypassSettings: function() {
		var firewall = this.settings && this.settings.firewall || {};
		return firewall.split && firewall.split.bypass || {};
	},

	isRuleActive: function(name) {
		return list(this.bypassSettings().services).indexOf(name) >= 0;
	},

	bypassSelectors: function(name, enabled) {
		var bypass = this.bypassSettings();
		var services = list(bypass.services).filter(function(item) { return item !== name; });
		if (enabled && services.indexOf(name) < 0) services.push(name);
		return services.concat(list(bypass.domains), list(bypass.cidrs));
	},

	bypassCommand: function(name, enabled) {
		var args = [ '--json', 'firewall', 'set', 'bypass' ].concat(this.bypassSelectors(name, enabled));
		var split = this.settings && this.settings.firewall && this.settings.firewall.split || {};
		list(split.excluded_sources).forEach(function(source) { args.push('--exclude-host', source); });
		return args;
	},

	refreshRules: function() {
		return Promise.all([
			this.execJSON([ '--json', 'settings', 'get' ]),
			this.execJSON([ '--json', 'services', 'list' ])
		]).then(L.bind(function(data) {
			this.settings = data[0] || this.settings;
			this.services = Array.isArray(data[1]) ? data[1] : [];
			this.rulesError = '';
			return data;
		}, this));
	},

	finishRuleAction: function(message) {
		return this.refreshRules().then(L.bind(function() {
			this.busy = false;
			this.ruleBusy = '';
			this.renderAgain();
			if (message) fastlaneShell.showToast(message, 'success');
		}, this));
	},

	failRuleAction: function(err, fallback) {
		this.busy = false;
		this.ruleBusy = '';
		this.renderAgain();
		fastlaneShell.showToast(fallback, 'error', err && err.message || String(err));
	},

	handleRuleToggle: function(name, ev) {
		if (this.busy) return Promise.resolve();
		var enabled = !!(ev && ev.target && ev.target.checked);
		this.busy = true;
		this.ruleBusy = name;
		this.renderAgain();
		return this.execJSON(this.bypassCommand(name, enabled)).then(L.bind(function() {
			return this.finishRuleAction(enabled ? _('Exclusion enabled.') : _('Exclusion disabled.'));
		}, this)).catch(L.bind(function(err) {
			this.failRuleAction(err, _('Could not change the exclusion.'));
		}, this));
	},

	handleRuleDelete: function(name, ev) {
		if (ev) ev.preventDefault();
		if (this.busy || !window.confirm(_('Delete this exclusion? Traffic matching it will follow the regular VPN route.'))) return Promise.resolve();
		this.busy = true;
		this.ruleBusy = name;
		this.renderAgain();
		var prepare = this.isRuleActive(name)
			? this.execJSON(this.bypassCommand(name, false))
			: Promise.resolve();
		return prepare.then(L.bind(function() {
			return this.execText([ '--json', 'services', 'delete', name ]);
		}, this)).then(L.bind(function() {
			return this.finishRuleAction(_('Exclusion deleted.'));
		}, this)).catch(L.bind(function(err) {
			this.failRuleAction(err, _('Could not delete the exclusion.'));
		}, this));
	},

	handleRuleOpen: function(rule, ev) {
		if (ev) ev.preventDefault();
		if (this.busy) return;
		var editing = !!rule;
		var nameInput = E('input', { type: 'text', value: editing ? trim(rule.name) : '', placeholder: 'roborock', disabled: editing ? 'disabled' : null, autocomplete: 'off', spellcheck: 'false' });
		var domainsInput = E('textarea', { placeholder: 'roborock.com\nmiot-spec.org', spellcheck: 'false', autocapitalize: 'none' }, [ list(rule && rule.domains).join('\n') ]);
		var cidrsInput = E('textarea', { placeholder: '192.0.2.10\n192.0.2.0/24', spellcheck: 'false', autocapitalize: 'none' }, [ list(rule && rule.cidrs).join('\n') ]);
		domainsInput.value = list(rule && rule.domains).join('\n');
		cidrsInput.value = list(rule && rule.cidrs).join('\n');
		var errorBox = E('div', { class: 'flr-form-error', 'aria-live': 'polite' });
		var submit = E('button', { class: 'flr-modal-button flr-modal-primary fl-dialog-button fl-dialog-primary', type: 'button' }, [ editing ? _('Save') : _('Add') ]);
		submit.addEventListener('click', L.bind(this.handleRuleSubmit, this, rule || null, nameInput, domainsInput, cidrsInput, errorBox, submit));
		ui.showModal(editing ? _('Edit exclusion') : _('Add exclusion'), [
			E('div', { class: 'flr-rule-form fl-dialog-form' }, [
				E('label', { class: 'flr-form-field fl-dialog-field' }, [ E('span', { class: 'flr-form-label fl-dialog-label' }, [ _('Name') ]), nameInput, E('span', { class: 'flr-form-help fl-dialog-help' }, [ _('Lowercase Latin letters, digits, and hyphens. The name cannot be changed later.') ]) ]),
				E('label', { class: 'flr-form-field fl-dialog-field' }, [ E('span', { class: 'flr-form-label fl-dialog-label' }, [ _('Domains') ]), domainsInput, E('span', { class: 'flr-form-help fl-dialog-help' }, [ _('One domain per line. Subdomains are included automatically.') ]) ]),
				E('label', { class: 'flr-form-field fl-dialog-field' }, [ E('span', { class: 'flr-form-label fl-dialog-label' }, [ _('IP addresses and networks') ]), cidrsInput, E('span', { class: 'flr-form-help fl-dialog-help' }, [ _('One IPv4 address, CIDR, or range per line.') ]) ]),
				errorBox
			]),
			E('div', { class: 'right fl-dialog-actions' }, [ E('button', { class: 'flr-modal-button fl-dialog-button', type: 'button', click: ui.hideModal }, [ _('Cancel') ]), submit ])
		]);
		var modal = document.querySelector('.modal');
		if (modal) {
			modal.classList.add('fastlane-routing-modal');
			modal.classList.add('fl-dialog');
		}
		window.requestAnimationFrame(function() { (editing ? domainsInput : nameInput).focus(); });
	},

	handleRuleSubmit: function(rule, nameInput, domainsInput, cidrsInput, errorBox, submit, ev) {
		if (ev) ev.preventDefault();
		var name = trim(rule && rule.name || nameInput.value).toLowerCase();
		var domains = tokens(domainsInput.value);
		var cidrs = tokens(cidrsInput.value);
		if (!/^[a-z][a-z0-9-]*$/.test(name)) {
			errorBox.textContent = _('Use lowercase Latin letters, digits, and hyphens; start with a letter.');
			nameInput.focus();
			return Promise.resolve();
		}
		if (!domains.length && !cidrs.length && !list(rule && rule.services).length) {
			errorBox.textContent = _('Add at least one domain or IP address.');
			domainsInput.focus();
			return Promise.resolve();
		}
		errorBox.textContent = '';
		submit.disabled = true;
		submit.textContent = rule ? _('Saving…') : _('Adding…');
		var selectors = list(rule && rule.services).concat(domains, cidrs);
		return this.execJSON([ '--json', 'services', 'set', name ].concat(selectors)).then(L.bind(function() {
			if (rule || this.isRuleActive(name)) return null;
			return this.execJSON(this.bypassCommand(name, true));
		}, this)).then(L.bind(function() {
			ui.hideModal();
			return this.finishRuleAction(rule ? _('Exclusion saved.') : _('Exclusion added and enabled.'));
		}, this)).catch(L.bind(function(err) {
			errorBox.textContent = err && err.message || String(err);
			submit.disabled = false;
			submit.textContent = rule ? _('Save') : _('Add');
		}, this));
	},

	renderRuleValues: function(values, emptyText) {
		values = list(values);
		if (!values.length) return E('div', { class: 'flr-rule-empty' }, [ emptyText ]);
		return E('div', { class: 'flr-rule-values' }, values.map(function(value) { return E('code', { class: 'flr-rule-value' }, [ value ]); }));
	},

	renderBypassRules: function() {
		var rules = this.customRules();
		var body = [];
		if (this.rulesError) body.push(E('div', { class: 'flr-bypass-error' }, [ _('Could not load exclusions.'), ' ', this.rulesError ]));
		if (!rules.length && !this.rulesError) body.push(E('div', { class: 'flr-bypass-empty' }, [ _('No exclusions yet. Add a service or device cloud that must use the regular internet connection.') ]));
		for (var i = 0; i < rules.length; i++) {
			var rule = rules[i];
			var active = this.isRuleActive(rule.name);
			var domainCount = list(rule.domains).length;
			var cidrCount = list(rule.cidrs).length;
			body.push(E('article', { class: 'flr-rule ' + (active ? 'flr-rule-active' : '') }, [
				E('div', { class: 'flr-rule-head' }, [
					E('div', { class: 'flr-rule-title' }, [ E('h3', {}, [ titleCase(rule.name) ]), E('div', { class: 'flr-rule-meta' }, [ domainCount + ' ' + _('domains') + ' · ' + cidrCount + ' ' + _('IP entries') ]) ]),
					E('label', { class: 'flr-rule-switch' }, [ E('input', { type: 'checkbox', checked: active ? 'checked' : null, disabled: this.busy ? 'disabled' : null, change: L.bind(this.handleRuleToggle, this, rule.name) }), E('span', { class: 'flr-rule-switch-track' }), E('span', { class: 'flr-rule-switch-label' }, [ active ? _('On') : _('Off') ]) ]),
					E('div', { class: 'flr-rule-actions' }, [ E('button', { class: 'flr-button', disabled: this.busy ? 'disabled' : null, click: ui.createHandlerFn(this, 'handleRuleOpen', rule) }, [ _('Edit') ]), E('button', { class: 'flr-button flr-button-danger', disabled: this.busy ? 'disabled' : null, click: ui.createHandlerFn(this, 'handleRuleDelete', rule.name) }, [ _('Delete') ]) ])
				]),
				E('div', { class: 'flr-rule-grid' }, [
					E('div', { class: 'flr-rule-column' }, [ E('div', { class: 'flr-rule-column-title' }, [ E('span', {}, [ _('Domains') ]), E('span', {}, [ String(domainCount) ]) ]), this.renderRuleValues(rule.domains, _('No domains')) ]),
					E('div', { class: 'flr-rule-column' }, [ E('div', { class: 'flr-rule-column-title' }, [ E('span', {}, [ _('IP addresses and networks') ]), E('span', {}, [ String(cidrCount) ]) ]), this.renderRuleValues(rule.cidrs, _('No IP addresses')) ])
				])
			]));
		}
		return E('section', { class: 'flr-bypass' }, [
			E('div', { class: 'flr-bypass-head' }, [ E('div', {}, [ E('h2', {}, [ _('Direct access exclusions') ]), E('p', {}, [ _('Traffic matching an enabled group bypasses VPN. Everything else follows the route above.') ]) ]), E('button', { class: 'flr-button flr-button-primary', disabled: this.busy ? 'disabled' : null, click: ui.createHandlerFn(this, 'handleRuleOpen', null) }, [ '+ ', _('Add exclusion') ]) ])
		].concat(body));
	},

	handleImportCheck: function(ev) {
		if (ev) ev.preventDefault();
		try {
			this.importProfile = decodeHappLink(this.importValue);
			this.importError = '';
			this.renderAgain();
		} catch (err) {
			this.importProfile = null;
			this.importError = err.message || String(err);
			this.renderAgain();
			fastlaneShell.showToast(_('Could not read the HAPP profile.'), 'error', this.importError);
		}
	},

	renderImportPreview: function() {
		if (!this.importProfile) return this.importError ? E('div', { class: 'flr-preview' }, [ this.importError ]) : '';
		var profile = this.importProfile;
		var directCount = (profile.DirectSites || []).length + (profile.DirectIp || []).length;
		var proxyCount = (profile.ProxySites || []).length + (profile.ProxyIp || []).length;
		var blockCount = (profile.BlockSites || []).length + (profile.BlockIp || []).length;
		return E('div', { class: 'flr-preview' }, [
			E('strong', {}, [ trim(profile.Name) || _('HAPP profile') ]),
			E('div', {}, [ _('Detected: direct') + ' — ' + directCount + ', ' + _('through VPN') + ' — ' + proxyCount + ', ' + _('blocked') + ' — ' + blockCount + '.' ]),
			E('div', {}, [ _('The link is valid. Partial application is disabled until Fast Lane can apply custom direct, proxy, and block rules atomically.') ])
		]);
	},

	renderContent: function() {
		var firewall = this.settings && this.settings.firewall || {};
		var countryRouting = this.settings && this.settings.country_routing || {};
		var enabled = countryRouting.enabled === true && firewall.enabled === true;
		var countryCode = this.countryCode || trim(countryRouting.country_code);
		var countryName = countryCode ? countries.name(countryCode) : _('Not selected');
		var firewallMode = trim(firewall.mode || 'disabled');
		var defaultAction = trim(firewall.split && firewall.split.default_action || 'direct');
		var restValue = _('Direct');
		var restNote = _('Fast Lane routing is off');
		if (firewall.enabled === true && firewallMode === 'split' && defaultAction === 'proxy') {
			restValue = _('Through VPN');
			restNote = _('Except the enabled exclusions below');
		} else if (firewall.enabled === true && firewallMode === 'hosts') {
			restValue = _('By device');
			restNote = _('VPN is used only for selected devices');
		} else if (firewall.enabled === true) {
			restValue = _('By rule');
			restNote = _('VPN is used only for selected destinations');
		}
		var geoReady = this.geodata && this.geodata.ready === true;
		return [
			E('div', { class: 'flr-head' }, [
				E('div', {}, [ E('h1', {}, [ _('Routing') ]), E('p', {}, [ _('Choose which traffic goes directly and which goes through VPN. Geo databases are managed automatically.') ]) ])
			]),
			E('section', { class: 'flr-control' }, [
				E('div', {}, [ E('h2', {}, [ _('Local-country traffic directly') ]), E('p', {}, [ _('LAN and traffic for the selected country bypass VPN. All other internet traffic follows the active Fast Lane routing policy.') ]) ]),
				E('div', { class: 'flr-control-actions' }, [
					E('label', { class: 'flr-country-label' }, [ _('Country'), E('select', { class: 'flr-select', disabled: this.busy ? 'disabled' : null, change: L.bind(this.handleCountryChange, this) }, [ E('option', { value: '', selected: countryCode ? null : 'selected' }, [ _('Choose a country') ]) ].concat(countries.options(countryCode))) ]),
					E('label', { class: 'flr-switch' }, [ E('input', { type: 'checkbox', checked: enabled ? 'checked' : null, disabled: this.busy || !countryCode ? 'disabled' : null, change: L.bind(this.handleToggle, this) }), E('span', { class: 'flr-switch-track' }), E('span', { class: 'flr-switch-label' }, [ enabled ? _('On') : _('Off') ]) ])
				])
			]),
			E('div', { class: 'flr-flow' }, [
				E('div', { class: 'flr-step' }, [ E('div', { class: 'flr-step-label' }, [ _('Home network') ]), E('div', { class: 'flr-step-value flr-ready' }, [ _('Always direct') ]), E('div', { class: 'flr-step-note' }, [ _('Admin panel and local devices') ]) ]),
				E('div', { class: 'flr-arrow', 'aria-hidden': 'true' }, [ '→' ]),
				E('div', { class: 'flr-step' }, [ E('div', { class: 'flr-step-label' }, [ countryName ]), E('div', { class: 'flr-step-value ' + (enabled ? 'flr-ready' : 'flr-warn') }, [ enabled ? _('Direct') : _('Through VPN') ]), E('div', { class: 'flr-step-note' }, [ enabled ? _('Managed GeoIP rule') + (countryCode === 'RU' || countryCode === 'CN' ? ' + GeoSite' : '') : _('Rule is off') ]) ]),
				E('div', { class: 'flr-arrow', 'aria-hidden': 'true' }, [ '→' ]),
				E('div', { class: 'flr-step' }, [ E('div', { class: 'flr-step-label' }, [ _('Rest of the internet') ]), E('div', { class: 'flr-step-value' }, [ restValue ]), E('div', { class: 'flr-step-note' }, [ restNote ]) ])
			]),
			E('div', { class: 'flr-actions' }, [
				E('button', { class: 'flr-button ' + (!geoReady ? 'flr-button-primary' : ''), disabled: this.busy ? 'disabled' : null, click: ui.createHandlerFn(this, 'handleGeoUpdate') }, [ geoReady ? _('Update Geo databases') : _('Install Geo databases') ]),
				E('span', { class: 'flr-progress', 'aria-live': 'polite' }, [ this.progress || (geoReady ? _('GeoIP and GeoSite are ready') : _('Databases will be installed automatically when routing is first enabled')) ])
			]),
			this.renderBypassRules(),
			E('details', { class: 'flr-advanced' }, [
				E('summary', {}, [ _('Import and advanced rules') ]),
				E('p', { class: 'flr-advanced-copy' }, [ _('You can validate a HAPP link. Fast Lane shows its contents and never silently drops unsupported rules.') ]),
				E('div', { class: 'flr-import' }, [ E('input', { class: 'flr-input', type: 'text', placeholder: 'happ://routing/onadd/…', value: this.importValue || '', input: L.bind(function(ev) { this.importValue = ev.target.value; }, this) }), E('button', { class: 'flr-button', click: ui.createHandlerFn(this, 'handleImportCheck') }, [ _('Validate link') ]) ]),
				this.renderImportPreview()
			])
		];
	},

	render: function(data) {
		this.settings = this.settings || (data && data[0]) || {};
		this.geodata = this.geodata || (data && data[1]) || { ready: false };
		this.services = this.services || (data && data[2] && data[2].value) || [];
		this.rulesError = this.rulesError || (data && data[2] && data[2].error) || '';
		return E('div', { class: 'fastlane-root' }, [
			fastlaneShell.renderStyles(), E('style', {}, [ css ]), fastlaneShell.renderHeader('routing'),
			E('main', { class: 'fl-shell' }, [ E('div', { class: 'flr-page' }, this.renderContent()) ])
		]);
	},

	handleSaveApply: null,
	handleSave: null,
	handleReset: null
});
