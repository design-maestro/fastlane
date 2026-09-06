'use strict';

const assert = require('node:assert/strict');
const fsNode = require('node:fs');
const path = require('node:path');

const repoRoot = process.argv[2];
if (!repoRoot)
	throw new Error('repository root argument is required');

const resourcesRoot = path.join(repoRoot, 'luci-app-fastlane', 'htdocs', 'luci-static', 'resources');
const menu = JSON.parse(fsNode.readFileSync(path.join(repoRoot, 'luci-app-fastlane', 'root', 'usr', 'share', 'luci', 'menu.d', 'luci-app-fastlane.json'), 'utf8'));

function productionViewPath(route) {
	const entry = menu['admin/services/fastlane/' + route];
	assert.ok(entry && entry.action && entry.action.type === 'view', 'missing production menu view for ' + route);
	return path.join(resourcesRoot, 'view', entry.action.path + '.js');
}

function clone(value) {
	return JSON.parse(JSON.stringify(value));
}

class FakeNode {
	constructor(tag, attrs, children) {
		this.tag = tag;
		this.attrs = attrs || {};
		this.children = Array.isArray(children) ? children : (children == null ? [] : [children]);
		this.value = this.attrs.value == null ? '' : String(this.attrs.value);
		this.checked = this.attrs.checked != null && this.attrs.checked !== false;
		this.disabled = this.attrs.disabled != null && this.attrs.disabled !== false;
		this.textContent = '';
		this.className = this.attrs.class || '';
		this.focused = false;
		this.listeners = {};
		this.parentNode = null;
		this.attributes = { ...this.attrs };
		this.classList = {
			add: (...names) => { this.className = [this.className, ...names].filter(Boolean).join(' '); },
			remove: (...names) => { this.className = this.className.split(/\s+/).filter((name) => name && !names.includes(name)).join(' '); }
		};
		for (const child of this.children) {
			if (child && typeof child === 'object') child.parentNode = this;
		}
	}

	addEventListener(name, handler) { this.listeners[name] = handler; }
	focus() { this.focused = true; }
	setAttribute(name, value) { this.attributes[name] = String(value); }
	hasAttribute(name) { return Object.prototype.hasOwnProperty.call(this.attributes, name); }
	removeAttribute(name) { delete this.attributes[name]; }
	appendChild(child) { this.children.push(child); if (child && typeof child === 'object') child.parentNode = this; return child; }
	removeChild(child) { this.children = this.children.filter((item) => item !== child); child.parentNode = null; }
	querySelector() { return null; }
}

function E(tag, attrs, children) {
	return new FakeNode(tag, attrs, children);
}

let queryResolver = () => null;
let idResolver = () => null;
let reloads = 0;
const documentStub = {
	documentElement: { lang: 'en' },
	body: new FakeNode('body'),
	hidden: false,
	listeners: {},
	addEventListener(name, handler) { this.listeners[name] = handler; },
	querySelector: (selector) => queryResolver(selector),
	getElementById: (id) => idResolver(id),
	createElementNS: (_namespace, tag) => new FakeNode(tag)
};
global.document = documentStub;
global._ = (value) => value;
global.window = {
	navigator: { language: 'en' },
	Intl,
	listeners: {},
	addEventListener(name, handler) { this.listeners[name] = handler; },
	sessionStorage: {
		values: {},
		getItem(key) { return Object.prototype.hasOwnProperty.call(this.values, key) ? this.values[key] : null; },
		setItem(key, value) { this.values[key] = String(value); }
	},
	confirm: () => true,
	requestAnimationFrame: (handler) => handler(),
	setTimeout: (handler) => { handler(); return 1; },
	clearTimeout: () => {},
	location: { reload: () => { reloads++; } }
};
global.atob = global.atob || ((value) => Buffer.from(value, 'base64').toString('binary'));
global.FileReader = class {
	readAsText(file) {
		this.result = file.content;
		this.onload();
	}
};

const L = {
	bind: (fn, self, ...args) => fn.bind(self, ...args),
	resource: (resource) => '/luci-static/resources/' + resource,
	url: (...parts) => '/' + parts.join('/')
};
const view = { extend: (value) => value };
const dom = { content: () => {} };
const poll = {
	entries: [],
	started: false,
	add(handler, interval) { this.entries.push({ handler, interval }); },
	remove(handler) { this.entries = this.entries.filter((entry) => entry.handler !== handler); },
	start() { this.started = true; }
};

const toasts = [];
const modals = [];
let modalHidden = 0;
const fastlaneShell = {
	showToast: (message, type, details) => { toasts.push({ message, type, details }); },
	renderStyles: () => E('style', {}, ['shared styles']),
	renderHeader: (section) => E('header', { class: 'fl-shell-nav', section }, ['Fast Lane', section])
};
const ui = {
	createHandlerFn: (self, method, ...args) => (event) => self[method](...args, event),
	showModal: (title, body) => { modals.push({ title, body }); },
	hideModal: () => { modalHidden++; }
};
const countries = {
	codes: ['AR', 'DE', 'EE', 'IR', 'JP', 'NL', 'PL', 'RU'],
	name: (code) => ({ AR: 'Argentina', DE: 'Germany', EE: 'Estonia', IR: 'Iran', JP: 'Japan', NL: 'Netherlands', PL: 'Poland', RU: 'Russia' }[code] || code),
	options: (selected) => ['RU', 'DE', 'IR'].map((code) => E('option', { value: code, selected: selected === code ? 'selected' : null }, [ ({ RU: 'Russia', DE: 'Germany', IR: 'Iran' }[code]) ]))
};
const uciValues = { lang: 'auto' };
const uci = {
	load: async () => {},
	get: (_config, _section, option) => uciValues[option],
	set: (_config, _section, option, value) => { uciValues[option] = value; },
	save: async () => {},
	apply: async () => {}
};

const subscriptionsFixture = [
	{
		id: 'durev', provider_name: 'Durev', last_updated_at: '2026-09-03T18:00:00Z', expires_at: '2099-09-10T18:00:00Z',
		nodes: [
			{ id: 'nl', name: 'Netherlands · Amsterdam', protocol: 'vless', address: 'nl.example', port: 443 },
			{ id: 'ee', name: 'Estonia · Tallinn', protocol: 'xhttp', address: 'ee.example', port: 443 }
		]
	},
	{
		id: 'blanc', provider_name: 'Blanc', last_updated_at: '2026-09-03T18:02:00Z',
		nodes: [{ id: 'pl', name: 'Poland · Warsaw', protocol: 'vless', address: 'pl.example', port: 443 }]
	}
];

const routingServicesFixture = [
	{
		name: 'roborock', source: 'custom', readonly: false, services: [],
		domains: ['roborock.com', 'miot-spec.org'], cidrs: ['192.0.2.0/24']
	}
];

const statusFixture = {
	state: {
		connected: true,
		mode: 'auto',
		active_subscription_id: 'durev',
		active_node_id: 'nl'
	},
	settings: {
		auto_mode: true,
		auto_excluded_nodes: ['durev/ee'],
		country_routing: { enabled: false, country_code: 'RU' }
	},
	active_subscription: subscriptionsFixture[0],
	active_node: subscriptionsFixture[0].nodes[0]
};

