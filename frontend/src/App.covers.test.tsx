import {act, fireEvent, render, screen, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import App from './App';
import {ListWorks, OpenWork, RemoveWorkCover, SelectWorkCoverWithOperation} from '../wailsjs/go/main/App';
import {EventsOn} from '../wailsjs/runtime/runtime';
import {main} from '../wailsjs/go/models';

vi.mock('../wailsjs/go/main/App', () => ({CreateWork: vi.fn(), ListWorks: vi.fn(), OpenWork: vi.fn(),
    LoadDisplaySettings: vi.fn().mockResolvedValue({registrationFormVisible: false}), SetRegistrationFormVisible: vi.fn(),
    LoadCoverWarnings: vi.fn().mockResolvedValue([]), SelectWorkCoverWithOperation: vi.fn(), RemoveWorkCover: vi.fn()}));
const base = {url: 'https://example.com/', siteName: '', notes: '', createdAt: '2026-10-19T00:00:00+09:00', updatedAt: '2026-10-19T00:00:00+09:00'};
const first = {...base, id: 1, title: '一', thumbnailPath: 'covers/old.png'};
const second = {...base, id: 2, title: '二', thumbnailPath: 'covers/second.png'};
const third = {...base, id: 3, title: '三', thumbnailPath: ''};
let listener: (...values: unknown[]) => void;
let frames: FrameRequestCallback[];
beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(ListWorks).mockReset().mockResolvedValue([first, second, third]);
    vi.mocked(SelectWorkCoverWithOperation).mockReset();
    vi.mocked(RemoveWorkCover).mockReset();
    vi.mocked(OpenWork).mockReset();
    vi.mocked(EventsOn).mockImplementation((_name, callback) => {listener = callback; return vi.fn();});
    frames = [];
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {frames.push(callback); return frames.length;});
    vi.stubGlobal('cancelAnimationFrame', vi.fn());
});
afterEach(() => vi.unstubAllGlobals());
function deferred<T>() {
    let resolve!: (value: T) => void;
    let reject!: (cause: unknown) => void;
    const promise = new Promise<T>((yes, no) => {resolve = yes; reject = no;});
    return {promise, resolve, reject};
}
async function boot() {
    const view = render(<App />);
    await screen.findByRole('heading', {name: '一'});
    await act(async () => {frames.splice(0).forEach((callback) => callback(0));});
    await act(async () => {frames.splice(0).forEach((callback) => callback(0));});
    return view;
}
const controls = (title: string) => within(screen.getByRole('group', {name: `${title}の画像操作`}));
const select = (title: string, label = '画像を変更') => controls(title).getByRole('button', {name: label});
const remove = (title: string) => controls(title).getByRole('button', {name: '画像を外す'});
const cancelled = {cancelled: true, work: null, warnings: []} as unknown as main.SelectWorkCoverResult;

it('選択枠と作品枠を分け、古い通知と同一tickの二重操作を無視する', async () => {
    const a = deferred<main.SelectWorkCoverResult>();
    const b = deferred<main.SelectWorkCoverResult>();
    vi.mocked(SelectWorkCoverWithOperation).mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise);
    vi.mocked(RemoveWorkCover).mockResolvedValue(new main.RemoveWorkCoverResult({work: {...second, thumbnailPath: ''}, warnings: []}));
    await boot();
    act(() => {fireEvent.click(select('一')); fireEvent.click(select('一'));});
    expect(SelectWorkCoverWithOperation).toHaveBeenCalledTimes(1);
    const id = vi.mocked(SelectWorkCoverWithOperation).mock.calls[0][1];
    expect(id).toMatch(/^[0-9a-f]{32}$/);
    expect(select('二').hasAttribute('disabled')).toBe(true);
    expect(remove('一').hasAttribute('disabled')).toBe(true);
    expect(remove('二').hasAttribute('disabled')).toBe(false);
    expect(screen.getByRole('button', {name: '一のURLを開く'}).hasAttribute('disabled')).toBe(false);
    await act(async () => listener({workID: 2, operationID: id}));
    expect(select('二').hasAttribute('disabled')).toBe(true);
    await act(async () => listener({workID: 1, operationID: id}));
    expect(select('二').hasAttribute('disabled')).toBe(false);
    expect(select('一').hasAttribute('disabled')).toBe(true);
    await userEvent.click(select('二'));
    await act(async () => {listener({workID: 1, operationID: id}); a.resolve(cancelled);});
    expect(select('三', '画像を設定').hasAttribute('disabled')).toBe(true);
    await act(async () => b.resolve(cancelled)); // Missing event falls back to Promise completion.
    expect(select('三', '画像を設定').hasAttribute('disabled')).toBe(false);
    expect(screen.queryByText('画像を設定しました。')).toBeNull();
});

