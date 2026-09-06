'use strict';
'require view';
'require fs';
'require ui';
'require fastlane.ui as fastlaneUI';

var fastlaneBinary = '/usr/bin/fastlane';
var fastlaneSelfUpdateHelper = '/usr/libexec/fastlane-self-update';
var whatsNewEntries = [
	{
		kind: _('New'),
		title: _('Xray Core Upgrade'),
		summary: _('Upgraded Xray core to v26.7.28 to support latest Reality parameters and prevent connection drops')
	},
	{
		kind: _('New'),
		title: _('Only Selected Devices Mode'),
		summary: _('Added Only Selected Devices Mode')
	},
	{
		kind: _('New'),
		title: _('Server List'),
		summary: _('Optimized subscriptions and single servers by introducing the Server List')
	},
	{
		kind: _('New'),
		title: _('Socks5 Proxy'),
		summary: _('Added Socks5 proxy support')
	},
	{
		kind: _('Fix'),
		title: _('Duplicate Auto Nodes Fix'),
		summary: _('Merged duplicate auto-connection servers into a single node that dynamically connects to the best one')
	}
];

function trim(value) {
	if (value == null)
		return '';

	return String(value).trim();
}

function notificationParagraph(message) {
	return E('p', {}, [ message ]);
}

function extractSelfUpdateStatus(output) {
	var match = String(output || '').match(/FASTLANE_SELF_UPDATE_STATUS=([^\n]+)/);
	return match ? trim(match[1]) : '';
}

function stripSelfUpdateStatus(output) {
	return trim(String(output || '').replace(/FASTLANE_SELF_UPDATE_STATUS=[^\n]*\n?/, ''));
}

function padNumber(value) {
	return String(value).padStart(2, '0');
}

function formatBuildDate(value) {
	var raw = trim(value);
	var parsed;

	if (raw === '')
		return 'unknown';

	parsed = new Date(raw);
	if (isNaN(parsed.getTime()))
		return raw;

	return String(parsed.getFullYear()) + '-' +
		padNumber(parsed.getMonth() + 1) + '-' +
		padNumber(parsed.getDate()) + ' ' +
		padNumber(parsed.getHours()) + ':' +
		padNumber(parsed.getMinutes()) + ':' +
		padNumber(parsed.getSeconds());
}

function renderWhatsNewCard(entry) {
	var className = 'fastlane-card fastlane-card-primary fastlane-about-update-card';
	if (entry.kind === _('New'))
		className += ' fastlane-about-update-card-new';
	else if (entry.kind === _('Fix'))
		className += ' fastlane-about-update-card-fix';

	return E('div', { 'class': className }, [
		E('div', { 'class': 'fastlane-card-accent' }, []),
		E('div', { 'class': 'fastlane-card-label' }, [ entry.kind ]),
		E('div', { 'class': 'fastlane-card-value fastlane-about-update-title' }, [ entry.title ]),
		E('p', { 'class': 'fastlane-about-update-summary' }, [ entry.summary ])
	]);
}

