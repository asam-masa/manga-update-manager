import {StrictMode} from 'react';
import {act, cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import App from './App';
import {CreateWork, DeleteWork, ListWorks, LoadDisplaySettings} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';

vi.mock('../wailsjs/go/main/App', () => ({CreateWork: vi.fn(), ListWorks: vi.fn(), DeleteWork: vi.fn(), OpenWork: vi.fn(), EditWork: vi.fn(),
    LoadDisplaySettings: vi.fn().mockResolvedValue({registrationFormVisible: false}), SetRegistrationFormVisible: vi.fn(),
    LoadCoverWarnings: vi.fn().mockResolvedValue([]), LoadCoverProgress: vi.fn().mockResolvedValue([])}));
const work = {id: 1, url: 'https://example.com/old', title: '漫画', siteName: 'Site', notes: 'Note', thumbnailPath: '', createdAt: '2026-10-23T00:00:00+09:00', updatedAt: '2026-10-23T00:00:00+09:00'};
function deferred<T>() {
    let resolve!: (value: T) => void; let reject!: (reason: unknown) => void;
    const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; }); return {promise, resolve, reject};
}
beforeEach(() => {
    vi.mocked(ListWorks).mockReset().mockResolvedValue([work]); vi.mocked(DeleteWork).mockReset();
    vi.mocked(LoadDisplaySettings).mockResolvedValue({registrationFormVisible: false});
    Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {configurable: true, value: function(this: HTMLDialogElement) { this.open = true; }});
    Object.defineProperty(HTMLDialogElement.prototype, 'close', {configurable: true, value: function(this: HTMLDialogElement) { this.open = false; }});
});
afterEach(() => { cleanup(); Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal'); Reflect.deleteProperty(HTMLDialogElement.prototype, 'close'); });
async function boot() { render(<StrictMode><App /></StrictMode>); await screen.findByRole('heading', {name: '漫画'}); }
async function open() { await userEvent.click(screen.getByRole('button', {name: '漫画の作品操作'})); await userEvent.click(screen.getByRole('button', {name: '漫画を削除'})); return screen.getByRole('dialog'); }

it('対象名とURLを表示し、初期キャンセルとEscは削除せず復帰する', async () => {
    await boot(); const dialog = await open();
    expect(within(dialog).getByText(work.url)).toBeTruthy();
    expect(document.activeElement).toBe(within(dialog).getByRole('button', {name: 'キャンセル'}));
    fireEvent(dialog, new Event('cancel', {cancelable: true}));
    expect(DeleteWork).not.toHaveBeenCalled(); expect(screen.queryByRole('dialog')).toBeNull();
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', {name: '漫画の作品操作'})));
});

it('5項目だけを一度送信し処理中の連打・Escを防ぎ、検索保持と警告・フォーカスを残す', async () => {
    const pending = deferred<main.DeleteWorkResult>(); vi.mocked(DeleteWork).mockReturnValue(pending.promise);
    await boot(); const search = screen.getByRole('searchbox'); await userEvent.type(search, '漫画');
    const dialog = await open(); const remove = within(dialog).getByRole('button', {name: '削除する'});
    act(() => { fireEvent.click(remove); fireEvent.click(remove); fireEvent(dialog, new Event('cancel', {cancelable: true})); });
    expect(DeleteWork).toHaveBeenCalledExactlyOnceWith(1, {url: work.url, title: work.title, siteName: work.siteName, notes: work.notes, thumbnailPath: ''});
    expect((within(dialog).getByRole('button', {name: 'キャンセル'}) as HTMLButtonElement).disabled).toBe(true);
    await act(async () => pending.resolve(new main.DeleteWorkResult({workID: 1, warnings: [{code: 'cover_cleanup_failed', message: '画像整理は未完了です。'}]})));
    expect(screen.queryByRole('dialog')).toBeNull(); expect(screen.queryByRole('heading', {name: '漫画'})).toBeNull();
    expect(screen.getByText('「漫画」を削除しました。')).toBeTruthy(); expect(screen.getByText('画像整理は未完了です。')).toBeTruthy();
    expect((search as HTMLInputElement).value).toBe('漫画');
    await waitFor(() => expect(document.activeElement).toBe(search));
});

it('削除前の一覧応答が遅れても作品が再出現しない', async () => {
    await boot(); const old = deferred<main.WorkDTO[]>(); vi.mocked(ListWorks).mockReturnValueOnce(old.promise);
    await userEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    vi.mocked(DeleteWork).mockResolvedValue(new main.DeleteWorkResult({workID: 1, warnings: []}));
    const dialog = await open(); await userEvent.click(within(dialog).getByRole('button', {name: '削除する'}));
    await act(async () => old.resolve([work]));
    expect(screen.queryByRole('heading', {name: '漫画'})).toBeNull();
});

it('再利用された同じIDの新規登録を削除済み扱いせず表示する', async () => {
    vi.mocked(LoadDisplaySettings).mockResolvedValue({registrationFormVisible: true});
    vi.mocked(DeleteWork).mockResolvedValue(new main.DeleteWorkResult({workID: 1, warnings: []}));
    await boot(); const dialog = await open(); await userEvent.click(within(dialog).getByRole('button', {name: '削除する'}));
    const created = {...work, title: '新しい登録', createdAt: '2026-10-23T01:00:00+09:00'};
    vi.mocked(CreateWork).mockResolvedValue(created); vi.mocked(ListWorks).mockResolvedValue([created]);
    await userEvent.type(screen.getByLabelText(/作品URL/), created.url); await userEvent.type(screen.getByLabelText(/タイトル/), created.title);
    await userEvent.click(screen.getByRole('button', {name: '登録する'}));
    expect(await screen.findByRole('heading', {name: created.title})).toBeTruthy();
});

it('対象変更では最新名とURLへ更新し、自動再送せず再確認してから削除する', async () => {
    await boot(); const latest = {...work, title: '変更後', url: 'https://example.com/new'};
    vi.mocked(DeleteWork).mockRejectedValueOnce({code: 'work_changed', message: '変更あり'}).mockResolvedValue(new main.DeleteWorkResult({workID:1, warnings:[]}));
    vi.mocked(ListWorks).mockResolvedValue([latest]);
    const dialog = await open(); await userEvent.click(within(dialog).getByRole('button', {name:'削除する'}));
    expect(await within(dialog).findByText(latest.url)).toBeTruthy(); expect(DeleteWork).toHaveBeenCalledTimes(1);
    await userEvent.click(within(dialog).getByRole('button', {name:'削除する'}));
    expect(vi.mocked(DeleteWork).mock.calls[1][1]).toEqual({url:latest.url, title:latest.title, siteName:work.siteName, notes:work.notes, thumbnailPath:''});
});

it('再確認の取得失敗では削除不可とし、再読込またはキャンセルできる', async () => {
    await boot(); vi.mocked(DeleteWork).mockRejectedValue({code:'work_changed', message:'変更あり'});
    vi.mocked(ListWorks).mockRejectedValueOnce(new Error('private read diagnostic')).mockResolvedValue([work]);
    const dialog = await open(); await userEvent.click(within(dialog).getByRole('button', {name:'削除する'}));
    const reload = await within(dialog).findByRole('button', {name:'最新情報を再読み込み'});
    expect((within(dialog).getByRole('button', {name:'削除する'}) as HTMLButtonElement).disabled).toBe(true);
    expect(document.body.textContent).not.toContain('private read diagnostic');
    await userEvent.click(reload); expect(DeleteWork).toHaveBeenCalledTimes(1);
    expect((within(dialog).getByRole('button', {name:'削除する'}) as HTMLButtonElement).disabled).toBe(false);
    await userEvent.click(within(dialog).getByRole('button', {name:'キャンセル'}));
});

it('不存在は今回の成功とせず一覧から除外する', async () => {
    vi.mocked(DeleteWork).mockRejectedValue({code:'work_not_found', message:'存在しません'});
    await boot(); const dialog = await open(); await userEvent.click(within(dialog).getByRole('button', {name:'削除する'}));
    expect(await screen.findByText('「漫画」は既に存在しません。')).toBeTruthy(); expect(screen.queryByRole('dialog')).toBeNull();
    expect(screen.queryByRole('heading', {name:'漫画'})).toBeNull();
});

it.each(['work_delete_outcome_unknown', 'transport', 'invalid-result'])('結果不明 %s で再送せず閉じられ、追加の編集削除を止める', async (kind) => {
    if (kind === 'transport') vi.mocked(DeleteWork).mockRejectedValue(new Error('private diagnostic'));
    else if (kind === 'invalid-result') vi.mocked(DeleteWork).mockResolvedValue(new main.DeleteWorkResult({workID:999, warnings:[]}));
    else vi.mocked(DeleteWork).mockRejectedValue({code:kind, message:'再起動して確認してください。'});
    await boot(); const dialog = await open(); await userEvent.click(within(dialog).getByRole('button', {name:'削除する'}));
    await within(dialog).findByRole('button', {name:'閉じる'});
    fireEvent.click(within(dialog).getByRole('button', {name:'削除する'})); expect(DeleteWork).toHaveBeenCalledTimes(1);
    expect(document.body.textContent).not.toContain('private diagnostic');
    await userEvent.click(within(dialog).getByRole('button', {name:'閉じる'}));
    await userEvent.click(screen.getByRole('button', {name: '漫画の作品操作'}));
    expect((screen.getByRole('button', {name:'漫画を削除'}) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('button', {name:'漫画を編集'}) as HTMLButtonElement).disabled).toBe(true);
});
