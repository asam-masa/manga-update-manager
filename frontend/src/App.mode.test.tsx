import {act, fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import {StrictMode} from 'react';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import App from './App';
import {ListWorks, LoadDisplaySettings, SetRegistrationFormVisible, LoadWorkListMode, SetWorkListMode,
    OpenWork, LoadCoverWarnings, LoadCoverProgress, SelectWorkCoverWithOperation} from '../wailsjs/go/main/App';
import type {main} from '../wailsjs/go/models';

vi.mock('../wailsjs/go/main/App', () => ({LoadShowFutureSchedules: async () => true, SetShowFutureSchedules: async () => {}, LoadWorkSort: async () => 'registration_asc', SetWorkSort: async () => {}, CreateWork: vi.fn(), ListWorks: vi.fn(), OpenWork: vi.fn(),
    LoadDisplaySettings: vi.fn(), SetRegistrationFormVisible: vi.fn(), LoadWorkListMode: vi.fn(), SetWorkListMode: vi.fn(),
    LoadCoverWarnings: vi.fn(), LoadCoverProgress: vi.fn(), SelectWorkCoverWithOperation: vi.fn(), RemoveWorkCoverWithOperation: vi.fn()}));
const work: main.WorkDTO = {id: 1, title: '作品', url: 'https://example.com/1', siteName: '', notes: '', thumbnailPath: '',
    createdAt: '2026-10-10T00:00:00+09:00', updatedAt: '2026-10-10T00:00:00+09:00'};
const load = vi.mocked(LoadWorkListMode), save = vi.mocked(SetWorkListMode);
const button = (name: string) => screen.getByRole('button', {name});
const scroll = vi.fn();
const originalScroll = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollIntoView');
let frames: FrameRequestCallback[];
function deferred<T>() {
    let resolve!: (value: T) => void, reject!: (reason: unknown) => void;
    const promise = new Promise<T>((yes, no) => {resolve = yes; reject = no;});
    return {promise, resolve, reject};
}
beforeEach(() => {
    vi.resetAllMocks();
    load.mockResolvedValue('cards'); save.mockResolvedValue(undefined);
    vi.mocked(LoadDisplaySettings).mockResolvedValue({registrationFormVisible: false});
    vi.mocked(SetRegistrationFormVisible).mockResolvedValue(undefined);
    vi.mocked(ListWorks).mockResolvedValue([work]);
    vi.mocked(LoadCoverWarnings).mockResolvedValue([]); vi.mocked(LoadCoverProgress).mockResolvedValue([]);
    Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {configurable: true, value: scroll});
    frames = [];
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {frames.push(callback); return frames.length;});
    vi.stubGlobal('cancelAnimationFrame', vi.fn());
});
afterEach(() => {
    vi.unstubAllGlobals();
    if (originalScroll) Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', originalScroll);
    else Reflect.deleteProperty(HTMLElement.prototype, 'scrollIntoView');
});
async function boot() {
    const view = render(<App />);
    await waitFor(() => expect((button('小型表示') as HTMLButtonElement).disabled).toBe(false));
    await screen.findByRole('heading', {name: '作品'});
    await act(async () => {frames.splice(0).forEach((callback) => callback(0));});
    await act(async () => {frames.splice(0).forEach((callback) => callback(0));});
    return view;
}

it.each([['compact', '小型表示'], ['rows', '行一覧'], ['cards', 'メモなしカード']])('保存済み%sを復元し、自動保存・フォーカス・スクロールは行わない', async (mode, name) => {
    load.mockResolvedValue(mode);
    await boot();
    expect(button(name).getAttribute('aria-pressed')).toBe('true');
    expect(document.activeElement).toBe(document.body);
    expect(scroll).not.toHaveBeenCalled(); expect(save).not.toHaveBeenCalled();
    expect(SetRegistrationFormVisible).not.toHaveBeenCalled();
    fireEvent.click(button(name)); expect(save).not.toHaveBeenCalled();
});

it.each(['failure', 'unknown'])('読込%sではカードと安全な警告で続行し、既存設定を自動修復しない', async (kind) => {
    if (kind === 'failure') load.mockRejectedValue('private diagnostic'); else load.mockResolvedValue('unknown');
    await boot();
    expect(button('メモなしカード').getAttribute('aria-pressed')).toBe('true');
    expect(screen.getByRole('alert').textContent).toContain('読み込めませんでした');
    expect(screen.queryByText('private diagnostic')).toBeNull(); expect(save).not.toHaveBeenCalled();
});

it('読込中は選択を待ち、StrictModeの古い復元応答を破棄する', async () => {
    const old = deferred<string>(), latest = deferred<string>();
    load.mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise);
    render(<StrictMode><App /></StrictMode>);
    expect((button('小型表示') as HTMLButtonElement).disabled).toBe(true);
    await act(async () => {latest.resolve('rows');});
    await act(async () => {old.resolve('compact');});
    expect(button('行一覧').getAttribute('aria-pressed')).toBe('true'); expect(save).not.toHaveBeenCalled();
});

