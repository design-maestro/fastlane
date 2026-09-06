'use strict';
'require baseclass';
'require ui';

var themePreferenceKey = 'fastlane.ui.theme.preference';

function trim(value) {
	if (value == null)
		return '';

	return String(value).trim();
}

function hasContent(value) {
	if (Array.isArray(value))
		return value.length > 0;

	return trim(value) !== '';
}

function pad2(value) {
	value = Number(value) || 0;
	return value < 10 ? '0' + value : String(value);
}

function appendClass(base, extra) {
	var suffix = trim(extra);

	if (suffix === '')
		return base;

	return trim(base + ' ' + suffix);
}

function normalizeChildren(value) {
	return Array.isArray(value) ? value : [ value ];
}

function readLocalStorageValue(key) {
	var normalizedKey = trim(key);

	if (normalizedKey === '' || typeof window === 'undefined' || !window.localStorage)
		return null;

	try {
		return window.localStorage.getItem(normalizedKey);
	}
	catch (err) {
		return null;
	}
}

function writeLocalStorageValue(key, value) {
	var normalizedKey = trim(key);

	if (normalizedKey === '' || typeof window === 'undefined' || !window.localStorage)
		return;

	try {
		window.localStorage.setItem(normalizedKey, String(value));
	}
	catch (err) {
	}
}

