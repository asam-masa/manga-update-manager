import {StrictMode} from 'react';
import {act, cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import App from './App';
import {CreateWork, DeleteWork, EditWork, ListWorks, LoadWorkListMode, LoadDisplaySettings, LoadCoverWarnings,
    LoadCoverProgress, OpenWork, SelectWorkCoverWithOperation} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';

vi.mock('../wailsjs/go/main/App', () => ({LoadTags: async () => [], LoadWorkTags: async () => [], LoadShowFutureSchedules: async () => true, SetShowFutureSchedules: async () => {}, LoadWorkSort: async () => 'registration_asc', SetWorkSort: async () => {}, CreateWork: vi.fn(), DeleteWork: vi.fn(), EditWork: vi.fn(), ListWorks: vi.fn(),
    LoadWorkListMode: vi.fn(), SetWorkListMode: vi.fn(), LoadDisplaySettings: vi.fn(), SetRegistrationFormVisible: vi.fn(),
    LoadCoverWarnings: vi.fn(), LoadCoverProgress: vi.fn(), OpenWork: vi.fn(), SelectWorkCoverWithOperation: vi.fn(), RemoveWorkCoverWithOperation: vi.fn()}));
const work = (id: number): main.WorkDTO => ({id, title: `作品${id}`, url: `https://example.com/${id}`, siteName: '', notes: '', thumbnailPath: '',
    createdAt: '2026-10-25T00:00:00+09:00', updatedAt: '2026-10-25T00:00:00+09:00'});
const works = (count: number) => Array.from({length: count}, (_, i) => work(i + 1));
const list = vi.mocked(ListWorks);
const top = () => within(screen.getByRole('navigation', {name: '一覧上部のページ切替'}));
const bottom = () => within(screen.getByRole('navigation', {name: '一覧下部のページ切替'}));
const page = (n: number) => top().getByRole('button', {name: `${n}ページ`});
const current = () => screen.getByRole('navigation', {name: '一覧上部のページ切替'}).querySelector('[aria-current="page"]');
const scroll = vi.fn();
const originalScroll = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollIntoView');
let frames: FrameRequestCallback[];
function deferred<T>() {
    let resolve!: (value: T) => void, reject!: (error: unknown) => void;
    const promise = new Promise<T>((yes, no) => {resolve = yes; reject = no;});
    return {promise, resolve, reject};
}
beforeEach(() => {
    vi.resetAllMocks();
    list.mockResolvedValue(works(41)); vi.mocked(LoadWorkListMode).mockResolvedValue('cards');
    vi.mocked(LoadDisplaySettings).mockResolvedValue({registrationFormVisible: false});
    vi.mocked(LoadCoverWarnings).mockResolvedValue([]); vi.mocked(LoadCoverProgress).mockResolvedValue([]);
    frames = [];
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {frames.push(callback); return frames.length;});
    vi.stubGlobal('cancelAnimationFrame', vi.fn());
    Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {configurable: true, value: scroll});
    Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {configurable: true, value: function(this: HTMLDialogElement) {this.open = true;}});
    Object.defineProperty(HTMLDialogElement.prototype, 'close', {configurable: true, value: function(this: HTMLDialogElement) {this.open = false;}});
});
afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    if (originalScroll) Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', originalScroll);
    else Reflect.deleteProperty(HTMLElement.prototype, 'scrollIntoView');
    Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal'); Reflect.deleteProperty(HTMLDialogElement.prototype, 'close');
});
async function boot(strict = false) {
    const view = render(strict ? <StrictMode><App /></StrictMode> : <App />);
    await screen.findByRole('heading', {name: '作品1'});
    await waitFor(() => expect((page(1) as HTMLButtonElement).disabled).toBe(false));
    await act(async () => {frames.splice(0).forEach((fn) => fn(0));});
    await act(async () => {frames.splice(0).forEach((fn) => fn(0));});
    return view;
}
function action(id: number, name: string) {
    fireEvent.click(screen.getByRole('button', {name: `作品${id}の作品操作`}));
    fireEvent.click(within(screen.getByRole('group', {name: `作品${id}の作品操作一覧`})).getByRole('button', {name}));
}
async function register(title: string) {
    fireEvent.click(screen.getByRole('button', {name: '作品を登録'}));
    fireEvent.change(screen.getByRole('textbox', {name: /作品URL/}), {target: {value: 'https://example.com/new'}});
    fireEvent.change(screen.getByRole('textbox', {name: /タイトル/}), {target: {value: title}});
    fireEvent.click(screen.getByRole('button', {name: '登録する'}));
}

