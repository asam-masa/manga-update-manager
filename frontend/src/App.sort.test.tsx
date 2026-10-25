import {StrictMode} from 'react';
import {act, cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import App from './App';
import {CreateWork, EditWork, ListWorks, LoadWorkSort, SetWorkSort, LoadWorkListMode, SetWorkListMode,
    LoadDisplaySettings, SetRegistrationFormVisible, LoadCoverWarnings, LoadCoverProgress, OpenWork, SelectWorkCoverWithOperation} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';

vi.mock('../wailsjs/go/main/App', () => ({CreateWork: vi.fn(), EditWork: vi.fn(), DeleteWork: vi.fn(), ListWorks: vi.fn(),
    LoadWorkSort: vi.fn(), SetWorkSort: vi.fn(), LoadWorkListMode: vi.fn(), SetWorkListMode: vi.fn(),
    LoadDisplaySettings: vi.fn(), SetRegistrationFormVisible: vi.fn(), LoadCoverWarnings: vi.fn(), LoadCoverProgress: vi.fn(),
    OpenWork: vi.fn(), SelectWorkCoverWithOperation: vi.fn(), RemoveWorkCoverWithOperation: vi.fn()}));
const work = (id: number): main.WorkDTO => ({id, title: `作品${id}`, url: `https://example.com/${id}`, siteName: '', notes: '', thumbnailPath: '',
    createdAt: '2026-10-25T00:00:00+09:00', updatedAt: '2026-10-25T00:00:00+09:00'});
const works = (n: number) => Array.from({length: n}, (_, i) => work(i + 1));
const load = vi.mocked(LoadWorkSort), save = vi.mocked(SetWorkSort), list = vi.mocked(ListWorks);
const select = () => screen.getByRole('combobox', {name: '並び順'}) as HTMLSelectElement;
const choose = (value: string) => fireEvent.change(select(), {target: {value}});
const top = () => within(screen.getByRole('navigation', {name: '一覧上部のページ切替'}));
const current = () => screen.getByRole('navigation', {name: '一覧上部のページ切替'}).querySelector('[aria-current="page"]')?.textContent;
const page = (n: number) => fireEvent.click(top().getByRole('button', {name: `${n}ページ`}));
const titles = () => within(screen.getByRole('region', {name: '登録した作品'})).getAllByRole('heading', {level: 3})
    .map((h) => h.textContent).filter((v) => v?.startsWith('作品'));
function deferred<T>() {
    let resolve!: (value: T) => void, reject!: (cause: unknown) => void;
    const promise = new Promise<T>((yes, no) => {resolve = yes; reject = no;});
    return {promise, resolve, reject};
}
let frames: FrameRequestCallback[];
const scroll = vi.fn();
beforeEach(() => {
    vi.resetAllMocks(); load.mockResolvedValue('registration_asc'); save.mockResolvedValue(undefined);
    list.mockResolvedValue(works(25)); vi.mocked(LoadWorkListMode).mockResolvedValue('cards'); vi.mocked(SetWorkListMode).mockResolvedValue(undefined);
    vi.mocked(LoadDisplaySettings).mockResolvedValue({registrationFormVisible: false});
    vi.mocked(SetRegistrationFormVisible).mockResolvedValue(undefined);
    vi.mocked(LoadCoverWarnings).mockResolvedValue([]); vi.mocked(LoadCoverProgress).mockResolvedValue([]);
    frames = [];
    vi.stubGlobal('requestAnimationFrame', (fn: FrameRequestCallback) => {frames.push(fn); return frames.length;});
    vi.stubGlobal('cancelAnimationFrame', vi.fn());
    Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {configurable: true, value: scroll});
    Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {configurable: true, value: function(this: HTMLDialogElement) {this.open = true;}});
    Object.defineProperty(HTMLDialogElement.prototype, 'close', {configurable: true, value: function(this: HTMLDialogElement) {this.open = false;}});
});
afterEach(() => {
    cleanup(); vi.unstubAllGlobals();
    Reflect.deleteProperty(HTMLElement.prototype, 'scrollIntoView');
    Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal'); Reflect.deleteProperty(HTMLDialogElement.prototype, 'close');
});
async function boot() {
    const view = render(<App />);
    await waitFor(() => expect(select().disabled).toBe(false));
    await screen.findAllByRole('heading', {name: /^作品\d+$/});
    await act(async () => {frames.splice(0).forEach((fn) => fn(0));});
    await act(async () => {frames.splice(0).forEach((fn) => fn(0));});
    return view;
}
function action(id: number, name: string) {
    fireEvent.click(screen.getByRole('button', {name: `作品${id}の作品操作`}));
    fireEvent.click(within(screen.getByRole('group', {name: `作品${id}の作品操作一覧`})).getByRole('button', {name}));
}
async function register() {
    fireEvent.click(screen.getByRole('button', {name: '作品を登録'}));
    fireEvent.change(screen.getByRole('textbox', {name: /作品URL/}), {target: {value: 'https://example.com/new'}});
    fireEvent.change(screen.getByRole('textbox', {name: /タイトル/}), {target: {value: '作品26'}});
    fireEvent.click(screen.getByRole('button', {name: '登録する'}));
}

