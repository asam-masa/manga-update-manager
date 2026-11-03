import type {Page} from '@playwright/test';
import {test, expect} from './fixture';

test.use({scenario: 'operations'});
const topPages = (page: Page) => page.getByRole('navigation', {name: '一覧上部のページ切替'});
async function selectManga(page: Page) {
    await page.getByRole('button', {name: /^タグで絞り込み/}).click();
    const search = page.getByLabel('候補のタグ名で検索');
    await search.fill('ﾏﾝｶﾞ');
    const candidate = page.getByRole('checkbox', {name: 'マンガ 23件', exact: true});
    await expect(search).toBeFocused();
    await page.keyboard.press('Tab');
    await expect(candidate).toBeFocused();
    await page.keyboard.press('Space');
    await expect(page.getByTitle('23件 / 全45件')).toBeVisible();
}
async function startRows(page: Page) {
    await page.goto('/');
    await page.getByRole('button', {name: '行一覧', exact: true}).click();
    await expect(page.getByTitle('45件 / 全45件')).toBeVisible();
}

test('検索・ANDタグ条件・表示切替・0件からの解除', async ({page}, testInfo) => {
    await page.goto('/');
    const search = page.getByRole('searchbox', {name: '漫画名で検索'});
    await search.fill('ﾏﾝｶﾞ');
    await expect(page.getByTitle('1件 / 全45件')).toBeVisible();
    await expect(page.getByRole('heading', {name: 'マンガ物語', exact: true})).toBeVisible();
    await search.clear();
    await selectManga(page);
    await page.getByLabel('候補のタグ名で検索').fill('連載中');
    await page.getByRole('checkbox', {name: '連載中 16件', exact: true}).check();
    await expect(page.getByTitle('16件 / 全45件')).toBeVisible();
    await testInfo.attach('and-filter', {body: await page.screenshot(), contentType: 'image/png'});
    await page.keyboard.press('Escape');
    await expect(page.getByRole('button', {name: /^タグで絞り込み/})).toBeFocused();
    for (const name of ['小型表示', '行一覧', 'メモなしカード']) {
        await page.getByRole('button', {name, exact: true}).click();
        await expect(page.getByTitle('16件 / 全45件')).toBeVisible();
        await expect(page.getByRole('group', {name: '選択中のタグ条件'})).toContainText('マンガ');
        await expect(page.getByRole('group', {name: '選択中のタグ条件'})).toContainText('連載中');
    }
    await page.getByRole('button', {name: /^タグで絞り込み/}).click();
    await page.getByLabel('候補のタグ名で検索').fill('未使用');
    await page.getByRole('checkbox', {name: '未使用 0件', exact: true}).check();
    await page.keyboard.press('Escape');
    await expect(page.getByRole('heading', {name: '表示条件に一致する作品がありません'})).toBeVisible();
    await page.getByRole('button', {name: 'タグ条件をすべて解除', exact: true}).click();
    await expect(page.getByTitle('45件 / 全45件')).toBeVisible();
});

test('下側のページ操作で先頭へフォーカスし、検索・自然順で探せる', async ({page}) => {
    await startRows(page);
    const bottom = page.getByRole('navigation', {name: '一覧下部のページ切替'});
    await bottom.getByRole('button', {name: '次へ', exact: true}).focus();
    await page.keyboard.press('Enter');
    await expect(topPages(page).getByRole('button', {name: '2ページ', exact: true})).toBeFocused();
    await expect(page.getByRole('heading', {name: '作品21', exact: true})).toBeVisible();
    await page.getByRole('searchbox', {name: '漫画名で検索'}).fill('作品45');
    await expect(topPages(page).getByRole('button', {name: '1ページ', exact: true})).toHaveAttribute('aria-current', 'page');
    await expect(page.getByRole('heading', {name: '作品45', exact: true})).toBeVisible();
    await page.getByRole('searchbox', {name: '漫画名で検索'}).clear();
    await page.getByRole('combobox', {name: '並び順'}).selectOption('registration_desc');
    await expect(page.locator('.work-title').first()).toHaveText('作品45');
    await page.getByRole('combobox', {name: '並び順'}).selectOption('title_asc');
    await expect(page.locator('.work-title').first()).toHaveText('マンガ物語');
    const titles = await page.locator('.work-title').allTextContents();
    expect(titles.indexOf('作品2')).toBeGreaterThanOrEqual(0);
    expect(titles.indexOf('作品10')).toBeGreaterThan(titles.indexOf('作品2'));
});