it.each([['compact', 20, 3], ['rows', 20, 3], ['cards', 12, 4]] as const)('%sは固定%d件で描画し、下側からのキー移動も上側現在ボタンへ戻る', async (mode, size, last) => {
    vi.mocked(LoadWorkListMode).mockResolvedValue(mode); await boot();
    expect(screen.getAllByRole('heading', {level: 3})).toHaveLength(size);
    expect((top().getByRole('button', {name: '前へ'}) as HTMLButtonElement).disabled).toBe(true);
    expect(scroll).not.toHaveBeenCalled();
    const user = userEvent.setup();
    bottom().getByRole('button', {name: '次へ'}).focus(); await user.keyboard('{Enter}');
    expect(document.activeElement).toBe(page(2)); expect(scroll).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('heading', {name: `作品${size + 1}`})).toBeTruthy();
    expect(screen.queryByRole('heading', {name: '作品1'})).toBeNull();
    fireEvent(window, new Event('resize'));
    expect(current()?.textContent).toBe('2');
    fireEvent.click(page(last));
    expect(screen.getByRole('heading', {name: '作品41'})).toBeTruthy();
    expect((bottom().getByRole('button', {name: '次へ'}) as HTMLButtonElement).disabled).toBe(true);
    expect(list).toHaveBeenCalledTimes(1);
});

it('多数ページは番号を省略し、0件では操作を出さない', async () => {
    list.mockResolvedValue(works(600)); await boot();
    expect(top().getAllByRole('button').length).toBeLessThanOrEqual(9);
    fireEvent.click(page(50)); expect(current()?.textContent).toBe('50');
    expect(screen.getByRole('heading', {name: '作品600'})).toBeTruthy();
    fireEvent.change(screen.getByRole('searchbox'), {target: {value: '存在しない'}});
    expect(screen.queryByRole('navigation')).toBeNull();
    expect(screen.getByText('検索条件に一致する作品がありません')).toBeTruthy();
    expect(screen.queryByText(/0 \/ 0ページ/)).toBeNull();
});

it.each([0, 1, 12, 13, 24, 25])('%d件の境界で空一覧と最終ページを区別する', async (count) => {
    list.mockResolvedValue(works(count)); render(<App />);
    await waitFor(() => expect(screen.queryByText('一覧を読み込んでいます…')).toBeNull());
    if (count === 0) {
        expect(screen.getByText('まだ作品が登録されていません')).toBeTruthy(); expect(screen.queryByRole('navigation')).toBeNull();
    } else {
        await waitFor(() => expect((page(1) as HTMLButtonElement).disabled).toBe(false));
        const last = Math.ceil(count / 12); fireEvent.click(page(last));
        expect(screen.getByRole('heading', {name: `作品${count}`})).toBeTruthy();
        expect((top().getByRole('button', {name: '次へ'}) as HTMLButtonElement).disabled).toBe(true);
    }
});

it('検索は全作品を対象とし、IME確定と検索解除は先頭へ戻し、検索欄のフォーカスは維持する', async () => {
    await boot(); fireEvent.click(page(3));
    const search = screen.getByRole('searchbox'); search.focus();
    fireEvent.compositionStart(search); fireEvent.change(search, {target: {value: '作品41'}});
    expect(current()?.textContent).toBe('3');
    expect(screen.getByRole('heading', {name: '作品25'})).toBeTruthy();
    fireEvent.compositionEnd(search, {data: '作品41'});
    expect(current()?.textContent).toBe('1'); expect(screen.getByRole('heading', {name: '作品41'})).toBeTruthy();
    expect(document.activeElement).toBe(search);
    fireEvent.change(search, {target: {value: ''}});
    expect(current()?.textContent).toBe('1'); expect(screen.getByRole('heading', {name: '作品1'})).toBeTruthy();
});

it('形式変更では先頭へ戻り、フォーム開閉と再読込では現在ページを維持する', async () => {
    await boot(); fireEvent.click(page(3));
    fireEvent.click(screen.getByRole('button', {name: '作品を登録'}));
    expect(current()?.textContent).toBe('3');
    fireEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2)); expect(current()?.textContent).toBe('3');
    fireEvent.click(screen.getByRole('button', {name: '行一覧'}));
    expect(current()?.textContent).toBe('1'); expect(screen.getAllByRole('heading', {level: 3})).toHaveLength(20);
});

