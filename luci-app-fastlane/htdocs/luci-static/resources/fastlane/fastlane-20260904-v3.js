'use strict';
'require baseclass';

function asset(name) {
	return L.resource('fastlane/assets/' + name);
}

var css = `
body:has(.fastlane-root){background:#02090c!important}body:has(.fastlane-root) #tabmenu,body:has(.fastlane-root) #maincontent>footer,body:has(.fastlane-root) .main-right>header.bg-primary,body:has(.fastlane-root) .main-right>.darkMask{display:none!important}body:has(.fastlane-root) #maincontent>.container{max-width:none!important;margin:0!important;padding:0!important}body:has(.fastlane-root) .main-right,body:has(.fastlane-root) #maincontent,body:has(.fastlane-root) #view{background:#02090c!important}body:has(.fastlane-root) .main-right{scrollbar-color:#29434a #02090c!important}body:has(.fastlane-root) .main-right::-webkit-scrollbar-track{background:#02090c!important}body:has(.fastlane-root) .main-right::-webkit-scrollbar-thumb{background:#29434a!important}
.fastlane-root{--fl-bg:#02090c;--fl-panel:#071115;--fl-panel-2:#0a1519;--fl-line:#1a2b31;--fl-line-strong:#29434a;--fl-text:#ddd4ca;--fl-muted:#a89f96;--fl-subtle:#756f69;--fl-green:#54df91;--fl-green-dim:#45a974;--fl-red:#ff555f;--fl-amber:#ffc528;background:var(--fl-bg)!important;color:var(--fl-text)!important;border:0;border-radius:0;min-height:calc(100vh - 64px);overflow:hidden;color-scheme:dark;font-family:"Avenir Next",ui-sans-serif,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;font-size:15px;line-height:1.4}
.fastlane-root *{box-sizing:border-box}.fastlane-root h1,.fastlane-root h2,.fastlane-root h3,.fastlane-root h4,.fastlane-root h5,.fastlane-root h6{margin:0!important;padding:0!important;border:0!important;border-radius:0!important;background:transparent!important;box-shadow:none!important;color:var(--fl-text)!important}.fastlane-root p{margin:0}.fastlane-root ::selection{background:#2f7e5a;color:#fff}.fl-shell-nav{height:74px;display:grid;grid-template-columns:1fr auto 1fr;align-items:center;padding:0 24px;border-bottom:1px solid var(--fl-line);background:var(--fl-bg)!important;position:relative}.fastlane-root .fl-shell-nav::before,.fastlane-root .fl-shell-nav::after{content:none!important;display:none!important}.fl-brand{display:flex;align-items:center;gap:13px;min-width:0}.fl-logo{width:42px;height:42px;object-fit:cover}.fastlane-root .fl-title{font-size:25px!important;font-weight:750!important;letter-spacing:-.025em!important;color:#ded2c5!important}.fl-nav-links{height:100%;display:flex;align-items:stretch;gap:28px}.fl-nav-link{position:relative;display:flex;align-items:center;padding:0 12px;color:var(--fl-muted)!important;text-decoration:none!important;font-size:16px;font-weight:560}.fl-nav-link:hover{color:var(--fl-text)!important}.fl-nav-link-active{color:var(--fl-green)!important}.fl-nav-link-active:after{content:"";position:absolute;left:0;right:0;bottom:0;height:3px;background:var(--fl-green)}.fl-shell-tools{display:flex;justify-content:flex-end}.fl-shell{padding:20px 24px 24px;background:var(--fl-bg)!important;color:var(--fl-text)!important}
.fl-toast-layer{position:fixed;z-index:10000;top:40px;left:50%;display:grid;gap:10px;width:max-content;max-width:calc(100vw - 32px);transform:translateX(-50%);pointer-events:none}.fl-toast{--fl-text:#ddd4ca;--fl-muted:#a89f96;--fl-line:#1a2b31;--fl-line-strong:#29434a;--fl-green:#54df91;position:relative;display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:8px 12px;width:max-content;min-width:240px;max-width:min(420px,calc(100vw - 32px));min-height:52px;padding:13px 18px;border:1px solid var(--fl-line-strong)!important;border-radius:12px;background:#0a1519!important;color:var(--fl-text)!important;box-shadow:0 16px 38px rgba(0,0,0,.44);font-family:"Avenir Next",ui-sans-serif,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;opacity:0;transform:translateY(-8px);transition:opacity .18s ease,transform .18s ease;pointer-events:auto}.fl-toast-visible{opacity:1;transform:translateY(0)}.fl-toast-success{border-color:rgba(84,223,145,.42)!important}.fl-toast-error{border-color:rgba(255,85,95,.48)!important}.fl-toast-message{display:block;min-width:0;color:var(--fl-text)!important;font-size:14px;font-weight:650;line-height:1.4;text-align:left}.fl-toast-success .fl-toast-message{color:#aaf0c7!important}.fl-toast-error .fl-toast-message{color:#ffb0b5!important}.fl-toast-actions{display:flex;align-items:center;gap:4px}.fl-toast-button{min-height:34px;border:0!important;border-radius:7px;background:transparent!important;color:var(--fl-muted)!important;font:inherit;font-size:12px;font-weight:650;cursor:pointer}.fl-toast-button:hover{background:#102126!important;color:var(--fl-text)!important}.fl-toast-details{grid-column:1/-1;margin:8px 0 0;padding:10px 12px;border-top:1px solid var(--fl-line);background:#050d10;color:#bdb4ab;font:11px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace;white-space:pre-wrap;overflow-wrap:anywhere;max-height:180px;overflow:auto}.fl-toast-button:focus-visible{outline:2px solid var(--fl-green);outline-offset:2px}
@media(max-width:1100px){.fl-shell-nav{grid-template-columns:1fr auto}.fl-nav-links{display:flex;gap:8px}.fl-nav-link{padding:0 10px}.fl-shell-tools{display:none}}
@media(max-width:760px){.fastlane-root{border-radius:8px}.fl-shell-nav{height:auto;grid-template-columns:1fr;padding:12px 16px 0}.fl-brand{padding-bottom:10px}.fl-logo{width:36px;height:36px}.fl-title{font-size:22px}.fl-nav-links{width:100%;height:48px;gap:4px;overflow-x:auto;scrollbar-width:none}.fl-nav-links::-webkit-scrollbar{display:none}.fl-nav-link{flex:1;justify-content:center;min-width:max-content;padding:0 12px;font-size:14px}.fl-shell{padding:14px}.fl-toast-layer{top:40px;left:50%;width:max-content;max-width:calc(100vw - 28px)}.fl-toast{width:max-content;min-width:0;max-width:calc(100vw - 28px);padding:13px 16px}.fl-toast-details{margin:8px 0 0}}
@media(prefers-reduced-motion:reduce){.fl-toast{transition:none}}
`;

