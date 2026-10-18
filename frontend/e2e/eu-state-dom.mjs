#!/usr/bin/env node
// Own headless Chrome/profile only; no browser installation or shared CDP session.
import { chromium } from 'playwright-core';
import { writeFile, readFile } from 'node:fs/promises';

const [api, directory, rowId] = process.argv.slice(2);
const browser = await chromium.launch({ channel: 'chrome', headless: true, timeout: 10000 });
try {
  if (api === '--preflight') process.exitCode = 0;
  else {
    if (!api || !directory || !rowId) throw new Error('usage: eu-state-dom.mjs API EVIDENCE ROW | --preflight');
    const oracle = JSON.parse(await readFile(new URL('../../scripts/lib/eu-state-oracle.json', import.meta.url)));
    const row = oracle.rows.find(row => row.id === rowId);
    const physicalPaths = { D: 'no supply', B: 'via CB1', F: 'via F2', BF: 'via CB1 + F2' };
    for (const view of ['cc', 'physical']) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
      const page = await context.newPage();
      page.setDefaultTimeout(15000);
      const rootSelector = view === 'cc'
        ? '[aria-label="CC observation snapshot"][data-cached="false"]'
        : '[aria-label="Physical System state"]';
      try {
        await page.goto(`${api.replace(/\/$/, '')}/substation${view === 'physical' ? '?view=physical-system' : ''}`, { waitUntil: 'domcontentloaded', timeout: 15000 });
        await page.locator(rootSelector).waitFor({ state: 'visible' });
        if (view === 'physical') await page.locator('[aria-controls="physical-all-points"]').click();
        const sections = row.supply.map((supply, i) => ({
          id: `S${i + 1}`, state: supply === 'D' ? 'DEAD' : 'LIVE',
          path: view === 'cc' ? row.cc_paths[i] : physicalPaths[supply],
        }));
        await page.waitForFunction(({ selector, sections }) => {
          const root = document.querySelector(selector);
          return sections.every(s => {
            const node = root?.querySelector(`[data-section="${s.id}"]`);
            return node?.getAttribute('data-state') === s.state && node?.getAttribute('data-path') === s.path;
          });
        }, { selector: rootSelector, sections }, { timeout: 15000 });
        // Both pages poll every 2s; leave one poll for their independent comparison.
        await page.waitForTimeout(2200);
        const dom = await page.locator(rootSelector).evaluate((root, view) => {
          const visible = node => !!node.getClientRects().length && getComputedStyle(node).visibility !== 'hidden';
          const all = (selector, read) => [...root.querySelectorAll(selector)].map(read);
          const f2Figure = root.querySelector('[data-device="F2"]')?.closest('figure');
          return {
            view, visible: visible(root), cached: root.getAttribute('data-cached') === 'true',
            // The shared SCADA banner is a sibling of either view's snapshot.
            viewState: document.querySelector('[data-view-state]')?.getAttribute('data-view-state'),
            observationUncertainty: root.querySelector('[data-observation-uncertainty]')?.getAttribute('data-observation-uncertainty'),
            capturedAt: new Date().toISOString(), url: location.href,
            text: root.textContent,
            sections: all('[data-section]', node => ({ section: node.dataset.section, state: node.dataset.state, path: node.dataset.path, kv: node.dataset.kv, text: node.textContent })),
            devices: all('[data-device]', node => ({ device: node.dataset.device, position: node.dataset.position, quality: node.dataset.quality, tap: node.dataset.tap, text: node.textContent })),
            f2Line: root.querySelector('[data-f2-line]')?.getAttribute('data-state'),
            f2Telemetry: [...(f2Figure?.querySelectorAll('p') ?? [])].find(node => node.textContent.startsWith('F2 ·'))?.textContent,
            measurements: all('[data-point]', node => ({ point: node.dataset.point, state: node.dataset.state, text: node.textContent, visible: visible(node) })),
            conditions: all('[aria-label="Abnormal conditions"] [data-condition]', node => ({ id: node.dataset.condition, text: node.textContent })),
            authorities: all('[data-authority]', node => ({ point: node.dataset.authority, text: node.textContent })),
            comparisons: all('[data-comparison]', node => ({ point: node.dataset.comparison, status: node.dataset.status })),
          };
        }, view);
        await writeFile(`${directory}/${view}-dom.json`, JSON.stringify(dom, null, 2) + '\n');
      } finally {
        await page.screenshot({ path: `${directory}/${view}.png`, fullPage: true, timeout: 5000 });
        await context.close();
      }
    }
  }
} finally {
  await browser.close();
}