it('再読込で最終ページが消えたら補正し、後から件数が増えても古いページへ戻らない', async () => {
    await boot(); fireEvent.click(page(4)); list.mockResolvedValue(works(24));
    fireEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    await waitFor(() => expect(current()?.textContent).toBe('2'));
    list.mockResolvedValue(works(41)); fireEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    await waitFor(() => expect(page(4)).toBeTruthy()); expect(current()?.textContent).toBe('2');
});

it('StrictModeの古い一覧応答で件数や現在ページを巻き戻さず、再マウントは1ページ目になる', async () => {
    const old = deferred<main.WorkDTO[]>(); list.mockReturnValueOnce(old.promise);
    const view = await boot(true); fireEvent.click(page(4));
    await act(async () => {old.resolve(works(2));});
    expect(current()?.textContent).toBe('4'); expect(screen.getByRole('heading', {name: '作品41'})).toBeTruthy();
    view.unmount(); await boot(); expect(current()?.textContent).toBe('1');
});

it('検索に一致する登録作品のページへ移動する', async () => {
    list.mockResolvedValue(works(24)); vi.mocked(CreateWork).mockResolvedValue(work(25)); await boot();
    fireEvent.change(screen.getByRole('searchbox'), {target: {value: '作品'}});
    list.mockResolvedValue(works(25)); await register('作品25');
    await screen.findByRole('heading', {name: '作品25'});
    expect(current()?.textContent).toBe('3'); expect(document.activeElement).toBe(page(3));
    expect((screen.getByRole('searchbox') as HTMLInputElement).value).toBe('作品');
    expect(CreateWork).toHaveBeenCalledTimes(1);
});

it('検索に一致しない登録は現在ページと検索を維持して案内する', async () => {
    list.mockResolvedValue(works(24)); const created = {...work(25), title: '対象外'};
    vi.mocked(CreateWork).mockResolvedValue(created); await boot();
    fireEvent.change(screen.getByRole('searchbox'), {target: {value: '作品'}}); fireEvent.click(page(2));
    list.mockResolvedValue([...works(24), created]); await register('対象外');
    await screen.findByText('作品を登録しました。検索条件に一致しないため非表示です。');
    expect(current()?.textContent).toBe('2'); expect(screen.queryByRole('heading', {name: '対象外'})).toBeNull();
});

it('登録中の後続ページ操作を、遅い登録後の移動で取り消さない', async () => {
    list.mockResolvedValue(works(24)); const pending = deferred<main.WorkDTO>(); vi.mocked(CreateWork).mockReturnValue(pending.promise);
    await boot(); await register('作品25'); fireEvent.click(page(2));
    list.mockResolvedValue(works(25)); await act(async () => {pending.resolve(work(25));});
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2)); expect(current()?.textContent).toBe('2');
    expect(document.activeElement).toBe(page(2));
});

it('登録待機中に表示形式の初期復元が終わっても、復元後の件数で新作品へ移動する', async () => {
    const mode = deferred<string>(), insert = deferred<main.WorkDTO>();
    vi.mocked(LoadWorkListMode).mockReturnValue(mode.promise); list.mockResolvedValue(works(24));
    vi.mocked(CreateWork).mockReturnValue(insert.promise); render(<App />);
    await screen.findByRole('heading', {name: '作品1'}); await screen.findByRole('button', {name: '作品を登録'});
    await register('作品25');
    await act(async () => {mode.resolve('rows');});
    list.mockResolvedValue(works(25)); await act(async () => {insert.resolve(work(25));});
    await waitFor(() => expect(current()?.textContent).toBe('2'));
    expect(screen.getByRole('heading', {name: '作品25'})).toBeTruthy();
});

it('登録応答が先に来た場合も、形式復元を待って新作品のページへ移動する', async () => {
    const mode = deferred<string>(); vi.mocked(LoadWorkListMode).mockReturnValue(mode.promise);
    list.mockResolvedValue(works(24)); vi.mocked(CreateWork).mockResolvedValue(work(25)); render(<App />);
    await screen.findByRole('heading', {name: '作品1'}); await screen.findByRole('button', {name: '作品を登録'});
    list.mockResolvedValue(works(25)); await register('作品25');
    await screen.findByText('作品を登録しました。'); expect(current()?.textContent).toBe('1');
    await act(async () => {mode.resolve('rows');});
    await waitFor(() => expect(current()?.textContent).toBe('2'));
    expect(screen.getByRole('heading', {name: '作品25'})).toBeTruthy();
});