const settingsFixture = {
	refresh_interval: '1h0m0s',
	health_check_interval: '30s',
	url_test_url: 'https://example.com/generate_204',
	url_test_timeout: '15s',
	switch_cooldown: '5m0s',
	latency_threshold: '50ms',
	strict_egress_check: true,
	country_routing: { enabled: false, country_code: 'RU' },
	firewall: { enabled: true, mode: 'split', split: { default_action: 'proxy', bypass: { services: ['roborock'], domains: [], cidrs: [] }, excluded_sources: ['192.168.1.50'] } }
};

const diagnosticsFixture = {
	status: clone(statusFixture),
	runtime: { running: true, service_state: 'running', config_path: '/etc/xray/config.json' },
	dns: { active: true, available: true, local_dns_listen: '127.0.0.1', local_dns_port: 1053, system_resolvers: ['1.1.1.1'] },
	ipv6: { available: true, runtime_disabled: true },
	files: { config: { exists: true }, state: { exists: true } }
};

let commands = [];
let resolver = defaultResolver;
const fsStub = {
	exec: async (commandPath, args) => {
		commands.push({ path: commandPath, args: [...args] });
		return resolver(commandPath, [...args]);
	}
};

function defaultResolver(commandPath, args) {
	const joined = args.join(' ');
	if (commandPath.endsWith('fastlane-geodata')) {
		if (joined === 'start') return { code: 0, stdout: JSON.stringify({ ready: false, updating: true, last_result: 'updating' }), stderr: '' };
		if (joined === 'status') {
			const started = commands.some((item) => item.path.endsWith('fastlane-geodata') && item.args[0] === 'start');
			return { code: 0, stdout: JSON.stringify(started
				? { ready: true, updating: false, last_result: 'ok' }
				: { ready: false, updating: false, last_result: 'never' }), stderr: '' };
		}
	}
	if (joined === '--json status') return { code: 0, stdout: JSON.stringify(statusFixture), stderr: '' };
	if (joined === '--json list subscriptions') return { code: 0, stdout: JSON.stringify(subscriptionsFixture), stderr: '' };
	if (joined === '--json inspect health-check-status') return { code: 0, stdout: JSON.stringify({ status: 'idle' }), stderr: '' };
	if (joined.startsWith('--json inspect health-check --subscription ')) return { code: 0, stdout: JSON.stringify({ status: 'queued', scope: args.at(-1) }), stderr: '' };
	if (joined === '--json settings get') return { code: 0, stdout: JSON.stringify(settingsFixture), stderr: '' };
	if (joined === '--json services list') return { code: 0, stdout: JSON.stringify(routingServicesFixture), stderr: '' };
	if (joined.startsWith('--json services set ')) return { code: 0, stdout: JSON.stringify({ name: args[3] }), stderr: '' };
	if (joined.startsWith('--json services delete ')) return { code: 0, stdout: 'Deleted\n', stderr: '' };
	if (joined.startsWith('--json firewall set bypass')) return { code: 0, stdout: JSON.stringify(settingsFixture), stderr: '' };
	if (joined.startsWith('--json settings patch ')) {
		const patch = JSON.parse(args.at(-1));
		const result = clone(settingsFixture);
		if (Object.prototype.hasOwnProperty.call(patch, 'country_direct')) result.country_routing.enabled = patch.country_direct;
		if (Object.prototype.hasOwnProperty.call(patch, 'direct_country')) result.country_routing.country_code = patch.direct_country;
		return { code: 0, stdout: JSON.stringify(result), stderr: '' };
	}
	if (joined === '--json diagnostics') return { code: 0, stdout: JSON.stringify(diagnosticsFixture), stderr: '' };
	if (joined.includes('--json inspect url-test')) {
		const nodeID = args[args.indexOf('--node') + 1];
		const latency = { nl: 131, pl: 144, ee: 168 }[nodeID] || 250;
		return { code: 0, stdout: JSON.stringify({ healthy: true, latency_ms: latency, checked_at: '2026-09-03T18:03:00Z', url: 'https://example.com/generate_204' }), stderr: '' };
	}
	return { code: 0, stdout: '{}', stderr: '' };
}

function loadModule(filePath, dependencies) {
	const source = fsNode.readFileSync(filePath, 'utf8');
	const names = Object.keys(dependencies);
	return new Function(...names, source)(...names.map((name) => dependencies[name]));
}

function loadPage(route) {
	return loadModule(productionViewPath(route), { view, fs: fsStub, ui, dom, poll, L, E, fastlaneShell, countries, countryCatalog: countries, uci });
}

function treeText(node) {
	if (node == null || node === false) return '';
	if (Array.isArray(node)) return node.map(treeText).join(' ');
	if (typeof node !== 'object') return String(node);
	return [node.textContent, treeText(node.children)].filter(Boolean).join(' ');
}

function resetHarness() {
	commands = [];
	toasts.length = 0;
	modals.length = 0;
	modalHidden = 0;
	reloads = 0;
	resolver = defaultResolver;
	queryResolver = () => null;
	idResolver = () => null;
	window.confirm = () => true;
	window.sessionStorage.values = {};
	uciValues.lang = 'auto';
	poll.entries = [];
	poll.started = false;
}

function commandSeen(expectedArgs, expectedPathSuffix = '/fastlane') {
	assert.ok(commands.some((item) => item.path.endsWith(expectedPathSuffix) && JSON.stringify(item.args) === JSON.stringify(expectedArgs)), 'command not seen: ' + expectedArgs.join(' '));
}

function commandCount(prefix) {
	return commands.filter((item) => item.args.join(' ').startsWith(prefix)).length;
}

function makeVPN() {
	const page = loadPage('vpn');
	page.pageData = [clone(statusFixture), clone(subscriptionsFixture), { status: 'idle' }];
	page.filter = 'all';
	page.showHidden = false;
	page.query = '';
	page.sort = 'latency';
	page.country = 'all';
	page.protocol = 'all';
	page.busy = '';
	page.error = null;
	page.dismissedErrors = {};
	page.expandedErrors = {};
	page.pings = {
		'durev:nl': { healthy: true, latency_ms: 131, checked_at: '2026-09-03T18:03:00Z', url_test: true },
		'blanc:pl': { healthy: true, latency_ms: 144, checked_at: '2026-09-03T18:03:00Z', url_test: true }
	};
	page.testingNodes = {};
	page.activeMenuKey = '';
	page.batchTesting = false;
	page.batchDone = 0;
	page.batchTotal = 0;
	page.update = () => {};
	page.refreshView = async () => {};
	return page;
}

function makeRouting(ready = false, enabled = false, firewall = settingsFixture.firewall) {
	const page = loadPage('routing');
	page.settings = { ...clone(settingsFixture), country_routing: { enabled, country_code: 'RU' }, firewall: clone(firewall) };
	page.countryCode = 'RU';
	page.geodata = { ready };
	page.services = clone(routingServicesFixture);
	page.rulesError = '';
	page.busy = false;
	page.ruleBusy = '';
	page.progress = '';
	page.geodataPollInterval = 0;
	page.importValue = '';
	page.renderAgain = () => {};
	return page;
}

