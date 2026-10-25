import {StrictMode} from 'react';
import {act, cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import App from './App';
import {EditWork, ListWorks, DeleteWork} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';

vi.mock('../wailsjs/go/main/App', () => ({LoadWorkSort: async () => 'registration_asc', SetWorkSort: async () => {}, LoadWorkListMode: async () => 'cards', SetWorkListMode: async () => {}, CreateWork: vi.fn(), ListWorks: vi.fn(), OpenWork: vi.fn(), EditWork: vi.fn(), DeleteWork: vi.fn(),
    LoadDisplaySettings: vi.fn().mockResolvedValue({registrationFormVisible: false}), SetRegistrationFormVisible: vi.fn(),
    LoadCoverWarnings: vi.fn().mockResolvedValue([]), LoadCoverProgress: vi.fn().mockResolvedValue([])}));
const work = {id: 1, url: 'https://example.com/old', title: '漫画', siteName: 'Site', notes: 'Note', thumbnailPath: '', createdAt: '2026-10-22T00:00:00+09:00', updatedAt: '2026-10-22T00:00:00+09:00'};
const updated = {...work, url: 'https://example.com/new', title: '新作', updatedAt: '2026-10-22T01:00:00+09:00'};
function deferred<T>() {
    let resolve!: (value: T) => void; let reject!: (reason: unknown) => void;
    const promise = new Promise<T>((yes, no) => {resolve = yes; reject = no;});
    return {promise, resolve, reject};
}
beforeEach(() => {
    vi.mocked(ListWorks).mockReset().mockResolvedValue([work]);
    vi.mocked(EditWork).mockReset();
    // jsdom does not implement the native modal/focus behavior; WebView2 verifies it.
    Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {configurable: true, value: function(this: HTMLDialogElement) { this.open = true; }});
    Object.defineProperty(HTMLDialogElement.prototype, 'close', {configurable: true, value: function(this: HTMLDialogElement) { this.open = false; }});
});
afterEach(() => { cleanup(); Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal'); Reflect.deleteProperty(HTMLDialogElement.prototype, 'close'); });
async function boot() { render(<StrictMode><App /></StrictMode>); await screen.findByRole('heading', {name: '漫画'}); }
async function open() { await userEvent.click(screen.getByRole('button', {name: '漫画の作品操作'})); await userEvent.click(screen.getByRole('button', {name: '漫画を編集'})); return screen.getByRole('dialog'); }
const field = (dialog: HTMLElement, label: RegExp) => within(dialog).getByLabelText(label) as HTMLInputElement;

it('別作品の保存結果を保持し、開閉だけでは消さず次の保存で対象通知だけ更新する', async () => {
    const other = {...work, id: 2, title: '別作品'};
    vi.mocked(ListWorks).mockResolvedValue([work, other]);
    vi.mocked(EditWork).mockImplementation(async (id) => new main.EditWorkResult({work: id === 1 ? work : other, warnings: []}));
    await boot();
    let dialog = await open();
    await userEvent.click(within(dialog).getByRole('button', {name: '保存する'}));
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', {name: '漫画の作品操作'})));
    await userEvent.click(screen.getByRole('button', {name: '別作品の作品操作'}));
    await userEvent.click(screen.getByRole('button', {name: '別作品を編集'}));
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', {name: '保存する'}));
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', {name: '別作品の作品操作'})));
    expect(screen.getByRole('region', {name: '漫画の編集結果'})).toBeTruthy();
    expect(screen.getByRole('region', {name: '別作品の編集結果'})).toBeTruthy();
    dialog = await open();
    await userEvent.click(within(dialog).getByRole('button', {name: 'キャンセル'}));
    expect(screen.getByRole('region', {name: '漫画の編集結果'})).toBeTruthy();
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', {name: '漫画の作品操作'})));
    const pending = deferred<main.EditWorkResult>();
    vi.mocked(EditWork).mockReturnValue(pending.promise);
    dialog = await open();
    await userEvent.click(within(dialog).getByRole('button', {name: '保存する'}));
    expect(screen.queryByRole('region', {name: '漫画の編集結果'})).toBeNull();
    expect(screen.getByRole('region', {name: '別作品の編集結果'})).toBeTruthy();
    const scheduled: FrameRequestCallback[] = [];
    const frame = vi.spyOn(window, 'requestAnimationFrame').mockImplementation((callback) => {scheduled.push(callback); return scheduled.length;});
    try {
        await act(async () => pending.resolve(new main.EditWorkResult({work, warnings: []})));
        await userEvent.click(within(screen.getByRole('region', {name: '別作品の編集結果'})).getByRole('button'));
        act(() => scheduled.splice(0).forEach((callback) => callback(0)));
        expect(document.activeElement).toBe(screen.getByRole('button', {name: '別作品の作品操作'}));
    } finally { frame.mockRestore(); }
});

it('キャンセルは保存せず入力を破棄し、再度開くと保存済みの値になる', async () => {
    await boot(); const dialog = await open();
    await userEvent.type(field(dialog, /タイトル/), '入力');
    await userEvent.click(within(dialog).getByRole('button', {name: 'キャンセル'}));
    expect(screen.queryByRole('dialog')).toBeNull(); expect(EditWork).not.toHaveBeenCalled();
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', {name: '漫画の作品操作'})));
    expect(field(await open(), /タイトル/).value).toBe('漫画');
});

it('4項目だけを一度送信し、保存中はキャンセルとEsc・連打で閉じない', async () => {
    const pending = deferred<main.EditWorkResult>(); vi.mocked(EditWork).mockReturnValue(pending.promise);
    await boot(); const dialog = await open();
    const form = dialog.querySelector('form')!;
    act(() => { fireEvent.submit(form); fireEvent.submit(form); fireEvent(dialog, new Event('cancel', {cancelable: true, bubbles: false})); });
    expect(EditWork).toHaveBeenCalledExactlyOnceWith(1, {url: work.url, title: work.title, siteName: work.siteName, notes: work.notes});
    expect((within(dialog).getByRole('button', {name: 'キャンセル'}) as HTMLButtonElement).disabled).toBe(true);
    expect(dialog).toBe(screen.getByRole('dialog'));
    await act(async () => pending.resolve(new main.EditWorkResult({work: updated, warnings: []})));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(await screen.findByRole('heading', {name: '新作'})).toBeTruthy();
});

it.each(['invalid_input', 'duplicate_url', 'work_busy', 'work_not_found'])('失敗 %s は入力を保持してキャンセル可能にする', async (code) => {
    vi.mocked(EditWork).mockRejectedValue({code, message: '入力を確認してください。'});
    await boot(); const dialog = await open();
    await userEvent.type(field(dialog, /メモ/), '追記');
    await userEvent.click(within(dialog).getByRole('button', {name: '保存する'}));
    expect(await within(dialog).findByRole('alert')).toBeTruthy();
    expect(field(dialog, /メモ/).value).toBe('Note追記');
    expect((within(dialog).getByRole('button', {name: '保存する'}) as HTMLButtonElement).disabled).toBe(false);
    expect(screen.getByRole('heading', {name: '漫画'})).toBeTruthy();
});

it('結果不明は入力を残して再保存を止め、閉じた後も編集を止める', async () => {
    vi.mocked(EditWork).mockRejectedValue(new Error('manga-update-manager:api-error:v1:' + JSON.stringify({code: 'work_edit_outcome_unknown', message: '再起動して状態を確認してください。'})));
    await boot(); const dialog = await open();
    await userEvent.click(within(dialog).getByRole('button', {name: '保存する'}));
    await within(dialog).findByRole('button', {name: '閉じる'});
    expect(field(dialog, /作品URL/).value).toBe(work.url);
    fireEvent.submit(dialog.querySelector('form')!); expect(EditWork).toHaveBeenCalledTimes(1);
    await userEvent.click(within(dialog).getByRole('button', {name: '閉じる'}));
    await userEvent.click(screen.getByRole('button', {name: '漫画の作品操作'}));
    expect((screen.getByRole('button', {name: '漫画を編集'}) as HTMLButtonElement).disabled).toBe(true);
});

it('検索を維持し、条件外への編集成功と確定後の警告はカードが消えても残る', async () => {
    vi.mocked(EditWork).mockResolvedValue(new main.EditWorkResult({work: updated, warnings: [{code: 'database_stopped_after_commit', message: '保存しましたが再起動してください。'}]}));
    await boot(); const search = screen.getByRole('searchbox'); await userEvent.type(search, '漫画');
    const dialog = await open(); await userEvent.click(within(dialog).getByRole('button', {name: '保存する'}));
    expect(await screen.findByText(/「新作」を保存しました/)).toBeTruthy();
    expect(screen.getByText('保存しましたが再起動してください。')).toBeTruthy();
    expect(screen.queryByRole('heading', {name: '漫画'})).toBeNull();
    expect((search as HTMLInputElement).value).toBe('漫画');
    await waitFor(() => expect(document.activeElement).toBe(search));
});

it('開いた入力は背景の一覧応答で置換せず、保存後の古い一覧でテキストを戻さない', async () => {
    await boot(); const oldList = deferred<main.WorkDTO[]>();
    vi.mocked(ListWorks).mockReturnValueOnce(oldList.promise);
    await userEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    const dialog = await open(); await userEvent.type(field(dialog, /メモ/), '入力');
    vi.mocked(EditWork).mockResolvedValue(new main.EditWorkResult({work: updated, warnings: []}));
    await userEvent.click(within(dialog).getByRole('button', {name: '保存する'}));
    await act(async () => oldList.resolve([work]));
    expect(screen.getByRole('heading', {name: '新作'})).toBeTruthy();
    expect(screen.getByRole('button', {name: '新作のURLを開く'}).textContent).toBe(updated.url);
});

it('保存前の再取得で入力を変更せず、IME確定Enterでは送信しない', async () => {
    await boot(); const oldList = deferred<main.WorkDTO[]>(); vi.mocked(ListWorks).mockReturnValueOnce(oldList.promise);
    await userEvent.click(screen.getByRole('button', {name: '再読み込み'})); const dialog = await open();
    await userEvent.type(field(dialog, /タイトル/), '入力');
    await act(async () => oldList.resolve([updated]));
    expect(field(dialog, /タイトル/).value).toBe('漫画入力');
    const event = new KeyboardEvent('keydown', {key: 'Enter', bubbles: true, cancelable: true, isComposing: true});
    field(dialog, /タイトル/).dispatchEvent(event); expect(event.defaultPrevented).toBe(true); expect(EditWork).not.toHaveBeenCalled();
});

it.each(['transport', 'invalid-result'])('結果を検証できない %s でも再送しない', async (kind) => {
    if (kind === 'transport') vi.mocked(EditWork).mockRejectedValue(new Error('private diagnostic'));
    else vi.mocked(EditWork).mockResolvedValue({work: {...updated, id: 999}, warnings: []} as unknown as main.EditWorkResult);
    await boot(); const dialog = await open();
    await userEvent.click(within(dialog).getByRole('button', {name: '保存する'}));
    await within(dialog).findByRole('button', {name: '閉じる'});
    fireEvent.submit(dialog.querySelector('form')!);
    expect(EditWork).toHaveBeenCalledTimes(1);
    expect(document.body.textContent).not.toContain('private diagnostic');
    expect(screen.getByRole('heading', {name: '漫画'})).toBeTruthy();
});

it('未保存のEscは保存せず閉じる', async () => {
    await boot(); const dialog = await open();
    fireEvent(dialog, new Event('cancel', {cancelable: true}));
    expect(screen.queryByRole('dialog')).toBeNull(); expect(EditWork).not.toHaveBeenCalled();
});

it('編集結果と古い一覧応答をまたいで最新の予定日を保ち、削除確認へ渡す', async () => {
    const original = {...work, scheduledDate: '2026-10-30'};
    const saved = {...updated, scheduledDate: '2026-11-01'};
    vi.mocked(ListWorks).mockResolvedValue([original]); await boot();
    const stale = deferred<main.WorkDTO[]>(); vi.mocked(ListWorks).mockReturnValueOnce(stale.promise);
    await userEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    const dialog = await open(); vi.mocked(EditWork).mockResolvedValue(new main.EditWorkResult({work: saved, warnings: []}));
    await userEvent.click(within(dialog).getByRole('button', {name: '保存する'}));
    await screen.findByRole('heading', {name: '新作'});
    await act(async () => {stale.resolve([original]);});
    await userEvent.click(screen.getByRole('button', {name: '新作の作品操作'}));
    await userEvent.click(screen.getByRole('button', {name: '新作を削除'}));
    vi.mocked(DeleteWork).mockResolvedValue(new main.DeleteWorkResult({workID: 1, warnings: []}));
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', {name: '削除する'}));
    expect(vi.mocked(DeleteWork).mock.calls.at(-1)?.[1].scheduledDate).toBe('2026-11-01');
    expect(vi.mocked(EditWork).mock.calls.at(-1)?.[1]).not.toHaveProperty('scheduledDate');
});
