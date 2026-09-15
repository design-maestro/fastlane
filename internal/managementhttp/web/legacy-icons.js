// Generated from the LuCI VPN icon renderer; do not edit.
window.fastlaneIcon = function icon(name) {
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
};