function makeSettings() {
	const page = loadPage('settings');
	page.settings = clone(settingsFixture);
	page.draft = clone(settingsFixture);
	return page;
}

const results = [];
async function smoke(section, name, run) {
	resetHarness();
	try {
		await run();
		results.push({ section, name, status: 'PASS' });
	} catch (error) {
		results.push({ section, name, status: 'FAIL', error: error.stack || String(error) });
	}
}

(async () => {
	await smoke('VPN', 'loads status and subscriptions from allowed read commands', async () => {
		const page = loadPage('vpn');
		const data = await page.load();
		assert.equal(data[0].state.connected, true);
		assert.equal(data[1].length, 2);
		commandSeen(['--json', 'status']);
		commandSeen(['--json', 'list', 'subscriptions']);
		commandSeen(['--json', 'inspect', 'health-check-status']);
		assert.equal(poll.started, true);
		assert.equal(poll.entries[0].interval, 5);
	});

	await smoke('VPN', 'keeps independent read failures visible without breaking render', async () => {
		resolver = async (_path, args) => args.join(' ') === '--json status'
			? { code: 1, stdout: '', stderr: 'status unavailable' }
			: defaultResolver(_path, args);
		const page = loadPage('vpn');
		const data = await page.load();
		assert.match(data[0].__error, /status unavailable/);
		assert.ok(page.render(data));
		assert.ok(toasts.some((toast) => toast.type === 'error'));
	});

	await smoke('VPN', 'renders connected, country-city rows and empty state', async () => {
		const page = makeVPN();
		let text = treeText(page.render(page.pageData));
		for (const expected of ['Fast Lane', 'VPN on', 'Netherlands', 'Amsterdam', 'Durev', 'VLESS', '131 ms', 'Active']) assert.match(text, new RegExp(expected, 'i'));
		page.pageData = [{ state: { connected: false }, settings: {} }, []];
		text = treeText(page.render(page.pageData));
		assert.match(text, /VPN off/);
		assert.match(text, /Add your first subscription/);
	});

	await smoke('VPN', 'uses one honest GET color scale including boundary and unknown values', async () => {
		const page = makeVPN();
		for (const [value, expected] of [[40, 'good'], [50, 'good'], [100, 'good'], [101, 'mid'], [200, 'mid'], [201, 'slow'], [1000, 'slow'], [1001, 'critical'], [1500, 'critical']]) {
			assert.equal(page.latencyClass(value, { healthy: true }), 'fl-latency-' + expected);
		}
		for (const value of [null, undefined, 0, -1, NaN, '']) assert.equal(page.latencyClass(value, {}), '');
		assert.equal(page.latencyClass(null, { healthy: false }), 'fl-latency-bad');
		assert.equal(page.latencyClass(40, { test_error: true }), '');
		page.pings['durev:nl'] = { latency_ms: 40, healthy: true };
		assert.match(page.renderStatus().children[3].children[1].className, /fl-status-cell-latency fl-latency-good/);
		page.pings['durev:nl'] = { latency_ms: 1500, healthy: true, checked_at: '2026-09-05T01:00:00Z' };
		page.pings['blanc:pl'] = { latency_ms: 1001, healthy: true, checked_at: '2026-09-05T01:00:00Z' };
		assert.match(page.renderStatus().children[3].children[1].className, /fl-latency-critical/);
		let text = treeText(page.renderTable());
		assert.match(text, /Active/);
		assert.match(text, /Slow/);
		assert.doesNotMatch(text, /Unavailable/);
		assert.equal(page.pings['blanc:pl'].healthy, true);
		page.pings['blanc:pl'].healthy = false;
		text = treeText(page.renderTable());
		assert.match(text, /Unavailable/);
		assert.doesNotMatch(text, /Slow/);
	});

	await smoke('VPN', 'filters sources, hidden servers, search, country and protocol', async () => {
		const page = makeVPN();
		page.handleFilter('durev', { preventDefault() {} });
		assert.equal(page.visibleRows().length, 1);
		page.handleFilter('hidden', { preventDefault() {} });
		assert.equal(page.visibleRows()[0].node.id, 'ee');
		page.handleFilter('all', { preventDefault() {} });
		page.handleSearch({ target: { value: ' poland ' } });
		assert.deepEqual(page.visibleRows().map((row) => row.node.id), ['pl']);
		page.query = '';
		page.handleCountry({ target: { value: 'NL' } });
		assert.deepEqual(page.visibleRows().map((row) => row.node.id), ['nl']);
		page.country = 'all';
		page.handleProtocol({ target: { value: 'vless' } });
		assert.deepEqual(page.visibleRows().map((row) => row.node.id).sort(), ['nl', 'pl']);
	});

	await smoke('VPN', 'normalizes localized country labels into one filter option', async () => {
		const page = makeVPN();
		page.pageData[1][1].nodes.push(
			{ id: 'ru-en', name: '🇷🇺 Russia, Extra Whitelist', protocol: 'vless', address: 'ru-one.example', port: 443 },
			{ id: 'ru-ru', name: 'Россия · Москва', protocol: 'vless', address: 'ru-two.example', port: 443 },
			{ id: 'ar-ru', name: 'Буэнос-Айрес, Аргентина, Extra', protocol: 'vless', address: 'ar.example', port: 443 }
		);
		assert.equal(page.filterCountries().filter((code) => code === 'RU').length, 1);
		page.handleCountry({ target: { value: 'RU' } });
		assert.deepEqual(page.visibleRows().map((row) => row.node.id).sort(), ['ru-en', 'ru-ru']);
		page.country = 'AR';
		const text = treeText(page.renderTable());
		assert.match(text, /Argentina/);
		assert.match(text, /Буэнос-Айрес/);
		assert.doesNotMatch(text, /Argentina.*Argentina/);
	});

	await smoke('VPN', 'sorts by GET latency, name and source', async () => {
		const page = makeVPN();
		page.pageData[1][1].nodes.push({ id: 'none', name: 'Zulu', protocol: 'vless', address: 'none.example', port: 443 });
		assert.deepEqual(page.visibleRows().map((row) => row.node.id), ['nl', 'pl', 'none']);
		page.handleSort({ target: { value: 'name' } });
		assert.deepEqual(page.visibleRows().map((row) => row.node.id), ['nl', 'pl', 'none']);
		page.handleSort({ target: { value: 'source' } });
		assert.deepEqual(page.visibleRows().map((row) => row.sub.id), ['blanc', 'blanc', 'durev']);
	});

	await smoke('VPN', 'keeps expired sources visible but out of the combined pool', async () => {
		const page = makeVPN();
		page.pageData[1][0].expires_at = '2020-01-01T00:00:00Z';
		page.filter = 'all';
		assert.deepEqual(page.poolSubscriptions().map((sub) => sub.id), ['blanc']);
		assert.deepEqual(page.visibleRows().map((row) => row.node.id), ['pl']);
		assert.equal(page.selectedSubscription().id, 'blanc');
		assert.doesNotMatch(treeText(page.renderTabs()), /All servers/);
		page.handleFilter('durev', { preventDefault() {} });
		page.pageData[0].state.connected = false;
		assert.deepEqual(page.visibleRows().map((row) => row.node.id), ['nl']);
		assert.match(treeText(page.renderTable()), /Expired/);
	});

	await smoke('VPN', 'manual mode focuses the first connectable row', async () => {
		const page = makeVPN();
		const row = new FakeNode('tr');
		queryResolver = (selector) => selector.includes('tbody tr[tabindex]') ? row : null;
		page.handleManualMode({ preventDefault() {} });
		assert.equal(row.focused, true);
		assert.ok(toasts.some((toast) => toast.type === 'info'));
	});

	await smoke('VPN', 'hides and restores a node through persisted exclusions', async () => {
		const page = makeVPN();
		const hide = page.handleHidden('durev', 'nl', true, { preventDefault() {}, stopPropagation() {} });
		assert.equal(page.isHidden('durev', 'nl'), true);
		assert.deepEqual(page.visibleRows().map((row) => row.node.id), ['pl']);
		await hide;
		commandSeen(['settings', 'set', 'auto.excluded-nodes', 'durev/ee, durev/nl']);
		commands = [];
		page.handleFilter('hidden');
		const restore = page.handleHidden('durev', 'ee', false);
		assert.equal(page.isHidden('durev', 'ee'), false);
		await restore;
		commandSeen(['settings', 'set', 'auto.excluded-nodes', 'durev/nl']);
	});

	await smoke('VPN', 'keeps one server action menu open and closes it outside', async () => {
		const page = makeVPN();
		const event = { preventDefault() {}, stopPropagation() {} };
		page.handleServerMenuToggle('durev:nl', event);
		assert.equal(page.activeMenuKey, 'durev:nl');
		page.handleServerMenuToggle('blanc:pl', event);
		assert.equal(page.activeMenuKey, 'blanc:pl');
		page.handleDocumentClick({ target: { closest: () => null } });
		assert.equal(page.activeMenuKey, '');
	});

	await smoke('VPN', 'rolls back optimistic hide when persistence fails', async () => {
		resolver = async (_path, args) => args.join(' ').startsWith('settings set auto.excluded-nodes')
			? { code: 1, stdout: '', stderr: 'save failed' }
			: defaultResolver(_path, args);
		const page = makeVPN();
		await page.handleHidden('durev', 'nl', true);
		assert.equal(page.isHidden('durev', 'nl'), false);
		assert.ok(toasts.some((toast) => toast.type === 'error' && /save failed/.test(toast.details)));
	});

	await smoke('VPN', 'refreshes all, selected and failed-source subscriptions', async () => {
		const page = makeVPN();
		await page.handleRefreshSubscriptions();
		commandSeen(['refresh', '--all']);
		commands = [];
		page.filter = 'durev';
		await page.handleRefreshSubscriptions();
		commandSeen(['refresh', '--subscription', 'durev']);
		commands = [];
		await page.handleRefreshSource('blanc');
		commandSeen(['refresh', '--subscription', 'blanc']);
	});

	await smoke('VPN', 'connects in auto/manual modes and disconnects', async () => {
		const page = makeVPN();
		await page.handleAuto();
		commandSeen(['--json', 'inspect', 'health-check', '--subscription', 'all']);
		commands = [];
		await page.handleConnect('durev', 'nl');
		commandSeen(['connect', '--subscription', 'durev', '--node', 'nl']);
		commands = [];
		await page.handleDisconnect();
		commandSeen(['disconnect']);
	});

	await smoke('VPN', 'supports Enter and Space keyboard activation only', async () => {
		const page = makeVPN();
		for (const key of ['Enter', ' ']) {
			let prevented = false;
			await page.handleRowKey('durev', 'nl', { key, preventDefault() { prevented = true; }, stopPropagation() {} });
			assert.equal(prevented, true);
		}
		assert.equal(commandCount('connect --subscription durev --node nl'), 2);
		commands = [];
		let interactivePrevented = false;
		const row = {};
		await page.handleRowKey('durev', 'nl', {
			key: 'Enter',
			currentTarget: row,
			target: { closest(selector) { assert.match(selector, /summary/); return {}; } },
			preventDefault() { interactivePrevented = true; }
		});
		assert.equal(interactivePrevented, false);
		assert.equal(commands.length, 0);
		await page.handleRowKey('durev', 'nl', { key: 'Escape', preventDefault() { throw new Error('must not prevent'); } });
		assert.equal(commands.length, 0);
	});

	await smoke('VPN', 'runs one HTTPS GET check and persists its result', async () => {
		const page = makeVPN();
		page.pings = {};
		await page.handleURLTest('durev', 'nl');
		commandSeen(['--json', 'inspect', 'url-test', '--subscription', 'durev', '--node', 'nl']);
		assert.equal(page.pings['durev:nl'].latency_ms, 131);
		assert.equal(JSON.parse(window.sessionStorage.values['fastlane.vpn.get.results.v1'])['durev:nl'].url_test, true);
	});

	await smoke('VPN', 'restores persisted GET health and active state after reload', async () => {
		const persistedStatus = clone(statusFixture);
		persistedStatus.state.health = {
			nl: {
				node_id: 'nl', healthy: true, last_latency: '131.4ms', average_latency: '140ms',
				last_checked_at: '2026-09-03T18:03:00Z'
			}
		};
		resolver = async (commandPath, args) => {
			if (args.join(' ') === '--json status')
				return { code: 0, stdout: JSON.stringify(persistedStatus), stderr: '' };
			return defaultResolver(commandPath, args);
		};
		const page = loadPage('vpn');
		await page.load();
		assert.equal(page.pings['durev:nl'].healthy, true);
		assert.equal(page.pings['durev:nl'].latency_ms, 131.4);
		assert.match(treeText(page.renderStatus()), /VPN on/);
		assert.match(treeText(page.renderStatus()), /131 ms/);
		assert.match(treeText(page.renderTable()), /Active/);
	});

	await smoke('VPN', 'keeps the connected node active when its last isolated GET failed', async () => {
		const subscriptions = clone([subscriptionsFixture[0]]);
		subscriptions[0].nodes = [subscriptions[0].nodes[0]];
		for (const lastLatency of ['0s', null]) {
			const persistedStatus = clone(statusFixture);
			const health = {
				node_id: 'nl', healthy: true, average_latency: '131ms',
				consecutive_failures: 1, last_failure_reason: 'GET timed out',
				last_checked_at: '2026-09-03T18:03:00Z'
			};
			if (lastLatency !== null) health.last_latency = lastLatency;
			persistedStatus.state.health = { nl: health };
			resolver = async (commandPath, args) => {
				if (args.join(' ') === '--json status')
					return { code: 0, stdout: JSON.stringify(persistedStatus), stderr: '' };
				if (args.join(' ') === '--json list subscriptions')
					return { code: 0, stdout: JSON.stringify(subscriptions), stderr: '' };
				return defaultResolver(commandPath, args);
			};
			const page = loadPage('vpn');
			await page.load();
			assert.equal(page.pings['durev:nl'].healthy, false);
			assert.equal(page.pings['durev:nl'].latency_ms, null);
			const text = treeText(page.renderTable());
			assert.match(text, /Active/);
			assert.doesNotMatch(text, /Unavailable/);
			assert.doesNotMatch(text, /(?:0|131) ms/);
			assert.doesNotMatch(text, /Ready/);
		}
	});

	await smoke('VPN', 'prefers only a newer session GET result over persisted health', async () => {
		const persistedStatus = clone(statusFixture);
		persistedStatus.state.health = {
			nl: {
				node_id: 'nl', healthy: true, last_latency: '131ms', average_latency: '131ms',
				last_checked_at: '2026-09-03T18:03:00Z'
			}
		};
		window.sessionStorage.values['fastlane.vpn.get.results.v1'] = JSON.stringify({
			'durev:nl': { healthy: false, latency_ms: null, checked_at: '2026-09-03T18:04:00Z', url_test: true }
		});
		resolver = async (commandPath, args) => {
			if (args.join(' ') === '--json status')
				return { code: 0, stdout: JSON.stringify(persistedStatus), stderr: '' };
			return defaultResolver(commandPath, args);
		};
		const page = loadPage('vpn');
		await page.load();
		assert.equal(page.pings['durev:nl'].healthy, false);
		assert.equal(page.pings['durev:nl'].latency_ms, null);

		window.sessionStorage.values['fastlane.vpn.get.results.v1'] = JSON.stringify({
			'durev:nl': { healthy: false, latency_ms: null, checked_at: '2026-09-03T18:02:00Z', url_test: true }
		});
		const reloaded = loadPage('vpn');
		await reloaded.load();
		assert.equal(reloaded.pings['durev:nl'].healthy, true);
		assert.equal(reloaded.pings['durev:nl'].latency_ms, 131);
	});

	await smoke('VPN', 'marks a failed single GET without displaying zero latency', async () => {
		const page = makeVPN();
		resolver = async (_path, args) => args.includes('url-test') ? { code: 1, stdout: '', stderr: 'timed out' } : defaultResolver(_path, args);
		await page.handleURLTest('durev', 'nl');
		assert.equal(page.pings['durev:nl'].healthy, false);
		assert.equal(page.pings['durev:nl'].latency_ms, null);
		assert.ok(toasts.some((toast) => toast.type === 'error' && /timed out/.test(toast.details)));
	});

	await smoke('VPN', 'queues the whole GET check on the router instead of running it in the browser', async () => {
		const page = makeVPN();
		await page.handleURLTests();
		commandSeen(['--json', 'inspect', 'health-check', '--subscription', 'all']);
		assert.equal(commandCount('--json inspect url-test'), 0);
		assert.ok(toasts.some((toast) => toast.type === 'success' && /close the page/.test(toast.message)));
	});

	await smoke('VPN', 'queues a selected subscription without narrowing top-level auto mode', async () => {
		const page = makeVPN();
		page.filter = 'durev';
		await page.handleURLTests();
		commandSeen(['--json', 'inspect', 'health-check', '--subscription', 'durev']);
		commands = [];
		await page.handleAuto();
		commandSeen(['--json', 'inspect', 'health-check', '--subscription', 'all']);
	});

	await smoke('VPN', 'merges partial router-side progress as each GET result finishes', async () => {
		const page = makeVPN();
		page.pings = {};
		page.mergeBackgroundPings({
			status: 'running', total: 3, done: 1,
			results: { nl: { node_id: 'nl', healthy: true, last_latency: '42ms', last_checked_at: '2026-09-03T18:04:00Z' } }
		}, page.subscriptions());
		assert.equal(page.pings['durev:nl'].latency_ms, 42);
		assert.equal(page.pings['durev:nl'].healthy, true);
		assert.equal(page.pings['blanc:pl'], undefined);
	});

	await smoke('VPN', 'keeps the last confirmed VPN state across a transient status read failure', async () => {
		const page = loadPage('vpn');
		await page.load();
		resolver = async (commandPath, args) => args.join(' ') === '--json status'
			? { code: 1, stdout: '', stderr: 'temporary ubus timeout' }
			: defaultResolver(commandPath, args);
		await page.fetchData();
		assert.equal(page.status().state.connected, true);
		assert.equal(page.fetchErrors.status, 'temporary ubus timeout');
		assert.match(treeText(page.renderStatus()), /VPN on/);
	});

	await smoke('VPN', 'labels never-checked rows truthfully', async () => {
		const page = makeVPN();
		page.pageData[0].state.connected = false;
		page.pings = {};
		const text = treeText(page.renderTable());
		assert.match(text, /Not checked/);
		assert.doesNotMatch(text, /Ready/);
	});

	await smoke('VPN', 'opens the branded add dialog and rejects an empty source', async () => {
		const page = makeVPN();
		page.handleAddOpen();
		assert.equal(modals.at(-1).title, 'Add servers');
		const name = E('input'), source = E('textarea'), files = E('input'), error = E('div'), submit = E('button');
		page.handleAddSubmit(name, source, files, () => 'subscription', error, submit);
		assert.equal(error.textContent, 'Paste a link or configuration.');
		assert.equal(source.focused, true);
	});

	await smoke('VPN', 'adds URL and raw subscriptions with optional naming', async () => {
		const page = makeVPN();
		let name = E('input'), source = E('textarea'), error = E('div'), submit = E('button');
		name.value = 'Liberty'; source.value = 'https://provider.example/sub';
		await page.handleAddSubmit(name, source, E('input'), () => 'subscription', error, submit);
		commandSeen(['add', '--name', 'Liberty', '--url', 'https://provider.example/sub']);
		assert.equal(modalHidden, 1);
		commands = [];
		name = E('input'); source = E('textarea'); error = E('div'); submit = E('button'); source.value = 'vless://key';
		await page.handleAddSubmit(name, source, E('input'), () => 'subscription', error, submit);
		commandSeen(['add', '--raw', 'vless://key']);
	});

	await smoke('VPN', 'adds multiple YAML files as independent sources', async () => {
		const page = makeVPN();
		const fileInput = E('input'), error = E('div'), submit = E('button');
		fileInput.files = [
			{ name: 'europe.yaml', size: 120, content: 'proxies:\n  - name: NL' },
			{ name: 'backup.yml', size: 110, content: 'payload:\n  - name: DE' }
		];
		await page.handleFileAddSubmit(fileInput, error, submit);
		commandSeen(['add', '--file-name', 'europe.yaml', '--raw', 'proxies:\n  - name: NL']);
		commandSeen(['add', '--file-name', 'backup.yml', '--raw', 'payload:\n  - name: DE']);
		assert.ok(toasts.some((toast) => toast.type === 'success' && /Files added: 2/.test(toast.message)));
	});

	await smoke('VPN', 'restores the add form after a backend error', async () => {
		const page = makeVPN();
		resolver = async () => ({ code: 1, stdout: '', stderr: 'invalid subscription' });
		const name = E('input'), source = E('textarea'), error = E('div'), submit = E('button');
		source.value = 'broken';
		await page.handleAddSubmit(name, source, E('input'), () => 'subscription', error, submit);
		assert.equal(submit.disabled, false);
		assert.equal(submit.textContent, 'Add');
		assert.equal(source.focused, true);
		assert.ok(toasts.some((toast) => toast.type === 'error'));
	});

	await smoke('VPN', 'removes only after confirmation', async () => {
		const page = makeVPN();
		window.confirm = () => false;
		await page.handleRemove('durev');
		assert.equal(commands.length, 0);
		window.confirm = () => true;
		await page.handleRemove('durev');
		commandSeen(['remove', 'durev']);
	});

	await smoke('Routes', 'loads routing and GeoIP/GeoSite readiness', async () => {
		const page = loadPage('routing');
		const data = await page.load();
		assert.equal(data[0].country_routing.enabled, false);
		assert.equal(data[1].ready, false);
		assert.equal(data[2].value[0].name, 'roborock');
		commandSeen(['--json', 'settings', 'get']);
		commandSeen(['--json', 'services', 'list']);
		commandSeen(['status'], '/fastlane-geodata');
	});

	await smoke('Routes', 'renders the local-country flow and HAPP safety note', async () => {
		const page = makeRouting(false, false);
		const text = treeText(page.render([page.settings, page.geodata]));
		for (const expected of ['Routing', 'Local-country traffic directly', 'Home network', 'Russia', 'Rest of the internet', 'Direct access exclusions', 'Roborock', 'roborock.com', '192.0.2.0/24', 'Import and advanced rules']) assert.match(text, new RegExp(expected));
	});

	await smoke('Routes', 'adds, toggles, and deletes a direct-access exclusion through the backend', async () => {
		const page = makeRouting(true, true);
		const nameInput = E('input', { value: 'camera-cloud' });
		const domainsInput = E('textarea', {}, []);
		const cidrsInput = E('textarea', {}, []);
		domainsInput.value = 'camera.example\napi.camera.example';
		cidrsInput.value = '198.51.100.0/24';
		const errorBox = E('div');
		const submit = E('button');
		await page.handleRuleSubmit(null, nameInput, domainsInput, cidrsInput, errorBox, submit);
		commandSeen(['--json', 'services', 'set', 'camera-cloud', 'camera.example', 'api.camera.example', '198.51.100.0/24']);
		assert.ok(commands.some((item) => item.args.slice(0, 4).join(' ') === '--json firewall set bypass' && item.args.includes('camera-cloud') && item.args.includes('--exclude-host') && item.args.includes('192.168.1.50')));
		assert.equal(modalHidden, 1);

		commands = [];
		await page.handleRuleToggle('roborock', { target: { checked: false } });
		assert.ok(commands.some((item) => item.args.slice(0, 4).join(' ') === '--json firewall set bypass' && !item.args.includes('roborock') && item.args.includes('192.168.1.50')));

		commands = [];
		await page.handleRuleDelete('roborock', { preventDefault() {} });
		commandSeen(['--json', 'firewall', 'set', 'bypass', '--exclude-host', '192.168.1.50']);
		commandSeen(['--json', 'services', 'delete', 'roborock']);
	});

	await smoke('Routes', 'describes the rest of the internet from the effective firewall policy', async () => {
		const scenarios = [
			{
				name: 'disabled',
				firewall: { enabled: false, mode: 'disabled', split: { default_action: 'direct' } },
				value: /Direct/,
				note: /routing is off/i
			},
			{
				name: 'target-selective',
				firewall: { enabled: true, mode: 'targets', split: { default_action: 'direct' } },
				value: /By rule/,
				note: /only for selected destinations/i
			},
			{
				name: 'split-selective',
				firewall: { enabled: true, mode: 'split', split: { default_action: 'direct' } },
				value: /By rule/,
				note: /only for selected destinations/i
			},
			{
				name: 'host-selective',
				firewall: { enabled: true, mode: 'hosts', split: { default_action: 'proxy' } },
				value: /By device/,
				note: /only for selected devices/i
			},
			{
				name: 'default-proxy',
				firewall: { enabled: true, mode: 'split', split: { default_action: 'proxy' } },
				value: /Through VPN/,
				note: /Except the enabled exclusions/i
			}
		];

		for (const scenario of scenarios) {
			const page = makeRouting(true, true, scenario.firewall);
			const flow = page.renderContent()[2];
			const restOfInternet = treeText(flow.children[4]);
			assert.match(restOfInternet, scenario.value, scenario.name + ': wrong route value');
			assert.match(restOfInternet, scenario.note, scenario.name + ': wrong route explanation');
			if (scenario.name !== 'default-proxy')
				assert.doesNotMatch(restOfInternet, /Through VPN/, scenario.name + ': UI must not claim all remaining traffic uses VPN');
		}
	});

	await smoke('Routes', 'installs missing geodata before enabling local-country routing', async () => {
		const page = makeRouting(false, false);
		let statusCalls = 0;
		resolver = async (commandPath, args) => {
			if (commandPath.endsWith('fastlane-geodata') && args[0] === 'start')
				return { code: 0, stdout: JSON.stringify({ ready: false, updating: true, last_result: 'updating' }), stderr: '' };
			if (commandPath.endsWith('fastlane-geodata') && args[0] === 'status') {
				statusCalls++;
				return { code: 0, stdout: JSON.stringify(statusCalls === 1
					? { ready: false, updating: true, last_result: 'updating' }
					: { ready: true, updating: false, last_result: 'ok' }), stderr: '' };
			}
			return defaultResolver(commandPath, args);
		};
		await page.handleToggle({ preventDefault() {} });
		commandSeen(['start'], '/fastlane-geodata');
		assert.equal(statusCalls, 2);
		assert.equal(commandCount('update'), 0);
		commandSeen(['--json', 'settings', 'patch', '{"country_direct":true,"direct_country":"RU"}']);
		assert.equal(page.settings.country_routing.enabled, true);
		assert.equal(page.geodata.ready, true);
	});

	await smoke('Routes', 'enables with ready geodata and disables without redundant downloads', async () => {
		let page = makeRouting(true, false);
		await page.handleToggle();
		assert.equal(commandCount('update'), 0);
		commandSeen(['--json', 'settings', 'patch', '{"country_direct":true,"direct_country":"RU"}']);
		commands = [];
		page = makeRouting(true, true);
		await page.handleToggle();
		assert.equal(commandCount('update'), 0);
		commandSeen(['--json', 'settings', 'patch', '{"country_direct":false,"direct_country":"RU"}']);
		assert.equal(page.settings.country_routing.enabled, false);
	});

	await smoke('Routes', 'updates geodata manually and surfaces helper failures', async () => {
		let page = makeRouting(false, false);
		await page.handleGeoUpdate();
		commandSeen(['start'], '/fastlane-geodata');
		commandSeen(['status'], '/fastlane-geodata');
		assert.equal(page.geodata.ready, true);
		commands = []; toasts.length = 0;
		page = makeRouting(false, false);
		resolver = async (commandPath, args) => {
			if (commandPath.endsWith('fastlane-geodata') && args[0] === 'start')
				return { code: 0, stdout: JSON.stringify({ ready: false, updating: true, last_result: 'updating' }), stderr: '' };
			if (commandPath.endsWith('fastlane-geodata') && args[0] === 'status')
				return { code: 0, stdout: JSON.stringify({ ready: false, updating: false, last_result: 'error', message: 'download failed' }), stderr: '' };
			return defaultResolver(commandPath, args);
		};
		await page.handleGeoUpdate();
		assert.equal(page.busy, false);
		assert.ok(toasts.some((toast) => toast.type === 'error' && /download failed/.test(toast.details)));
	});

	await smoke('Routes', 'stops polling geodata at the UI timeout without enabling routing', async () => {
		const page = makeRouting(false, false);
		page.geodataPollAttempts = 2;
		resolver = async (commandPath, args) => commandPath.endsWith('fastlane-geodata')
			? { code: 0, stdout: JSON.stringify({ ready: false, updating: true, last_result: 'updating' }), stderr: '' }
			: defaultResolver(commandPath, args);
		await page.handleToggle();
		assert.equal(commandCount('status'), 2);
		assert.equal(commandCount('--json settings patch'), 0);
		assert.equal(page.busy, false);
		assert.ok(toasts.some((toast) => toast.type === 'error' && /running in the background/.test(toast.details)));
	});

	await smoke('Routes', 'previews a valid HAPP profile without applying partial rules', async () => {
		const page = makeRouting();
		const profile = { Name: 'Home', DirectSites: ['ru'], DirectIp: ['10.0.0.0/8'], ProxySites: ['example.com'], BlockSites: ['ads.example'] };
		page.importValue = 'happ://routing/onadd/' + Buffer.from(JSON.stringify(profile)).toString('base64url');
		page.handleImportCheck();
		const text = treeText(page.renderImportPreview());
		assert.match(text, /Home/);
		assert.match(text, /direct — 2/);
		assert.match(text, /through VPN — 1/);
		assert.match(text, /Partial application is disabled/);
		assert.equal(commands.length, 0);
	});

	await smoke('Routes', 'rejects malformed HAPP links without applying settings', async () => {
		const page = makeRouting();
		page.importValue = 'https://example.com/not-happ';
		page.handleImportCheck();
		assert.match(page.importError, /happ:\/\/routing\/onadd/);
		assert.equal(commands.length, 0);
		assert.ok(toasts.some((toast) => toast.type === 'error'));
	});

	await smoke('Diagnostics', 'loads the snapshot and subscriptions independently', async () => {
		const page = loadPage('diagnostics');
		const data = await page.load();
		assert.equal(data[0].value.runtime.running, true);
		assert.equal(data[1].value.length, 2);
		commandSeen(['--json', 'diagnostics']);
		commandSeen(['--json', 'list', 'subscriptions']);
	});

	await smoke('Diagnostics', 'renders healthy connected operational and technical states', async () => {
		const page = loadPage('diagnostics');
		const text = treeText(page.render([{ value: clone(diagnosticsFixture) }, { value: clone(subscriptionsFixture) }]));
		for (const expected of ['Diagnostics', 'VPN Connected', 'VPN service Running', 'Subscription Durev', 'DNS Running', 'Mode Automatic', 'Technical details']) assert.match(text, new RegExp(expected));
	});

	await smoke('Diagnostics', 'renders the active server name with address and id fallbacks', async () => {
		const page = loadPage('diagnostics');
		const snapshot = clone(diagnosticsFixture);
		let text = treeText(page.render([{ value: snapshot }, { value: clone(subscriptionsFixture) }]));
		assert.match(text, /Server: Netherlands · Amsterdam/);
		assert.doesNotMatch(text, /Server: nl(?:\s|$)/);

		snapshot.status.active_node.name = '';
		snapshot.status.active_node.remark = 'Amsterdam backup';
		text = treeText(page.render([{ value: snapshot }, { value: clone(subscriptionsFixture) }]));
		assert.match(text, /Server: Amsterdam backup/);

		snapshot.status.active_node.remark = '';
		text = treeText(page.render([{ value: snapshot }, { value: clone(subscriptionsFixture) }]));
		assert.match(text, /Server: nl\.example/);

		snapshot.status.active_node.address = '';
		text = treeText(page.render([{ value: snapshot }, { value: clone(subscriptionsFixture) }]));
		assert.match(text, /Server: nl(?:\s|$)/);
	});

	await smoke('Diagnostics', 'renders disconnected fallback and guards zero timestamps', async () => {
		const page = loadPage('diagnostics');
		const snapshot = clone(diagnosticsFixture);
		snapshot.status.state.connected = false;
		snapshot.status.state.active_subscription_id = '';
		snapshot.status.state.active_node_id = '';
		snapshot.runtime.running = false;
		snapshot.dns.active = false;
		snapshot.runtime.config_path = '0001-01-01T00:00:00Z';
		const text = treeText(page.render([{ value: snapshot }, { value: [] }]));
		assert.match(text, /VPN Disconnected/);
		assert.match(text, /Internet traffic is direct/);
		assert.doesNotMatch(text, /0001-01-01/);
	});

	await smoke('Diagnostics', 'surfaces a read failure once and keeps the page renderable', async () => {
		const page = loadPage('diagnostics');
		resolver = async (commandPath, args) => args.join(' ') === '--json diagnostics'
			? { code: 1, stdout: '', stderr: 'daemon unavailable' }
			: defaultResolver(commandPath, args);
		const data = await page.load();
		assert.ok(page.render(data));
		assert.equal(toasts.filter((toast) => toast.type === 'error').length, 1);
		page.render(data);
		assert.equal(toasts.filter((toast) => toast.type === 'error').length, 1);
	});

	await smoke('Diagnostics', 'refresh button reloads the current LuCI page', async () => {
		const page = loadPage('diagnostics');
		page.handleRefresh({ preventDefault() {} });
		assert.equal(reloads, 1);
	});

	await smoke('Settings', 'loads and renders all seven operational controls', async () => {
		const page = loadPage('settings');
		const data = await page.load();
		const text = treeText(page.render(data));
		for (const expected of ['Subscription update', 'Automatic server check', 'URL test address', 'URL test timeout', 'Pause between switches', 'Minimum improvement', 'Strict internet check', 'Interface language']) assert.match(text, new RegExp(expected));
		commandSeen(['--json', 'settings', 'get']);
	});

	await smoke('Settings', 'persists and applies the interface language before reload', async () => {
		const page = loadPage('settings');
		await page.handleLanguageChange({ target: { value: 'en' } });
		assert.equal(uciValues.lang, 'en');
		assert.equal(reloads, 1);
	});

	await smoke('Settings', 'loads update state without initiating a check or install', async () => {
		resolver = async (path, args) => args.join(' ') === '--json update status'
			? { code: 0, stdout: JSON.stringify({ status: 'installing', current_version: '1.2.2', message: 'Устанавливаю…' }) }
			: defaultResolver(path, args);
		const page = loadPage('settings');
		const data = await page.load();
		assert.match(treeText(page.render(data)), /Fast Lane update/);
		assert.match(treeText(page.renderUpdateContents()), /close the admin panel/);
		commandSeen(['--json', 'update', 'status']);
		assert.equal(commandCount('--json update check'), 0);
		assert.equal(commandCount('--json update install'), 0);
		assert.equal(page.updateBusy(), true);
	});

	await smoke('Settings', 'checks in background, pins confirmation and never applies after cancel', async () => {
		const page = makeSettings();
		await page.handleUpdateCheck();
		commandSeen(['--json', 'update', 'check']);
		page.updateState = { status: 'available', candidate: { id: 10, version: '1.2.3' } };
		window.confirm = () => false;
		await page.handleUpdateInstall();
		assert.equal(commandCount('--json update install'), 0);
		window.confirm = () => true;
		await page.handleUpdateInstall();
		commandSeen(['--json', 'update', 'install', '--release', '10']);
		assert.equal(commandCount('--json settings patch'), 0);
	});

	await smoke('Settings', 'blocks duplicate update commands and preserves drafts on status refresh', async () => {
		const page = makeSettings();
		page.draft.url_test_timeout = '9s';
		page.updateState = { status: 'installing' };
		await page.handleUpdateCheck();
		await page.handleUpdateInstall();
		await page.handleUninstall();
		await page.handleSaveSettings();
		assert.equal(commandCount('--json update check'), 0);
		assert.equal(commandCount('--json update install'), 0);
		assert.equal(commandCount('--json settings patch'), 0);
		assert.equal(commands.some(item => item.path.endsWith('fastlane-uninstall')), false);
		await page.refreshUpdate();
		assert.equal(page.draft.url_test_timeout, '9s');
	});

	await smoke('Settings', 'keeps settings usable if update status is temporarily unavailable', async () => {
		resolver = async (path, args) => args.join(' ') === '--json update status'
			? { code: 1, stderr: 'RPC disconnected' } : defaultResolver(path, args);
		const page = loadPage('settings');
		await page.load();
		assert.ok(page.draft.url_test_url);
		assert.match(treeText(page.renderUpdateContents()), /Could not read update status/);
	});

	await smoke('Settings', 'updates text, boolean and digit-only duration drafts', async () => {
		const page = makeSettings();
		page.handleInput('url_test_url', { target: { value: 'https://new.example/204' } });
		page.handleBool('strict_egress_check', { target: { checked: false } });
		const input = { value: '9x' };
		page.handleDurationInput('refresh_interval', ['h', 'm', 's'], 'm', { target: input });
		assert.equal(page.draft.url_test_url, 'https://new.example/204');
		assert.equal(page.draft.strict_egress_check, false);
		assert.equal(input.value, '9');
		assert.equal(page.draft.refresh_interval, '1h9m0s');
	});

	await smoke('Settings', 'saves every changed setting in one atomic patch', async () => {
		const page = makeSettings();
		page.draft = {
			refresh_interval: '2h3m4s', health_check_interval: '45s', url_test_url: 'https://new.example/204',
			url_test_timeout: '12s', switch_cooldown: '2m5s', latency_threshold: '70ms', strict_egress_check: false
		};
		await page.handleSaveSettings({ preventDefault() {} });
		const patchCommand = commands.find((item) => item.args[0] === '--json' && item.args[1] === 'settings' && item.args[2] === 'patch');
		assert.ok(patchCommand, 'atomic settings patch command not seen');
		assert.deepEqual(JSON.parse(patchCommand.args[3]), {
			'refresh-interval': '2h3m4s',
			'health-check-interval': '45s',
			'url-test-url': 'https://new.example/204',
			'url-test-timeout': '12s',
			'switch-cooldown': '2m5s',
			'latency-threshold': '70ms',
			'strict-egress-check': 'false'
		});
		assert.equal(commandCount('--json settings patch'), 1);
		assert.ok(toasts.some((toast) => toast.type === 'success'));
	});

	await smoke('Settings', 'locks controls during save and syncs normalized backend values', async () => {
		const page = makeSettings();
		const toggleLabel = { textContent: '' };
		const textControl = {
			type: 'text', value: '', disabled: false,
			getAttribute: (name) => name === 'data-setting-key' ? 'url_test_url' : null
		};
		const durationControl = {
			type: 'text', value: '', disabled: false,
			getAttribute: (name) => ({
				'data-setting-key': 'refresh_interval',
				'data-duration-unit': 'm',
				'data-duration-units': 'h,m,s'
			})[name] || null
		};
		const boolControl = {
			type: 'checkbox', checked: true, disabled: false,
			getAttribute: (name) => name === 'data-setting-key' ? 'strict_egress_check' : null,
			parentNode: { querySelector: () => toggleLabel }
		};
		const saveButton = { disabled: false };
		const settingControls = [textControl, durationControl, boolControl];
		const allControls = [...settingControls, saveButton];
		page.settingsRoot = {
			querySelectorAll: (selector) => selector === '[data-setting-key]' ? settingControls : allControls
		};
		page.draft.refresh_interval = '2h9m4s';
		page.draft.url_test_url = 'https://new.example/204';
		page.draft.strict_egress_check = false;

		let finishRequest;
		resolver = async (_path, args) => args[0] === '--json' && args[1] === 'settings' && args[2] === 'patch'
			? new Promise((resolve) => { finishRequest = resolve; })
			: defaultResolver(_path, args);
		const saving = page.handleSaveSettings();
		await Promise.resolve();
		assert.ok(allControls.every((control) => control.disabled), 'controls stayed enabled while save was pending');

		finishRequest({ code: 0, stdout: JSON.stringify(settingsFixture), stderr: '' });
		await saving;
		assert.ok(allControls.every((control) => !control.disabled), 'controls stayed disabled after save');
		assert.equal(textControl.value, settingsFixture.url_test_url);
		assert.equal(durationControl.value, '0');
		assert.equal(boolControl.checked, true);
		assert.equal(toggleLabel.textContent, 'On');
	});

	await smoke('Settings', 'does not write unchanged settings', async () => {
		const page = makeSettings();
		await page.handleSaveSettings();
		assert.equal(commandCount('--json settings patch'), 0);
		assert.ok(toasts.some((toast) => toast.type === 'info'));
	});

	await smoke('Settings', 'surfaces an atomic backend validation error', async () => {
		const page = makeSettings();
		page.draft.refresh_interval = 'bad';
		page.draft.health_check_interval = '45s';
		resolver = async (_path, args) => args[0] === '--json' && args[1] === 'settings' && args[2] === 'patch'
			? { code: 1, stdout: '', stderr: 'invalid duration' }
			: defaultResolver(_path, args);
		await page.handleSaveSettings();
		assert.equal(commandCount('--json settings patch'), 1);
		assert.ok(toasts.some((toast) => toast.type === 'error' && /invalid duration/.test(toast.details)));
	});

	const failed = results.filter((result) => result.status === 'FAIL');
	for (const result of results) {
		process.stdout.write(`${result.status}\t${result.section}\t${result.name}` + (result.error ? `\n${result.error}` : '') + '\n');
	}
	process.stdout.write(`SUMMARY\t${results.length - failed.length}/${results.length} PASS\n`);
	process.exitCode = failed.length ? 1 : 0;
})();
