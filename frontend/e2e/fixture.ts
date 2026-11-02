import {test as base, expect} from '@playwright/test';
import type {LibrarySnapshot} from '../src/library-snapshot';

const instant = '2026-11-02T09:00:00+09:00';
const snapshot: LibrarySnapshot = {
    works: [1, 2].map((id) => ({
        id: String(id), url: `https://example.invalid/manga/${id}`, title: `ブラウザー確認${id}`,
        siteName: '合成サイト', thumbnailPath: id === 1 ? 'covers/synthetic.png' : '', notes: '',
        createdAt: instant, updatedAt: instant, lastAccessedAt: null, scheduledDate: null,
    })),
    tags: [{id: '1', name: '合成タグ'}], links: [{workID: '1', tagID: '1'}],
};
const visualSnapshot: LibrarySnapshot = {
    works: Array.from({length: 6}, (_, index) => ({
        ...snapshot.works[0], id: String(index + 1),
        title: index === 0 ? '長いタイトルの架空作品・異世界の図書館で見つけた物語と仲間たちの冒険'.repeat(3) : `表示確認の架空作品${index + 1}`,
        url: `https://example.invalid/manga/${index + 1}/${'long-path-'.repeat(index === 0 ? 18 : 0)}`,
        thumbnailPath: index === 0 || index === 2 ? `covers/synthetic-${index}.png` : '',
        siteName: index === 1 ? '' : '合成サイト',
        lastAccessedAt: index % 2 === 0 ? instant : null,
        scheduledDate: index % 2 === 0 ? '2026-11-02' : null,
    })), tags: snapshot.tags, links: snapshot.links,
};
// Authored here, not downloaded. Never placed under src/ or public/.
const cover = '<svg xmlns="http://www.w3.org/2000/svg" width="160" height="220"><rect width="160" height="220" fill="#4e8b75"/><circle cx="80" cy="90" r="45" fill="#c9e6cb"/></svg>';

export const test = base.extend<{scenario: 'smoke' | 'visual'}>({
    scenario: ['smoke', {option: true}],
    page: async ({page, context, baseURL, scenario}, use) => {
        const problems: string[] = [];
        page.on('pageerror', (error) => problems.push(error.message));
        await context.route('**/*', async (route) => {
            const url = new URL(route.request().url());
            if (url.origin !== baseURL) {
                problems.push(`Blocked external request: ${url.origin}`);
                await route.abort('blockedbyclient');
            } else if (url.pathname === '/__covers/works/1') {
                await route.fulfill({contentType: 'image/svg+xml', body: cover});
            } else if (scenario === 'visual' && url.pathname === '/__covers/works/3') {
                await route.fulfill({status: 404, body: ''});
            } else if (url.pathname.startsWith('/__covers/')) {
                problems.push(`Unexpected cover request: ${url.pathname}`);
                await route.abort('blockedbyclient');
            } else {
                await route.continue();
            }
        });
        await page.clock.setFixedTime(new Date(instant));
        await page.addInitScript((initial) => {
            let formVisible = false;
            let mode = 'cards';
            const calls: string[] = [];
            const api: Record<string, (...args: unknown[]) => Promise<unknown>> = {
                LoadLibrarySnapshot: async () => structuredClone(initial),
                LoadDisplaySettings: async () => ({registrationFormVisible: formVisible}),
                LoadWorkListMode: async () => mode,
                SetWorkListMode: async (value) => {
                    if (!['compact', 'rows', 'cards'].includes(String(value))) throw new Error('Invalid synthetic mode');
                    mode = String(value);
                },
                LoadWorkSort: async () => 'registration_asc',
                LoadShowFutureSchedules: async () => true,
                LoadCoverWarnings: async () => [],
                SetRegistrationFormVisible: async (visible) => { formVisible = visible === true; },
            };
            const target = window as unknown as {
                go: unknown; runtime: unknown; __browserTestCalls: string[];
            };
            target.__browserTestCalls = calls;
            target.go = {main: {App: new Proxy(api, {
                get(methods, name: string) {
                    return async (...args: unknown[]) => {
                        calls.push(name);
                        if (!Object.hasOwn(methods, name)) {
                            calls.push(`unsupported:${name}`);
                            throw new Error(`Unsupported synthetic API: ${name}`);
                        }
                        return methods[name](...args);
                    };
                },
            })}};
            target.runtime = {
                EventsOnMultiple: (name: string, _callback: unknown, count: number) => {
                    if (name !== 'cover-selection-finished' || count !== -1) throw new Error('Unsupported synthetic event');
                    calls.push(`subscribe:${name}`);
                    return () => { calls.push(`unsubscribe:${name}`); };
                },
            };
        }, scenario === 'visual' ? visualSnapshot : snapshot);
        await use(page);
        const unsupported = await page.evaluate(() =>
            (window as unknown as {__browserTestCalls?: string[]}).__browserTestCalls?.filter((name) => name.startsWith('unsupported:')) ?? []);
        expect(unsupported, 'Synthetic APIs must be implemented explicitly').toEqual([]);
        expect(problems, 'Unexpected page errors or network access').toEqual([]);
    },
});

export {expect} from '@playwright/test';
