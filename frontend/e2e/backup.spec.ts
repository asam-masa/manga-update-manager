import type {Page} from '@playwright/test';
import {test, expect} from './fixture';

async function control(page: Page, patch: Record<string, unknown>) {
    await page.evaluate((patch) => Object.assign((window as unknown as {__backupTest: Record<string, unknown>}).__backupTest, patch), patch);
}
async function open(page: Page) {
    await page.getByRole('button', {name: 'バックアップ', exact: true}).click();
    return page.getByRole('dialog', {name: 'バックアップ', exact: true});
}

test('バックアップの保存・選択取消・結果保持・入口へのフォーカス復帰', async ({page}, info) => {
    await page.goto('/'); const dialog = await open(page);
    await expect(dialog.getByRole('heading', {name: 'バックアップ', exact: true})).toBeFocused();
    await dialog.getByRole('button', {name: '保存先を選んで開始'}).click();
    await expect(dialog.getByText('バックアップは保存されています。')).toBeVisible();
    await expect(dialog.getByRole('region', {name: '保存先と保全候補'})).toContainText('選んだ保存先');
    await control(page, {selectionCancelled: true});
    await dialog.getByRole('button', {name: '保存先を選び直す'}).click();
    await expect(dialog.getByText('バックアップは保存されています。')).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.getByRole('button', {name: 'バックアップ', exact: true})).toBeFocused();
    await expect(page.getByRole('alert', {name: 'バックアップのお知らせ'})).toContainText('一部の画像を除外');
    await open(page); await expect(dialog.getByText('バックアップは保存されています。')).toBeVisible();
    await page.screenshot({path: info.outputPath('backup-result.png')});
});

test('処理中の再接続・取消後の片付け待ち・背景操作遮断', async ({page}) => {
    await page.goto('/'); await control(page, {hold: true}); let dialog = await open(page);
    await dialog.getByRole('button', {name: '保存先を選んで開始'}).click();
    await expect(dialog.getByText('処理段階：DBコピー')).toBeVisible();
    await page.reload(); dialog = page.getByRole('dialog', {name: 'バックアップ', exact: true});
    await expect(dialog).toBeVisible();
    await page.keyboard.press('Escape'); await expect(dialog).toBeVisible();
    await expect(dialog.getByRole('button', {name: '閉じる', exact: true})).toBeDisabled();
    await dialog.getByRole('button', {name: 'バックアップをキャンセル'}).click();
    await expect(dialog.getByText(/片付け・解放が終わるまで/)).toBeVisible();
    await page.keyboard.press('Tab');
    await expect.poll(() => dialog.evaluate((d) => d.contains(document.activeElement))).toBe(true);
    await control(page, {state: 'finished'}); await dialog.getByRole('button', {name: '再読み込み', exact: true}).click();
    await expect(dialog.getByRole('button', {name: '閉じる', exact: true})).toBeEnabled();
});

test('狭い幅の結果不明は再保存を出さず概要を保持する', async ({page}, info) => {
    await page.setViewportSize({width: 390, height: 740}); await page.goto('/');
    await control(page, {unknown: true}); const dialog = await open(page);
    await dialog.getByRole('button', {name: '保存先を選んで開始'}).click();
    await expect(dialog.getByRole('heading', {name: '保存の成否は不明です'})).toBeVisible();
    await expect(dialog.getByRole('button', {name: '保存先を選び直す'})).toHaveCount(0);
    const locations = dialog.getByRole('region', {name: '保存先と保全候補'});
    await expect(locations).toContainText('成否不明の保存先');
    await expect(locations).toContainText('DBコピー');
    await expect(locations).toContainText('作業フォルダー');
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await dialog.locator('.backup-dialog-body').evaluate((el) => el.scrollTop = el.scrollHeight);
    await expect(dialog.getByRole('button', {name: '閉じる', exact: true})).toBeInViewport();
    await page.screenshot({path: info.outputPath('backup-unknown-narrow.png')});
});

test('場所取得の失敗を保存失敗にせず、詳細だけを再取得する', async ({page}) => {
    await page.goto('/'); await control(page, {locationsFailed: true}); const dialog = await open(page);
    await dialog.getByRole('button', {name: '保存先を選んで開始'}).click();
    await expect(dialog.getByText('バックアップは保存されています。')).toBeVisible();
    await expect(dialog.getByRole('alert')).toContainText('保存先・保全候補を取得できません');
    const starts = () => page.evaluate(() => (window as unknown as {__browserTestCalls: string[]}).__browserTestCalls.filter(x => x === 'SelectAndStartBackup').length);
    expect(await starts()).toBe(1); await control(page, {locationsFailed: false});
    await dialog.getByRole('button', {name: '場所の詳細を再読み込み'}).click();
    await expect(dialog.getByRole('region', {name: '保存先と保全候補'})).toContainText('saved.zip');
    expect(await starts()).toBe(1); await expect(dialog.getByText('バックアップは保存されています。')).toBeVisible();
});

