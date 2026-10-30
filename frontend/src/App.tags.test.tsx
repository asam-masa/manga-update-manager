import {StrictMode} from 'react';
import {act, cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import App from './App';
import {LoadTagUsage, RenameTag} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';

vi.mock('../wailsjs/go/main/App', () => ({
    LoadShowFutureSchedules: async () => true, SetShowFutureSchedules: async () => {},
    LoadWorkSort: async () => 'registration_asc', SetWorkSort: async () => {},
    LoadWorkListMode: async () => 'rows', SetWorkListMode: async () => {},
    LoadDisplaySettings: async () => ({registrationFormVisible: false}), SetRegistrationFormVisible: vi.fn(),
    LoadCoverWarnings: async () => [], LoadCoverProgress: async () => [],
    ListWorks: async () => Array.from({length: 23}, (_, i) => ({id: i + 1, title: `漫画${i + 1}`, url: `https://example.com/${i + 1}`,
        siteName: '', notes: '', thumbnailPath: '', createdAt: '2026-10-30T00:00:00+09:00', updatedAt: '2026-10-30T00:00:00+09:00'})),
    LoadTagUsage: vi.fn(), RenameTag: vi.fn(), CreateWork: vi.fn(), OpenWork: vi.fn(),
}));
beforeEach(() => {
    vi.mocked(LoadTagUsage).mockReset().mockResolvedValue([main.TagUsageDTO.createFrom({tag: {id: '1', name: 'マンガ'}, workCount: 23})]);
    vi.mocked(RenameTag).mockReset();
    Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {configurable: true, value: function(this: HTMLDialogElement) {this.open = true;}});
    Object.defineProperty(HTMLDialogElement.prototype, 'close', {configurable: true, value: function(this: HTMLDialogElement) {this.open = false;}});
});
afterEach(() => {cleanup(); Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal'); Reflect.deleteProperty(HTMLDialogElement.prototype, 'close');});

it('作品検索・現在ページを保持し、Esc後にタグ管理ボタンへ戻り、開き直すと最新情報を読む', async () => {
    render(<StrictMode><App /></StrictMode>); await screen.findByRole('heading', {name: '漫画1'});
    await userEvent.type(screen.getByRole('searchbox', {name: '漫画名で検索'}), '漫画');
    await userEvent.click(screen.getAllByRole('button', {name: '次へ'})[0]);
    await screen.findByRole('heading', {name: '漫画21'});
    const origin = screen.getByRole('button', {name: 'タグ管理'});
    await userEvent.click(origin); const dialog = screen.getByRole('dialog');
    await within(dialog).findByText('23作品');
    await userEvent.type(within(dialog).getByRole('searchbox'), 'ﾏﾝｶﾞ');
    expect(within(dialog).getByText('23作品')).toBeTruthy();
    fireEvent(dialog, new Event('cancel', {cancelable: true}));
    await waitFor(() => expect(document.activeElement).toBe(origin));
    expect((screen.getByRole('searchbox', {name: '漫画名で検索'}) as HTMLInputElement).value).toBe('漫画');
    expect(screen.getByRole('heading', {name: '漫画21'})).toBeTruthy();
    const calls = vi.mocked(LoadTagUsage).mock.calls.length;
    await userEvent.click(origin); await within(screen.getByRole('dialog')).findByText('23作品');
    expect(vi.mocked(LoadTagUsage).mock.calls.length).toBeGreaterThan(calls);
});

it('管理画面で検出した停止を閉じても残し、開き直しても再取得しない', async () => {
    vi.mocked(LoadTagUsage).mockRejectedValue({code: 'database_stopped', message: '停止'});
    render(<App />); await screen.findByRole('heading', {name: '漫画1'});
    await userEvent.click(screen.getByRole('button', {name: 'タグ管理'}));
    await within(screen.getByRole('dialog')).findByRole('alert');
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', {name: '閉じる'}));
    expect(screen.getByText(/タグ操作でDB停止または結果不明/)).toBeTruthy();
    const calls = vi.mocked(LoadTagUsage).mock.calls.length;
    await userEvent.click(screen.getByRole('button', {name: 'タグ管理'}));
    await act(async () => {});
    expect(LoadTagUsage).toHaveBeenCalledTimes(calls);
    expect((within(screen.getByRole('dialog')).getByRole('button', {name: '再読み込み'}) as HTMLButtonElement).disabled).toBe(true);
});

it.each([false, true])('改名の結果と確定後警告を閉じた後も親で保持する: warning=%s', async (warning) => {
    vi.mocked(RenameTag).mockResolvedValue(main.TagChangeResult.createFrom({tag: {id: '1', name: '保存した名前'}, warnings: warning
        ? [{code: 'database_stopped_after_commit', message: '保存済み・接続を確認できません'}] : []}));
    render(<App />); await screen.findByRole('heading', {name: '漫画1'});
    await userEvent.click(screen.getByRole('button', {name: 'タグ管理'}));
    const dialog = screen.getByRole('dialog');
    await userEvent.click(await within(dialog).findByRole('button', {name: '「マンガ」の名前変更'}));
    fireEvent.change(within(dialog).getByRole('textbox', {name: '新しいタグ名'}), {target: {value: '保存した名前'}});
    await userEvent.click(within(dialog).getByRole('button', {name: '保存'}));
    await within(dialog).findByText('タグ「マンガ」を「保存した名前」に変更しました。');
    await userEvent.click(within(dialog).getByRole('button', {name: '閉じる'}));
    expect(screen.getByText('タグ「マンガ」を「保存した名前」に変更しました。')).toBeTruthy();
    if (warning) {
        expect(screen.getByText('保存済み・接続を確認できません')).toBeTruthy();
        expect(screen.getByText(/タグ操作でDB停止または結果不明/)).toBeTruthy();
    }
});