function parseDurationMilliseconds(value) {
	var normalized = trim(value);
	var pattern = /(-?\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/g;
	var unitMap = {
		'ns': 0.000001,
		'us': 0.001,
		'µs': 0.001,
		'ms': 1,
		's': 1000,
		'm': 60000,
		'h': 3600000
	};
	var match;
	var matchedLength = 0;
	var total = 0;

	if (typeof value === 'number' && isFinite(value))
		return value;

	if (normalized === '')
		return null;

	if (/^-?\d+(?:\.\d+)?$/.test(normalized))
		return Number(normalized);

	while ((match = pattern.exec(normalized)) !== null) {
		matchedLength += match[0].length;
		total += Number(match[1]) * unitMap[match[2]];
	}

	if (matchedLength !== normalized.length)
		return null;

	return total;
}

return baseclass.extend({
	formatTimestamp: function(value) {
		var normalized = trim(value);
		var parsed;

		if (normalized === '' || normalized.indexOf('0001-01-01') === 0)
			return '';

		parsed = new Date(normalized);
		if (isNaN(parsed.getTime()))
			return normalized;

		return parsed.getFullYear() + '-' +
			pad2(parsed.getMonth() + 1) + '-' +
			pad2(parsed.getDate()) + ' ' +
			pad2(parsed.getHours()) + ':' +
			pad2(parsed.getMinutes()) + ':' +
			pad2(parsed.getSeconds());
	},

	durationToMilliseconds: function(value) {
		return parseDurationMilliseconds(value);
	},

	formatLatencyMS: function(value) {
		var milliseconds = parseDurationMilliseconds(value);

		if (milliseconds == null || !isFinite(milliseconds))
			return '';

		return Math.round(milliseconds) + ' ms';
	},

	readSessionJSON: function(key) {
		var normalizedKey = trim(key);
		var raw;

		if (normalizedKey === '' || typeof window === 'undefined' || !window.sessionStorage)
			return null;

		try {
			raw = window.sessionStorage.getItem(normalizedKey);
			return raw ? JSON.parse(raw) : null;
		}
		catch (err) {
			return null;
		}
	},

	writeSessionJSON: function(key, value) {
		var normalizedKey = trim(key);

		if (normalizedKey === '' || typeof window === 'undefined' || !window.sessionStorage)
			return;

		try {
			window.sessionStorage.setItem(normalizedKey, JSON.stringify(value));
		}
		catch (err) {
		}
	},

	copyValueToClipboard: function(text) {
		var value = trim(text);
		var input;

		if (value === '')
			return Promise.reject(new Error('missing clipboard text'));

		if (typeof navigator !== 'undefined' && navigator.clipboard && typeof navigator.clipboard.writeText === 'function')
			return navigator.clipboard.writeText(value);

		if (typeof document === 'undefined' || !document.body || typeof document.execCommand !== 'function')
			return Promise.reject(new Error('clipboard unavailable'));

		input = document.createElement('textarea');
		input.value = value;
		input.setAttribute('readonly', 'readonly');
		input.style.position = 'fixed';
		input.style.opacity = '0';
		input.style.pointerEvents = 'none';
		document.body.appendChild(input);
		input.focus();
		input.select();

		try {
			if (!document.execCommand('copy'))
				throw new Error('clipboard copy failed');
		}
		finally {
			document.body.removeChild(input);
		}

		return Promise.resolve();
	},

	currentTheme: function() {
		var stored = trim(readLocalStorageValue(themePreferenceKey)).toLowerCase();

		return stored === 'light' ? 'light' : 'dark';
	},

	setThemePreference: function(value) {
		var normalized = trim(value).toLowerCase();

		writeLocalStorageValue(themePreferenceKey, normalized === 'light' ? 'light' : 'dark');
	},

	withThemeClass: function(className) {
		return appendClass(trim(className), 'fastlane-theme-' + this.currentTheme());
	},

	statusTone: function(connected) {
		return connected === true ? 'connected' : 'disconnected';
	},

	isPendingAction: function(view, key) {
		var normalizedKey = trim(key);
		var actions = view && view.pendingActions;

		if (normalizedKey === '' || !actions)
			return false;

		return actions[normalizedKey] != null;
	},

	pendingActionMessage: function(view, key) {
		var normalizedKey = trim(key);
		var actions = view && view.pendingActions;

		if (normalizedKey === '' || !actions || !actions[normalizedKey])
			return '';

		return trim(actions[normalizedKey].message);
	},

	runPendingAction: function(view, key, executor, options) {
		var normalizedKey = trim(key);
		var settings = options || {};
		var actions;

		if (normalizedKey === '')
			return Promise.reject(new Error('missing action key'));

		if (typeof executor !== 'function')
			return Promise.reject(new Error('missing action executor'));

		view.pendingActions = view.pendingActions || {};
		actions = view.pendingActions;
		if (actions[normalizedKey] != null)
			return Promise.resolve(false);

		actions[normalizedKey] = {
			'message': trim(settings.message)
		};

		if (view && typeof view.renderIntoRoot === 'function')
			view.renderIntoRoot();

		return Promise.resolve().then(executor).finally(function() {
			delete actions[normalizedKey];
			if (view && typeof view.renderIntoRoot === 'function')
				view.renderIntoRoot();
		});
	},

	showModal: function(title, body, options) {
		var settings = options || {};
		var buttons = Array.isArray(settings.actions) ? settings.actions.slice() : [];
		var themeClass = 'fastlane-theme-' + this.currentTheme();
		var modalClass = appendClass(trim(settings.modalClass || settings.bodyClass), themeClass);
		var bodyClass = appendClass(appendClass('fastlane-modal-body', settings.bodyClass), themeClass);

		if (buttons.length === 0) {
			buttons.push(E('button', {
				'class': 'cbi-button',
				'click': function(ev) {
					ui.hideModal();
					return false;
				}
			}, [ _('Close') ]));
		}

		var args = [
			title,
			[
				E('div', { 'class': bodyClass }, normalizeChildren(body)),
				E('div', { 'class': 'fastlane-modal-actions' }, buttons)
			]
		];

		if (modalClass !== '') {
			var classes = modalClass.split(/\s+/);
			for (var i = 0; i < classes.length; i++) {
				if (classes[i] !== '') {
					args.push(classes[i]);
				}
			}
		}

		ui.showModal.apply(ui, args);
	},

	renderSharedStyles: function() {
		return E('style', { 'type': 'text/css' }, [
			'.fastlane-theme-dark { --fastlane-bg:#070b14; --fastlane-bg-soft:#0b1220; --fastlane-surface:#0f1725; --fastlane-surface-elevated:#141f31; --fastlane-surface-muted:rgba(116, 151, 196, 0.08); --fastlane-border:rgba(146, 178, 224, 0.16); --fastlane-border-strong:rgba(132, 191, 255, 0.34); --fastlane-text-primary:#eef4ff; --fastlane-text-secondary:#c7d4e8; --fastlane-text-muted:#91a2bd; --fastlane-accent:#58c4ff; --fastlane-accent-strong:#2ea7ff; --fastlane-accent-soft:rgba(88, 196, 255, 0.14); --fastlane-success:#2ed8aa; --fastlane-success-soft:rgba(46, 216, 170, 0.14); --fastlane-danger:#ff7b8c; --fastlane-danger-soft:rgba(255, 123, 140, 0.14); --fastlane-shadow:0 26px 60px rgba(0, 0, 0, 0.34); --fastlane-shadow-soft:0 18px 36px rgba(0, 0, 0, 0.24); }',
			'.fastlane-theme-light { --fastlane-bg:#f3f6fb; --fastlane-bg-soft:#e8eef5; --fastlane-surface:#f8fbfd; --fastlane-surface-elevated:#fcfdfe; --fastlane-surface-muted:rgba(116, 134, 156, 0.08); --fastlane-border:rgba(125, 146, 170, 0.22); --fastlane-border-strong:rgba(37, 99, 235, 0.24); --fastlane-text-primary:#162638; --fastlane-text-secondary:#41566d; --fastlane-text-muted:#6a7c91; --fastlane-accent:#2563eb; --fastlane-accent-strong:#1d4ed8; --fastlane-accent-soft:rgba(37, 99, 235, 0.1); --fastlane-success:#15803d; --fastlane-success-soft:rgba(22, 163, 74, 0.12); --fastlane-danger:#b91c1c; --fastlane-danger-soft:rgba(220, 38, 38, 0.1); --fastlane-shadow:0 22px 52px rgba(63, 87, 118, 0.12); --fastlane-shadow-soft:0 16px 30px rgba(63, 87, 118, 0.08); }',
			'.fastlane-page-shell { position:relative; width:100%; max-width:100%; min-width:0; padding:22px 0 34px; color:var(--fastlane-text-primary); }',
			'.fastlane-page-shell, .fastlane-page-shell * { box-sizing:border-box; }',
			'.fastlane-page-shell.fastlane-theme-dark::before { content:""; position:absolute; inset:-18px -12px auto; height:260px; border-radius:32px; background:radial-gradient(circle at 0% 0%, rgba(88, 196, 255, 0.18) 0%, rgba(88, 196, 255, 0) 52%), radial-gradient(circle at 100% 0%, rgba(130, 108, 255, 0.12) 0%, rgba(130, 108, 255, 0) 44%), linear-gradient(180deg, rgba(17, 25, 41, 0.94) 0%, rgba(7, 11, 20, 0.98) 100%); z-index:0; pointer-events:none; }',
			'.fastlane-page-shell.fastlane-theme-dark::after { content:""; position:absolute; inset:180px 12% auto auto; width:240px; height:240px; border-radius:999px; background:radial-gradient(circle, rgba(88, 196, 255, 0.09) 0%, rgba(88, 196, 255, 0) 68%); filter:blur(8px); z-index:0; pointer-events:none; }',
			'.fastlane-page-shell.fastlane-theme-light::before { content:""; position:absolute; inset:-18px -12px auto; height:260px; border-radius:32px; background:radial-gradient(circle at 0% 0%, rgba(147, 197, 253, 0.16) 0%, rgba(147, 197, 253, 0) 50%), radial-gradient(circle at 100% 0%, rgba(191, 219, 254, 0.12) 0%, rgba(191, 219, 254, 0) 42%), linear-gradient(180deg, rgba(248, 250, 253, 0.98) 0%, rgba(239, 244, 249, 0.99) 100%); z-index:0; pointer-events:none; }',
			'.fastlane-page-shell.fastlane-theme-light::after { content:""; position:absolute; inset:180px 12% auto auto; width:240px; height:240px; border-radius:999px; background:radial-gradient(circle, rgba(37, 99, 235, 0.05) 0%, rgba(37, 99, 235, 0) 68%); filter:blur(8px); z-index:0; pointer-events:none; }',
			'.fastlane-page-shell > * { position:relative; z-index:1; }',
			'.fastlane-page-shell h2 { margin:0 0 10px; color:var(--fastlane-text-primary); font-size:clamp(28px, 1.6vw + 22px, 42px); line-height:1.06; letter-spacing:-0.04em; }',
			'.fastlane-page-shell h3 { margin:0; color:var(--fastlane-text-primary); font-size:clamp(18px, 1vw + 14px, 26px); line-height:1.16; letter-spacing:-0.03em; }',
			'.fastlane-page-shell p, .fastlane-page-shell li, .fastlane-page-shell label, .fastlane-page-shell summary, .fastlane-page-shell pre, .fastlane-page-shell code { color:var(--fastlane-text-secondary); line-height:1.62; }',
			'.fastlane-page-shell .cbi-section-descr, .fastlane-page-shell .cbi-value-description { margin:0; color:var(--fastlane-text-muted); font-size:15px; line-height:1.7; }',
			'.fastlane-page-shell .cbi-value-title { display:block; margin-bottom:8px; color:var(--fastlane-text-secondary); font-size:12px; font-weight:800; letter-spacing:.1em; text-transform:uppercase; }',
			'.fastlane-page-shell .cbi-section, .fastlane-surface { position:relative; margin:0 0 18px; padding:20px; border:1px solid var(--fastlane-border); border-radius:24px; background:linear-gradient(180deg, rgba(20, 31, 49, 0.94) 0%, rgba(12, 20, 33, 0.98) 100%); box-shadow:var(--fastlane-shadow-soft), inset 0 1px 0 rgba(255, 255, 255, 0.04); overflow:hidden; }',
			'.fastlane-surface::before, .fastlane-page-shell .cbi-section::before { content:""; position:absolute; inset:0 0 auto; height:1px; background:linear-gradient(90deg, rgba(88, 196, 255, 0.38) 0%, rgba(88, 196, 255, 0.08) 42%, rgba(88, 196, 255, 0) 100%); pointer-events:none; }',
			'.fastlane-surface-elevated { background:linear-gradient(180deg, rgba(20, 31, 49, 0.98) 0%, rgba(13, 21, 35, 1) 100%); border-color:var(--fastlane-border-strong); box-shadow:var(--fastlane-shadow), inset 0 1px 0 rgba(255, 255, 255, 0.05); }',
			'.fastlane-overview-grid { display:grid; grid-template-columns:repeat(auto-fit, minmax(220px, 1fr)); gap:14px; margin:0 0 18px; }',
			'.fastlane-card { border:1px solid rgba(125, 159, 204, 0.16); border-radius:20px; padding:16px 16px 17px; min-height:104px; background:linear-gradient(180deg, rgba(16, 25, 40, 0.96) 0%, rgba(11, 18, 31, 1) 100%); box-shadow:0 18px 34px rgba(0, 0, 0, 0.24), inset 0 1px 0 rgba(255, 255, 255, 0.03); overflow:hidden; }',
			'.fastlane-card-primary { border-color:rgba(88, 196, 255, 0.28); box-shadow:0 24px 42px rgba(0, 0, 0, 0.28), inset 0 1px 0 rgba(255, 255, 255, 0.05); }',
			'.fastlane-card-accent { height:4px; width:96px; border-radius:999px; margin-bottom:14px; background:linear-gradient(90deg, var(--fastlane-accent) 0%, #89d8ff 100%); box-shadow:0 0 18px rgba(88, 196, 255, 0.28); }',
			'.fastlane-card-label { color:var(--fastlane-text-muted); font-size:11px; margin-bottom:10px; text-transform:uppercase; letter-spacing:.14em; font-weight:800; }',
			'.fastlane-card-value { color:var(--fastlane-text-primary); font-size:16px; font-weight:700; line-height:1.45; word-break:break-word; }',
			'.fastlane-card-primary .fastlane-card-value { font-size:18px; }',
			'.fastlane-card-connected { border-color:rgba(46, 216, 170, 0.26); background:linear-gradient(180deg, rgba(12, 34, 32, 0.96) 0%, rgba(10, 22, 25, 1) 100%); }',
			'.fastlane-card-connected .fastlane-card-label { color:#90d8c5; }',
			'.fastlane-card-connected .fastlane-card-value { color:#ecfff8; }',
			'.fastlane-card-connected.fastlane-card-primary .fastlane-card-accent { background:linear-gradient(90deg, var(--fastlane-success) 0%, #74ffd8 100%); box-shadow:0 0 18px rgba(46, 216, 170, 0.3); }',
			'.fastlane-card-disconnected { border-color:rgba(145, 162, 189, 0.18); background:linear-gradient(180deg, rgba(19, 26, 38, 0.96) 0%, rgba(11, 18, 30, 1) 100%); }',
			'.fastlane-card-disconnected .fastlane-card-label { color:#a7b7ce; }',
			'.fastlane-card-disconnected .fastlane-card-value { color:#e8eef7; }',
			'.fastlane-card-disconnected.fastlane-card-primary .fastlane-card-accent { background:linear-gradient(90deg, #7b91b5 0%, #adc1df 100%); box-shadow:0 0 18px rgba(123, 145, 181, 0.24); }',
			'.fastlane-page-hero { display:grid; grid-template-columns:minmax(0, 1.3fr) minmax(320px, .9fr); gap:20px; align-items:start; margin-bottom:18px; }',
			'.fastlane-page-hero-copy { min-width:0; }',
			'.fastlane-page-kicker { display:inline-flex; align-items:center; min-height:30px; padding:0 12px; border-radius:999px; margin-bottom:14px; background:var(--fastlane-accent-soft); color:#9ddfff; font-size:11px; font-weight:800; letter-spacing:.16em; text-transform:uppercase; }',
			'.fastlane-page-hero-title { margin:0 0 10px; color:var(--fastlane-text-primary); font-size:clamp(34px, 2.2vw + 22px, 56px); line-height:1; letter-spacing:-0.05em; }',
			'.fastlane-page-hero-description { margin:0; max-width:64ch; color:var(--fastlane-text-muted); line-height:1.72; }',
			'.fastlane-page-hero-meta { display:flex; flex-wrap:wrap; gap:10px; margin-top:16px; }',
			'.fastlane-page-hero-meta-item { display:grid; gap:4px; min-width:150px; padding:12px 14px; border:1px solid rgba(145, 175, 220, 0.14); border-radius:18px; background:rgba(8, 15, 26, 0.36); }',
			'.fastlane-page-hero-meta-label { color:var(--fastlane-text-muted); font-size:10px; font-weight:800; letter-spacing:.12em; text-transform:uppercase; }',
			'.fastlane-page-hero-meta-value { color:var(--fastlane-text-primary); font-size:15px; font-weight:700; word-break:break-word; }',
			'.fastlane-page-hero-actions { display:grid; gap:12px; align-content:start; }',
			'.fastlane-section-heading { display:flex; flex-wrap:wrap; justify-content:space-between; gap:12px 18px; align-items:end; margin-bottom:14px; }',
			'.fastlane-section-heading-copy { display:grid; gap:6px; min-width:0; }',
			'.fastlane-section-heading-copy p { margin:0; color:var(--fastlane-text-muted); }',
			'.fastlane-section-heading-actions { display:flex; flex-wrap:wrap; gap:10px; align-items:center; }',
			'.fastlane-page-shell .table, .fastlane-data-table { width:100%; margin:0; border-collapse:separate; border-spacing:0; border:1px solid rgba(145, 175, 220, 0.14); border-radius:18px; background:rgba(7, 11, 20, 0.34); overflow:hidden; }',
			'.fastlane-page-shell .table .th, .fastlane-page-shell .table .td, .fastlane-data-table .th, .fastlane-data-table .td { padding:16px 14px; border-top:1px solid rgba(145, 175, 220, 0.1); color:var(--fastlane-text-secondary); background:transparent; vertical-align:top; }',
			'.fastlane-page-shell .table .tr:first-child .th, .fastlane-page-shell .table .tr:first-child .td, .fastlane-data-table .tr:first-child .th, .fastlane-data-table .tr:first-child .td { border-top:0; }',
			'.fastlane-page-shell .table .th, .fastlane-data-table .th { color:var(--fastlane-text-muted); font-size:11px; font-weight:800; letter-spacing:.12em; text-transform:uppercase; background:rgba(145, 175, 220, 0.04); }',
			'.fastlane-page-shell .table .td, .fastlane-data-table .td { color:var(--fastlane-text-primary); }',
			'.fastlane-page-shell .table .tr:hover .td, .fastlane-data-table .tr:hover .td { background:rgba(88, 196, 255, 0.03); }',
			'.fastlane-page-shell .label { display:inline-flex; align-items:center; min-height:28px; padding:0 11px; border-radius:999px; border:1px solid rgba(145, 175, 220, 0.16); background:rgba(145, 175, 220, 0.08); color:var(--fastlane-text-secondary); font-size:11px; font-weight:800; letter-spacing:.08em; text-transform:uppercase; }',
			'.fastlane-page-shell .label.notice { border-color:rgba(88, 196, 255, 0.28); background:var(--fastlane-accent-soft); color:#9ddfff; }',
			'.fastlane-page-shell .label.warning { border-color:rgba(255, 123, 140, 0.28); background:var(--fastlane-danger-soft); color:#ffb7c0; }',
			'.fastlane-page-shell .cbi-input-text, .fastlane-page-shell .cbi-input-textarea, .fastlane-page-shell select, .fastlane-page-shell textarea, .fastlane-page-shell input[type="text"], .fastlane-page-shell input[type="number"] { width:100%; max-width:100%; min-height:46px; padding:0 14px; border:1px solid rgba(145, 175, 220, 0.16); border-radius:16px; background:rgba(6, 12, 22, 0.72); color:var(--fastlane-text-primary); box-shadow:inset 0 1px 0 rgba(255, 255, 255, 0.03); }',
			'.fastlane-page-shell textarea, .fastlane-page-shell .cbi-input-textarea { min-height:148px; padding:14px 16px; resize:vertical; }',
			'.fastlane-page-shell .cbi-input-text::placeholder, .fastlane-page-shell .cbi-input-textarea::placeholder, .fastlane-page-shell textarea::placeholder, .fastlane-page-shell input::placeholder { color:var(--fastlane-text-muted); opacity:.84; }',
			'.fastlane-page-shell .cbi-input-text:focus, .fastlane-page-shell .cbi-input-textarea:focus, .fastlane-page-shell select:focus, .fastlane-page-shell textarea:focus, .fastlane-page-shell input:focus { outline:none; border-color:rgba(88, 196, 255, 0.54); box-shadow:0 0 0 1px rgba(88, 196, 255, 0.18), 0 0 0 8px rgba(88, 196, 255, 0.05); }',
			'.fastlane-page-shell pre { margin:0; padding:14px 16px; border:1px solid rgba(145, 175, 220, 0.14); border-radius:18px; background:rgba(6, 12, 22, 0.72); color:var(--fastlane-text-primary); overflow:auto; box-shadow:inset 0 1px 0 rgba(255, 255, 255, 0.03); }',
			'.fastlane-page-shell code { display:inline-block; padding:1px 6px; border-radius:8px; background:rgba(145, 175, 220, 0.08); color:var(--fastlane-text-primary); }',
			'.fastlane-page-shell pre code { display:inline; padding:0; border-radius:0; background:transparent; }',
			'.fastlane-page-shell .cbi-page-actions { display:flex; flex-wrap:wrap; gap:10px; background:transparent !important; border:none !important; padding:0 !important; box-shadow:none !important; margin-top:12px !important; }',
			'.fastlane-page-shell .cbi-button, .fastlane-page-shell .btn, .fastlane-button-primary, .fastlane-button-secondary, .fastlane-button-danger { display:inline-flex; align-items:center; justify-content:center; min-height:46px; padding:0 18px; border:1px solid rgba(145, 175, 220, 0.18); border-radius:16px; background:rgba(15, 24, 38, 0.82); color:var(--fastlane-text-primary); box-shadow:0 12px 24px rgba(0, 0, 0, 0.18), inset 0 1px 0 rgba(255, 255, 255, 0.03); transition:transform .16s ease, border-color .16s ease, box-shadow .16s ease, background .16s ease, color .16s ease; }',
			'.fastlane-page-shell .cbi-button:hover, .fastlane-page-shell .btn:hover, .fastlane-button-primary:hover, .fastlane-button-secondary:hover, .fastlane-button-danger:hover { transform:translateY(-1px); border-color:rgba(145, 190, 246, 0.28); box-shadow:0 16px 26px rgba(0, 0, 0, 0.22), inset 0 1px 0 rgba(255, 255, 255, 0.04); }',
			'.fastlane-page-shell .cbi-button:focus, .fastlane-page-shell .btn:focus, .fastlane-button-primary:focus, .fastlane-button-secondary:focus, .fastlane-button-danger:focus { outline:none; border-color:rgba(88, 196, 255, 0.56); box-shadow:0 0 0 1px rgba(88, 196, 255, 0.18), 0 0 0 8px rgba(88, 196, 255, 0.06); }',
			'.fastlane-page-shell .cbi-button[disabled], .fastlane-page-shell .cbi-button:disabled, .fastlane-page-shell .btn[disabled], .fastlane-page-shell .btn:disabled, .fastlane-button-primary[disabled], .fastlane-button-secondary[disabled], .fastlane-button-danger[disabled] { opacity:.46; cursor:not-allowed; transform:none; box-shadow:none; }',
			'.fastlane-page-shell .cbi-button-apply, .fastlane-page-shell .btn.cbi-button-apply, .fastlane-button-primary { border-color:rgba(88, 196, 255, 0.34); background:linear-gradient(180deg, rgba(52, 147, 235, 0.92) 0%, rgba(30, 116, 211, 0.94) 100%); color:#f4fbff; box-shadow:0 18px 32px rgba(30, 116, 211, 0.24), inset 0 1px 0 rgba(255, 255, 255, 0.14); }',
			'.fastlane-page-shell .cbi-button-action, .fastlane-page-shell .btn.cbi-button-action, .fastlane-button-secondary { border-color:rgba(120, 160, 214, 0.2); background:rgba(12, 20, 34, 0.82); color:#a8d7ff; }',
			'.fastlane-page-shell .cbi-button-negative, .fastlane-page-shell .cbi-button-reset, .fastlane-page-shell .btn.cbi-button-negative, .fastlane-page-shell .btn.cbi-button-reset, .fastlane-button-danger { border-color:rgba(255, 123, 140, 0.28); background:rgba(52, 16, 26, 0.82); color:#ffb7c0; box-shadow:0 16px 28px rgba(52, 16, 26, 0.24), inset 0 1px 0 rgba(255, 255, 255, 0.05); }',
			'.fastlane-page-shell .alert-message, .fastlane-page-shell .fastlane-page-banner { padding:14px 16px; border:1px solid rgba(145, 175, 220, 0.16); border-radius:16px; background:rgba(7, 11, 20, 0.62); color:var(--fastlane-text-secondary); line-height:1.55; }',
			'.fastlane-page-shell .alert-message.notice, .fastlane-page-shell .fastlane-page-banner-info { border-color:rgba(88, 196, 255, 0.24); background:rgba(12, 37, 52, 0.72); color:#b8e8ff; }',
			'.fastlane-page-shell .alert-message.warning, .fastlane-page-shell .fastlane-page-banner-warning { border-color:rgba(255, 123, 140, 0.24); background:rgba(47, 16, 26, 0.72); color:#ffc8cf; }',
			'.fastlane-theme-light .fastlane-surface, .fastlane-page-shell.fastlane-theme-light .cbi-section { background:linear-gradient(180deg, rgba(250, 252, 254, 0.98) 0%, rgba(243, 247, 251, 0.98) 100%); border-color:rgba(125, 146, 170, 0.18); box-shadow:0 16px 30px rgba(63, 87, 118, 0.08), inset 0 1px 0 rgba(255, 255, 255, 0.84); }',
			'.fastlane-theme-light .fastlane-surface::before, .fastlane-page-shell.fastlane-theme-light .cbi-section::before { background:linear-gradient(90deg, rgba(14, 165, 233, 0.28) 0%, rgba(14, 165, 233, 0.08) 42%, rgba(14, 165, 233, 0) 100%); }',
			'.fastlane-theme-light .fastlane-surface-elevated { background:linear-gradient(180deg, rgba(252, 253, 254, 0.99) 0%, rgba(246, 249, 252, 1) 100%); border-color:rgba(37, 99, 235, 0.2); box-shadow:0 20px 36px rgba(63, 87, 118, 0.1), inset 0 1px 0 rgba(255, 255, 255, 0.88); }',
			'.fastlane-theme-light .fastlane-card { border-color:rgba(125, 146, 170, 0.15); background:linear-gradient(180deg, rgba(251, 252, 254, 0.98) 0%, rgba(245, 249, 252, 0.98) 100%); box-shadow:0 12px 24px rgba(63, 87, 118, 0.08), inset 0 1px 0 rgba(255, 255, 255, 0.88); }',
			'.fastlane-theme-light .fastlane-card-primary { border-color:rgba(37, 99, 235, 0.2); box-shadow:0 16px 28px rgba(63, 87, 118, 0.1), inset 0 1px 0 rgba(255, 255, 255, 0.9); }',
			'.fastlane-theme-light .fastlane-card-connected { border-color:rgba(22, 163, 74, 0.2); background:linear-gradient(180deg, rgba(250, 252, 252, 0.99) 0%, rgba(238, 248, 242, 0.99) 100%); }',
			'.fastlane-theme-light .fastlane-card-connected .fastlane-card-label { color:#0f766e; }',
			'.fastlane-theme-light .fastlane-card-connected .fastlane-card-value { color:#14532d; }',
			'.fastlane-theme-light .fastlane-card-disconnected { border-color:rgba(148, 163, 184, 0.18); background:linear-gradient(180deg, rgba(251, 252, 254, 0.98) 0%, rgba(242, 246, 250, 0.98) 100%); }',
			'.fastlane-theme-light .fastlane-card-disconnected .fastlane-card-label { color:#64748b; }',
			'.fastlane-theme-light .fastlane-card-disconnected .fastlane-card-value { color:#1e293b; }',
			'.fastlane-theme-light .fastlane-page-kicker { background:rgba(37, 99, 235, 0.08); color:#1d4ed8; }',
			'.fastlane-theme-light .fastlane-page-hero-meta-item { background:rgba(249, 251, 253, 0.9); border-color:rgba(125, 146, 170, 0.18); box-shadow:0 8px 18px rgba(63, 87, 118, 0.06); }',
			'.fastlane-page-shell.fastlane-theme-light .table, .fastlane-theme-light .fastlane-data-table { border-color:rgba(125, 146, 170, 0.16); background:rgba(249, 251, 253, 0.9); }',
			'.fastlane-page-shell.fastlane-theme-light .table .th, .fastlane-theme-light .fastlane-data-table .th { background:rgba(125, 146, 170, 0.06); }',
			'.fastlane-page-shell.fastlane-theme-light .table .tr:hover .td, .fastlane-theme-light .fastlane-data-table .tr:hover .td { background:rgba(14, 165, 233, 0.04); }',
			'.fastlane-page-shell.fastlane-theme-light .label { border-color:rgba(106, 133, 164, 0.16); background:rgba(255, 255, 255, 0.9); color:var(--fastlane-text-secondary); }',
			'.fastlane-page-shell.fastlane-theme-light .cbi-input-text, .fastlane-page-shell.fastlane-theme-light .cbi-input-textarea, .fastlane-page-shell.fastlane-theme-light select, .fastlane-page-shell.fastlane-theme-light textarea, .fastlane-page-shell.fastlane-theme-light input[type="text"], .fastlane-page-shell.fastlane-theme-light input[type="number"] { border-color:rgba(125, 146, 170, 0.18); background:linear-gradient(180deg, rgba(249, 251, 253, 0.96) 0%, rgba(243, 247, 251, 0.96) 100%); box-shadow:inset 0 1px 0 rgba(255, 255, 255, 0.86), 0 6px 16px rgba(63, 87, 118, 0.05); }',
			'.fastlane-page-shell.fastlane-theme-light .cbi-input-text:focus, .fastlane-page-shell.fastlane-theme-light .cbi-input-textarea:focus, .fastlane-page-shell.fastlane-theme-light select:focus, .fastlane-page-shell.fastlane-theme-light textarea:focus, .fastlane-page-shell.fastlane-theme-light input:focus { border-color:rgba(37, 99, 235, 0.4); box-shadow:0 0 0 1px rgba(37, 99, 235, 0.12), 0 0 0 6px rgba(37, 99, 235, 0.04); }',
			'.fastlane-page-shell.fastlane-theme-light .cbi-section-descr, .fastlane-page-shell.fastlane-theme-light .cbi-value-description { color:var(--fastlane-text-secondary); }',
			'.fastlane-page-shell.fastlane-theme-light pre { border-color:rgba(125, 146, 170, 0.16); background:linear-gradient(180deg, rgba(250, 252, 254, 0.98) 0%, rgba(243, 247, 251, 0.98) 100%); box-shadow:inset 0 1px 0 rgba(255, 255, 255, 0.86), 0 8px 18px rgba(63, 87, 118, 0.06); }',
			'.fastlane-page-shell.fastlane-theme-light code { background:rgba(37, 99, 235, 0.08); color:#1e3a8a; }',
			'.fastlane-page-shell.fastlane-theme-light .cbi-button, .fastlane-page-shell.fastlane-theme-light .btn, .fastlane-theme-light .fastlane-button-primary, .fastlane-theme-light .fastlane-button-secondary, .fastlane-theme-light .fastlane-button-danger { border-color:rgba(125, 146, 170, 0.16); background:linear-gradient(180deg, rgba(250, 252, 254, 0.98) 0%, rgba(241, 246, 251, 0.98) 100%); color:var(--fastlane-text-primary); box-shadow:0 10px 20px rgba(63, 87, 118, 0.08), inset 0 1px 0 rgba(255, 255, 255, 0.88); }',
			'.fastlane-page-shell.fastlane-theme-light .cbi-button:hover, .fastlane-page-shell.fastlane-theme-light .btn:hover, .fastlane-theme-light .fastlane-button-primary:hover, .fastlane-theme-light .fastlane-button-secondary:hover, .fastlane-theme-light .fastlane-button-danger:hover { border-color:rgba(37, 99, 235, 0.22); box-shadow:0 12px 22px rgba(63, 87, 118, 0.1), inset 0 1px 0 rgba(255, 255, 255, 0.9); }',
			'.fastlane-page-shell.fastlane-theme-light .cbi-button-apply, .fastlane-page-shell.fastlane-theme-light .btn.cbi-button-apply, .fastlane-theme-light .fastlane-button-primary { border-color:rgba(37, 99, 235, 0.34); background:linear-gradient(180deg, #2563eb 0%, #1d4ed8 100%); color:#f8fbff; box-shadow:0 14px 28px rgba(37, 99, 235, 0.18), inset 0 1px 0 rgba(255, 255, 255, 0.16); }',
			'.fastlane-page-shell.fastlane-theme-light .cbi-button-apply:hover, .fastlane-page-shell.fastlane-theme-light .btn.cbi-button-apply:hover, .fastlane-theme-light .fastlane-button-primary:hover { border-color:rgba(29, 78, 216, 0.42); background:linear-gradient(180deg, #1d4ed8 0%, #1e40af 100%); color:#ffffff; box-shadow:0 16px 30px rgba(29, 78, 216, 0.22), inset 0 1px 0 rgba(255, 255, 255, 0.18); }',
			'.fastlane-page-shell.fastlane-theme-light .cbi-button-action, .fastlane-page-shell.fastlane-theme-light .btn.cbi-button-action, .fastlane-theme-light .fastlane-button-secondary { border-color:rgba(37, 99, 235, 0.18); background:linear-gradient(180deg, rgba(243, 248, 253, 0.98) 0%, rgba(232, 240, 248, 0.98) 100%); color:#1d4ed8; }',
			'.fastlane-page-shell.fastlane-theme-light .cbi-button-negative, .fastlane-page-shell.fastlane-theme-light .cbi-button-reset, .fastlane-page-shell.fastlane-theme-light .btn.cbi-button-negative, .fastlane-page-shell.fastlane-theme-light .btn.cbi-button-reset, .fastlane-theme-light .fastlane-button-danger { border-color:rgba(220, 38, 38, 0.18); background:linear-gradient(180deg, rgba(253, 246, 246, 0.98) 0%, rgba(249, 237, 237, 0.98) 100%); color:#b91c1c; box-shadow:0 12px 22px rgba(127, 29, 29, 0.06), inset 0 1px 0 rgba(255, 255, 255, 0.9); }',
			'.fastlane-page-shell.fastlane-theme-light .alert-message, .fastlane-page-shell.fastlane-theme-light .fastlane-page-banner { background:rgba(255, 255, 255, 0.88); border-color:rgba(106, 133, 164, 0.16); color:var(--fastlane-text-secondary); }',
			'.fastlane-page-shell.fastlane-theme-light .alert-message.notice, .fastlane-page-shell.fastlane-theme-light .fastlane-page-banner-info { background:rgba(239, 248, 255, 0.94); color:#075985; border-color:rgba(56, 189, 248, 0.2); }',
			'.fastlane-page-shell.fastlane-theme-light .alert-message.warning, .fastlane-page-shell.fastlane-theme-light .fastlane-page-banner-warning { background:rgba(254, 242, 242, 0.96); color:#b91c1c; border-color:rgba(239, 68, 68, 0.2); }',
			'.fastlane-modal-body { width:100%; max-width:100%; min-width:0; box-sizing:border-box; overflow:hidden; }',
			'.fastlane-modal-actions { display:flex; flex-wrap:wrap; justify-content:flex-end; gap:8px; margin-top:14px; }',
			'@media (max-width: 980px) { .fastlane-page-hero { grid-template-columns:minmax(0, 1fr); } .fastlane-page-hero-actions { grid-template-columns:minmax(0, 1fr); } }',
			'@media (max-width: 700px) { .fastlane-page-shell { padding-top:14px; } .fastlane-page-shell .cbi-section, .fastlane-surface { padding:16px; border-radius:20px; } .fastlane-overview-grid { gap:12px; } .fastlane-section-heading, .fastlane-section-heading-actions, .fastlane-page-hero-meta { flex-direction:column; align-items:stretch; } .fastlane-page-hero-title { font-size:clamp(28px, 8vw, 42px); } .fastlane-page-shell .cbi-button { width:100%; } }',
			'@media (max-width: 560px) { .fastlane-page-shell .table, .fastlane-data-table { border-radius:16px; } .fastlane-card { min-height:0; } .fastlane-page-hero-meta-item { width:100%; } }'
		]);
	},

	renderSummaryCard: function(label, value, options) {
		var settings = options || {};
		var className = 'fastlane-card';
		var content = value;
		var attrs = {
			'class': className
		};

		if (trim(settings.id) !== '')
			attrs.id = settings.id;

		if (trim(settings.tone) !== '')
			attrs['class'] += ' fastlane-card-' + trim(settings.tone);

		if (settings.primary === true)
			attrs['class'] += ' fastlane-card-primary';

		if (!hasContent(content))
			content = settings.fallback != null ? settings.fallback : '-';

		if (!Array.isArray(content))
			content = [ content ];

		var valueAttrs = { 'class': 'fastlane-card-value' };
		if (trim(settings.valueId) !== '')
			valueAttrs.id = settings.valueId;

		var children = [];

		if (settings.primary === true)
			children.push(E('div', { 'class': 'fastlane-card-accent' }, []));

		children.push(E('div', { 'class': 'fastlane-card-label' }, [ label ]));
		children.push(E('div', valueAttrs, content));

		return E('div', attrs, children);
	}
});
