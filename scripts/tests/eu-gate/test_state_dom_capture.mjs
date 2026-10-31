// Exercise the exact Playwright evaluate callback without a browser/dependencies.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { captureDom } from '../../../frontend/e2e/eu-state-dom.mjs';

const fixture = JSON.parse(await readFile(new URL('./fixtures/eu-state-dom.json', import.meta.url)));
const leaf = value => ({ nodeType: 3, nodeValue: value });
function element(parts = [], dataset = {}) {
  return {
    nodeType: 1, dataset,
    childNodes: parts.map(value => typeof value === 'string' ? leaf(value) : value),
    // Like browser textContent, deliberately no separator between descendants.
    get textContent() {
      const raw = node => node.nodeType === 3 ? node.nodeValue : node.childNodes.map(raw).join('');
      return raw(this);
    },
    getClientRects: () => [{}],
    getAttribute: name => dataset[name.replace(/^data-/, '').replace(/-([a-z])/g, (_, c) => c.toUpperCase())] ?? null,
  };
}
const records = values => values.map(value => element(
  (value.textNodes ?? [value.text]).map(part => element([part])), value));
const devices = records(fixture.devices);
const sections = records(fixture.sections);
const measurements = records(fixture.measurements);
const conditions = fixture.conditions.map(value => element([value.text], { condition: value.id }));
const authorities = [element([element(['RMU1']), element(['LOCAL'])], { authority: 'rmu-1-remote' })];
const root = element([
  // Whitespace-only nodes must not introduce doubled separators.
  ...fixture.textNodes.map(value => element([value])), element([' \n ']),
  ...devices, ...sections, ...measurements, ...conditions,
], { cached: 'false' });
const f2 = devices.find(node => node.dataset.device === 'F2');
f2.closest = () => ({ querySelectorAll: () => [element(['F2 · IEC 104 · indication only: ', element(['NORMAL']), ' · I 11.2 A · P 0.33 MW'])] });
const collections = {
  '[data-device]': devices, '[data-section]': sections, '[data-point]': measurements,
  '[aria-label="Abnormal conditions"] [data-condition]': conditions,
  '[data-authority]': authorities, '[data-comparison]': [],
};
root.querySelectorAll = selector => collections[selector];
root.querySelector = selector => ({
  '[data-device="F2"]': f2,
  '[data-f2-line]': element([], { state: fixture.f2Line }),
  '[data-observation-uncertainty]': element([], { observationUncertainty: fixture.observationUncertainty }),
})[selector];
globalThis.document = {
  querySelector: () => element([], { viewState: fixture.viewState }),
};
globalThis.location = { href: 'offline:fixture' };
globalThis.getComputedStyle = () => ({ visibility: 'visible' });

const t1 = devices.find(node => node.dataset.device === 'T1');
assert.equal(t1.textContent, fixture.devices.find(d => d.device === 'T1').text);
assert.match(t1.textContent, /GOODMANUAL/);
const captured = captureDom(root, 'cc');
assert.equal(captured.devices.find(d => d.device === 'T1').text,
  'T1 110/20 kV · OLTC Tap 0 · GOOD MANUAL · GOOD');
assert.match(captured.text, /^Protection TRIPPED lockout CB1/); // HTML + SVG.
assert.equal(captured.authorities[0].text, 'RMU1 LOCAL');
assert.equal(captured.sections[0].text, 'S1 · LIVE 19.80 kV · GOOD via CB1');
assert.equal(captured.f2Telemetry, 'F2 · IEC 104 · indication only: NORMAL · I 11.2 A · P 0.33 MW');
assert.equal(captured.measurements[0].text, fixture.measurements[0].text);
assert.equal(captured.conditions[0].text, fixture.conditions[0].text);
assert.equal(captured.text.includes('  '), false);
// The other mode has exactly the same adjacent-SVG-node failure.
t1.childNodes[2] = element(['AUTO · GOOD']);
assert.match(t1.textContent, /GOODAUTO/);
assert.match(captureDom(root, 'physical').devices.find(d => d.device === 'T1').text, /\bAUTO\b/);
console.log(JSON.stringify(captured));