test('除外52画像・103作品を分割し、戻るとページ・位置・フォーカスを復元する', async ({page}, info) => {
    await page.goto('/'); await control(page, {imageTotal: 52, workTotal: 103}); const dialog = await open(page);
    await dialog.getByRole('button', {name: '保存先を選んで開始'}).click();
    await dialog.getByRole('button', {name: '除外画像の詳細', exact: true}).click();
    const images = dialog.getByRole('navigation', {name: '除外画像のページ'});
    await expect(images).toContainText('1 / 2ページ・全52画像');
    const image = dialog.getByRole('button', {name: '除外画像45の対象作品', exact: true});
    await image.focus();
    const scroll = await dialog.locator('.backup-dialog-body').evaluate(el => el.scrollTop);
    await image.click();
    const works = dialog.getByRole('navigation', {name: '対象作品のページ'});
    await expect(works).toContainText('1 / 3ページ・全103作品');
    await works.getByRole('button', {name: '次へ', exact: true}).click();
    await expect(works).toContainText('2 / 3ページ');
    await works.getByRole('button', {name: '次へ', exact: true}).click();
    await expect(works).toContainText('3 / 3ページ');
    await expect(dialog.getByRole('list').last().getByRole('listitem')).toHaveCount(3);
    await expect(works.getByRole('button', {name: '次へ'})).toBeDisabled();
    await page.keyboard.press('Escape');
    await expect(image).toBeFocused();
    await expect.poll(() => dialog.locator('.backup-dialog-body').evaluate(el => el.scrollTop)).toBe(scroll);
    await images.getByRole('button', {name: '次へ'}).click();
    await expect(images).toContainText('2 / 2ページ');
    await expect(dialog.getByRole('button', {name: /除外画像\d+の対象作品/})).toHaveCount(2);
    await dialog.getByRole('button', {name: '除外画像52の対象作品', exact: true}).click();
    await expect(works).toContainText('1 / 3ページ');
    await dialog.getByRole('button', {name: '戻る', exact: true}).click();
    await expect(dialog.getByRole('button', {name: '除外画像52の対象作品', exact: true})).toBeFocused();
    await expect(images).toContainText('2 / 2ページ');
    await page.keyboard.press('Escape');
    await expect(dialog.getByRole('button', {name: '除外画像の詳細', exact: true})).toBeFocused();
    await page.screenshot({path: info.outputPath('backup-details-return.png')});
});

test('狭い幅でも詳細の失敗だけを再取得し、保存結果は変えない', async ({page}, info) => {
    await page.setViewportSize({width: 390, height: 740}); await page.goto('/');
    await control(page, {detailsFailed: true, imageTotal: 52, workTotal: 103}); const dialog = await open(page);
    await dialog.getByRole('button', {name: '保存先を選んで開始'}).click();
    await dialog.getByRole('button', {name: '除外画像の詳細', exact: true}).click();
    await expect(dialog.getByRole('alert')).toBeVisible();
    await expect(dialog.getByText('バックアップは保存されています。')).toBeVisible();
    await control(page, {detailsFailed: false});
    await dialog.getByRole('button', {name: '詳細を再読み込み'}).click();
    await dialog.getByRole('button', {name: '除外画像1の対象作品', exact: true}).click();
    await expect(dialog.getByRole('navigation', {name: '対象作品のページ'})).toContainText('全103作品');
    await control(page, {detailsFailed: true});
    await dialog.getByRole('button', {name: '詳細を再読み込み'}).click();
    await expect(dialog.getByRole('alert')).toBeVisible();
    await control(page, {detailsFailed: false});
    await dialog.getByRole('button', {name: '詳細を再読み込み'}).click();
    await expect(dialog.getByRole('navigation', {name: '対象作品のページ'})).toContainText('全103作品');
    await expect.poll(() => dialog.locator('.backup-dialog-body').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
    await expect(dialog.getByRole('button', {name: '戻る', exact: true})).toBeInViewport();
    await page.screenshot({path: info.outputPath('backup-works-narrow.png')});
    expect(await page.evaluate(() => (window as unknown as {__browserTestCalls: string[]}).__browserTestCalls.filter(x => x === 'SelectAndStartBackup').length)).toBe(1);
});
