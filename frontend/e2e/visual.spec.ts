import {release} from 'node:os';
import {writeFile} from 'node:fs/promises';
import {test, expect} from './fixture';

test.use({scenario: 'visual'});

for (const width of [1200, 600]) {
    for (const [mode, label] of [['compact', '小型表示'], ['rows', '行一覧'], ['cards', 'メモなしカード']] as const) {
        test(`${mode} ${width}pxの配置と画像`, async ({page, browser}, testInfo) => {
            await page.setViewportSize({width, height: 900});
            await page.goto('/');
            await expect(page.getByTitle('6件 / 全6件')).toBeVisible();
            await page.getByRole('button', {name: label, exact: true}).click();
            await expect(page.getByRole('button', {name: label, exact: true})).toHaveAttribute('aria-pressed', 'true');
            await expect(page.getByText('表示形式を保存しています…')).toHaveCount(0);
            await page.evaluate(async () => { await document.fonts.ready; });
            const items = page.locator(mode === 'rows' ? '.work-row' : '.work-card');
            await expect(items).toHaveCount(6);
            const image = page.locator('.work-cover img');
            if (mode === 'rows') {
                await expect(image).toHaveCount(0);
            } else {
                await expect(page.getByText('画像を表示できません', {exact: true})).toHaveCount(1);
                await expect(image).toHaveCount(1);
                await expect(image).toHaveJSProperty('naturalWidth', 160);
                await expect(image).not.toHaveClass('cover-loading');
            }
            await expect(page.getByRole('alert')).toHaveCount(0);
            await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
            const boxes = await items.evaluateAll((nodes) => nodes.map((node) => {
                const {x, y, width, height} = node.getBoundingClientRect();
                return {x, y, width, height};
            }));
            const columns = mode === 'rows' ? 1 : mode === 'compact' ? width === 1200 ? 5 : 2 : width === 1200 ? 2 : 1;
            expect(boxes.filter((box) => Math.abs(box.y - boxes[0].y) < 1)).toHaveLength(columns);
            expect(Math.max(...boxes.map((box) => box.width)) - Math.min(...boxes.map((box) => box.width))).toBeLessThan(1);
            if (mode === 'rows') {
                const overflow = await page.locator('.work-rows-scroll').evaluate((node) => node.scrollWidth > node.clientWidth);
                expect(overflow).toBe(width === 600);
                if (overflow) {
                    const scroller = page.locator('.work-rows-scroll');
                    await expect(scroller).toHaveCSS('overflow-x', /^(auto|scroll)$/);
                    expect(await scroller.evaluate((node) => {
                        const before = node.scrollLeft;
                        node.scrollLeft = node.scrollWidth;
                        const moved = node.scrollLeft > before;
                        node.scrollLeft = before;
                        return moved;
                    }), 'Horizontal content must actually be scrollable').toBe(true);
                }
                const backgrounds = await items.evaluateAll((nodes) => nodes.slice(0, 2).map((node) => getComputedStyle(node).backgroundColor));
                expect(backgrounds[0]).not.toBe(backgrounds[1]);
            } else {
                const aligned = await items.evaluateAll((nodes) => nodes.every((node) => {
                    const title = node.querySelector('.work-title')!.getBoundingClientRect();
                    const action = node.querySelector('.work-actions-trigger')!.getBoundingClientRect();
                    return title.right <= action.left + 1 || title.bottom <= action.top + 1;
                }));
                expect(aligned, 'Title and action button must not overlap').toBe(true);
            }
            const title = page.locator('.work-title').first();
            const lines = mode === 'rows' ? 1 : mode === 'compact' ? 3 : 2;
            expect(await title.evaluate((node, limit) => node.clientHeight <= parseFloat(getComputedStyle(node).lineHeight) * limit + 1, lines)).toBe(true);
            if (mode !== 'compact') {
                const url = page.locator('.work-url').first();
                expect(await url.evaluate((node) => node.scrollWidth > node.clientWidth)).toBe(true);
                await expect(url).toHaveAttribute('title', /long-path-/);
            }
            await page.mouse.move(0, 0);
            const session = await page.context().newCDPSession(page);
            await session.send('DOM.enable');
            await session.send('CSS.enable');
            const {root} = await session.send('DOM.getDocument');
            const {nodeId} = await session.send('DOM.querySelector', {nodeId: root.nodeId, selector: '.work-title'});
            const {fonts} = await session.send('CSS.getPlatformFontsForNode', {nodeId});
            const fallbackFonts: Record<string, unknown> = {};
            for (const selector of ['.work-actions-trigger', '.reload-button', '.cover-placeholder > span']) {
                const found = await session.send('DOM.querySelector', {nodeId: root.nodeId, selector});
                if (found.nodeId) fallbackFonts[selector] = (await session.send('CSS.getPlatformFontsForNode', {nodeId: found.nodeId})).fonts;
            }
            await session.detach();
            const environmentPath = testInfo.outputPath('environment.json');
            await writeFile(environmentPath, JSON.stringify({
                platform: process.platform, release: release(), node: process.version, browser: browser.version(),
                width, height: 900, scale: 1, locale: 'ja-JP', timezone: 'Asia/Tokyo', fonts, fallbackFonts,
            }, null, 2));
            await testInfo.attach('environment', {path: environmentPath, contentType: 'application/json'});
            await expect(page).toHaveScreenshot(`${mode}-${width}.png`, {fullPage: true, animations: 'disabled', caret: 'hide', scale: 'css'});
        });
    }
}