it('別作品の解除は選択中も可能で、成功と後処理警告を分ける', async () => {
    const a = deferred<main.SelectWorkCoverResult>();
    vi.mocked(SelectWorkCoverWithOperation).mockReturnValue(a.promise);
    vi.mocked(RemoveWorkCover).mockResolvedValue(new main.RemoveWorkCoverResult({work: {...second, thumbnailPath: ''}, warnings: [{code: 'cover_cleanup_failed', message: '旧画像の後処理を確認できませんでした。'}]}));
    await boot();
    await userEvent.click(select('一'));
    await userEvent.click(remove('二'));
    expect(RemoveWorkCover).toHaveBeenCalledWith(2);
    expect(await screen.findByText('画像を外しました。')).toBeTruthy();
    expect(screen.getByText('旧画像の後処理を確認できませんでした。')).toBeTruthy();
    expect(select('二', '画像を設定').hasAttribute('disabled')).toBe(true);
    await userEvent.click(controls('二').getByRole('button', {name: '画像操作のお知らせを閉じる'}));
    expect(document.activeElement).toBe(screen.getByRole('group', {name: '二の画像操作'}));
    await act(async () => a.resolve(cancelled));
});

it('検索で隠しても結果を保持し、検索とフォーカスを変えず、古い一覧とアクセス結果で画像を戻さない', async () => {
    const a = deferred<main.SelectWorkCoverResult>();
    const list = deferred<main.WorkDTO[]>();
    const access = deferred<main.WorkDTO>();
    vi.mocked(SelectWorkCoverWithOperation).mockReturnValue(a.promise);
    vi.mocked(OpenWork).mockReturnValue(access.promise);
    const unset = {...first, thumbnailPath: ''};
    vi.mocked(ListWorks).mockResolvedValue([unset, second, third]);
    const view = await boot();
    await userEvent.click(screen.getByRole('button', {name: '一のURLを開く'}));
    vi.mocked(ListWorks).mockReturnValueOnce(list.promise);
    await userEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    await userEvent.click(select('一', '画像を設定'));
    const search = screen.getByRole('searchbox', {name: '漫画名で検索'});
    await userEvent.type(search, '二');
    await act(async () => a.resolve(new main.SelectWorkCoverResult({cancelled: false, work: {...first, thumbnailPath: 'covers/new.png'}, warnings: []})));
    expect(screen.queryByRole('heading', {name: '一'})).toBeNull();
    expect(document.activeElement).toBe(search);
    await act(async () => {list.resolve([unset, second, third]); access.resolve({...unset, lastAccessedAt: '2026-10-19T10:00:00+09:00'});});
    await userEvent.clear(search);
    expect(await screen.findByText('画像を設定しました。')).toBeTruthy();
    // Removing proves the saved path survived both stale responses.
    expect(remove('一')).toBeTruthy();
    expect(view.container.querySelectorAll('img')).toHaveLength(2);
    expect(screen.getByText('2026/10/19 10:00:00')).toBeTruthy();
});

it('解除失敗では設定済み操作を維持し、生の内部エラーを表示しない', async () => {
    vi.mocked(RemoveWorkCover).mockRejectedValue(new Error('C:\\secret\\source.png'));
    await boot();
    await userEvent.click(remove('一'));
    expect(await screen.findByText(/画像の操作結果を取得できませんでした/)).toBeTruthy();
    expect(remove('一')).toBeTruthy();
    expect(document.body.textContent).not.toContain('secret');
    vi.mocked(RemoveWorkCover).mockRejectedValueOnce({code: 'cover_unavailable', message: '画像機能を利用できません。再起動してください。'});
    await userEvent.click(remove('一'));
    expect(await screen.findByText('画像機能を利用できません。再起動してください。')).toBeTruthy();
    await userEvent.click(controls('一').getByRole('button', {name: '画像操作のお知らせを閉じる'}));
    expect(document.activeElement).toBe(select('一'));
});