function navLink(section, activeSection, label) {
	return E('a', {
		'class': 'fl-nav-link' + (section === activeSection ? ' fl-nav-link-active' : ''),
		'href': L.url('admin/services/fastlane/' + section),
		'aria-current': section === activeSection ? 'page' : null
	}, [ label ]);
}

return baseclass.extend({
	showToast: function(message, type, details) {
		var layer = document.getElementById('fastlane-toast-layer');
		if (!layer) {
			layer = E('div', { id: 'fastlane-toast-layer', class: 'fl-toast-layer', 'aria-live': 'polite' });
			document.body.appendChild(layer);
		}
		var toast;
		var timer;
		var close = function() {
			if (!toast || !toast.parentNode) return;
			toast.classList.remove('fl-toast-visible');
			window.setTimeout(function() { if (toast.parentNode) toast.parentNode.removeChild(toast); }, 180);
		};
		var actions = [];
		if (details) {
			actions.push(E('button', { class: 'fl-toast-button', click: function() {
				var panel = toast.querySelector('.fl-toast-details');
				var hidden = panel.hasAttribute('hidden');
				if (hidden) {
					panel.removeAttribute('hidden');
					this.textContent = _('Hide');
					window.clearTimeout(timer);
				}
				else {
					panel.setAttribute('hidden', 'hidden');
					this.textContent = _('Details');
					timer = window.setTimeout(close, 3000);
				}
			} }, [ _('Details') ]));
		}
		toast = E('div', { class: 'fl-toast fl-toast-' + (type || 'info'), role: type === 'error' ? 'alert' : 'status' }, [
			E('div', { class: 'fl-toast-message' }, [ message ]),
			actions.length ? E('div', { class: 'fl-toast-actions' }, actions) : '',
			details ? E('pre', { class: 'fl-toast-details', hidden: 'hidden', tabindex: '0' }, [ details ]) : ''
		]);
		layer.appendChild(toast);
		window.requestAnimationFrame(function() { toast.classList.add('fl-toast-visible'); });
		timer = window.setTimeout(close, 3000);
		return toast;
	},

	renderStyles: function() {
		return E('style', { 'type': 'text/css' }, [ css ]);
	},

	renderHeader: function(activeSection) {
		return E('header', { 'class': 'fl-shell-nav' }, [
			E('div', { 'class': 'fl-brand' }, [
				E('img', { 'class': 'fl-logo', 'src': asset('fastlane-mark.png'), 'alt': '' }),
				E('h2', { 'class': 'fl-title' }, [ 'Fast Lane' ])
			]),
			E('nav', { 'class': 'fl-nav-links', 'aria-label': _('Fast Lane sections') }, [
				navLink('vpn', activeSection, 'VPN'),
				navLink('routing', activeSection, _('Routing')),
				navLink('diagnostics', activeSection, _('Diagnostics')),
				navLink('settings', activeSection, _('Settings'))
			]),
			E('div', { 'class': 'fl-shell-tools' })
		]);
	}
});