it('登録成功後の一覧取得失敗ではページを戻さず再登録も行わない', async () => {
    list.mockResolvedValue(works(24)); vi.mocked(CreateWork).mockResolvedValue(work(25)); await boot(); fireEvent.click(page(2));
    list.mockRejectedValue('private diagnostic'); await register('作品25');
    await screen.findByText('作品一覧を取得できません。再読み込みをお試しください。');
    expect(current()?.textContent).toBe('2'); expect(CreateWork).toHaveBeenCalledTimes(1);
    expect(screen.getByText('作品を登録しました。')).toBeTruthy();
});

it('モーダル開始後は背景からのページ変更を受け付けない', async () => {
    await boot(); action(1, '作品1を編集');
    fireEvent.click(top().getByRole('button', {name: '次へ'}));
    expect(current()?.textContent).toBe('1'); expect(screen.getByRole('dialog')).toBeTruthy();
});

it('最終ページの削除は他作品があれば維持し、空になった時だけ最後へ補正する', async () => {
    list.mockResolvedValue(works(26)); vi.mocked(DeleteWork).mockImplementation(async (id) => new main.DeleteWorkResult({workID: id, warnings: []}));
    await boot(); fireEvent.click(page(3)); action(26, '作品26を削除');
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', {name: '削除する'}));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull()); expect(current()?.textContent).toBe('3');
    action(25, '作品25を削除'); fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', {name: '削除する'}));
    await waitFor(() => expect(current()?.textContent).toBe('2'));
    expect(screen.getByRole('region', {name: '作品25の削除結果'})).toBeTruthy();
    expect(screen.getByRole('region', {name: '作品26の削除結果'})).toBeTruthy();
});

it('編集で検索から外れた最後の作品を除き、補正と通知を維持する', async () => {
    list.mockResolvedValue(works(25)); vi.mocked(EditWork).mockResolvedValue(new main.EditWorkResult({work: {...work(25), title: '別名'}, warnings: []}));
    await boot(); fireEvent.change(screen.getByRole('searchbox'), {target: {value: '作品'}}); fireEvent.click(page(3));
    action(25, '作品25を編集'); await waitFor(() => expect((within(screen.getByRole('dialog')).getByRole('button', {name: '保存する'}) as HTMLButtonElement).disabled).toBe(false)); fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', {name: '保存する'}));
    await waitFor(() => expect(current()?.textContent).toBe('2'));
    expect(screen.getByText('「別名」を保存しました。検索条件に一致しないため非表示です。')).toBeTruthy();
});

it('URL処理中のページ往復でも二重受付を防ぎ、非表示の失敗通知を閉じると検索へ戻る', async () => {
    const pending = deferred<main.WorkDTO>(); vi.mocked(OpenWork).mockReturnValue(pending.promise); await boot();
    fireEvent.click(screen.getByRole('button', {name: '作品1のURLを開く'})); fireEvent.click(page(2));
    expect(screen.getByText('「作品1」のページを開いています…')).toBeTruthy();
    fireEvent.click(page(1)); expect((screen.getByRole('button', {name: '作品1のURLを開く'}) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(page(2)); await act(async () => {pending.reject('private diagnostic');});
    const notice = screen.getByRole('region', {name: '作品1のURL操作'});
    fireEvent.click(within(notice).getByRole('button')); expect(document.activeElement).toBe(screen.getByRole('searchbox'));
    expect(OpenWork).toHaveBeenCalledTimes(1); expect(current()?.textContent).toBe('2');
});

it('画像処理の非表示ページで完了しても警告を残し、戻ると更新済み画像操作になる', async () => {
    const pending = deferred<main.SelectWorkCoverResult>(); vi.mocked(SelectWorkCoverWithOperation).mockReturnValue(pending.promise); await boot();
    action(1, '画像を設定'); fireEvent.click(page(2));
    expect(screen.getByText('画像を処理しています…')).toBeTruthy();
    await act(async () => {pending.resolve({cancelled: false, work: {...work(1), thumbnailPath: 'covers/new.png'}, warnings: [{code: 'cover_cleanup_failed', message: '合成整理警告'}]} as main.SelectWorkCoverResult);});
    expect(screen.getByText('合成整理警告')).toBeTruthy(); fireEvent.click(page(1));
    fireEvent.click(screen.getByRole('button', {name: '作品1の作品操作'}));
    expect(screen.getByRole('button', {name: '画像を変更'})).toBeTruthy();
    expect(screen.getByText('合成整理警告')).toBeTruthy(); expect(SelectWorkCoverWithOperation).toHaveBeenCalledTimes(1);
});