it.each(['小型表示', '行一覧', 'メモなしカード'])('%sでも全件を整列してからページ分割し、検索で順序を保つ', async (mode) => {
    await boot(); page(2); select().focus(); choose('title_desc');
    expect(current()).toBe('1'); expect(document.activeElement).toBe(select());
    expect(titles().slice(0, 3)).toEqual(['作品25', '作品24', '作品23']);
    fireEvent.click(screen.getByRole('button', {name: mode})); expect(select().value).toBe('title_desc');
    fireEvent.change(screen.getByRole('searchbox'), {target: {value: '作品2'}});
    expect(titles()).toEqual(['作品25', '作品24', '作品23', '作品22', '作品21', '作品20', '作品2']);
    expect(list).toHaveBeenCalledTimes(1);
});
it('復元だけで保存やスクロール・フォーカスを行わず、StrictModeの古い応答を破棄する', async () => {
    const old = deferred<string>(), latest = deferred<string>(); load.mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise);
    render(<StrictMode><App /></StrictMode>); expect(select().disabled).toBe(true);
    await act(async () => {latest.resolve('title_desc');});
    await act(async () => {old.resolve('title_asc');});
    expect(select().value).toBe('title_desc'); expect(titles()[0]).toBe('作品25');
    expect(save).not.toHaveBeenCalled(); expect(scroll).not.toHaveBeenCalled(); expect(document.activeElement).toBe(document.body);
});
it.each(['unknown', 'failure'])('読込%sは登録古い順と安全な警告で続行し、自動修復しない', async (kind) => {
    if (kind === 'unknown') load.mockResolvedValue('future'); else load.mockRejectedValue('private diagnostic');
    await boot(); expect(titles()[0]).toBe('作品1'); expect(select().value).toBe('registration_asc');
    expect(screen.getByRole('alert').textContent).toContain('並び順を読み込めません');
    expect(document.body.textContent).not.toContain('private diagnostic'); expect(save).not.toHaveBeenCalled();
});
it('フォーム・形式と共通の保存列を使い、古い失敗で最新選択を戻さない', async () => {
    const first = deferred<void>(); save.mockReturnValueOnce(first.promise); await boot();
    choose('title_desc'); await waitFor(() => expect(save).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByRole('button', {name: '作品を登録'}));
    fireEvent.click(screen.getByRole('button', {name: '行一覧'})); choose('last_access_asc');
    expect(SetRegistrationFormVisible).not.toHaveBeenCalled(); expect(SetWorkListMode).not.toHaveBeenCalled();
    await act(async () => {first.reject('old private error');});
    await waitFor(() => expect(save).toHaveBeenCalledTimes(2));
    expect(save.mock.calls.map(([v]) => v)).toEqual(['title_desc', 'last_access_asc']);
    expect(SetRegistrationFormVisible).toHaveBeenCalledWith(true); expect(SetWorkListMode).toHaveBeenCalledWith('rows');
    expect(select().value).toBe('last_access_asc'); expect(screen.queryByRole('alert')).toBeNull();
    save.mockRejectedValueOnce('private'); choose('title_asc');
    await screen.findByText(/並び順を保存できません/); expect(select().value).toBe('title_asc');
    choose('registration_desc'); await waitFor(() => expect(screen.queryByRole('alert')).toBeNull());
});
it('アンマウント後は待機中の追加保存を始めず、再起動相当では順序だけ復元する', async () => {
    const first = deferred<void>(); save.mockReturnValueOnce(first.promise); const view = await boot();
    choose('title_desc'); await waitFor(() => expect(save).toHaveBeenCalledTimes(1)); choose('title_asc'); view.unmount();
    await act(async () => {first.resolve();}); expect(save).toHaveBeenCalledTimes(1);
    load.mockResolvedValue('title_desc'); await boot(); expect(select().value).toBe('title_desc'); expect(current()).toBe('1');
    expect((screen.getByRole('searchbox') as HTMLInputElement).value).toBe('');
});
it.each([true, false])('登録応答と初期並び順の復元が前後しても復元後の作品ページへ移動する（復元先行=%s）', async (restoreFirst) => {
    const order = deferred<string>(), insert = deferred<main.WorkDTO>(); load.mockReturnValue(order.promise);
    vi.mocked(CreateWork).mockReturnValue(insert.promise); render(<App />);
    await screen.findByRole('button', {name: '作品を登録'}); await register(); list.mockResolvedValue(works(26));
    if (restoreFirst) await act(async () => {order.resolve('registration_desc');});
    await act(async () => {insert.resolve(work(26));});
    if (!restoreFirst) await act(async () => {order.resolve('registration_desc');});
    await waitFor(() => expect(titles()[0]).toBe('作品26')); expect(current()).toBe('1');
});
it('登録中の後続並び順変更とページ移動を遅い登録応答で取り消さない', async () => {
    const insert = deferred<main.WorkDTO>(); vi.mocked(CreateWork).mockReturnValue(insert.promise); await boot(); await register();
    choose('registration_desc'); page(2); list.mockResolvedValue(works(26));
    await act(async () => {insert.resolve(work(26));}); await waitFor(() => expect(list).toHaveBeenCalledTimes(2));
    expect(select().value).toBe('registration_desc'); expect(current()).toBe('2');
});
it('日時保存後の整列で作品が別ページへ移っても現在ページと成功通知を残し、古い一覧で戻さない', async () => {
    const values = works(25).map((w) => ({...w, lastAccessedAt: `2026-10-25T00:00:${String(w.id).padStart(2, '0')}Z`}));
    list.mockResolvedValue(values); load.mockResolvedValue('last_access_asc');
    const pending = deferred<main.WorkDTO>(), stale = deferred<main.WorkDTO[]>(); vi.mocked(OpenWork).mockReturnValue(pending.promise); await boot();
    list.mockReturnValueOnce(stale.promise); fireEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    fireEvent.click(screen.getByRole('button', {name: '作品1のURLを開く'}));
    await act(async () => {pending.resolve({...values[0], lastAccessedAt: '2026-10-26T00:00:00Z'});});
    expect(current()).toBe('1'); expect(screen.queryByRole('button', {name: '作品1のURLを開く'})).toBeNull();
    const notice = screen.getByRole('region', {name: '作品1のURL操作'}); expect(notice.textContent).toContain('保存しました');
    await act(async () => {stale.resolve(values);}); expect(titles()[0]).toBe('作品2');
    fireEvent.click(within(notice).getByRole('button')); expect(document.activeElement).toBe(screen.getByRole('searchbox'));
});
it('保存失敗では日時と順序を変えず、並び順変更後も処理と失敗通知を保持する', async () => {
    load.mockResolvedValue('last_access_desc'); const pending = deferred<main.WorkDTO>(); vi.mocked(OpenWork).mockReturnValue(pending.promise); await boot();
    fireEvent.click(screen.getByRole('button', {name: '作品1のURLを開く'})); choose('registration_desc');
    expect(screen.getByText('「作品1」のページを開いています…')).toBeTruthy();
    await act(async () => {pending.reject({code: 'access_save_failed', message: 'ページを開きましたが保存できませんでした'});});
    choose('last_access_desc'); expect(titles()[0]).toBe('作品1'); expect(screen.getByRole('alert').textContent).toContain('保存できません');
});
it('タイトル編集後は現在ページを保って整列し、モーダル中の背景の並び順変更を拒否する', async () => {
    load.mockResolvedValue('title_asc'); await boot(); page(2); action(13, '作品13を編集');
    choose('title_desc'); expect(select().value).toBe('title_asc'); expect(save).not.toHaveBeenCalled();
    const dialog = within(screen.getByRole('dialog')); vi.mocked(EditWork).mockResolvedValue(new main.EditWorkResult({work: {...work(13), title: 'AAA'}, warnings: []}));
    fireEvent.change(dialog.getByRole('textbox', {name: /タイトル/}), {target: {value: 'AAA'}});
    fireEvent.click(dialog.getByRole('button', {name: '保存する'}));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull()); expect(current()).toBe('2');
    expect(screen.queryByRole('heading', {name: 'AAA'})).toBeNull(); expect(screen.getByText(/「AAA」を保存しました/)).toBeTruthy();
});
it('画像処理中に整列しても二重操作を防ぎ、非表示作品の結果と警告を残す', async () => {
    const pending = deferred<main.SelectWorkCoverResult>(); vi.mocked(SelectWorkCoverWithOperation).mockReturnValue(pending.promise); await boot();
    action(1, '画像を設定'); await waitFor(() => expect(SelectWorkCoverWithOperation).toHaveBeenCalledTimes(1)); choose('registration_desc');
    expect(screen.getByText('画像を処理しています…')).toBeTruthy();
    await act(async () => {pending.resolve({cancelled: false, work: {...work(1), thumbnailPath: 'covers/test.png'}, warnings: [{code: 'cover_cleanup_failed', message: '画像の整理を確認してください'}]} as main.SelectWorkCoverResult);});
    expect(screen.getByText('画像の整理を確認してください')).toBeTruthy(); choose('registration_asc');
    expect(screen.getByText('画像の整理を確認してください')).toBeTruthy();
});
