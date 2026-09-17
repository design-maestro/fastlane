/* Faithful port of settings-20260910-hide-keywords-v8.js. Screen renderers,
 * duration controls, chips and CSS are retained from LuCI; transport is typed HTTP.
 * mount(container,{api,operation,notice,confirmAction,getSnapshot}) -> controller.
 * Call controller.refresh(snapshot) on EVERY root reload. mount only after login.
 * No operation's boolean return value is treated as completed work.
 * Load legacy.css (shared tokens), settings-panel.css, then this script.
 * Browser language: fastlane.panel.language; dispatches fastlane:languagechange.
 * OpenWrt updates/removal use fixed installed helpers. LuCI language is separate.
 */
(function () {
'use strict';
const translations = {
  "hours": "часы",
  "minutes": "минуты",
  "seconds": "секунды",
  "milliseconds": "миллисекунды",
  "h": "ч",
  "min": "мин",
  "s": "с",
  "ms": "мс",
  "On": "Включено",
  "Off": "Выключено",
  "Remove hide rule": "Удалить правило скрытия",
  "This hide rule already exists.": "Такое правило скрытия уже есть.",
  "Type a word and press Enter": "Введите слово и нажмите Enter",
  "Hide by keywords": "Скрывать по ключевым словам",
  "Matches server titles and subtitles. Rules apply immediately and remain after subscription updates.": "Ищет совпадения в названии и подписи сервера. Правила применяются сразу и сохраняются после обновления подписок.",
  "Checking…": "Проверяю…",
  "Check for updates": "Проверить обновления",
  "Install": "Установить",
  "What is new": "Что нового",
  "Reload page": "Обновить страницу",
  "Fast Lane update": "Обновление Fast Lane",
  "Installed version:": "Установлена версия:",
  "unknown": "неизвестно",
  "Only stable GitHub releases are used, and installation always requires confirmation.": "Используются только стабильные релизы GitHub, а установка всегда требует подтверждения.",
  "Check whether a new version is available.": "Проверьте наличие новой версии.",
  "You can close the admin panel; the task will continue on the router.": "Можно закрыть админку — задача продолжится на роутере.",
  "Fast Lane settings": "Настройки Fast Lane",
  "Only parameters that affect VPN selection and stability.": "Только параметры, влияющие на выбор VPN и стабильность.",
  "Save": "Сохранить",
  "Subscriptions and checks": "Подписки и проверки",
  "Background updates run automatically; every action can also be started manually on the VPN page.": "Фоновые обновления запускаются автоматически; каждое действие также можно запустить вручную на странице VPN.",
  "Subscription update": "Обновление подписок",
  "Background update interval": "Интервал фонового обновления",
  "Automatic server check": "Автопроверка серверов",
  "0 min 0 s disables automatic checks": "0 мин 0 с отключает автоматические проверки",
  "URL test address": "Адрес URL-теста",
  "HTTPS page with a fast 204 response": "HTTPS-страница с быстрым ответом 204",
  "URL test timeout": "Тайм-аут URL-теста",
  "Maximum wait time": "Максимальное время ожидания",
  "Automatic selection": "Автовыбор",
  "Choose how carefully Fast Lane keeps the active server.": "Выберите, насколько бережно Fast Lane сохраняет текущий сервер.",
  "Selection profile": "Профиль выбора",
  "Games": "Игры",
  "Streaming": "Стриминг",
  "Balanced": "Баланс",
  "Custom": "Свой",
  "Keep the current server. Switch only after a confirmed connection loss.": "Сохраняем текущий сервер. Переключаемся только при подтверждённой потере связи.",
  "Keep the current server. Switch after sustained connection degradation or a connection loss.": "Сохраняем текущий сервер. Переключаемся при устойчивом ухудшении соединения или потере связи.",
  "Choose a stable server and switch after a confirmed improvement.": "Выбираем стабильный сервер и переключаемся при подтверждённом улучшении.",
  "Configure automatic selection yourself.": "Настройте автоматический выбор самостоятельно.",
  "Allow optimization": "Разрешить оптимизацию",
  "Switch to a measurably better server when the connection is healthy": "Переключаться на заметно лучший сервер при рабочем соединении",
  "Current latency ceiling": "Порог текущей задержки",
  "Start optimization only above this average latency; 0 disables the ceiling": "Начинать оптимизацию только выше этой средней задержки; 0 отключает порог",
  "Required absolute latency improvement": "Требуемый выигрыш по задержке в миллисекундах",
  "Relative improvement": "Относительный выигрыш",
  "Required share of improvement, for example 0.35": "Требуемая доля выигрыша, например 0.35",
  "Confirmed measurements": "Подтверждённые замеры",
  "Consecutive better measurements from the same candidate": "Последовательные лучшие замеры одного кандидата",
  "Failure threshold": "Порог ошибок",
  "Consecutive failed checks before emergency switching": "Последовательные неудачные проверки до аварийного переключения",
  "Fast Lane avoids reconnecting for insignificant latency differences.": "Fast Lane не переподключается при незначительной разнице в задержке.",
  "Pause between switches": "Пауза между переключениями",
  "Prevents constant server hopping": "Предотвращает постоянные переключения между серверами",
  "Minimum improvement": "Минимальный выигрыш",
  "How much faster a new server must be": "Насколько быстрее должен быть новый сервер",
  "Strict internet check": "Строгая проверка интернета",
  "Restore the previous server if HTTPS does not work after connecting": "Вернуть прежний сервер, если после подключения не работает HTTPS",
  "Interface language": "Язык интерфейса",
  "Automatic follows the LuCI language. Other languages fall back to English.": "Автоматически использует язык LuCI. Для остальных языков включается английский.",
  "Automatic": "Автоматически",
  "Application management": "Управление приложениями",
  "Remove other services through the FriendlyWrt package manager. Fast Lane can be removed here.": "Удаляйте другие службы через менеджер пакетов FriendlyWrt. Fast Lane можно удалить здесь.",
  "FriendlyWrt package manager": "Менеджер пакетов FriendlyWrt",
  "Remove Fast Lane": "Удалить Fast Lane",
  "Not selected": "Не выбрана",
  "Manual": "Вручную",
  "Disconnected": "Отключён",
  "Could not read Fast Lane status.": "Не удалось получить состояние Fast Lane.",
  "The last connection failed.": "Последнее подключение не удалось.",
  "Fast Lane service": "Служба Fast Lane",
  "Running": "Работает",
  "Stopped": "Остановлена",
  "VPN configuration": "Конфигурация VPN",
  "Local DNS": "Локальный DNS",
  "Unavailable": "Недоступен",
  "System DNS": "Системные DNS",
  "Not detected": "Не определено",
  "Disabled to prevent leaks": "Отключён для защиты от утечек",
  "Enabled": "Включено",
  "Unsupported": "Не поддерживается",
  "Service files": "Служебные файлы",
  "available": "на месте",
  "Reserve": "Резерв",
  "Reserve candidate": "Кандидат в резерв",
  "score": "оценка",
  "samples": "измерений",
  "Diagnostics": "Диагностика",
  "Current Fast Lane and connection status.": "Текущее состояние Fast Lane и подключения.",
  "Refresh": "Обновить",
  "Fast Lane status": "Состояние Fast Lane",
  "Connected": "Подключён",
  "Traffic goes through the selected server.": "Трафик проходит через выбранный сервер.",
  "Internet traffic is direct.": "Интернет-трафик идёт напрямую.",
  "VPN service": "Служба VPN",
  "The service process is running.": "Служебный процесс запущен.",
  "It starts when a server is connected.": "Запустится при подключении к серверу.",
  "Subscription": "Подписка",
  "Server:": "Сервер:",
  "No server selected yet.": "Сервер пока не выбран.",
  "Inactive": "Неактивен",
  "DNS requests are handled by Fast Lane.": "DNS-запросы обрабатываются Fast Lane.",
  "It starts together with VPN.": "Запустится вместе с VPN.",
  "Mode": "Режим",
  "Server selection method.": "Способ выбора сервера.",
  "No active connection.": "Нет активного подключения.",
  "Technical details": "Технические данные"
};
let preference = 'auto';
try { preference = localStorage.getItem('fastlane.panel.language') || 'auto'; } catch (error) {}
function language() { return preference === 'auto' ? (/^ru(?:-|$)/i.test(navigator.language) ? 'ru' : 'en') : preference; }
function _(text) { return language() === 'ru' ? (translations[text] || text) : text; }
function local(en, ru) { return language() === 'ru' ? ru : en; }
document.documentElement.lang = language();
function E(tag, attrs, children) {
  const node = document.createElement(tag);
  for (const [key,value] of Object.entries(attrs || {})) {
    if (value == null || value === false) continue;
    if (typeof value === 'function') node.addEventListener(key,value);
    else if (key === 'checked' || key === 'selected' || key === 'disabled') node[key] = !!value;
    else node.setAttribute(key, String(value));
  }
  for (const child of (Array.isArray(children) ? children : [children])) {
    if (child != null && child !== false) node.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
  return node;
}
const dom = { content: (node, children) => node.replaceChildren(...children) };
const L = { bind: (fn, scope, ...args) => fn.bind(scope, ...args), url: () => location.pathname };
const ui = { createHandlerFn: (scope, name, ...args) => scope[name].bind(scope, ...args) };
function trim(value) { return value == null ? '' : String(value).trim(); }
function normalizeAutoHideKeywords(values) {
	var seen = {};
	return (Array.isArray(values) ? values : []).map(function(value) {
		return trim(value).replace(/\s+/g, ' ');
	}).filter(function(value) {
		var key = value.toLocaleLowerCase();
		if (!key || seen[key]) return false;
		seen[key] = true;
		return true;
	});
}
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
function durationUnitLabel(unit) {
	return { h: _('h'), m: _('min'), s: _('s'), ms: _('ms') }[unit] || unit;
}

function create(container, shared) {
  for (const key of ['api','operation','notice','confirmAction','getSnapshot']) if (typeof shared[key] !== 'function') throw Error('Missing shared API: '+key);
  const fastlaneShell = { showToast: (message, tone) => shared.notice(message, tone === 'error') };
  const view = {

	handleInput: function(key, ev) {
		this.draft[key] = ev && ev.target ? ev.target.value : '';
	},


	handleBool: function(key, ev) {
		this.draft[key] = !!(ev && ev.target && ev.target.checked);
		var label = ev && ev.target && ev.target.parentNode ? ev.target.parentNode.querySelector('[data-toggle-label]') : null;
		if (label) label.textContent = this.draft[key] ? _('On') : _('Off');
		if (key === 'auto_allow_optimization') this.paint();
	},

	autoProfile: function() { return this.draft.auto_profile || 'balanced'; },

	prepareAutoDraft: function(source) {
		var policy = source.custom_auto_policy || {};
		this.draft.auto_profile = source.auto_profile || 'custom';
		this.draft.auto_allow_optimization = !!policy.allow_optimization;
		this.draft.auto_current_latency_ceiling = policy.current_latency_ceiling || '0s';
		this.draft.auto_latency_improvement = policy.latency_improvement || '70ms';
		this.draft.auto_relative_improvement = policy.relative_improvement == null ? 0.35 : policy.relative_improvement;
		this.draft.auto_required_candidate_wins = policy.required_candidate_wins || 4;
		this.draft.auto_cooldown = policy.cooldown || '20m0s';
		this.draft.auto_failure_threshold = policy.failure_threshold || 2;
	},

	handleAutoProfile: function(profile) {
		this.draft.auto_profile = profile;
		this.paint();
	},

	autoProfileControl: function() {
		var profile = this.autoProfile();
		var options = [ ['games', _('Games')], ['streaming', _('Streaming')], ['balanced', _('Balanced')], ['custom', _('Custom')] ];
		var copy = {
			games: _('Keep the current server. Switch only after a confirmed connection loss.'),
			streaming: _('Keep the current server. Switch after sustained connection degradation or a connection loss.'),
			balanced: _('Choose a stable server and switch after a confirmed improvement.'),
			custom: _('Configure automatic selection yourself.')
		};
		var nodes = [ E('div', { class: 'fls-segments', role: 'group', 'aria-label': _('Selection profile') }, options.map(L.bind(function(option) {
			return E('button', { type: 'button', class: 'fls-segment' + (profile === option[0] ? ' fls-segment-active' : ''), 'aria-pressed': profile === option[0] ? 'true' : 'false', click: L.bind(this.handleAutoProfile, this, option[0]) }, [ option[1] ]);
		}, this))), E('p', { class: 'fls-profile-copy' }, [ copy[profile] ]) ];
		if (profile === 'custom') {
			var customFields = [
			E('div', { class: 'fls-field fls-field-toggle' }, [ E('label', {}, [ _('Allow optimization'), E('span', { class: 'fls-hint' }, [ _('Switch to a measurably better server when the connection is healthy') ]) ]), E('label', { class: 'fls-toggle' }, [ E('input', { type: 'checkbox', 'data-setting-key': 'auto_allow_optimization', checked: this.draft.auto_allow_optimization ? 'checked' : null, change: L.bind(this.handleBool, this, 'auto_allow_optimization') }), E('span', { 'data-toggle-label': 'auto_allow_optimization' }, [ this.draft.auto_allow_optimization ? _('On') : _('Off') ]) ]) ]),
			this.field('auto_failure_threshold', _('Failure threshold'), _('Consecutive failed checks before emergency switching'), 'number')
			];
			if (this.draft.auto_allow_optimization) customFields.splice(1, 0,
			this.durationField('auto_current_latency_ceiling', _('Current latency ceiling'), _('Start optimization only above this average latency; 0 disables the ceiling'), [ 'ms' ]),
			this.durationField('auto_latency_improvement', _('Minimum improvement'), _('Required absolute latency improvement'), [ 'ms' ]),
			this.field('auto_relative_improvement', _('Relative improvement'), _('Required share of improvement, for example 0.35'), 'number'),
			this.field('auto_required_candidate_wins', _('Confirmed measurements'), _('Consecutive better measurements from the same candidate'), 'number'),
			this.durationField('auto_cooldown', _('Pause between switches'), _('Prevents constant server hopping'), [ 'm', 's' ])
			);
			nodes.push(E('div', { class: 'fls-fields fls-custom-policy' }, customFields));
		}
		return E('div', { class: 'fls-profile-control' }, nodes);
	},


	autoHideKeywords: function() {
		return normalizeAutoHideKeywords(this.draft && this.draft.auto_hide_keywords);
	},


	renderAutoHideKeywordChips: function() {
		return this.autoHideKeywords().map(L.bind(function(keyword) {
			return E('span', { class: 'fls-chip' }, [
				E('span', { class: 'fls-chip-text', title: keyword }, [ keyword ]),
				E('button', { type: 'button', class: 'fls-chip-remove', disabled: this.keywordSaving ? 'disabled' : null, 'aria-label': _('Remove hide rule') + ': ' + keyword, click: ui.createHandlerFn(this, 'handleAutoHideKeywordRemove', keyword) }, [ '×' ])
			]);
		}, this));
	},


	syncAutoHideKeywordControls: function() {
		if (this.keywordInput) this.keywordInput.disabled = !!this.keywordSaving;
		if (this.keywordList) dom.content(this.keywordList, this.renderAutoHideKeywordChips());
	},


	handleAutoHideKeywordKeydown: function(ev) {
		if (!ev || ev.key !== 'Enter') return Promise.resolve();
		ev.preventDefault();
		if (this.keywordSaving || this.saving) return Promise.resolve();
		var keyword = trim(ev.target && ev.target.value).replace(/\s+/g, ' ');
		if (!keyword) return Promise.resolve();
		var previous = this.autoHideKeywords();
		var duplicate = previous.some(function(value) { return value.toLocaleLowerCase() === keyword.toLocaleLowerCase(); });
		if (ev.target) ev.target.value = '';
		if (duplicate) {
			fastlaneShell.showToast(_('This hide rule already exists.'), 'info');
			return Promise.resolve();
		}
		return this.persistAutoHideKeywords(previous.concat([ keyword ]), previous);
	},


	handleAutoHideKeywordRemove: function(keyword, ev) {
		if (ev) { ev.preventDefault(); ev.stopPropagation(); }
		if (this.keywordSaving || this.saving) return Promise.resolve();
		var previous = this.autoHideKeywords();
		var lowered = trim(keyword).toLocaleLowerCase();
		return this.persistAutoHideKeywords(previous.filter(function(value) { return value.toLocaleLowerCase() !== lowered; }), previous);
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
					E('span', { class: 'fls-duration-unit', 'aria-hidden': 'true' }, [ durationUnitLabel(unit) ])
				]);
			}, this)))
		]);
	},


	autoHideKeywordField: function() {
		this.keywordInput = E('input', { class: 'fls-input', type: 'text', maxlength: '64', placeholder: _('Type a word and press Enter'), disabled: this.keywordSaving ? 'disabled' : null, keydown: L.bind(this.handleAutoHideKeywordKeydown, this) });
		this.keywordList = E('div', { class: 'fls-chip-list', 'aria-live': 'polite' }, this.renderAutoHideKeywordChips());
		return E('div', { class: 'fls-field fls-keyword-field' }, [
			E('label', {}, [ _('Hide by keywords'), E('span', { class: 'fls-hint' }, [ _('Matches server titles and subtitles. Rules apply immediately and remain after subscription updates.') ]) ]),
			E('div', { class: 'fls-keyword-control' }, [ this.keywordInput, this.keywordList ])
		]);
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


	render: function(data) {
		this.settings = this.settings || data || {};
		this.draft = this.draft || Object.assign({}, this.settings);
		this.updateBox = E('section', { class: 'fls-card fls-manage fls-update' }, this.renderUpdateContents());
		var settingsContent = E('div', { class: 'fastlane-settings' }, [
			E('div', { class: 'fls-head' }, [
				E('div', {}, [ E('h2', {}, [ _('Fast Lane settings') ]), E('p', {}, [ _('Only parameters that affect VPN selection and stability.') ]) ]),
				E('div', { class: 'fls-actions' }, [ E('button', { class: 'fls-button fls-primary', click: ui.createHandlerFn(this, 'handleSaveSettings') }, [ _('Save') ]) ])
			]),
			E('div', { class: 'fls-grid' }, [
				E('section', { class: 'fls-card' }, [ E('h3', {}, [ _('Subscriptions and checks') ]), E('p', {}, [ _('Background updates run automatically; every action can also be started manually on the VPN page.') ]), E('div', { class: 'fls-fields' }, [
					this.durationField('refresh_interval', _('Subscription update'), _('Background update interval'), [ 'h', 'm', 's' ]),
					this.durationField('health_check_interval', _('Automatic server check'), _('0 min 0 s disables automatic checks'), [ 'm', 's' ]),
					this.field('url_test_url', _('URL test address'), _('HTTPS page with a fast 204 response'), 'url'),
					this.durationField('url_test_timeout', _('URL test timeout'), _('Maximum wait time'), [ 's' ])
				]) ]),
				E('section', { class: 'fls-card' }, [ E('h3', {}, [ _('Automatic selection') ]), E('p', {}, [ _('Choose how carefully Fast Lane keeps the active server.') ]), this.autoProfileControl(), E('div', { class: 'fls-fields' }, [
					E('div', { class: 'fls-field fls-field-toggle' }, [ E('label', {}, [ _('Strict internet check'), E('span', { class: 'fls-hint' }, [ _('Restore the previous server if HTTPS does not work after connecting') ]) ]), E('label', { class: 'fls-toggle' }, [ E('input', { type: 'checkbox', 'data-setting-key': 'strict_egress_check', checked: this.draft.strict_egress_check ? 'checked' : null, change: L.bind(this.handleBool, this, 'strict_egress_check') }), E('span', { 'data-toggle-label': 'strict_egress_check' }, [ this.draft.strict_egress_check ? _('On') : _('Off') ]) ]) ]),
					this.autoHideKeywordField()
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
						E('button', { type: 'button', class: 'fls-button', disabled: true, 'data-unavailable': 'true' }, [ _('FriendlyWrt package manager') ]),
						E('button', { class: 'fls-button fls-danger', click: ui.createHandlerFn(this, 'handleUninstall') }, [ _('Remove Fast Lane') ])
					])
				])
			])
		]);

		this.settingsRoot = settingsContent;
		return settingsContent;
	},

    updateBusy() { return ['checking','installing'].includes(this.updateState?.status); },
    syncUpdate() {
      if (!this.updateBox) return;
      dom.content(this.updateBox, this.renderUpdateContents());
      if (this.updateState?.status === 'unsupported') {
        this.updateBox.querySelectorAll('button').forEach(button => { button.disabled = true; button.dataset.unavailable = 'true'; });
        this.updateBox.querySelector('[role=status]').textContent = local('Update check and installation are unavailable: the standalone service has no updater adapter.', 'Проверка и установка обновлений недоступны: у отдельной панели нет адаптера обновления.');
      }
    },
    async handleUpdateCheck() {
      if (this.updateState?.status === 'unsupported') return;
      this.updateRequest = true; this.setSaving(true); this.syncUpdate();
      try { await this.queue('settings-panel/update/check', 'POST', {}, 'settings-panel-update-check'); }
      catch (error) { shared.notice(_('Could not start the update check. Try again.'),true); }
      finally { this.updateRequest = false; this.setSaving(false); await this.refreshUpdate(); }
    },
    handleUpdateInstall() {
      const candidate = this.updateState?.candidate;
      if (!candidate || this.updateState.status !== 'available') return;
      shared.confirmAction(_('Install Fast Lane')+' '+candidate.version+'? '+_('Saved settings and subscriptions will remain. VPN may reconnect briefly. Do not turn off the router until installation finishes.'), async () => {
        this.updateRequest = true; this.setSaving(true); this.syncUpdate();
        try { await this.queue('settings-panel/update/install', 'POST', {release_id:candidate.id,confirm:true}, 'settings-panel-update-install'); }
        catch (error) { shared.notice(_('Could not confirm installation. Check the current status before trying again.'),true); }
        finally { this.updateRequest = false; this.setSaving(false); await this.refreshUpdate(); }
      });
    },
    async refreshUpdate() {
      try { this.updateState = await shared.api('settings-panel/update'); this.updateTransportError = ''; }
      catch (error) { this.updateTransportError = _('Could not read update status. Try again; during installation, wait for the connection to return.'); }
      // Installation can restart the daemon, resetting its in-memory job ID.
      // The installed updater's persisted, verified release result is then the
      // authoritative completion record. Do not infer success from reconnecting.
      const pending = this.pending;
      if (pending?.kind === 'settings-panel-update-install') {
        const state = this.updateState;
        if (state?.status === 'updated' && state.candidate?.id === pending.releaseID && state.candidate?.version === pending.releaseVersion) pending.resolve();
        else if (!this.updateTransportError && !['checking','installing'].includes(state?.status) && (shared.getSnapshot()?.job?.sequence || 0) < pending.before) pending.reject(Error('update_completion_unconfirmed'));
      } else if (pending?.kind === 'settings-panel-update-check' && (shared.getSnapshot()?.job?.sequence || 0) < pending.before && !this.updateBusy()) pending.reject(Error('update_completion_unconfirmed'));
      this.syncUpdate();
    },
    handleUninstall() {
      if (this.unavailable.uninstall || this.updateBusy()) return;
      shared.confirmAction(_('Remove Fast Lane from the router? Settings and subscriptions will also be removed.'), async () => {
        this.setSaving(true);
        try {
          await this.queue('settings-panel/uninstall','POST',{confirm:true},'settings-panel-uninstall');
          container.replaceChildren(E('p',{role:'status'},[local('Fast Lane was removed.','Fast Lane удалён.')]));
        } catch (error) { shared.notice(local('Removal could not be confirmed. Check the router before retrying.','Не удалось подтвердить удаление. Проверьте роутер перед повторной попыткой.'),true); }
        finally { this.setSaving(false); }
      });
    },
    handleLanguageChange(ev) {
      const next = ev.target.value;
      if (!['auto','en','ru'].includes(next) || next === preference) return;
      const apply = async () => {
        try { localStorage.setItem('fastlane.panel.language', next); }
        catch (error) { ev.target.value = preference; shared.notice(local('Could not save the interface language.', 'Не удалось сохранить язык интерфейса.'), true); return; }
        preference = next;
        document.documentElement.lang = language();
        this.interfaceLanguage = next;
        this.paint();
        window.dispatchEvent(new CustomEvent('fastlane:languagechange', {detail:{preference:next,language:language()}}));
      };
      const dirty = trim(this.keywordInput?.value) || Object.keys(this.draft).some(key => JSON.stringify(this.draft[key]) !== JSON.stringify(this.settings[key]));
      if (dirty) {
        ev.target.value = preference;
        shared.confirmAction(local('Change language and reload the panel? Unsaved settings will be lost.','Изменить язык и перезагрузить панель? Несохранённые настройки будут потеряны.'),apply);
      } else apply();
    },
    setSaving(saving) { this.saving = !!saving; this.syncBusy(); },
    syncBusy() {
      if (!this.settingsRoot) return;
      const busy = this.saving || this.remoteBusy || this.updateBusy();
      this.settingsRoot.querySelectorAll('input,select,button').forEach(control => {
        control.disabled = !!busy || control.dataset.unavailable === 'true';
      });
    },
    // A root operation returns admission only. Resolve only the exact observed
    // completed job, never a later unrelated job or a successful HTTP response.
    async queue(path, method, body, kind) {
      if (this.pending || shared.getSnapshot()?.job?.running) throw Error('job_already_running');
      const before = shared.getSnapshot()?.job?.sequence || 0;
      let resolve, reject;
      const completion = new Promise((yes,no) => {resolve=yes;reject=no;});
      completion.catch(() => {});
      const pending = {before,kind,resolve,reject,releaseID:body?.release_id,releaseVersion:this.updateState?.candidate?.version};
      this.pending = pending;
      try {
        if (!await shared.operation(path,method,body)) throw Error('operation_failed');
        // Removing the application can remove the HTTP server before its next
        // snapshot. Never wait forever or equate admission with cleanup.
        if (kind === 'settings-panel-uninstall') throw Error('uninstall_completion_unconfirmed');
        this.observeJob(shared.getSnapshot());
        await completion;
      } finally { if (this.pending === pending) this.pending = null; }
    },
    observeJob(snapshot) {
      const pending = this.pending, job = snapshot?.job;
      this.remoteBusy = !!job?.running;
      this.syncBusy();
      if (!pending || !job || job.sequence <= pending.before) return;
      if (job.kind !== pending.kind) { pending.reject(Error('job_result_unavailable')); return; }
      if (!job.running) {
        if (job.succeeded) pending.resolve(); else pending.reject(Error(job.error || 'operation_failed'));
      }
    },
    async persistAutoHideKeywords(values, previous) {
      this.draft.auto_hide_keywords = normalizeAutoHideKeywords(values);
      this.keywordSaving = true;
      this.setSaving(true);
      this.syncAutoHideKeywordControls();
      try {
        await this.queue('settings-panel/hide-keywords','POST',{keywords:this.draft.auto_hide_keywords},'settings-panel-keywords');
        const result = await shared.api('settings-panel');
        this.settings.auto_hide_keywords = result.settings.auto_hide_keywords || [];
        this.draft.auto_hide_keywords = this.settings.auto_hide_keywords.slice();
        shared.notice(_('Server hide rules updated.'));
      } catch (error) {
        this.draft.auto_hide_keywords = previous.slice();
        shared.notice(_('Could not update server hide rules.'),true);
      } finally {
        this.keywordSaving = false;
        this.setSaving(false);
        this.syncAutoHideKeywordControls();
        this.syncBusy();
      }
    },
    async handleSaveSettings(ev) {
      ev?.preventDefault();
      if (this.saving || this.remoteBusy || this.updateBusy()) return;
      const patch = {};
			const savedPolicy = this.settings.custom_auto_policy || {};
			const saved = { ...this.settings, auto_allow_optimization: !!savedPolicy.allow_optimization, auto_current_latency_ceiling: savedPolicy.current_latency_ceiling || '0s', auto_latency_improvement: savedPolicy.latency_improvement || '70ms', auto_relative_improvement: savedPolicy.relative_improvement == null ? 0.35 : savedPolicy.relative_improvement, auto_required_candidate_wins: savedPolicy.required_candidate_wins || 4, auto_cooldown: savedPolicy.cooldown || '20m0s', auto_failure_threshold: savedPolicy.failure_threshold || 2 };
			const numeric = new Set(['auto_relative_improvement','auto_required_candidate_wins','auto_failure_threshold']);
			for (const key of ['refresh_interval','health_check_interval','url_test_url','url_test_timeout','switch_cooldown','latency_threshold','strict_egress_check','auto_profile','auto_allow_optimization','auto_current_latency_ceiling','auto_latency_improvement','auto_relative_improvement','auto_required_candidate_wins','auto_cooldown','auto_failure_threshold']) {
        if (this.draft[key] === saved[key]) continue;
				patch[key] = numeric.has(key) ? Number(this.draft[key]) : this.draft[key];
      }
      if (!Object.keys(patch).length) { shared.notice(_('No settings changed.')); return; }
      if (!this.settingsRoot.querySelector('input[type=url]').reportValidity()) return;
      this.setSaving(true);
      try {
        await this.queue('settings-panel','PATCH',patch,'settings-panel-save');
        const result = await shared.api('settings-panel');
		this.settings = result.settings;
		this.draft = {...result.settings};
		this.prepareAutoDraft(result.settings);
        this.syncSettingsControls();
        shared.notice(_('Fast Lane settings saved.'));
      } catch (error) { shared.notice(_('Could not save settings.'), true); }
      finally { this.setSaving(false); }
    },
    paint() {
      const pendingWord = this.keywordInput?.value || '';
      container.replaceChildren(this.render(this.settings));
      this.keywordInput.value = pendingWord;
      this.settingsRoot.querySelector('.fls-select').parentNode.querySelector('p').textContent = local('Interface language for the entire Fast Lane panel. Automatic follows your browser language. Changing the language reloads the page.','Язык всей панели Fast Lane. Автоматический режим использует язык браузера. При смене языка страница перезагрузится.');
      const remove = this.settingsRoot.querySelector('.fls-danger');
      if (this.unavailable.uninstall) { remove.disabled = true; remove.dataset.unavailable = 'true'; }
      remove.closest('section').querySelector('p').textContent = this.unavailable.uninstall
        ? local('Removal and the FriendlyWrt package manager are unavailable on this host. Use LuCI for application management.','Удаление и менеджер пакетов FriendlyWrt недоступны на этом хосте. Управление приложениями доступно через LuCI.')
        : local('Fast Lane can be removed here. Open LuCI to manage other packages.','Fast Lane можно удалить здесь. Для управления другими пакетами откройте LuCI.');
      // Associate the original LuCI field labels with their controls.
      this.settingsRoot.querySelectorAll('.fls-field').forEach((field,index) => {
        const label = field.querySelector('label'), input = field.querySelector('input:not([data-duration-unit])');
        if (label && input) { input.id ||= 'fastlane-setting-'+index; label.htmlFor = input.id; }
      });
      this.syncUpdate();
      this.syncBusy();
    },
    async refresh(snapshot) {
      this.observeJob(snapshot);
      if (this.updateRequest || this.updateBusy()) await this.refreshUpdate();
      // Preserve every unsaved value and focus across root polling. Only merge
      // externally changed settings into controls which are still clean.
      if (!this.settings || this.saving) return;
      const fresh = snapshot?.status?.settings;
      if (fresh) {
        for (const key of Object.keys(fresh)) {
          if (JSON.stringify(this.draft[key]) === JSON.stringify(this.settings[key])) this.draft[key] = fresh[key];
        }
        this.settings = fresh;
        this.syncSettingsControls();
        if (!this.keywordSaving && document.activeElement !== this.keywordInput) this.syncAutoHideKeywordControls();
      }
      this.syncBusy();
    },
    async load() {
      try {
        const result = await shared.api('settings-panel');
        this.settings = result.settings;
        this.draft = {...result.settings};
        this.interfaceLanguage = preference;
        this.updateState = result.update;
        this.unavailable = result.unavailable || {};
        this.paint();
        this.observeJob(shared.getSnapshot());
      } catch (error) {
        container.replaceChildren(E('section',{class:'fastlane-settings'},[
          E('p',{role:'alert'},[local('Could not load settings.','Не удалось загрузить настройки.')]),
          E('button',{class:'fls-button',click:()=>this.load()},[local('Retry','Повторить')])
        ]));
      }
    }
  };
  container.replaceChildren(E('p',{role:'status'},[local('Loading settings…','Загрузка настроек…')]));
  const controller = {refresh:snapshot=>view.refresh(snapshot), ready:view.load()};
  return controller;
}
window.FastLaneSettings = {mount:create, _ui:{E,_,local,ui,L}};
})();
