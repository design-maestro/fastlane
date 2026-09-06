'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const repoRoot = process.argv[2];
const casesJSON = process.argv[3];
if (!repoRoot || !casesJSON)
	throw new Error('repository root and duration cases are required');

const settingsPath = path.join(
	repoRoot,
	'luci-app-fastlane',
	'htdocs',
	'luci-static',
	'resources',
	'view',
	'fastlane',
	'settings-20260905-updates-v6.js'
);
const source = fs.readFileSync(settingsPath, 'utf8');
const helpersStart = source.indexOf('function trim(value)');
const helpersEnd = source.indexOf('function durationUnitName(unit)');
assert.notEqual(helpersStart, -1, 'settings view must define trim() before duration helpers');
assert.notEqual(helpersEnd, -1, 'settings view must define durationUnitName() after duration helpers');

const helpersSource = source.slice(helpersStart, helpersEnd);
const helpers = new Function(
	`${helpersSource}\nreturn { durationMilliseconds, durationParts, durationValue };`
)();

for (const testCase of JSON.parse(casesJSON)) {
	const parts = helpers.durationParts(testCase.goValue, testCase.units);
	assert.deepEqual(parts, testCase.wantParts, `${testCase.name}: Go duration must render in the expected UI units`);

	const savedValue = helpers.durationValue(parts, testCase.units);
	const parsedSavedValue = helpers.durationMilliseconds(savedValue);
	assert.equal(parsedSavedValue.matched, true, `${testCase.name}: UI value must remain a duration`);
	assert.equal(parsedSavedValue.total, testCase.wantMilliseconds, `${testCase.name}: UI round-trip must preserve milliseconds`);
}

process.stdout.write(`PASS\tSettings\tGo duration round-trip across h/m/s/ms (${JSON.parse(casesJSON).length} cases)\n`);
