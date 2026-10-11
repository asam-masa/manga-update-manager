import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const root = new URL('../../', import.meta.url);
const config = JSON.parse(readFileSync(new URL('.github/renovate.json', root), 'utf8'));
const workflow = readFileSync(new URL('.github/workflows/ci.yml', root), 'utf8');
const goMod = readFileSync(new URL('go.mod', root), 'utf8');

test('regular updates have a weekly Tokyo window, a small budget and no automerge', () => {
  assert.equal(config.timezone, 'Asia/Tokyo');
  assert.deepEqual(config.schedule, ['* 0-5 * * 1']);
  assert.equal(config.prConcurrentLimit, 3);
  assert.equal(config.prHourlyLimit, 0);
  assert.equal(config.automerge, false);
  assert.equal(config.platformAutomerge, false);
  assert.equal(config.separateMajorMinor, true);
  assert.equal(config.vulnerabilityAlerts.enabled, true);
  assert.equal(config.vulnerabilityAlerts.automerge, false);
  assert.equal(config.vulnerabilityAlerts.schedule, undefined);
  assert.equal(config.vulnerabilityAlerts.prConcurrentLimit, undefined);
});

test('only the approved managers are enabled and Go directives are not updated', () => {
  assert.deepEqual(config.enabledManagers, ['gomod', 'npm', 'github-actions', 'custom.regex']);
  const manualGo = config.packageRules.find((rule) => rule.matchDepTypes?.includes('golang'));
  assert.deepEqual(manualGo.matchManagers, ['gomod']);
  assert.deepEqual(manualGo.matchDepTypes, ['golang', 'toolchain']);
  assert.equal(manualGo.enabled, false);
  const manualRuntime = config.packageRules.find((rule) => rule.matchManagers?.includes('github-actions'));
  assert.deepEqual(manualRuntime.matchDepTypes, ['github-runner', 'uses-with', 'docker', 'container', 'service']);
  assert.equal(manualRuntime.enabled, false);
  assert.ok(config.extends.includes('helpers:pinGitHubActionDigests'));
});

test('the custom manager extracts only the Wails v2 CLI from the CI file', () => {
  assert.equal(config.customManagers.length, 1);
  const manager = config.customManagers[0];
  const filePattern = manager.managerFilePatterns[0];
  const fileRegex = new RegExp(filePattern.slice(1, -1));
  assert.ok(fileRegex.test('.github/workflows/ci.yml'));
  assert.ok(!fileRegex.test('.github/workflows/other.yml'));
  assert.ok(!fileRegex.test('docs/ci.yml'));
  assert.equal(manager.datasourceTemplate, 'go');
  assert.equal(manager.versioningTemplate, 'go');

  const regex = new RegExp(manager.matchStrings[0], 'g');
  const matches = [...workflow.matchAll(regex)];
  assert.equal(matches.length, 1);
  const { depName, currentValue } = matches[0].groups;
  assert.equal(depName, 'github.com/wailsapp/wails/v2');
  const moduleMatch = goMod.match(/^\s*github\.com\/wailsapp\/wails\/v2\s+(\S+)/m);
  assert.ok(moduleMatch);
  assert.equal(currentValue, moduleMatch[1]);

  for (const command of [
    'go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0',
    'go install github.com/wailsapp/wails/v2/cmd/wails@v2.100.12',
  ]) {
    assert.ok(new RegExp(manager.matchStrings[0]).test(command));
  }
  for (const command of [
    'go install example.com/tool@v2.14.0',
    'go install github.com/wailsapp/wails/v3/cmd/wails@v3.0.0',
    'go install github.com/wailsapp/wails/v2/cmd/wails@latest',
  ]) {
    assert.ok(!new RegExp(manager.matchStrings[0]).test(command));
  }

  const group = config.packageRules.find((rule) => rule.matchDepNames?.includes(depName));
  assert.equal(group.groupName, 'Wails module and CLI');
});