test('タグ改名の入力・Esc・保存後の選択IDとページを保つ', async ({page}, testInfo) => {
    await startRows(page);
    await selectManga(page);
    await page.keyboard.press('Escape');
    await topPages(page).getByRole('button', {name: '2ページ', exact: true}).click();
    await page.getByRole('button', {name: 'タグ管理', exact: true}).click();
    const dialog = page.getByRole('dialog', {name: 'タグ管理'});
    await dialog.getByRole('button', {name: '「マンガ」の名前変更', exact: true}).click();
    const name = dialog.getByRole('textbox', {name: '新しいタグ名'});
    await expect(name).toBeFocused();
    await expect(dialog.getByRole('searchbox', {name: 'タグ名で検索'})).toBeDisabled();
    await name.fill('マンガ確認');
    await page.keyboard.press('Tab');
    await expect(dialog.getByRole('button', {name: '保存', exact: true})).toBeFocused();
    await page.keyboard.press('Shift+Tab');
    await expect(name).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(dialog.getByRole('button', {name: '編集に戻る', exact: true})).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(name).toBeFocused();
    await expect(name).toHaveValue('マンガ確認');
    await testInfo.attach('rename-input', {body: await dialog.screenshot(), contentType: 'image/png'});
    await page.keyboard.press('Enter');
    await expect(dialog.getByRole('button', {name: '「マンガ確認」の名前変更', exact: true})).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
    await expect(page.getByRole('button', {name: 'タグ管理', exact: true})).toBeFocused();
    await expect(page.getByRole('group', {name: '選択中のタグ条件'})).toContainText('マンガ確認');
    await expect(topPages(page).getByRole('button', {name: '2ページ', exact: true})).toHaveAttribute('aria-current', 'page');
    await expect(page.getByText('タグ「マンガ」を「マンガ確認」に変更しました。', {exact: true})).toBeVisible();
});

test('タグ削除のEscは戻り、確定すると作品を残して条件とページを戻す', async ({page}) => {
    await startRows(page);
    await selectManga(page);
    await page.keyboard.press('Escape');
    await topPages(page).getByRole('button', {name: '2ページ', exact: true}).click();
    await page.getByRole('button', {name: 'タグ管理', exact: true}).click();
    const dialog = page.getByRole('dialog', {name: 'タグ管理'});
    await dialog.getByRole('button', {name: '「マンガ」を削除', exact: true}).click();
    await expect(dialog.getByRole('region', {name: 'タグが付いた作品'}).getByRole('listitem')).toHaveCount(23);
    await page.keyboard.press('Escape');
    await expect(dialog.getByRole('button', {name: '「マンガ」の名前変更', exact: true})).toBeFocused();
    await dialog.getByRole('button', {name: '「マンガ」を削除', exact: true}).click();
    await dialog.getByRole('button', {name: '削除する', exact: true}).click();
    await expect(dialog.getByRole('button', {name: '「マンガ」の名前変更', exact: true})).toHaveCount(0);
    await page.keyboard.press('Escape');
    await expect(page.getByTitle('45件 / 全45件')).toBeVisible();
    await expect(page.getByRole('group', {name: '選択中のタグ条件'})).toHaveCount(0);
    await expect(topPages(page).getByRole('button', {name: '1ページ', exact: true})).toHaveAttribute('aria-current', 'page');
    await expect(page.getByText('タグ「マンガ」を削除しました。作品は残っています。', {exact: true})).toBeVisible();
    await expect(page.getByRole('region', {name: 'タグ条件の変更'})).toContainText('削除');
});

test('一覧の再取得失敗では前回表示とタグ条件を残し条件変更を止める', async ({page}) => {
    await page.goto('/');
    await selectManga(page);
    await page.keyboard.press('Escape');
    await page.evaluate(() => {
        const target = window as unknown as {go: {main: {App: {LoadLibrarySnapshot: () => Promise<unknown>}}}};
        target.go.main.App.LoadLibrarySnapshot = async () => {throw {code: 'storage_error', message: '合成データの再取得に失敗しました。'};};
    });
    await page.getByRole('button', {name: '再読み込み', exact: true}).click();
    await expect(page.getByRole('alert')).toContainText('合成データの再取得に失敗しました。');
    await expect(page.getByTitle('23件 / 全45件')).toBeVisible();
    await expect(page.getByRole('group', {name: '選択中のタグ条件'})).toContainText('マンガ');
    await page.getByRole('button', {name: /^タグで絞り込み/}).click();
    await expect(page.getByRole('checkbox', {name: 'マンガ 23件（未確認）', exact: true})).toBeDisabled();
    await expect(page.getByRole('button', {name: 'すべて解除', exact: true})).toBeDisabled();
});
