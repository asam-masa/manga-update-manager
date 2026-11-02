import {test, expect} from './fixture';

test('実React画面を合成API・画像から表示できる', async ({page}, testInfo) => {
    await page.goto('/');
    await expect(page.getByRole('heading', {name: 'ブラウザー確認1', exact: true})).toBeVisible();
    await expect(page.getByRole('heading', {name: 'ブラウザー確認2', exact: true})).toBeVisible();
    await expect(page.getByTitle('2件 / 全2件')).toBeVisible();
    const image = page.locator('.work-cover img');
    await expect(image).toHaveCount(1);
    await expect(image).toHaveJSProperty('naturalWidth', 160);
    await expect(image).not.toHaveClass('cover-loading');
    await expect(page.getByRole('alert')).toHaveCount(0);
    await page.getByRole('button', {name: '作品を登録', exact: true}).click();
    await expect(page.getByRole('heading', {name: '作品を登録', exact: true})).toBeVisible();
    await expect(page.getByText('表示設定を保存しています…')).toHaveCount(0);
    await testInfo.attach('browser-smoke', {body: await page.screenshot(), contentType: 'image/png'});
});

test('別テストは初期状態から開始しイベント境界も用意される', async ({page}) => {
    await page.goto('/');
    await expect(page.getByRole('heading', {name: 'ブラウザー確認1', exact: true})).toBeVisible();
    await expect(page.getByRole('button', {name: '作品を登録', exact: true})).toHaveAttribute('aria-expanded', 'false');
    await expect(page.getByRole('heading', {name: '作品を登録', exact: true})).toBeHidden();
    const calls = await page.evaluate(() => (window as unknown as {__browserTestCalls: string[]}).__browserTestCalls);
    expect(calls).toContain('LoadLibrarySnapshot');
    expect(calls).toContain('subscribe:cover-selection-finished');
});