it('形式とフォームの保存を操作順に送り、古い保存失敗で最新の形式や検索を戻さない', async () => {
    const first = deferred<void>(); save.mockReturnValueOnce(first.promise);
    await boot();
    fireEvent.change(screen.getByRole('searchbox'), {target: {value: '作品'}});
    fireEvent.click(button('小型表示'));
    await waitFor(() => expect(save).toHaveBeenCalledWith('compact'));
    fireEvent.click(button('作品を登録'));
    fireEvent.click(button('行一覧'));
    expect(button('行一覧').getAttribute('aria-pressed')).toBe('true');
    expect(SetRegistrationFormVisible).not.toHaveBeenCalled(); expect(save).toHaveBeenCalledTimes(1);
    await act(async () => {first.reject('old error');});
    await waitFor(() => expect(save).toHaveBeenCalledTimes(2));
    expect(save.mock.calls.map(([mode]) => mode)).toEqual(['compact', 'rows']);
    const middleOrder = vi.mocked(SetRegistrationFormVisible).mock.invocationCallOrder[0];
    expect(middleOrder).toBeGreaterThan(save.mock.invocationCallOrder[0]);
    expect(middleOrder).toBeLessThan(save.mock.invocationCallOrder[1]);
    expect((screen.getByRole('searchbox') as HTMLInputElement).value).toBe('作品');
    expect(screen.queryByRole('alert')).toBeNull(); expect(scroll).toHaveBeenCalledTimes(2);
});

it('保存失敗は形式を戻さず、フォームの成功で警告を消さず、次の形式保存で回復する', async () => {
    save.mockRejectedValueOnce('private diagnostic'); await boot();
    fireEvent.click(button('行一覧'));
    expect((await screen.findByRole('alert')).textContent).toContain('表示形式を保存できません');
    expect(button('行一覧').getAttribute('aria-pressed')).toBe('true');
    fireEvent.click(button('作品を登録'));
    await waitFor(() => expect(SetRegistrationFormVisible).toHaveBeenCalledWith(true));
    expect(screen.getByRole('alert').textContent).toContain('表示形式を保存できません');
    fireEvent.click(button('小型表示'));
    await waitFor(() => expect(screen.queryByRole('alert')).toBeNull());
});

it('アンマウント後に待機中の形式保存を開始しない', async () => {
    const first = deferred<void>(); save.mockReturnValueOnce(first.promise);
    const view = await boot(); fireEvent.click(button('小型表示'));
    await waitFor(() => expect(save).toHaveBeenCalledTimes(1));
    fireEvent.click(button('行一覧')); view.unmount();
    await act(async () => {first.resolve();});
    expect(save).toHaveBeenCalledTimes(1);
});

it('URL処理中に形式を切り替えても二重起動を防ぎ、失敗通知を保持する', async () => {
    const pending = deferred<main.WorkDTO>(); vi.mocked(OpenWork).mockReturnValue(pending.promise);
    await boot(); fireEvent.click(button('作品のURLを開く')); fireEvent.click(button('小型表示'));
    expect((button('作品のURLを開く') as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText('「作品」のページを開いています…')).toBeTruthy();
    await act(async () => {pending.reject('private diagnostic');});
    const notice = await screen.findByRole('region', {name: '作品のURL操作'});
    fireEvent.click(button('行一覧'));
    expect(screen.getByRole('region', {name: '作品のURL操作'})).toBe(notice);
    expect(OpenWork).toHaveBeenCalledTimes(1);
});

it('画像処理中の形式変更は操作パネルを閉じ、処理結果と警告を共通欄に残す', async () => {
    const pending = deferred<main.SelectWorkCoverResult>(); vi.mocked(SelectWorkCoverWithOperation).mockReturnValue(pending.promise);
    await boot(); fireEvent.click(button('作品の作品操作'));
    fireEvent.click(within(screen.getByRole('group', {name: '作品の作品操作一覧'})).getByRole('button', {name: '画像を設定'}));
    await waitFor(() => expect(SelectWorkCoverWithOperation).toHaveBeenCalledTimes(1));
    fireEvent.click(button('小型表示'));
    expect(screen.queryByRole('group', {name: '作品の作品操作一覧'})).toBeNull();
    fireEvent.click(button('作品の作品操作'));
    expect((within(screen.getByRole('group', {name: '作品の作品操作一覧'})).getByRole('button', {name: '画像を設定'}) as HTMLButtonElement).disabled).toBe(true);
    await act(async () => {pending.resolve({cancelled: false, work: {...work, thumbnailPath: 'covers/new.png'}, warnings: [{code: 'cover_cleanup_failed', message: '画像の整理を確認してください。'}]} as main.SelectWorkCoverResult);});
    expect(screen.getByText('画像の整理を確認してください。')).toBeTruthy();
    fireEvent.click(button('行一覧'));
    expect(screen.getByText('画像の整理を確認してください。')).toBeTruthy();
    expect(SelectWorkCoverWithOperation).toHaveBeenCalledTimes(1);
});
