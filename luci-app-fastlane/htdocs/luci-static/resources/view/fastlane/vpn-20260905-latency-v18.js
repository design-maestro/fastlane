'use strict';
'require view';
'require fs';
'require ui';
'require dom';
'require poll';
'require fastlane.fastlane-20260904-v3 as fastlaneShell';

var binary = '/usr/bin/fastlane';
var pingKey = 'fastlane.vpn.get.results.v1';
var vpnPollInterval = 5;

function trim(value) {
	return value == null ? '' : String(value).trim();
}

function nodeRawName(node) {
	return trim(node && (node.name || node.remark || node.address));
}

function flagEmoji(value) {
	var match = trim(value).match(/(?:\uD83C[\uDDE6-\uDDFF]){2}/);
	return match ? match[0] : '';
}

function flagEmojiFromCode(code) {
	code = trim(code).toUpperCase();
	if (!/^[A-Z]{2}$/.test(code))
		return '';
	return String.fromCodePoint(
		0x1F1E6 + code.charCodeAt(0) - 65,
		0x1F1E6 + code.charCodeAt(1) - 65
	);
}

function nodeName(node) {
	return nodeRawName(node)
		.replace(/^\[[^\]]+\]\s*/, '')
		.replace(/(?:\uD83C[\uDDE6-\uDDFF]){2}/g, '')
		.replace(/\s{2,}/g, ' ')
		.trim() || _('Untitled');
}

function sourceName(sub) {
	return trim(sub && (sub.provider_name || sub.display_name || sub.id)) || _('Subscription');
}