it('同作品の次の選択を前回の遅延通知では解除せず、アンマウント時に購読を解除する', async () => {
    const a = deferred<main.SelectWorkCoverResult>();
    const b = deferred<main.SelectWorkCoverResult>();
    vi.mocked(SelectWorkCoverWithOperation).mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise);
    const off = vi.fn();
    vi.mocked(EventsOn).mockImplementation((_name, callback) => {listener = callback; return off;});
    const view = await boot();
    select('一').focus();
    await userEvent.keyboard('{Enter}');
    const old = vi.mocked(SelectWorkCoverWithOperation).mock.calls[0][1];
    await act(async () => a.resolve(cancelled));
    await userEvent.click(select('一'));
    await act(async () => listener({workID: 1, operationID: old}));
    expect(select('二').hasAttribute('disabled')).toBe(true);
    view.unmount();
    expect(off).toHaveBeenCalledTimes(1);
    await act(async () => {listener({workID: 1, operationID: old}); b.resolve(cancelled);});
});

it('画像変更の遅い応答で新しい最終アクセスを戻さず、次の操作で前回通知を消す', async () => {
    const a = deferred<main.SelectWorkCoverResult>();
    vi.mocked(SelectWorkCoverWithOperation).mockReturnValue(a.promise);
    vi.mocked(OpenWork).mockResolvedValue({...first, lastAccessedAt: '2026-10-19T12:00:00+09:00'});
    await boot();
    await userEvent.click(select('一'));
    await userEvent.click(screen.getByRole('button', {name: '一のURLを開く'}));
    await act(async () => a.resolve(new main.SelectWorkCoverResult({cancelled: false, work: first, warnings: []})));
    expect(screen.getByText('2026/10/19 12:00:00')).toBeTruthy();
    expect(screen.getByText('画像を設定しました。')).toBeTruthy();
    vi.mocked(SelectWorkCoverWithOperation).mockResolvedValueOnce(cancelled);
    await userEvent.click(select('一'));
    expect(screen.queryByText('画像を設定しました。')).toBeNull();
});

it.each(['cover_outcome_unknown', 'database_stopped', 'cover_unavailable'])('停止・結果不明 %s では旧表示を保持し、成功扱いや自動再試行をしない', async (code) => {
    const message = `${code}: アプリを再起動して状態を確認してください。`;
    vi.mocked(SelectWorkCoverWithOperation).mockRejectedValue(new Error('manga-update-manager:api-error:v1:' + JSON.stringify({code, message})));
    const view = await boot();
    const oldSource = view.container.querySelector('img')!.getAttribute('src');
    const listCalls = vi.mocked(ListWorks).mock.calls.length;
    const search = screen.getByRole('searchbox', {name: '漫画名で検索'});
    await userEvent.type(search, '一');
    await userEvent.click(select('一'));
    expect(await screen.findByText(message)).toBeTruthy();
    expect(screen.queryByText('画像を設定しました。')).toBeNull();
    expect(view.container.querySelector('img')!.getAttribute('src')).toBe(oldSource);
    expect((search as HTMLInputElement).value).toBe('一');
    expect(SelectWorkCoverWithOperation).toHaveBeenCalledTimes(1);
    expect(ListWorks).toHaveBeenCalledTimes(listCalls);
    expect(RemoveWorkCover).not.toHaveBeenCalled();
    expect(select('一').hasAttribute('disabled')).toBe(false);
});

it.each(['cover_release_unconfirmed', 'database_stopped_after_commit'])('確定後の停止警告 %s では新画像と成功を保持する', async (code) => {
    const message = '後始末を確認できません。アプリを再起動して状態を確認してください。';
    vi.mocked(SelectWorkCoverWithOperation).mockResolvedValue(new main.SelectWorkCoverResult({
        cancelled: false, work: {...first, thumbnailPath: 'covers/committed.png'}, warnings: [{code, message}],
    }));
    const view = await boot();
    const oldSource = view.container.querySelector('img')!.getAttribute('src');
    await userEvent.click(select('一'));
    expect(await screen.findByText('画像を設定しました。')).toBeTruthy();
    expect(screen.getByText(message)).toBeTruthy();
    expect(view.container.querySelector('img')!.getAttribute('src')).not.toBe(oldSource);
    expect(SelectWorkCoverWithOperation).toHaveBeenCalledTimes(1);
    expect(RemoveWorkCover).not.toHaveBeenCalled();
});
