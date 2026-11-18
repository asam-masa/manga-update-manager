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
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await dialog.locator('.backup-dialog-body').evaluate((el) => el.scrollTop = el.scrollHeight);
    await expect(dialog.getByRole('button', {name: '閉じる', exact: true})).toBeInViewport();
    await page.screenshot({path: info.outputPath('backup-unknown-narrow.png')});
});