return view.extend({
	load: function() {
		return Promise.all([
			this.execJSON([ '--json', 'version' ]).catch(function(err) {
				return { __error__: err.message || String(err) };
			})
		]);
	},

	execJSON: function(argv) {
		return fs.exec(fastlaneBinary, argv).then(function(res) {
			var stderr = trim(res.stderr);
			var stdout = trim(res.stdout);

			if (res.code !== 0)
				throw new Error(stderr || stdout || _('Fast Lane command failed.'));

			if (stdout === '')
				throw new Error(_('Fast Lane returned empty JSON output.'));

			try {
				return JSON.parse(stdout);
			}
			catch (err) {
				throw new Error(_('Fast Lane returned invalid JSON output.'));
			}
		});
	},

	execHelper: function(command, argv) {
		return fs.exec(command, argv || []).then(function(res) {
			var stderr = trim(res.stderr);
			var stdout = trim(res.stdout);

			if (res.code !== 0)
				throw new Error(stderr || stdout || _('Fast Lane command failed.'));

			return {
				stdout: stdout,
				stderr: stderr
			};
		});
	},

	handleUpgrade: function(ev) {
		if (ev)
			ev.preventDefault();

		if (!window.confirm(_('Download the latest Fast Lane release and install it over the current router version? Existing /etc/fastlane state is preserved by the installer.')))
			return Promise.resolve();

		return this.execHelper(fastlaneSelfUpdateHelper).then(function(res) {
			var status = extractSelfUpdateStatus(res.stdout);
			var message = stripSelfUpdateStatus(res.stdout);

			ui.addNotification(null, notificationParagraph(message || _('Upgrade completed. Reloading the page...')), 'info');
			if (status !== 'up-to-date') {
				window.setTimeout(function() {
					window.location.reload();
				}, 1500);
			}
		}).catch(function(err) {
			ui.addNotification(null, notificationParagraph(err.message || String(err)));
			throw err;
		});
	},

	showWhatsNewModal: function() {
		var body = [
			E('p', { 'class': 'fastlane-modal-help' }, [
				_('Recent user-facing changes in the simplified LuCI experience.')
			]),
			E('div', { 'class': 'fastlane-overview-grid fastlane-about-update-grid' }, whatsNewEntries.map(renderWhatsNewCard))
		];
		var actions = [
			E('button', {
				'class': 'cbi-button',
				'type': 'button',
				'click': function(ev) {
					ui.hideModal();
					return false;
				}
			}, [ _('Close') ])
		];

		fastlaneUI.showModal(_('What\'s New'), body, {
			'bodyClass': 'fastlane-modal-whats-new',
			'modalClass': 'fastlane-modal-whats-new',
			'actions': actions
		});
	},

	handleShowWhatsNew: function(ev) {
		if (ev)
			ev.preventDefault();

		this.showWhatsNewModal();
		return false;
	},

	handleRestart: function(ev) {
		if (ev)
			ev.preventDefault();

		if (!window.confirm(_('Restart the Fast Lane service and clear all LuCI caches? This can help resolve temporary connection or display issues.')))
			return Promise.resolve();

		ui.showIndicator();

		return fs.exec(fastlaneBinary, [ 'restart' ]).then(L.bind(function(res) {
			ui.hideIndicator();
			ui.addNotification(null, notificationParagraph(_('Fast Lane service restarted and LuCI cache cleared successfully. Reloading...')), 'info');
			window.setTimeout(function() {
				window.location.reload();
			}, 2000);
		}, this)).catch(L.bind(function(err) {
			ui.hideIndicator();
			ui.addNotification(null, notificationParagraph(err.message || String(err)));
			throw err;
		}, this));
	},

	render: function(data) {
		var info = data[0] || {};
		var content = [];
		var version = trim(info.version) || 'dev';
		var commit = trim(info.commit) || 'unknown';
		var formattedBuildDate = formatBuildDate(info.build_date);
		var versionText = 'Fast Lane ' + version + '\nCommit: ' + commit + '\nBuilt: ' + formattedBuildDate;

		if (info.__error__)
			ui.addNotification(null, notificationParagraph(_('Version error: %s').format(info.__error__)));

		content.push(fastlaneUI.renderSharedStyles());
		content.push(E('style', { 'type': 'text/css' }, [
			'.fastlane-about-pre { white-space:pre-wrap; margin:0; }',
			'.fastlane-about-update-grid { display:grid !important; grid-template-columns:repeat(2, 1fr) !important; align-items:stretch; }',
			'@media (max-width: 560px) { .fastlane-about-update-grid { grid-template-columns:1fr !important; } }',
			'.fastlane-about-update-card { min-height:168px; }',
			'.fastlane-about-update-card-new .fastlane-card-accent { background:linear-gradient(90deg, #22c55e 0%, #16a34a 100%); }',
			'.fastlane-about-update-card-fix .fastlane-card-accent { background:linear-gradient(90deg, #f59e0b 0%, #d97706 100%); }',
			'.fastlane-theme-light .fastlane-about-update-card-new { border-color:rgba(34, 197, 94, 0.2); background:linear-gradient(180deg, rgba(250, 252, 250, 0.99) 0%, rgba(240, 253, 244, 0.99) 100%); }',
			'.fastlane-theme-light .fastlane-about-update-card-new .fastlane-card-label { color:#15803d; }',
			'.fastlane-theme-light .fastlane-about-update-card-new .fastlane-about-update-title { color:#14532d; }',
			'.fastlane-theme-light .fastlane-about-update-card-fix { border-color:rgba(245, 158, 11, 0.2); background:linear-gradient(180deg, rgba(253, 250, 245, 0.99) 0%, rgba(254, 243, 199, 0.38) 100%); }',
			'.fastlane-theme-light .fastlane-about-update-card-fix .fastlane-card-label { color:#b45309; }',
			'.fastlane-theme-light .fastlane-about-update-card-fix .fastlane-about-update-title { color:#78350f; }',
			'.fastlane-about-update-title { margin-bottom:10px; }',
			'.fastlane-about-update-summary { margin:0; color:var(--fastlane-text-secondary); line-height:1.6; }',
			'.fastlane-modal-help { margin:0 0 12px; color:var(--fastlane-text-secondary); max-width:100%; overflow-wrap:anywhere; word-break:break-word; line-height:1.45; }',
			'.fastlane-modal-whats-new.modal { border-radius:24px !important; padding:24px 28px !important; max-width:800px !important; width:92% !important; border:1px solid var(--fastlane-border) !important; box-shadow:var(--fastlane-shadow) !important; transition:background-color .22s ease, border-color .22s ease, color .22s ease; }',
			'.fastlane-theme-dark.fastlane-modal-whats-new.modal { background:linear-gradient(180deg, rgba(20, 31, 49, 0.98) 0%, rgba(13, 21, 35, 1) 100%) !important; border-color:var(--fastlane-border-strong) !important; color:var(--fastlane-text-primary) !important; }',
			'.fastlane-theme-light.fastlane-modal-whats-new.modal { background:linear-gradient(180deg, rgba(252, 253, 254, 0.99) 0%, rgba(246, 249, 252, 1) 100%) !important; border-color:rgba(37, 99, 235, 0.2) !important; color:var(--fastlane-text-primary) !important; }',
			'.fastlane-modal-whats-new h4 { margin:0 0 16px !important; font-size:clamp(20px, 1.2vw + 15px, 26px) !important; font-weight:800 !important; letter-spacing:-0.03em !important; line-height:1.2 !important; color:var(--fastlane-text-primary) !important; }',
			'.fastlane-modal-whats-new .fastlane-modal-actions { display:flex !important; justify-content:flex-end !important; gap:10px !important; margin-top:20px !important; padding-top:16px !important; border-top:1px solid var(--fastlane-border) !important; }',
			'.fastlane-modal-whats-new .fastlane-modal-actions .cbi-button { min-height:42px !important; padding:0 22px !important; border-radius:14px !important; font-size:14px !important; font-weight:700 !important; cursor:pointer !important; display:inline-flex !important; align-items:center !important; justify-content:center !important; border:1px solid rgba(145, 175, 220, 0.18) !important; background:rgba(15, 24, 38, 0.82) !important; color:var(--fastlane-text-primary) !important; box-shadow:0 12px 24px rgba(0, 0, 0, 0.18), inset 0 1px 0 rgba(255, 255, 255, 0.03) !important; transition:transform .16s ease, border-color .16s ease, box-shadow .16s ease, background .16s ease !important; }',
			'.fastlane-modal-whats-new .fastlane-modal-actions .cbi-button:hover { transform:translateY(-1px) !important; border-color:rgba(145, 190, 246, 0.28) !important; box-shadow:0 16px 26px rgba(0, 0, 0, 0.22), inset 0 1px 0 rgba(255, 255, 255, 0.04) !important; }',
			'.fastlane-theme-light.fastlane-modal-whats-new .fastlane-modal-actions .cbi-button { border:1px solid rgba(125, 146, 170, 0.16) !important; background:linear-gradient(180deg, rgba(250, 252, 254, 0.98) 0%, rgba(241, 246, 251, 0.98) 100%) !important; color:var(--fastlane-text-primary) !important; box-shadow:0 10px 20px rgba(63, 87, 118, 0.08), inset 0 1px 0 rgba(255, 255, 255, 0.88) !important; }',
			'.fastlane-theme-light.fastlane-modal-whats-new .fastlane-modal-actions .cbi-button:hover { border-color:rgba(37, 99, 235, 0.22) !important; box-shadow:0 12px 22px rgba(63, 87, 118, 0.1) !important; inset 0 1px 0 rgba(255, 255, 255, 0.9) !important; }'
		]));

		content.push(E('h2', {}, [ _('Fast Lane - About') ]));
		content.push(E('p', { 'class': 'cbi-section-descr' }, [
			_('Fast Lane build information, update actions, and recent user-facing changes.')
		]));

		content.push(E('div', { 'class': 'fastlane-overview-grid' }, [
			fastlaneUI.renderSummaryCard(_('Version'), version),
			fastlaneUI.renderSummaryCard(_('Commit'), commit),
			fastlaneUI.renderSummaryCard(_('Build Date'), formattedBuildDate)
		]));

		content.push(E('div', { 'class': 'cbi-section' }, [
			E('h3', {}, [ _('fastlane version') ]),
			E('pre', { 'class': 'fastlane-about-pre' }, [ versionText ])
		]));

		content.push(E('div', { 'class': 'cbi-section' }, [
			E('h3', {}, [ _('Update') ]),
			E('p', { 'class': 'cbi-section-descr' }, [
				_('Download and install the latest published Fast Lane release on this router. The installer preserves existing /etc/fastlane state files.')
			]),
			E('div', { 'class': 'cbi-page-actions' }, [
				E('button', {
					'class': 'cbi-button cbi-button-action',
					'type': 'button',
					'click': ui.createHandlerFn(this, 'handleShowWhatsNew')
				}, [ _('What\'s New') ]),
				E('button', {
					'class': 'cbi-button cbi-button-apply',
					'type': 'button',
					'click': ui.createHandlerFn(this, 'handleUpgrade')
				}, [ _('Update to new version') ])
			])
		]));

		content.push(E('div', { 'class': 'cbi-section' }, [
			E('h3', {}, [ _('Maintenance') ]),
			E('p', { 'class': 'cbi-section-descr' }, [
				_('Restart the Fast Lane service and clear LuCI caches. This is useful for troubleshooting or resolving display glitches.')
			]),
			E('div', { 'class': 'cbi-page-actions' }, [
				E('button', {
					'class': 'cbi-button cbi-button-action',
					'type': 'button',
					'click': ui.createHandlerFn(this, 'handleRestart')
				}, [ _('Restart Fast Lane') ])
			]),
			E('p', { 'class': 'cbi-section-descr', 'style': 'margin-top:20px;' }, [
				_('About intentionally keeps destructive maintenance actions out of LuCI. For full removal over SSH, use the documented uninstall.sh command from README.')
			])
		]));

		return E('div', {
			'class': fastlaneUI.withThemeClass('fastlane-page-shell fastlane-page-shell-about')
		}, content);
	},

	handleSave: null,
	handleSaveApply: null,
	handleReset: null
});