function formatTime(value) {
	if (!value)
		return _('never');
	var date = new Date(value);
	if (isNaN(date.getTime()))
		return String(value);
	return date.toLocaleString(undefined, { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' });
}

function subscriptionExpiryPresentation(value) {
	if (!value)
		return null;
	var expires = new Date(value);
	if (isNaN(expires.getTime()))
		return null;
	var now = new Date();
	var exact = expires.toLocaleString(undefined, { day: 'numeric', month: 'long', year: 'numeric', hour: '2-digit', minute: '2-digit' });
	if (expires.getTime() <= now.getTime())
		return { text: _('Expired'), className: 'fl-tab-expiry-expired', title: _('Expired on') + ' ' + exact };
	var today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
	var expiryDay = new Date(expires.getFullYear(), expires.getMonth(), expires.getDate());
	var days = Math.max(0, Math.round((expiryDay.getTime() - today.getTime()) / 86400000));
	if (days === 0)
		return { text: _('Expires today'), className: 'fl-tab-expiry-soon', title: _('Expires on') + ' ' + exact };
	if (days === 1)
		return { text: _('Expires tomorrow'), className: 'fl-tab-expiry-soon', title: _('Expires on') + ' ' + exact };
	if (days <= 7) {
		return { text: _('Expires in') + ' ' + days + ' ' + _('days'), className: 'fl-tab-expiry-soon', title: _('Expires on') + ' ' + exact };
	}
	var options = { day: 'numeric', month: 'long' };
	if (expires.getFullYear() !== now.getFullYear())
		options.year = 'numeric';
	return { text: _('Expires on') + ' ' + expires.toLocaleDateString(), className: '', title: _('Expires on') + ' ' + exact };
}

function isSubscriptionExpired(sub) {
	if (!sub || !sub.expires_at)
		return false;
	var expires = new Date(sub.expires_at);
	return !isNaN(expires.getTime()) && expires.getTime() <= Date.now();
}

function positiveLatency(value) {
	if (value == null || value === '')
		return null;
	var number = Number(value);
	return isFinite(number) && number > 0 ? number : null;
}

function formatLatency(value) {
	var number = positiveLatency(value);
	return number == null ? '—' : Math.round(number) + ' ' + _('ms');
}

function durationMilliseconds(value) {
	var raw = trim(value);
	if (!raw)
		return null;
	var units = { ns: 0.000001, us: 0.001, 'µs': 0.001, 'μs': 0.001, ms: 1, s: 1000, m: 60000, h: 3600000 };
	var matcher = /([+-]?(?:\d+(?:\.\d*)?|\.\d+))(ns|us|µs|μs|ms|s|m|h)/g;
	var total = 0;
	var end = 0;
	var match;
	while ((match = matcher.exec(raw)) !== null) {
		if (match.index !== end)
			return null;
		total += Number(match[1]) * units[match[2]];
		end = matcher.lastIndex;
	}
	return end === raw.length && isFinite(total) && total >= 0 ? total : null;
}

function observationTime(value) {
	var parsed = Date.parse(trim(value && (value.checked_at || value.last_checked_at)));
	return isFinite(parsed) ? parsed : 0;
}

function persistedObservation(value) {
	if (!value || typeof value !== 'object')
		return null;
	var checkedAt = trim(value.last_checked_at);
	var latency = positiveLatency(durationMilliseconds(value.last_latency));
	var lastCheckFailed = Number(value.consecutive_failures || 0) > 0 || trim(value.last_failure_reason) !== '';
	var healthy = checkedAt !== '' && value.healthy === true && !lastCheckFailed && latency != null;
	return {
		node_id: trim(value.node_id),
		healthy: healthy,
		latency_ms: healthy ? latency : null,
		checked_at: checkedAt,
		last_latency: value.last_latency,
		url_test: true
	};
}

function sessionObservation(value) {
	if (!value || typeof value !== 'object')
		return null;
	var latency = positiveLatency(value.latency_ms);
	var checkedAt = trim(value.checked_at);
	var lastCheckFailed = value.healthy === false || Number(value.consecutive_failures || 0) > 0 || trim(value.last_failure_reason) !== '';
	var healthy = value.healthy === true && !lastCheckFailed && latency != null;
	var normalized = Object.assign({}, value, {
		healthy: healthy,
		latency_ms: healthy ? latency : null,
		checked_at: checkedAt
	});
	return normalized;
}

function fresherObservation(persisted, session) {
	if (!persisted)
		return session || {};
	if (!session)
		return persisted;
	return observationTime(session) >= observationTime(persisted)
		? session
		: persisted;
}

function nodeLocation(node) {
	var value = nodeName(node);
	var parts = value.split(/\s*[·—|-]\s*/).filter(Boolean);
	return { country: parts[0] || value, city: parts.slice(1).join(' · ') };
}

function nodeFlag(node) {
	var haystack = (nodeName(node) + ' ' + trim(node && node.address)).toLowerCase();
	var flags = {
		nl: /netherlands|amsterdam|\bnl[.-]/,
		pl: /poland|warsaw|warszawa|\bpl[.-]/,
		se: /sweden|stockholm|\bse[.-]/,
		ee: /estonia|tallinn|\bee[.-]/,
		de: /germany|frankfurt|\bde[.-]/
	};
	for (var code in flags)
		if (flags[code].test(haystack)) return code;
	return '';
}

function commandError(result) {
	var message = trim(result && (result.stderr || result.stdout));
	return message || _('The Fast Lane command failed.');
}

function friendlyError(value, fallback) {
	var details = trim(value) || _('No technical details were returned.');
	var normalized = details.toLowerCase();
	var message = fallback || _('Could not complete the action. Try again.');

	if (normalized.indexOf('all nodes are excluded') >= 0 || normalized.indexOf('no nodes to select') >= 0)
		message = _('No servers are available for automatic selection. Add a subscription or restore hidden servers.');
	else if (normalized.indexOf('egress probe failed') >= 0 || normalized.indexOf('candidate verify failed') >= 0)
		message = _('Internet access failed after connecting. Fast Lane restored the previous route.');
	else if (normalized.indexOf('timeout') >= 0 || normalized.indexOf('timed out') >= 0)
		message = _('The server did not respond in time. Try again or choose another server.');
	else if (normalized.indexOf('unauthorized') >= 0 || normalized.indexOf('forbidden') >= 0 || /\b(401|403)\b/.test(normalized))
		message = _('The subscription is not accessible. Check its link and credentials.');
	else if (normalized.indexOf('invalid') >= 0 || normalized.indexOf('parse') >= 0 || normalized.indexOf('decode') >= 0)
		message = _('Fast Lane did not recognize the format. Check the link, key, or JSON.');
	else if (normalized.indexOf('xray') >= 0 && (normalized.indexOf('not found') >= 0 || normalized.indexOf('no such file') >= 0))
		message = _('Xray was not found. Reinstall Fast Lane.');

	return { message: message, details: details };
}

function icon(name) {
	var paths = {
		server: 'M12 3v18M5 7h14M5 17h14',
		info: 'M12 17v-5M12 8h.01M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0',
		close: 'M6 6l12 12M18 6 6 18',
		refresh: 'M20 11a8.1 8.1 0 0 0-15.5-2M4 4v5h5M4 13a8.1 8.1 0 0 0 15.5 2M20 20v-5h-5',
		bolt: 'm13 2-9 12h7l-1 8 9-12h-7l1-8',
		plus: 'M12 5v14M5 12h14',
		trash: 'M4 7h16M9 7V4h6v3M7 7l1 13h8l1-13M10 11v5M14 11v5',
		eyeOff: 'm3 3 18 18M10.6 10.7a2 2 0 0 0 2.7 2.7M9.9 4.2A10.4 10.4 0 0 1 12 4c5 0 8.5 4 9.5 6-0.5 0.9-1.2 1.8-2 2.7M6.6 6.6C4.7 7.8 3.5 9.4 2.5 11c1.8 3 5 6 9.5 6 1.2 0 2.3-.2 3.3-.6',
		eye: 'M2.5 12s3.5-6 9.5-6 9.5 6 9.5 6-3.5 6-9.5 6-9.5-6-9.5-6M12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6'
	};
	var namespace = 'http://www.w3.org/2000/svg';
	var svg = document.createElementNS(namespace, 'svg');
	var path = document.createElementNS(namespace, 'path');
	svg.setAttribute('class', 'fl-icon');
	svg.setAttribute('viewBox', '0 0 24 24');
	svg.setAttribute('aria-hidden', 'true');
	svg.setAttribute('focusable', 'false');
	path.setAttribute('d', paths[name] || paths.server);
	path.setAttribute('fill', 'none');
	path.setAttribute('stroke', 'currentColor');
	path.setAttribute('stroke-width', '1.8');
	path.setAttribute('stroke-linecap', 'round');
	path.setAttribute('stroke-linejoin', 'round');
	svg.appendChild(path);
	return svg;
}

var css = `
.fastlane-root{--fl-bg:#02090c;--fl-panel:#071115;--fl-panel-2:#0a1519;--fl-line:#1a2b31;--fl-line-strong:#29434a;--fl-text:#ddd4ca;--fl-muted:#a89f96;--fl-subtle:#756f69;--fl-green:#54df91;--fl-green-dim:#45a974;--fl-red:#ff555f;--fl-amber:#ffc528;background:var(--fl-bg);color:var(--fl-text);border:1px solid #0b171b;border-radius:12px;min-height:760px;overflow:hidden;font-family:"Avenir Next",ui-sans-serif,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;font-size:15px;line-height:1.4}
.fastlane-root *{box-sizing:border-box}.fastlane-root h2,.fastlane-root h3,.fastlane-root p{margin:0}.fastlane-root ::selection{background:#2f7e5a;color:#fff}.fl-shell-nav{height:74px;display:grid;grid-template-columns:1fr auto 1fr;align-items:center;padding:0 24px;border-bottom:1px solid var(--fl-line)}.fl-brand{display:flex;align-items:center;gap:13px;min-width:0}.fl-logo{width:42px;height:42px;object-fit:cover}.fl-title{font-size:25px;font-weight:750;letter-spacing:-.025em;color:#ded2c5}.fl-nav-links{height:100%;display:flex;align-items:stretch;gap:28px}.fl-nav-link{position:relative;display:flex;align-items:center;padding:0 12px;color:var(--fl-muted)!important;text-decoration:none!important;font-size:16px;font-weight:560}.fl-nav-link:hover{color:var(--fl-text)!important}.fl-nav-link-active{color:var(--fl-green)!important}.fl-nav-link-active:after{content:"";position:absolute;left:0;right:0;bottom:0;height:3px;background:var(--fl-green)}.fl-shell-tools{display:flex;justify-content:flex-end}.fl-shell{padding:20px 24px 24px}
.fl-button{appearance:none;display:inline-flex;align-items:center;justify-content:center;gap:8px;min-height:44px;padding:9px 15px;border:1px solid var(--fl-line-strong);border-radius:8px;background:transparent;color:var(--fl-muted);font:inherit;font-weight:600;cursor:pointer;transition:background-color .15s ease,border-color .15s ease,color .15s ease}.fl-button:hover{border-color:#426068;background:#0d1c21;color:var(--fl-text)}.fl-button:disabled{opacity:.45;cursor:wait}.fl-button-primary{border-color:#55bd87;background:#45ae78;color:#f7fff9}.fl-button-primary:hover{border-color:#65d99a;background:#50c187}.fl-button-danger{border-color:#e93b47;color:#ff626b}.fl-button-danger:hover{border-color:#ff5d66;background:rgba(255,85,95,.08);color:#ff727a}.fl-button-quiet{border-color:transparent}.fl-icon{width:20px;height:20px;display:block;flex:0 0 auto;color:inherit!important}.fl-icon-more circle{fill:currentColor!important}
.fl-status{display:grid;grid-template-columns:minmax(180px,1.1fr) minmax(210px,1fr) minmax(150px,1fr) minmax(180px,.9fr) auto auto;align-items:stretch;border:1px solid var(--fl-line);border-radius:8px;background:var(--fl-panel);min-height:70px;margin-bottom:16px}.fl-status-cell{display:flex;align-items:center;gap:4px;min-width:0;padding:12px;border-right:1px solid var(--fl-line)}.fl-status-cell-label{color:var(--fl-muted)}.fl-status-cell-value{color:var(--fl-text);font-size:16px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.fl-status-cell-latency{color:var(--fl-amber);font-variant-numeric:tabular-nums}.fl-status-main{color:var(--fl-green);font-size:16px;white-space:nowrap}.fl-dot{width:11px;height:11px;border-radius:50%;background:var(--fl-red);box-shadow:0 0 0 4px rgba(255,85,95,.1);flex:0 0 auto}.fl-dot-on{background:var(--fl-green);box-shadow:0 0 0 4px rgba(84,223,145,.1)}.fl-mode-switch{display:grid;grid-template-columns:1fr 1fr;align-self:center;width:230px;margin:0 12px;border:1px solid var(--fl-line-strong);border-radius:7px;overflow:hidden}.fl-mode-option{appearance:none;min-height:40px;border:0;background:transparent;color:var(--fl-muted);font:inherit;cursor:pointer}.fl-mode-option-active{background:#43af78;color:#f7fff9}.fl-status-disconnect{align-self:center;margin-right:14px}
.fl-sourcebar{display:flex;align-items:stretch;min-height:90px;margin-bottom:16px;border:1px solid var(--fl-line);border-radius:8px;background:#050d10;overflow:hidden}.fl-tabs{display:flex;flex:1 1 auto;min-width:0;overflow-x:auto;scrollbar-color:#29434a transparent;scrollbar-width:thin}.fl-tabs::-webkit-scrollbar{height:7px}.fl-tabs::-webkit-scrollbar-track{background:transparent}.fl-tabs::-webkit-scrollbar-thumb{background:#29434a;border-radius:999px}.fl-tab{position:relative;flex:0 0 auto;min-width:220px;padding:17px 28px;border:0;border-right:1px solid var(--fl-line);background:transparent;color:var(--fl-text);text-align:left;cursor:pointer}.fl-tab:hover{background:#091518}.fl-tab-active{background:#0a1917;box-shadow:inset 0 -2px 0 var(--fl-green)}.fl-tab-top{display:flex;align-items:center;gap:9px;font-size:17px}.fl-count{color:var(--fl-muted);font-variant-numeric:tabular-nums}.fl-tab-meta{display:flex;align-items:center;gap:7px;margin-top:4px;color:var(--fl-muted);font-size:13px}.fl-source-status{display:inline-flex;width:9px;height:9px;border-radius:50%;background:var(--fl-green-dim);box-shadow:0 0 0 3px rgba(84,223,145,.08)}.fl-source-status-error{background:var(--fl-red);box-shadow:none}.fl-source-actions{position:relative;z-index:2;display:flex;flex:0 0 auto;align-items:stretch;margin-left:0;background:#050d10;box-shadow:-12px 0 20px rgba(2,9,12,.42)}.fl-source-actions .fl-button{min-width:210px;border:0;border-left:1px solid var(--fl-line);border-radius:0;font-size:15px;white-space:nowrap}.fl-source-actions .fl-button-danger{color:var(--fl-red)}
.fl-server-panel{border:1px solid var(--fl-line);border-radius:8px;background:var(--fl-panel);overflow:visible}.fl-toolbar{display:flex;align-items:center;gap:26px;min-height:76px;padding:14px 18px;border-bottom:1px solid var(--fl-line)}.fl-toolbar-actions{display:flex;align-items:center;gap:26px;flex:1}.fl-search-wrap{position:relative;min-width:310px}.fl-search{width:100%;height:44px!important;padding:9px 14px!important;border:1px solid var(--fl-line-strong)!important;border-radius:7px!important;background:#050d10!important;color:var(--fl-text)!important;font:inherit!important}.fl-search::placeholder{color:var(--fl-subtle)}.fl-select{height:44px!important;min-width:190px;padding:8px 36px 8px 14px!important;border:1px solid var(--fl-line-strong)!important;border-radius:7px!important;background:#050d10!important;color:var(--fl-muted)!important;font:inherit!important}.fl-toolbar-buttons{display:flex;align-items:center;gap:8px;margin-left:auto}.fl-toolbar-buttons .fl-button{white-space:nowrap}.fl-toolbar-meta{display:none}
.fl-table-wrap{overflow:visible}.fl-table{width:100%;border-collapse:separate;border-spacing:0;table-layout:fixed}.fl-table th{height:58px;padding:15px 26px;border-bottom:1px solid var(--fl-line);background:#040c0f;color:var(--fl-muted);font-size:14px;font-weight:560;text-align:left}.fl-table td{height:84px;padding:13px 26px;border-bottom:1px solid var(--fl-line);vertical-align:middle}.fl-table th:last-child,.fl-table td.fl-actions-cell{padding-left:8px;padding-right:14px}.fl-table tr:last-child td{border-bottom:0}.fl-table tbody tr{cursor:pointer;transition:background-color .15s ease}.fl-table tbody tr:hover{background:#09171a}.fl-table tbody tr.fl-active-row{background:#0a201b;box-shadow:inset 3px 0 0 var(--fl-green)}.fl-table tbody tr.fl-hidden-row{cursor:default}.fl-server{display:flex;align-items:center;gap:15px;min-width:0}.fl-server-mark{width:42px;height:42px;border:1px solid var(--fl-line-strong);border-radius:50%;background:#0c1a1f;display:grid;place-items:center;color:var(--fl-green);flex:0 0 auto}.fl-server-flag-emoji{overflow:hidden;background:#071115;color:initial}.fl-server-flag-glyph{display:block;font-family:"Apple Color Emoji","Segoe UI Emoji","Noto Color Emoji",sans-serif;font-size:26px;line-height:1;transform:translateY(-1px)}.fl-server-text{min-width:0}.fl-server-name{color:var(--fl-text);font-size:16px;font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.fl-active-row .fl-server-name{color:var(--fl-green)}.fl-server-address{margin-top:2px;color:var(--fl-muted);font-size:13px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.fl-source{color:var(--fl-muted);font-size:15px}.fl-protocol{color:var(--fl-muted);font-size:14px;text-transform:uppercase}.fl-latency{color:var(--fl-amber);font-size:15px;font-weight:650;font-variant-numeric:tabular-nums}.fl-latency-kind{display:none}.fl-latency-good,.fl-latency-mid{color:var(--fl-amber)}.fl-latency-bad{color:var(--fl-red)}.fl-node-status{display:flex;align-items:center;gap:9px;color:var(--fl-muted)}.fl-node-status-active{color:var(--fl-green);text-transform:uppercase;font-size:13px}.fl-node-status-dot{width:9px;height:9px;border-radius:50%;background:var(--fl-green-dim);flex:0 0 auto}.fl-node-status-bad .fl-node-status-dot{background:var(--fl-red)}.fl-empty{padding:72px 20px;text-align:center;color:var(--fl-muted)}
.fastlane-root .fl-table,.fastlane-root .fl-table thead,.fastlane-root .fl-table tbody{background:#071115!important;color:var(--fl-text)!important}.fastlane-root .fl-table thead tr,.fastlane-root .fl-table thead tr:nth-of-type(2n){background:#040c0f!important}.fastlane-root .fl-table tbody tr,.fastlane-root .fl-table tbody tr:nth-of-type(2n){background:#071115!important;color:var(--fl-text)!important}.fastlane-root .fl-table tbody tr:hover{background:#09171a!important}.fastlane-root .fl-table tbody tr.fl-active-row{background:#0a201b!important}.fastlane-root .fl-table th{background:#040c0f!important;color:var(--fl-muted)!important;border-top:0!important;border-bottom:1px solid var(--fl-line)!important}.fastlane-root .fl-table td{background:transparent!important;color:var(--fl-text)!important;border-top:0!important;border-bottom:1px solid var(--fl-line)!important}.fastlane-root .fl-table tbody tr:last-child td{border-bottom:0!important}
.fl-error,.fl-busy{margin:0 0 14px;border:1px solid rgba(255,85,95,.3);border-radius:8px;background:rgba(255,85,95,.06);color:#ffb0b5;padding:12px 15px}.fl-busy{display:flex;align-items:center;gap:10px;border-color:rgba(84,223,145,.25);background:rgba(84,223,145,.06);color:#9be8bd}.fl-error-notice{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:start;gap:10px 16px}.fl-error-message{min-width:0;padding-block:6px;font-weight:600;line-height:1.5;overflow-wrap:anywhere}.fl-error-actions{display:flex;align-items:center;gap:4px}.fl-error-action{width:38px;height:38px;min-height:38px;padding:0;border-color:transparent;color:#ffb0b5}.fl-error-action:hover{border-color:rgba(255,176,181,.24);background:rgba(255,255,255,.05);color:#fff}.fl-error-action .fl-icon{width:19px;height:19px}.fl-error-details{grid-column:1/-1;margin:0;padding:12px 14px;border-top:1px solid rgba(255,85,95,.2);background:#090d10;color:#c9c1ba;font:12px/1.55 ui-monospace,SFMono-Regular,Menlo,monospace;white-space:pre-wrap;overflow-wrap:anywhere;max-height:240px;overflow:auto}.fl-error-row{display:flex;align-items:center;justify-content:space-between;gap:12px}.fl-button:focus-visible,.fl-tab:focus-visible,.fl-nav-link:focus-visible,.fl-mode-option:focus-visible,.fl-search:focus-visible,.fl-select:focus-visible,.fl-table tbody tr:focus-visible,.fl-more summary:focus-visible{outline:2px solid var(--fl-green);outline-offset:3px}.fl-more{position:relative;margin-left:auto}.fl-more summary{list-style:none;display:grid;place-items:center;width:44px;height:44px;border:0;border-radius:7px;background:transparent;color:var(--fl-muted);cursor:pointer}.fl-more summary:before{content:"";width:4px;height:4px;border-radius:50%;background:currentColor;box-shadow:0 -7px 0 currentColor,0 7px 0 currentColor}.fl-more summary:hover,.fl-more[open] summary{background:#102126;color:var(--fl-text)}.fl-more summary::-webkit-details-marker{display:none}.fl-more-menu{position:absolute;right:0;top:48px;z-index:20;display:grid;gap:2px;min-width:210px;padding:6px;border:1px solid var(--fl-line-strong);border-radius:8px;background:#081216;box-shadow:0 18px 42px rgba(0,0,0,.5)}.fl-more-menu .fl-button{display:flex!important;width:100%!important;justify-content:flex-start!important;padding:11px 14px!important;border-color:transparent!important;text-align:left!important}.fl-sr-only{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}.fl-add-form{display:grid;gap:14px;min-width:min(620px,80vw);color:var(--fl-text)}.fl-add-form label{display:grid;gap:6px;font-weight:600}.fl-add-form input,.fl-add-form textarea{width:100%;border:1px solid var(--fl-line-strong)!important;background:#050d10!important;color:var(--fl-text)!important;border-radius:7px;padding:12px;font:inherit}.fl-add-form textarea{min-height:190px;resize:vertical}.fl-modal-help{font-size:12px;color:var(--fl-muted);line-height:1.5}.fl-modal-error{min-height:20px;color:#ff9da3;font-size:12px}.fastlane-modal{background:#071115!important;color:#ddd4ca!important}.fastlane-modal h4{color:#ddd4ca!important}.fastlane-modal .right{background:#071115!important;border-top-color:#1a2b31!important}
.fl-inline-loader{display:inline-block;width:16px;height:16px;border:2px solid rgba(84,223,145,.22);border-top-color:var(--fl-green);border-radius:50%;animation:fl-spin .7s linear infinite;vertical-align:-3px}.fl-testing-label{display:inline-flex;align-items:center;gap:8px;color:var(--fl-green)}@keyframes fl-spin{to{transform:rotate(360deg)}}
@media(max-width:1450px){.fl-toolbar{align-items:stretch;flex-direction:column;gap:10px}.fl-toolbar-actions{display:grid;grid-template-columns:minmax(220px,1.4fr) repeat(3,minmax(150px,1fr));gap:10px}.fl-toolbar-buttons{display:grid;grid-template-columns:1fr 1fr;margin:0}}
@media(max-width:1100px){.fl-shell-nav{grid-template-columns:1fr auto}.fl-status{grid-template-columns:1fr 1fr 1fr}.fl-status-cell:nth-child(4){border-right:0}.fl-mode-switch{width:auto;margin:12px 18px}.fl-status-disconnect{margin:12px 14px 12px 0}.fl-toolbar{align-items:stretch;flex-direction:column;gap:10px}.fl-toolbar-actions{display:grid;grid-template-columns:minmax(220px,1.4fr) repeat(3,minmax(150px,1fr));gap:10px}.fl-toolbar-buttons{display:grid;grid-template-columns:1fr 1fr;margin:0}.fl-tab{min-width:190px}}
@media(max-width:760px){.fastlane-root{border-radius:8px}.fl-shell{padding:14px}.fl-status{grid-template-columns:1fr 1fr}.fl-status-cell{padding:12px;border-bottom:1px solid var(--fl-line)}.fl-status-cell:nth-child(even){border-right:0}.fl-mode-switch{grid-column:1/-1;width:calc(100% - 24px);margin:12px}.fl-status-disconnect{grid-column:1/-1;width:calc(100% - 24px);margin:0 12px 12px}.fl-sourcebar{min-height:78px}.fl-tab{min-width:160px;padding:13px 16px}.fl-tab-top{font-size:15px}.fl-source-actions .fl-button{min-width:170px}.fl-toolbar{padding:12px}.fl-toolbar-actions{grid-template-columns:1fr 1fr}.fl-search-wrap{grid-column:1/-1;min-width:0}.fl-select{min-width:0;width:100%}.fl-toolbar-actions .fl-select:last-child{grid-column:1/-1}.fl-toolbar-buttons{grid-template-columns:1fr 1fr}.fl-toolbar-buttons .fl-button{width:100%;min-width:0;padding-inline:8px;white-space:normal;font-size:13px;line-height:1.2}.fl-table thead{display:none}.fl-table,.fl-table tbody{display:block;width:100%}.fl-table tbody tr{position:relative;display:grid;grid-template-columns:repeat(4,minmax(0,1fr));align-items:center;gap:0;padding:13px 14px 16px;border-bottom:1px solid var(--fl-line)}.fl-table td{display:inline-flex;width:auto;height:auto;align-items:center;padding:0;border:0}.fl-table td:before{display:none}.fl-table td:first-child{display:block;grid-column:1/-1;padding:0 44px 16px 0}.fl-table .fl-meta-cell{min-width:0;justify-content:center}.fl-table .fl-meta-source{justify-content:flex-start}.fl-table .fl-meta-status{grid-column:4;justify-content:flex-end}.fl-table td:last-child{position:absolute;right:8px;top:12px}.fl-server-mark{width:38px;height:38px}.fl-server-flag-glyph{font-size:24px}.fl-source,.fl-protocol,.fl-latency,.fl-node-status{white-space:nowrap}.fl-node-status{font-size:12px}.fl-error-row{align-items:stretch;flex-direction:column}.fl-error-row .fl-button{width:100%}.fl-more-menu{min-width:min(220px,calc(100vw - 42px))}}
@media(prefers-reduced-motion:reduce){.fl-button,.fl-tab,.fl-table tbody tr{transition:none}}
.fl-tab-expiry{display:block;margin-top:3px;color:var(--fl-muted);font-size:12px}.fl-tab-expiry-soon{color:var(--fl-amber)}.fl-tab-expiry-expired{color:var(--fl-red)}
body:has(.fastlane-modal) #modal_overlay{position:fixed!important;inset:0!important;display:grid!important;place-items:center!important;padding:24px!important;background:rgba(0,6,8,.78)!important;overflow-y:auto!important}.fastlane-modal{position:relative!important;inset:auto!important;width:min(600px,calc(100vw - 32px))!important;max-width:600px!important;max-height:calc(100vh - 48px)!important;margin:auto!important;padding:0!important;border:1px solid #29434a!important;border-radius:16px!important;background:#071115!important;color:#ddd4ca!important;box-shadow:0 24px 70px rgba(0,0,0,.56)!important;overflow:auto!important;font-family:"Avenir Next",ui-sans-serif,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif!important}.fastlane-modal h4{margin:0!important;padding:24px 24px 8px!important;border:0!important;background:transparent!important;color:#ddd4ca!important;font-size:22px!important;font-weight:750!important;letter-spacing:-.02em!important}.fl-add-form{display:grid;gap:17px!important;min-width:0!important;padding:12px 24px 20px;color:#ddd4ca}.fl-add-field{display:grid;gap:7px}.fl-add-label{display:flex;align-items:baseline;justify-content:space-between;gap:12px;font-size:14px;font-weight:700}.fl-add-optional{color:#756f69;font-size:12px;font-weight:500}.fl-add-form input,.fl-add-form textarea{position:static!important;left:auto!important;top:auto!important;width:100%!important;margin:0!important;border:1px solid #29434a!important;background:#030b0e!important;color:#ddd4ca!important;border-radius:11px!important;padding:12px 14px!important;font:inherit!important;line-height:1.45!important;box-shadow:none!important}.fl-add-form input{height:48px!important}.fl-add-form textarea{min-height:150px!important;max-height:260px!important;resize:vertical}.fl-add-form input::placeholder,.fl-add-form textarea::placeholder{color:#756f69!important;opacity:1}.fl-add-form input:focus-visible,.fl-add-form textarea:focus-visible{border-color:#54df91!important;outline:2px solid #54df91!important;outline-offset:2px}.fl-modal-help{margin:0!important;color:#a89f96!important;font-size:12px;line-height:1.5}.fl-modal-status{min-height:0;margin:0;padding:10px 12px;border:1px solid rgba(255,85,95,.3);border-radius:9px;background:rgba(255,85,95,.06);color:#ffb0b5;font-size:12px;line-height:1.45}.fl-modal-status:empty{display:none}.fl-modal-status-working{border-color:rgba(84,223,145,.3);background:rgba(84,223,145,.06);color:#9be8bd}.fastlane-modal .right{display:flex!important;justify-content:flex-end!important;gap:10px!important;margin:0!important;padding:16px 24px 24px!important;border:0!important;border-top:1px solid #1a2b31!important;background:#071115!important}.fastlane-modal .fl-modal-button{min-width:112px!important;min-height:44px!important;margin:0!important;border:1px solid #29434a!important;border-radius:10px!important;padding:10px 16px!important;background:#0d1c21!important;color:#ddd4ca!important;font:inherit!important;font-size:14px!important;font-weight:700!important;line-height:1!important;box-shadow:none!important;cursor:pointer}.fastlane-modal .fl-modal-button:hover{background:#10252b!important;border-color:#3c626b!important}.fastlane-modal .fl-modal-primary{border-color:#45a974!important;background:#45a974!important;color:#04110a!important}.fastlane-modal .fl-modal-primary:hover{border-color:#54df91!important;background:#54df91!important}.fastlane-modal .fl-modal-button:disabled{opacity:.5;cursor:wait}.fastlane-modal .fl-modal-button:focus-visible{outline:2px solid #54df91!important;outline-offset:3px!important}
@media(max-width:760px){body:has(.fastlane-modal) #modal_overlay{place-items:end center!important;padding:12px!important}.fastlane-modal{width:calc(100vw - 24px)!important;max-height:calc(100vh - 24px)!important;border-radius:14px!important}.fastlane-modal h4{padding:20px 18px 6px!important;font-size:20px!important}.fl-add-form{gap:14px!important;padding:10px 18px 16px}.fl-add-form textarea{min-height:132px!important;max-height:220px!important}.fastlane-modal .right{padding:14px 18px 18px!important}.fastlane-modal .fl-modal-button{flex:1;min-width:0!important}}
body:not(.modal-overlay-active) #modal_overlay:has(.fastlane-modal){display:none!important}
.fl-source-actions{flex-direction:column}.fl-source-actions .fl-button{flex:1 1 0;min-height:45px}.fl-source-actions .fl-button+.fl-button{border-top:1px solid var(--fl-line)}
.fastlane-root .fl-icon path{fill:none!important;stroke:currentColor!important}
.fastlane-root .fl-server-flag-glyph{transform:translateY(2px)}
.fl-add-mode{display:grid;grid-template-columns:1fr 1fr;width:100%;padding:3px;border:1px solid #29434a;border-radius:11px;background:#030b0e}.fl-add-mode-button{appearance:none;min-height:42px;border:0;border-radius:8px;background:transparent;color:#a89f96;font:inherit;font-weight:700;cursor:pointer}.fl-add-mode-button-active{background:#173328;color:#8df0b8}.fl-add-pane{display:grid;gap:17px}.fl-add-pane[hidden]{display:none!important}.fl-file-picker{display:grid;place-items:center;min-height:132px;padding:20px;border:1px dashed #3a5961;border-radius:11px;background:#030b0e;color:#a89f96;text-align:center;cursor:pointer}.fl-file-picker:hover{border-color:#54df91;color:#ddd4ca}.fl-file-picker input{position:absolute!important;width:1px!important;height:1px!important;opacity:0!important;pointer-events:none}.fl-file-picker strong{display:block;margin-bottom:5px;color:#ddd4ca;font-size:15px}.fl-file-list{display:grid;gap:6px}.fl-file-item{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:9px 11px;border:1px solid #1a2b31;border-radius:8px;background:#081216;color:#ddd4ca;font-size:13px}.fl-file-size{color:#756f69;white-space:nowrap}.fl-expired-note{margin-bottom:14px;padding:11px 13px;border:1px solid rgba(255,85,95,.3);border-radius:9px;background:rgba(255,85,95,.06);color:#ffb0b5;font-size:13px}.fl-expired-row{opacity:.74}.fl-expired-row:hover{background:#071115!important}.fl-node-status-expired .fl-node-status-dot{background:var(--fl-red)}
@media(max-width:760px){.fl-source-actions .fl-button{min-width:170px}.fl-table-single tbody tr{grid-template-columns:repeat(3,minmax(0,1fr))}.fl-table-single .fl-meta-status{grid-column:3}.fastlane-root .fl-table td{border-bottom:0!important}}
`;

// Shared GET latency scale for the active connection and server rows.
css += '.fastlane-root .fl-latency,.fastlane-root .fl-status-cell-latency{color:var(--fl-muted)}.fastlane-root .fl-latency-good{color:var(--fl-green)}.fastlane-root .fl-latency-mid{color:var(--fl-amber)}.fastlane-root .fl-latency-slow{color:var(--fl-orange,#f0a35a)}.fastlane-root .fl-latency-critical,.fastlane-root .fl-latency-bad{color:var(--fl-red)}';

return view.extend({
	load: function() {
		this.filter = this.filter || 'all';
		this.showHidden = this.showHidden || false;
		this.query = this.query || '';
		this.sort = this.sort || 'latency';
		this.country = this.country || 'all';
		this.protocol = this.protocol || 'all';
		this.busy = '';
		this.error = null;
		this.dismissedErrors = this.dismissedErrors || {};
		this.expandedErrors = this.expandedErrors || {};
		this.pings = this.readPings();
		this.testingNodes = this.testingNodes || {};
		this.activeMenuKey = '';
		this.batchTesting = false;
		this.batchDone = 0;
		this.batchTotal = 0;
		return this.fetchData().then(L.bind(function(data) {
			this.startPolling();
			return data;
		}, this));
	},

	fetchData: function() {
		var previous = this.pageData || [];
		return Promise.all([
			this.execJSON([ '--json', 'status' ]).catch(function(err) { return { __error: err.message }; }),
			this.execJSON([ '--json', 'list', 'subscriptions' ]).catch(function(err) { return { __error: err.message }; }),
			this.execJSON([ '--json', 'inspect', 'health-check-status' ]).catch(function() { return { status: 'idle' }; })
		]).then(L.bind(function(data) {
			this.fetchErrors = {};
			var status = data[0];
			var subscriptions = data[1];
			if (status && status.__error) {
				this.fetchErrors.status = status.__error;
				if (previous[0] && !previous[0].__error)
					status = previous[0];
			} else if (this.toastErrors) {
				delete this.toastErrors.status;
			}
			if (subscriptions && subscriptions.__error) {
				this.fetchErrors.subscriptions = subscriptions.__error;
				if (Array.isArray(previous[1]))
					subscriptions = previous[1];
			} else if (this.toastErrors) {
				delete this.toastErrors.subscriptions;
			}
			this.pageData = [ status, subscriptions, data[2] || { status: 'idle' } ];
			var subscriptionList = Array.isArray(subscriptions) ? subscriptions : [];
			this.mergePersistedPings(status, subscriptionList);
			this.mergeBackgroundPings(data[2], subscriptionList);
			if (!this.showHidden && this.filter !== 'all' && !subscriptionList.some(L.bind(function(sub) { return sub.id === this.filter; }, this)))
				this.filter = 'all';
			return this.pageData;
		}, this));
	},

	execJSON: function(args) {
		return fs.exec(binary, args).then(function(result) {
			if (result.code !== 0)
				throw new Error(commandError(result));
			try {
				return JSON.parse(trim(result.stdout));
			}
			catch (err) {
				throw new Error(_('Fast Lane returned an invalid response.'));
			}
		});
	},

	exec: function(args) {
		return fs.exec(binary, args).then(function(result) {
			if (result.code !== 0)
				throw new Error(commandError(result));
			return result;
		});
	},

	readPings: function() {
		try {
			var parsed = JSON.parse(window.sessionStorage.getItem(pingKey) || '{}');
			if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed))
				return {};
			var restored = {};
			Object.keys(parsed).forEach(function(key) {
				// A failed invocation says nothing reliable about the server and
				// must not survive a page reload as a red availability result.
				if (!parsed[key] || parsed[key].test_error !== true)
					restored[key] = parsed[key];
			});
			return restored;
		}
		catch (err) {
			return {};
		}
	},

	mergePersistedPings: function(status, subscriptions) {
		var state = status && !status.__error ? status.state || {} : {};
		var health = state.health && typeof state.health === 'object' ? state.health : {};
		var stored = this.readPings();
		var current = this.pings && typeof this.pings === 'object' ? this.pings : {};
		var session = {};
		Object.keys(stored).forEach(function(key) {
			var observation = sessionObservation(stored[key]);
			if (key && observation) session[key] = observation;
		});
		Object.keys(current).forEach(function(key) {
			var observation = sessionObservation(current[key]);
			if (key && observation) session[key] = fresherObservation(session[key], observation);
		});

		var merged = {};
		for (var i = 0; i < subscriptions.length; i++) {
			var sub = subscriptions[i] || {};
			var nodes = Array.isArray(sub.nodes) ? sub.nodes : [];
			for (var n = 0; n < nodes.length; n++) {
				var node = nodes[n] || {};
				var key = trim(sub.id) + ':' + trim(node.id);
				var persisted = persistedObservation(health[node.id] || health[key]);
				merged[key] = fresherObservation(persisted, session[key]);
			}
		}
		Object.keys(session).forEach(function(key) {
			if (!Object.prototype.hasOwnProperty.call(merged, key))
				merged[key] = session[key];
		});
		this.pings = merged;
	},

	mergeBackgroundPings: function(progress, subscriptions) {
		var results = progress && progress.results && typeof progress.results === 'object' ? progress.results : {};
		for (var i = 0; i < subscriptions.length; i++) {
			var sub = subscriptions[i] || {};
			var nodes = Array.isArray(sub.nodes) ? sub.nodes : [];
			for (var n = 0; n < nodes.length; n++) {
				var node = nodes[n] || {};
				var key = trim(sub.id) + ':' + trim(node.id);
				var observed = persistedObservation(results[node.id] || results[key]);
				if (observed)
					this.pings[key] = fresherObservation(this.pings[key], observed);
			}
		}
	},

	writePings: function() {
		var persisted = {};
		Object.keys(this.pings || {}).forEach(L.bind(function(key) {
			if (this.pings[key] && this.pings[key].test_error !== true)
				persisted[key] = this.pings[key];
		}, this));
		try { window.sessionStorage.setItem(pingKey, JSON.stringify(persisted)); }
		catch (err) {}
	},

	refreshView: function() {
		return this.fetchData().then(L.bind(function() { this.update(); }, this));
	},

	startPolling: function() {
		if (this.pollFn)
			return;
		this.pollFn = L.bind(function() {
			if (this.busy || this.backgroundRefreshPromise)
				return Promise.resolve();
			this.backgroundRefreshPromise = this.fetchData()
				.then(L.bind(function() { this.update(); }, this))
				.finally(L.bind(function() { this.backgroundRefreshPromise = null; }, this));
			return this.backgroundRefreshPromise;
		}, this);
		poll.add(this.pollFn, vpnPollInterval);
		poll.start();
		if (!this.visibilityHandler) {
			this.visibilityHandler = L.bind(function() {
				if (!document.hidden && this.pollFn)
					this.pollFn();
			}, this);
			document.addEventListener('visibilitychange', this.visibilityHandler);
		}
		if (!this.beforeUnloadHandler) {
			this.beforeUnloadHandler = L.bind(function() {
				if (this.pollFn)
					poll.remove(this.pollFn);
				this.pollFn = null;
			}, this);
			window.addEventListener('beforeunload', this.beforeUnloadHandler);
		}
		if (!this.documentClickHandler) {
			this.documentClickHandler = L.bind(this.handleDocumentClick, this);
			document.addEventListener('click', this.documentClickHandler);
		}
	},

	update: function() {
		var target = document.getElementById('fastlane-content');
		if (target)
			dom.content(target, this.renderContent());
	},

	runAction: function(label, promise, success) {
		this.busy = label;
		this.error = null;
		this.update();
		return Promise.resolve(promise).then(L.bind(function(result) {
			this.busy = '';
			if (success)
				fastlaneShell.showToast(success, 'success');
			return this.refreshView().then(function() { return result; });
		}, this)).catch(L.bind(function(err) {
			this.busy = '';
			this.error = null;
			var error = friendlyError(err.message || String(err));
			fastlaneShell.showToast(error.message, 'error', error.details);
			return this.refreshView().catch(L.bind(function() { this.update(); }, this));
		}, this));
	},

	handleToggleError: function(key, ev) {
		if (ev) ev.preventDefault();
		this.expandedErrors[key] = !this.expandedErrors[key];
		this.update();
	},

	handleDismissError: function(key, ev) {
		if (ev) ev.preventDefault();
		if (key === 'action')
			this.error = null;
		else
			this.dismissedErrors[key] = true;
		delete this.expandedErrors[key];
		this.update();
	},

	handleServerMenuToggle: function(key, ev) {
		if (ev) { ev.preventDefault(); ev.stopPropagation(); }
		this.activeMenuKey = this.activeMenuKey === key ? '' : key;
		this.update();
	},

	handleDocumentClick: function(ev) {
		if (!this.activeMenuKey)
			return;
		var target = ev && ev.target;
		if (target && target.closest && target.closest('.fl-more'))
			return;
		this.activeMenuKey = '';
		this.update();
	},

	renderError: function(key, value, fallback, retry) {
		if (this.dismissedErrors[key])
			return '';

		var error = value && value.message && value.details ? value : friendlyError(value, fallback);
		var expanded = !!this.expandedErrors[key];
		var actions = [];

		if (retry)
			actions.push(E('button', { class: 'fl-button', disabled: this.busy ? 'disabled' : null, click: retry }, [ _('Retry') ]));
		actions.push(E('button', {
			class: 'fl-button fl-error-action',
			title: expanded ? _('Hide details') : _('Show technical error'),
			'aria-label': expanded ? _('Hide details') : _('Show technical error'),
			'aria-expanded': expanded ? 'true' : 'false',
			click: ui.createHandlerFn(this, 'handleToggleError', key)
		}, [ icon('info') ]));
		actions.push(E('button', {
			class: 'fl-button fl-error-action',
			title: _('Close'),
			'aria-label': _('Close error'),
			click: ui.createHandlerFn(this, 'handleDismissError', key)
		}, [ icon('close') ]));

		return E('div', { class: 'fl-error fl-error-notice', role: 'alert' }, [
			E('div', { class: 'fl-error-message' }, [ error.message ]),
			E('div', { class: 'fl-error-actions' }, actions),
			expanded ? E('pre', { class: 'fl-error-details', tabindex: '0' }, [ error.details ]) : ''
		]);
	},

	handleFilter: function(id, ev) {
		if (ev) ev.preventDefault();
		this.activeMenuKey = '';
		this.showHidden = id === 'hidden';
		this.filter = this.showHidden ? 'all' : id;
		this.update();
	},

	autoExcludedNodeKey: function(subID, nodeID) {
		return trim(subID) && trim(nodeID) ? trim(subID) + '/' + trim(nodeID) : '';
	},

	hiddenNodeKeys: function() {
		var settings = this.status().settings || {};
		var raw = Array.isArray(settings.auto_excluded_nodes) ? settings.auto_excluded_nodes : [];
		var seen = {};
		return raw.map(trim).filter(function(value) {
			if (!value || seen[value]) return false;
			seen[value] = true;
			return true;
		}).sort();
	},

	isHidden: function(subID, nodeID) {
		return this.hiddenNodeKeys().indexOf(this.autoExcludedNodeKey(subID, nodeID)) >= 0;
	},

	setLocalHiddenNodeKeys: function(values) {
		var status = this.status();
		status.settings = status.settings || {};
		status.settings.auto_excluded_nodes = (values || []).slice();
	},

	handleHidden: function(subID, nodeID, shouldHide, ev) {
		if (ev) { ev.preventDefault(); ev.stopPropagation(); }
		var target = this.autoExcludedNodeKey(subID, nodeID);
		var previous = this.hiddenNodeKeys();
		var values = previous.filter(function(value) { return value !== target; });
		if (shouldHide && target) values.push(target);
		values.sort();
		this.activeMenuKey = '';
		this.setLocalHiddenNodeKeys(values);
		this.update();
		var action = this.exec([ 'settings', 'set', 'auto.excluded-nodes', values.join(', ') ])
			.catch(L.bind(function(err) {
				this.setLocalHiddenNodeKeys(previous);
				this.update();
				throw err;
			}, this));
		return this.runAction(
			shouldHide ? _('Hiding server…') : _('Restoring server…'),
			action,
			shouldHide ? _('Server hidden and excluded from automatic selection.') : _('Server restored to the list and automatic selection.')
		);
	},

	handleSearch: function(ev) {
		this.query = trim(ev && ev.target && ev.target.value).toLowerCase();
		var table = document.querySelector('#fastlane-content .fl-table-wrap');
		var count = document.querySelector('#fastlane-content .fl-visible-count');
		if (table) dom.content(table, this.renderTable());
		if (count) count.textContent = String(this.visibleRows().length) + ' ' + _('in the list');
	},

	handleCountry: function(ev) {
		this.country = trim(ev && ev.target && ev.target.value) || 'all';
		this.update();
	},

	handleProtocol: function(ev) {
		this.protocol = trim(ev && ev.target && ev.target.value) || 'all';
		this.update();
	},

	handleManualMode: function(ev) {
		if (ev) ev.preventDefault();
		var row = document.querySelector('#fastlane-content .fl-table tbody tr[tabindex]');
		if (row) row.focus();
		fastlaneShell.showToast(_('Choose a server from the list to pin it manually.'), 'info');
	},

	handleSort: function(ev) {
		this.sort = trim(ev && ev.target && ev.target.value) || 'latency';
		this.update();
	},

	handleReload: function(ev) {
		if (ev) ev.preventDefault();
		return this.runAction(_('Refreshing interface data…'), Promise.resolve(), _('List refreshed.'));
	},

	handleRefreshSubscriptions: function(ev) {
		if (ev) ev.preventDefault();
		var selected = this.selectedSubscription();
		var args = selected ? [ 'refresh', '--subscription', selected.id ] : [ 'refresh', '--all' ];
		return this.runAction(_('Updating subscriptions…'), this.exec(args), _('Subscriptions updated.'));
	},

	handleRefreshSource: function(subID, ev) {
		if (ev) { ev.preventDefault(); ev.stopPropagation(); }
		delete this.dismissedErrors['subscription-' + subID];
		return this.runAction(_('Retrying subscription update…'), this.exec([ 'refresh', '--subscription', subID ]), _('Subscription updated.'));
	},

	handleAuto: function(ev) {
		if (ev) ev.preventDefault();
		return this.runAction(
			_('Starting the check on the router…'),
			this.execJSON([ '--json', 'inspect', 'health-check', '--subscription', 'all' ]),
			_('Background GET check started. You can close the page.')
		);
	},

	handleDisconnect: function(ev) {
		if (ev) ev.preventDefault();
		return this.runAction(_('Disconnecting VPN…'), this.exec([ 'disconnect' ]), _('VPN disconnected.'));
	},

	handleConnect: function(subID, nodeID, ev) {
		if (ev) { ev.preventDefault(); ev.stopPropagation(); }
		this.activeMenuKey = '';
		return this.runAction(_('Connecting the selected server…'), this.exec([ 'connect', '--subscription', subID, '--node', nodeID ]), _('Server pinned manually.'));
	},

	handleRowKey: function(subID, nodeID, ev) {
		if (!ev || (ev.key !== 'Enter' && ev.key !== ' ')) return;
		var target = ev.target;
		if (target && target !== ev.currentTarget && target.closest && target.closest('a, button, input, select, textarea, summary, details')) return;
		ev.preventDefault();
		return this.handleConnect(subID, nodeID, ev);
	},

	applyURLTestResult: function(subID, nodeID, result) {
		var latency = positiveLatency(result && result.latency_ms);
		if (latency == null)
			throw new Error(_('GET did not return a positive latency.'));
		this.pings = this.pings || {};
		this.pings[subID + ':' + nodeID] = {
			node_id: nodeID,
			healthy: true,
			latency_ms: latency,
			checked_at: result.checked_at,
			url: result.url,
			url_test: true
		};
		this.writePings();
		this.update();
	},

	handleURLTests: function(ev) {
		if (ev) ev.preventDefault();
		var selected = this.selectedSubscription();
		var scope = selected && !isSubscriptionExpired(selected) ? selected.id : 'all';
		return this.runAction(
			_('Queuing GET check…'),
			this.execJSON([ '--json', 'inspect', 'health-check', '--subscription', scope ]),
			_('Background GET check started. You can close the page.')
		);
	},

	handleURLTest: function(subID, nodeID, ev) {
		if (ev) { ev.preventDefault(); ev.stopPropagation(); }
		this.activeMenuKey = '';
		var key = subID + ':' + nodeID;
		if (this.testingNodes[key]) return Promise.resolve();
		this.testingNodes[key] = true;
		this.update();
		var action = this.execJSON([ '--json', 'inspect', 'url-test', '--subscription', subID, '--node', nodeID ])
			.then(L.bind(this.applyURLTestResult, this, subID, nodeID))
			.then(L.bind(function() { fastlaneShell.showToast(_('Ping (GET) updated.'), 'success'); }, this))
			.catch(L.bind(function(err) {
				this.pings[key] = { node_id: nodeID, healthy: false, latency_ms: null, checked_at: new Date().toISOString(), url_test: true, test_error: true };
				this.writePings();
				var error = friendlyError(err.message || String(err));
				fastlaneShell.showToast(error.message, 'error', error.details);
			}, this))
			.then(L.bind(function() { delete this.testingNodes[key]; this.update(); }, this));
		return action;
	},

	handleAddOpen: function(ev) {
		if (ev) ev.preventDefault();
		var name = E('input', { placeholder: _('For example, Liberty'), autocomplete: 'off' });
		var source = E('textarea', { placeholder: _('Subscription link, VLESS key, Base64, or Xray JSON'), spellcheck: 'false', autocapitalize: 'none' });
		var files = E('input', { type: 'file', multiple: 'multiple', accept: '.yaml,.yml,.json,.txt,application/x-yaml,text/yaml' });
		var fileList = E('div', { class: 'fl-file-list' });
		var subscriptionPane = E('div', { class: 'fl-add-pane' }, [
			E('label', { class: 'fl-add-field' }, [
				E('span', { class: 'fl-add-label' }, [ E('span', {}, [ _('Name') ]), E('span', { class: 'fl-add-optional' }, [ _('Optional') ]) ]),
				name
			]),
			E('label', { class: 'fl-add-field' }, [ E('span', { class: 'fl-add-label' }, [ _('Link or configuration') ]), source ]),
			E('p', { class: 'fl-modal-help' }, [ _('The format is detected automatically. Links are refreshed on schedule.') ])
		]);
		var filePane = E('div', { class: 'fl-add-pane', hidden: 'hidden' }, [
			E('label', { class: 'fl-file-picker' }, [ files, E('span', {}, [ E('strong', {}, [ _('Choose YAML files') ]), _('You can add several files at once') ]) ]),
			fileList,
			E('p', { class: 'fl-modal-help' }, [ _('Use a Clash/Mihomo YAML file with a proxies: array or a provider file with payload:. Each file becomes a separate source; removing it also removes its servers.') ])
		]);
		var mode = 'subscription';
		var subscriptionButton = E('button', { class: 'fl-add-mode-button fl-add-mode-button-active', type: 'button' }, [ _('Subscription') ]);
		var fileButton = E('button', { class: 'fl-add-mode-button', type: 'button' }, [ _('From file') ]);
		function setMode(next) {
			mode = next;
			subscriptionPane.hidden = next !== 'subscription';
			filePane.hidden = next !== 'file';
			subscriptionButton.className = 'fl-add-mode-button' + (next === 'subscription' ? ' fl-add-mode-button-active' : '');
			fileButton.className = 'fl-add-mode-button' + (next === 'file' ? ' fl-add-mode-button-active' : '');
		}
		subscriptionButton.addEventListener('click', function() { setMode('subscription'); source.focus(); });
		fileButton.addEventListener('click', function() { setMode('file'); files.focus(); });
		files.addEventListener('change', function() {
			var selected = Array.prototype.slice.call(files.files || []);
			dom.content(fileList, selected.map(function(file) {
				return E('div', { class: 'fl-file-item' }, [ E('span', {}, [ file.name ]), E('span', { class: 'fl-file-size' }, [ Math.max(1, Math.round(file.size / 1024)) + ' KB' ]) ]);
			}));
		});
		var error = E('div', { class: 'fl-modal-status', role: 'status', 'aria-live': 'polite' });
		var submit = E('button', { class: 'fl-modal-button fl-modal-primary', type: 'button' }, [ _('Add') ]);
		submit.addEventListener('click', L.bind(this.handleAddSubmit, this, name, source, files, function() { return mode; }, error, submit));
		ui.showModal(_('Add servers'), [
			E('div', { class: 'fl-add-form' }, [
				E('div', { class: 'fl-add-mode', role: 'tablist', 'aria-label': _('Add method') }, [ subscriptionButton, fileButton ]),
				subscriptionPane,
				filePane,
				error
			]),
			E('div', { class: 'right' }, [
				E('button', { class: 'fl-modal-button', type: 'button', click: ui.hideModal }, [ _('Cancel') ]),
				submit
			])
		]);
		var modal = document.querySelector('.modal');
		if (modal) modal.classList.add('fastlane-modal');
		window.requestAnimationFrame(function() { source.focus(); });
	},

	handleAddSubmit: function(nameInput, sourceInput, fileInput, modeGetter, errorBox, submitButton, ev) {
		if (ev) ev.preventDefault();
		if (modeGetter() === 'file')
			return this.handleFileAddSubmit(fileInput, errorBox, submitButton);
		var value = trim(sourceInput.value);
		if (!value) {
			errorBox.className = 'fl-modal-status';
			errorBox.textContent = _('Paste a link or configuration.');
			sourceInput.focus();
			return;
		}
		var args = [ 'add' ];
		if (trim(nameInput.value))
			args.push('--name', trim(nameInput.value));
		if (/^https?:\/\/\S+$/.test(value))
			args.push('--url', value);
		else
			args.push('--raw', value);
		errorBox.className = 'fl-modal-status fl-modal-status-working';
		errorBox.textContent = _('Adding and checking subscription…');
		submitButton.disabled = true;
		submitButton.textContent = _('Adding…');
		return this.exec(args).then(L.bind(function() {
			ui.hideModal();
			fastlaneShell.showToast(_('Subscription added.'), 'success');
			return this.refreshView();
		}, this)).catch(function(err) {
			var error = friendlyError((err && err.message) || String(err), _('Could not add the subscription. Check the link or configuration.'));
			errorBox.className = 'fl-modal-status';
			errorBox.textContent = '';
			fastlaneShell.showToast(error.message, 'error', error.details);
			submitButton.disabled = false;
			submitButton.textContent = _('Add');
			sourceInput.focus();
		});
	},

	handleFileAddSubmit: function(fileInput, errorBox, submitButton) {
		var selected = Array.prototype.slice.call(fileInput.files || []);
		if (!selected.length) {
			errorBox.className = 'fl-modal-status';
			errorBox.textContent = _('Choose at least one YAML file.');
			return;
		}
		if (selected.length > 10 || selected.some(function(file) { return file.size > 96 * 1024; })) {
			errorBox.className = 'fl-modal-status';
			errorBox.textContent = selected.length > 10 ? _('You can add no more than 10 files at once.') : _('A file is larger than 96 KB. Split it into several provider files.');
			return;
		}
		var self = this;
		errorBox.className = 'fl-modal-status fl-modal-status-working';
		errorBox.textContent = _('Reading files…');
		submitButton.disabled = true;
		submitButton.textContent = _('Adding…');
		function read(file) {
			return new Promise(function(resolve, reject) {
				var reader = new FileReader();
				reader.onload = function() { resolve(String(reader.result || '')); };
				reader.onerror = function() { reject(new Error(_('Could not read') + ' ' + file.name)); };
				reader.readAsText(file);
			});
		}
		var chain = Promise.resolve();
		selected.forEach(function(file, index) {
			chain = chain.then(function() {
				errorBox.textContent = _('Adding') + ' ' + (index + 1) + ' / ' + selected.length + ': ' + file.name;
				return read(file).then(function(content) {
					return self.exec([ 'add', '--file-name', file.name, '--raw', content ]);
				});
			});
		});
		return chain.then(function() {
			ui.hideModal();
			fastlaneShell.showToast(_('Files added:') + ' ' + selected.length + '.', 'success');
			return self.refreshView();
		}).catch(function(err) {
			var error = friendlyError((err && err.message) || String(err), _('Could not add the file. Check the YAML format.'));
			errorBox.className = 'fl-modal-status';
			errorBox.textContent = error.message;
			submitButton.disabled = false;
			submitButton.textContent = _('Add');
		});
	},

	handleRemove: function(subID, ev) {
		if (ev) ev.preventDefault();
		var sub = this.subscriptions().filter(function(item) { return item.id === subID; })[0];
		var fileSource = sub && sub.source_type === 'file';
		if (!window.confirm(fileSource ? _('Remove this file and all its servers?') : _('Remove this subscription?')))
			return;
		return this.runAction(fileSource ? _('Removing file…') : _('Removing subscription…'), this.exec([ 'remove', subID ]), fileSource ? _('File and its servers removed.') : _('Subscription removed.'));
	},

	subscriptions: function() {
		var value = this.pageData && this.pageData[1];
		return Array.isArray(value) ? value : [];
	},

	status: function() {
		var value = this.pageData && this.pageData[0];
		return value && !value.__error ? value : {};
	},

	backgroundCheck: function() {
		var value = this.pageData && this.pageData[2];
		return value && typeof value === 'object' ? value : { status: 'idle' };
	},

	selectedSubscription: function() {
		var subscriptions = this.subscriptions();
		if (this.showHidden) return null;
		var available = subscriptions.filter(function(sub) { return !isSubscriptionExpired(sub); });
		if (available.length === 1 && this.filter === 'all') return available[0];
		return subscriptions.filter(L.bind(function(sub) { return sub.id === this.filter; }, this))[0];
	},

	poolSubscriptions: function() {
		var subscriptions = this.subscriptions();
		if (!this.showHidden && this.filter !== 'all')
			return subscriptions.filter(L.bind(function(sub) { return sub.id === this.filter; }, this));
		return subscriptions.filter(function(sub) { return !isSubscriptionExpired(sub); });
	},

	visibleRows: function() {
		var rows = [];
		var subscriptions = this.poolSubscriptions();
		for (var i = 0; i < subscriptions.length; i++) {
			var sub = subscriptions[i];
			if (this.filter !== 'all' && sub.id !== this.filter)
				continue;
			var nodes = Array.isArray(sub.nodes) ? sub.nodes : [];
			for (var n = 0; n < nodes.length; n++) {
				var node = nodes[n];
				var hidden = this.isHidden(sub.id, node.id);
				if (this.showHidden !== hidden)
					continue;
				var haystack = (nodeName(node) + ' ' + sourceName(sub) + ' ' + trim(node.protocol) + ' ' + trim(node.address)).toLowerCase();
				if (this.query && haystack.indexOf(this.query) < 0)
					continue;
				if (this.country !== 'all' && nodeLocation(node).country !== this.country)
					continue;
				if (this.protocol !== 'all' && trim(node.protocol).toLowerCase() !== this.protocol)
					continue;
					var observed = this.pings[sub.id + ':' + node.id] || {};
					var latency = positiveLatency(observed.latency_ms);
				rows.push({ sub: sub, node: node, observed: observed, latency: latency, hidden: hidden });
			}
		}
		rows.sort(L.bind(function(a, b) {
			if (this.sort === 'name') return nodeName(a.node).localeCompare(nodeName(b.node));
			if (this.sort === 'source') return sourceName(a.sub).localeCompare(sourceName(b.sub));
			var av = positiveLatency(a.latency), bv = positiveLatency(b.latency);
			if (av == null && bv == null) return nodeName(a.node).localeCompare(nodeName(b.node));
			if (av == null) return 1;
			if (bv == null) return -1;
			return av - bv || nodeName(a.node).localeCompare(nodeName(b.node));
		}, this));
		return rows;
	},

	filterCountries: function() {
		var seen = {};
		var subscriptions = this.poolSubscriptions();
		for (var i = 0; i < subscriptions.length; i++) {
			var nodes = subscriptions[i].nodes || [];
			for (var n = 0; n < nodes.length; n++)
				seen[nodeLocation(nodes[n]).country] = true;
		}
		return Object.keys(seen).filter(Boolean).sort();
	},

	filterProtocols: function() {
		var seen = {};
		var subscriptions = this.poolSubscriptions();
		for (var i = 0; i < subscriptions.length; i++) {
			var nodes = subscriptions[i].nodes || [];
			for (var n = 0; n < nodes.length; n++) {
				var protocol = trim(nodes[n].protocol).toLowerCase();
				if (protocol) seen[protocol] = true;
			}
		}
		return Object.keys(seen).sort();
	},

	latencyClass: function(value, observed) {
		if (observed && observed.test_error === true) return '';
		if (observed && observed.healthy === false) return 'fl-latency-bad';
		if (positiveLatency(value) == null) return '';
		if (Number(value) <= 100) return 'fl-latency-good';
		if (Number(value) <= 200) return 'fl-latency-mid';
		if (Number(value) > 1000) return 'fl-latency-critical';
		return 'fl-latency-slow';
	},

	renderStatus: function() {
		var status = this.status();
		var state = status.state || {};
		var connected = state.connected === true;
		var hasAvailableNodes = this.subscriptions().filter(function(sub) { return !isSubscriptionExpired(sub); }).some(L.bind(function(sub) {
			return (sub.nodes || []).some(L.bind(function(node) { return !this.isHidden(sub.id, node.id); }, this));
		}, this));
		var mode = connected ? (state.mode === 'auto' ? 'auto' : 'manual') : 'disconnected';
		var activeSubscription = status.active_subscription;
		if (!activeSubscription && connected && state.active_subscription_id)
			activeSubscription = this.subscriptions().filter(function(sub) { return sub.id === state.active_subscription_id; })[0];
		var activeSource = connected && activeSubscription ? sourceName(activeSubscription) : '—';
		var activeNode = connected && status.active_node ? nodeName(status.active_node) : '—';
		var observed = this.pings[state.active_subscription_id + ':' + state.active_node_id] || {};
		return E('div', { class: 'fl-status' }, [
			E('div', { class: 'fl-status-cell fl-status-main' }, [ E('span', { class: 'fl-dot ' + (connected ? 'fl-dot-on' : '') }), connected ? _('VPN on') : _('VPN off') ]),
			E('div', { class: 'fl-status-cell' }, [ E('span', { class: 'fl-status-cell-label' }, [ _('Server') ]), E('span', { class: 'fl-status-cell-value' }, [ nodeLocation({ name: activeNode }).country ]) ]),
			E('div', { class: 'fl-status-cell' }, [ E('span', { class: 'fl-status-cell-label' }, [ _('Source') ]), E('span', { class: 'fl-status-cell-value' }, [ activeSource ]) ]),
			E('div', { class: 'fl-status-cell', title: _('GET: up to 100 ms is low latency; 101–200 ms is medium; 201–1000 ms is high; over 1000 ms is very high. A successful GET means the server is reachable even if it is slow. This is not a download speed test.') }, [ E('span', { class: 'fl-status-cell-label' }, [ _('Ping (GET)') ]), E('span', { class: 'fl-status-cell-value fl-status-cell-latency ' + this.latencyClass(observed.latency_ms, observed) }, [ formatLatency(observed.latency_ms) ]) ]),
			E('div', { class: 'fl-mode-switch', 'aria-label': _('Connection mode') }, [
				E('button', { class: 'fl-mode-option ' + (mode === 'auto' ? 'fl-mode-option-active' : ''), disabled: this.busy || mode === 'auto' || !hasAvailableNodes ? 'disabled' : null, title: hasAvailableNodes ? '' : _('Add a subscription first'), click: ui.createHandlerFn(this, 'handleAuto') }, [ _('Auto') ]),
				E('button', { class: 'fl-mode-option ' + (mode === 'manual' ? 'fl-mode-option-active' : ''), disabled: this.busy || mode === 'manual' || !hasAvailableNodes ? 'disabled' : null, title: hasAvailableNodes ? '' : _('Add a subscription first'), click: ui.createHandlerFn(this, 'handleManualMode') }, [ _('Manual') ])
			]),
			E('button', { class: 'fl-button fl-button-danger fl-status-disconnect', disabled: this.busy || !connected ? 'disabled' : null, click: ui.createHandlerFn(this, 'handleDisconnect') }, [ _('Disconnect') ])
		]);
	},

	renderTabs: function() {
		var subscriptions = this.subscriptions();
		var availableSubscriptions = subscriptions.filter(function(sub) { return !isSubscriptionExpired(sub); });
		var hiddenCount = 0;
		var total = 0;
		for (var t = 0; t < subscriptions.length; t++) {
			if (isSubscriptionExpired(subscriptions[t]))
				continue;
			var totalNodes = Array.isArray(subscriptions[t].nodes) ? subscriptions[t].nodes : [];
			for (var x = 0; x < totalNodes.length; x++) {
				if (this.isHidden(subscriptions[t].id, totalNodes[x].id)) hiddenCount++;
				else total++;
			}
		}
		var tabs = [];
		if (availableSubscriptions.length > 1)
			tabs.push(E('button', { class: 'fl-tab ' + (!this.showHidden && this.filter === 'all' ? 'fl-tab-active' : ''), role: 'tab', 'aria-selected': !this.showHidden && this.filter === 'all' ? 'true' : 'false', click: ui.createHandlerFn(this, 'handleFilter', 'all') }, [
				E('span', { class: 'fl-tab-top' }, [ E('span', {}, [ _('All servers') ]), E('span', { class: 'fl-count' }, [ String(total) ]) ]),
				E('span', { class: 'fl-tab-meta' }, [ E('span', { class: 'fl-source-status' }), _('Combined pool') ])
			]));
		for (var i = 0; i < subscriptions.length; i++) {
			var sub = subscriptions[i];
			var subActive = !this.showHidden && (this.filter === sub.id || (availableSubscriptions.length === 1 && this.filter === 'all' && !isSubscriptionExpired(sub)));
			var visibleCount = (sub.nodes || []).filter(L.bind(function(node) { return !this.isHidden(sub.id, node.id); }, this)).length;
			var subMeta = sub.last_error ? _('Update failed') : (sub.last_updated_at ? _('Updated') + ' ' + formatTime(sub.last_updated_at) : _('Ready'));
			var expiry = subscriptionExpiryPresentation(sub.expires_at);
			tabs.push(E('button', { class: 'fl-tab ' + (subActive ? 'fl-tab-active' : ''), role: 'tab', 'aria-selected': subActive ? 'true' : 'false', click: ui.createHandlerFn(this, 'handleFilter', sub.id), 'aria-label': sourceName(sub) + (sub.last_error ? ': ' + _('update failed') : '') }, [
				E('span', { class: 'fl-tab-top' }, [ E('span', {}, [ sourceName(sub) ]), E('span', { class: 'fl-count' }, [ String(visibleCount) ]) ]),
				E('span', { class: 'fl-tab-meta' }, [ E('span', { class: 'fl-source-status ' + (sub.last_error ? 'fl-source-status-error' : '') }), subMeta ]),
				expiry ? E('span', { class: 'fl-tab-expiry ' + expiry.className, title: expiry.title }, [ expiry.text ]) : ''
			]));
		}
		if (hiddenCount)
			tabs.push(E('button', { class: 'fl-tab ' + (this.showHidden ? 'fl-tab-active' : ''), role: 'tab', 'aria-selected': this.showHidden ? 'true' : 'false', click: ui.createHandlerFn(this, 'handleFilter', 'hidden') }, [
				E('span', { class: 'fl-tab-top' }, [ E('span', {}, [ _('Hidden') ]), E('span', { class: 'fl-count' }, [ String(hiddenCount) ]) ]),
				E('span', { class: 'fl-tab-meta' }, [ _('Excluded from Auto') ])
			]));
		return tabs;
	},

	renderTable: function() {
		var rows = this.visibleRows();
		var all = (this.poolSubscriptions().length > 1 && this.filter === 'all') || this.showHidden;
		var state = this.status().state || {};
		var headings = [ E('th', { style: 'width:30%' }, [ _('Server') ]) ];
		if (all) headings.push(E('th', { style: 'width:18%' }, [ _('Source') ]));
		headings.push(E('th', { style: 'width:14%' }, [ _('Protocol') ]), E('th', { style: 'width:15%' }, [ _('Ping (GET)') ]), E('th', { style: 'width:16%' }, [ _('Status') ]), E('th', { style: 'width:7%' }, [ '' ]));
		var body = [];
		for (var i = 0; i < rows.length; i++) {
			var row = rows[i], active = state.active_subscription_id === row.sub.id && state.active_node_id === row.node.id && state.connected;
			var actionKey = row.sub.id + ':' + row.node.id;
			var expired = isSubscriptionExpired(row.sub);
			var testing = !!this.testingNodes[row.sub.id + ':' + row.node.id];
			var checked = observationTime(row.observed) > 0;
			var unavailable = checked && row.observed.healthy === false && !row.observed.test_error && !active;
			var slow = row.observed.healthy === true && !row.observed.test_error && positiveLatency(row.latency) > 1000;
			var statusText = testing ? _('Checking')
				: active ? _('Active')
					: expired ? _('Expired')
						: row.observed.test_error ? _('Check failed')
							: unavailable ? _('Unavailable')
								: checked ? (slow ? _('Slow') : _('Ready'))
									: _('Not checked');
			var location = nodeLocation(row.node);
			var emoji = flagEmoji(nodeRawName(row.node)) || flagEmojiFromCode(nodeFlag(row.node));
			var marker = emoji
				? E('div', { class: 'fl-server-mark fl-server-flag-emoji', 'aria-hidden': 'true' }, [ E('span', { class: 'fl-server-flag-glyph' }, [ emoji ]) ])
				: E('div', { class: 'fl-server-mark' }, [ icon(active ? 'bolt' : 'server') ]);
			var secondary = location.city || trim(row.node.address) + ':' + String(row.node.port || '');
			var cells = [ E('td', { 'data-label': _('Server') }, [ E('div', { class: 'fl-server' }, [ marker, E('div', { class: 'fl-server-text' }, [ E('div', { class: 'fl-server-name' }, [ location.country ]), E('div', { class: 'fl-server-address' }, [ secondary ]) ]) ]) ]) ];
			if (all) cells.push(E('td', { class: 'fl-meta-cell fl-meta-source', 'data-label': _('Source') }, [ E('span', { class: 'fl-source' }, [ sourceName(row.sub) ]) ]));
			cells.push(
				E('td', { class: 'fl-meta-cell', 'data-label': _('Protocol') }, [ E('span', { class: 'fl-protocol' }, [ trim(row.node.protocol) || '—' ]) ]),
				E('td', { class: 'fl-meta-cell', 'data-label': _('Ping (GET)') }, [ testing ? E('span', { class: 'fl-testing-label' }, [ E('span', { class: 'fl-inline-loader' }), _('Checking') ]) : E('span', { class: 'fl-latency ' + this.latencyClass(row.latency, row.observed), title: (slow ? _('The server is available, but latency is very high.') + ' ' : '') + (row.observed.url || _('HTTPS GET through this server, bypassing the active VPN')) }, [ formatLatency(row.latency) ]) ]),
				E('td', { class: 'fl-meta-cell fl-meta-status', 'data-label': _('Status') }, [ E('span', { class: 'fl-node-status ' + (active && !unavailable ? 'fl-node-status-active' : '') + (unavailable ? ' fl-node-status-bad' : '') + (expired ? ' fl-node-status-expired' : '') }, [ testing ? E('span', { class: 'fl-inline-loader' }) : E('span', { class: 'fl-node-status-dot' }), statusText ]) ]),
				E('td', { class: 'fl-actions-cell', 'data-label': _('Actions') }, [ expired ? '' : E('details', { class: 'fl-more', open: this.activeMenuKey === actionKey ? 'open' : null }, [
					E('summary', { 'aria-label': _('Server actions'), 'aria-expanded': this.activeMenuKey === actionKey ? 'true' : 'false', click: ui.createHandlerFn(this, 'handleServerMenuToggle', actionKey) }),
					E('div', { class: 'fl-more-menu', click: function(ev) { ev.stopPropagation(); } }, [
						row.hidden ? E('button', { class: 'fl-button fl-button-primary', disabled: this.busy ? 'disabled' : null, click: ui.createHandlerFn(this, 'handleHidden', row.sub.id, row.node.id, false) }, [ _('Restore') ]) : E('button', { class: 'fl-button fl-button-primary', disabled: this.busy ? 'disabled' : null, click: ui.createHandlerFn(this, 'handleConnect', row.sub.id, row.node.id) }, [ active && state.mode === 'manual' ? _('Pinned') : _('Connect') ]),
						row.hidden ? '' : E('button', { class: 'fl-button', disabled: testing ? 'disabled' : null, click: ui.createHandlerFn(this, 'handleURLTest', row.sub.id, row.node.id) }, [ testing ? _('Checking…') : _('Check ping (GET)') ]),
						row.hidden ? '' : E('button', { class: 'fl-button fl-button-danger', disabled: this.busy ? 'disabled' : null, click: ui.createHandlerFn(this, 'handleHidden', row.sub.id, row.node.id, true) }, [ _('Hide') ])
					])
				]) ])
			);
			var rowAttrs = { class: (active ? 'fl-active-row ' : '') + (row.hidden ? 'fl-hidden-row ' : '') + (expired ? 'fl-expired-row' : ''), 'aria-current': active ? 'true' : null };
			if (!row.hidden && !expired) {
				rowAttrs.tabindex = '0';
				rowAttrs.role = 'button';
				rowAttrs.title = _('Connect this server manually');
				rowAttrs.click = ui.createHandlerFn(this, 'handleConnect', row.sub.id, row.node.id);
				rowAttrs.keydown = L.bind(this.handleRowKey, this, row.sub.id, row.node.id);
			}
			body.push(E('tr', rowAttrs, cells));
		}
		if (!body.length)
			return E('div', { class: 'fl-empty' }, [ this.showHidden ? _('There are no hidden servers.') : (this.subscriptions().length ? _('No servers match this filter.') : _('Add your first subscription. Links, VLESS, Base64, and Xray JSON are supported.')) ]);
		return E('table', { class: 'fl-table ' + (all ? 'fl-table-all' : 'fl-table-single') }, [ E('thead', {}, [ E('tr', {}, headings) ]), E('tbody', {}, body) ]);
	},

	renderContent: function() {
		var subscriptions = this.subscriptions();
		var selected = this.selectedSubscription();
		var selectedExpired = !!selected && isSubscriptionExpired(selected);
		var selectableSubscriptions = subscriptions.filter(function(sub) { return !isSubscriptionExpired(sub); });
		var countries = this.filterCountries();
		var protocols = this.filterProtocols();
		this.toastErrors = this.toastErrors || {};
		var errors = [];
		if (this.fetchErrors && this.fetchErrors.status) errors.push({ key: 'status', value: this.fetchErrors.status, fallback: _('Could not refresh VPN status. Showing the last confirmed state.') });
		if (this.fetchErrors && this.fetchErrors.subscriptions) errors.push({ key: 'subscriptions', value: this.fetchErrors.subscriptions, fallback: _('Could not refresh subscriptions. Showing the last loaded data.') });
		if (this.pageData && this.pageData[0] && this.pageData[0].__error && !(this.fetchErrors && this.fetchErrors.status)) errors.push({ key: 'status', value: this.pageData[0].__error, fallback: _('Could not read VPN status. Reload the page.') });
		if (this.pageData && this.pageData[1] && this.pageData[1].__error && !(this.fetchErrors && this.fetchErrors.subscriptions)) errors.push({ key: 'subscriptions', value: this.pageData[1].__error, fallback: _('Could not load subscriptions. Reload the page.') });
		var subscriptionErrors = subscriptions.filter(function(sub) { return !!sub.last_error; });
		if (selected)
			subscriptionErrors = subscriptionErrors.filter(function(sub) { return sub.id === selected.id; });
		for (var e = 0; e < errors.length; e++) {
			if (this.toastErrors[errors[e].key]) continue;
			var pageError = friendlyError(errors[e].value, errors[e].fallback);
			fastlaneShell.showToast(pageError.message, 'error', pageError.details);
			this.toastErrors[errors[e].key] = true;
		}
		for (var s = 0; s < subscriptionErrors.length; s++) {
			var errorKey = 'subscription-' + subscriptionErrors[s].id;
			if (this.toastErrors[errorKey]) continue;
			var subscriptionError = friendlyError(subscriptionErrors[s].last_error, _('Could not update') + ' “' + sourceName(subscriptionErrors[s]) + '”. ' + _('Check the link and router internet access.'));
			fastlaneShell.showToast(subscriptionError.message, 'error', subscriptionError.details);
			this.toastErrors[errorKey] = true;
		}
		var background = this.backgroundCheck();
		var backgroundActive = background.status === 'queued' || background.status === 'running';
		var backgroundLabel = background.status === 'queued'
			? _('GET check queued on the router…')
			: _('The router is checking servers in the background') + (Number(background.total) > 0 ? ': ' + Number(background.done || 0) + ' / ' + Number(background.total) : '') + '…';
		return E('div', {}, [
			fastlaneShell.renderHeader('vpn'),
			E('main', { class: 'fl-shell' }, [
			this.renderStatus(),
			this.busy ? E('div', { class: 'fl-busy', role: 'status', 'aria-live': 'polite' }, [ this.busy ]) : '',
			backgroundActive ? E('div', { class: 'fl-busy', role: 'status', 'aria-live': 'polite' }, [ E('span', { class: 'fl-inline-loader' }), backgroundLabel ]) : '',
			selectedExpired ? E('div', { class: 'fl-expired-note' }, [ _('This subscription has expired. Its servers remain viewable but are excluded from updates, GET checks, and automatic selection.') ]) : '',
			E('section', { class: 'fl-sourcebar', 'aria-label': _('Server sources') }, [
				E('div', { class: 'fl-tabs', role: 'tablist', 'aria-label': _('Server sources') }, this.renderTabs()),
				E('div', { class: 'fl-source-actions' }, [
					E('button', { class: 'fl-button fl-source-add', disabled: this.busy ? 'disabled' : null, click: ui.createHandlerFn(this, 'handleAddOpen') }, [ icon('plus'), _('Add servers') ]),
					selected ? E('button', { class: 'fl-button fl-button-danger', disabled: this.busy ? 'disabled' : null, click: ui.createHandlerFn(this, 'handleRemove', selected.id) }, [ icon('trash'), selected.source_type === 'file' ? _('Remove file') : _('Remove subscription') ]) : ''
				])
			]),
			E('section', { class: 'fl-server-panel', 'aria-label': _('Servers') }, [
			E('div', { class: 'fl-toolbar' }, [
				E('div', { class: 'fl-toolbar-actions' }, [
					E('label', { class: 'fl-search-wrap' }, [ E('span', { class: 'fl-sr-only' }, [ _('Search servers') ]), E('input', { class: 'fl-search', value: this.query, placeholder: _('Search servers…'), input: L.bind(this.handleSearch, this) }) ]),
					E('select', { class: 'fl-select', 'aria-label': _('Country'), change: L.bind(this.handleCountry, this) }, [ E('option', { value: 'all', selected: this.country === 'all' ? 'selected' : null }, [ _('All countries') ]) ].concat(countries.map(L.bind(function(country) { return E('option', { value: country, selected: this.country === country ? 'selected' : null }, [ country ]); }, this)))),
					E('select', { class: 'fl-select', 'aria-label': _('Protocol'), change: L.bind(this.handleProtocol, this) }, [ E('option', { value: 'all', selected: this.protocol === 'all' ? 'selected' : null }, [ _('All protocols') ]) ].concat(protocols.map(L.bind(function(protocol) { return E('option', { value: protocol, selected: this.protocol === protocol ? 'selected' : null }, [ protocol.toUpperCase() ]); }, this)))),
					E('select', { class: 'fl-select', 'aria-label': _('Server sorting'), change: L.bind(this.handleSort, this) }, [ E('option', { value: 'latency', selected: this.sort === 'latency' ? 'selected' : null }, [ _('Sort: ping (GET)') ]), E('option', { value: 'name', selected: this.sort === 'name' ? 'selected' : null }, [ _('Sort: name') ]), E('option', { value: 'source', selected: this.sort === 'source' ? 'selected' : null }, [ _('Sort: source') ]) ])
				]),
				E('div', { class: 'fl-toolbar-buttons' }, [
					E('button', { class: 'fl-button fl-button-quiet fl-toolbar-refresh', disabled: this.busy || !selectableSubscriptions.length || selectedExpired ? 'disabled' : null, click: ui.createHandlerFn(this, 'handleRefreshSubscriptions') }, [ icon('refresh'), _('Update subscriptions') ]),
					E('button', { class: 'fl-button fl-button-quiet', disabled: this.busy || backgroundActive || !selectableSubscriptions.length || selectedExpired ? 'disabled' : null, click: ui.createHandlerFn(this, 'handleURLTests') }, [ backgroundActive ? E('span', { class: 'fl-inline-loader' }) : icon('bolt'), backgroundActive ? _('Check running in background') : _('Check ping (GET)') ])
				])
			]),
			E('div', { class: 'fl-table-wrap' }, [ this.renderTable() ])
			])
			])
		]);
	},

	render: function(data) {
		this.pageData = data;
		return E('div', { class: 'fastlane-root' }, [ E('style', {}, [ css ]), fastlaneShell.renderStyles(), E('div', { id: 'fastlane-content' }, [ this.renderContent() ]) ]);
	},

	handleSave: null,
	handleSaveApply: null,
	handleReset: null
});
