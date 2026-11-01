import {StrictMode} from 'react';
import {act, cleanup, renderHook, waitFor} from '@testing-library/react';
import {afterEach, expect, it, vi} from 'vitest';
import {useLibrarySnapshot} from './use-library-snapshot';

afterEach(cleanup);
const fixture = (title = '作品', id = '1') => ({works: [{id, title, url: 'https://example.com/1', siteName: '', notes: '', thumbnailPath: '',
    createdAt: '2026-11-01T10:00:00+09:00', updatedAt: '2026-11-01T10:00:00+09:00', lastAccessedAt: null, scheduledDate: null}],
tags: [{id: '1', name: 'タグ'}], links: [{workID: id, tagID: '1'}]});
function deferred() {
    let resolve!: (value: unknown) => void, reject!: (error: unknown) => void;
    const promise = new Promise<unknown>((yes, no) => {resolve = yes; reject = no;});
    return {promise, resolve, reject};
}
const stoppedError = () => new Error('manga-update-manager:api-error:v1:' + JSON.stringify({code: 'database_stopped', message: 'DB停止。再起動してください。'}));

it('一式の検証と安全な数値変換が成功するまで初回を未取得として保持する', async () => {
    const read = deferred(), load = vi.fn(() => read.promise);
    const {result} = renderHook(() => useLibrarySnapshot(load));
    expect(result.current).toMatchObject({data: null, loading: true, fresh: false, stopped: false});
    await act(async () => {read.resolve(fixture());});
    expect(result.current).toMatchObject({loading: false, fresh: true, stopped: false, error: ''});
    expect(result.current.data?.snapshot.works[0].id).toBe('1');
    expect(result.current.data?.works[0].id).toBe(1);
    expect(load).toHaveBeenCalledTimes(1);
});

it('再取得中と失敗時は前回の一式を残し、診断を表示しない。再試行成功で一式更新する', async () => {
    const next = deferred(), load = vi.fn<() => Promise<unknown>>().mockResolvedValueOnce(fixture()).mockReturnValueOnce(next.promise).mockResolvedValueOnce(fixture('最新'));
    const {result} = renderHook(() => useLibrarySnapshot(load));
    await waitFor(() => expect(result.current.fresh).toBe(true)); const previous = result.current.data;
    let pending!: Promise<unknown>; act(() => {pending = result.current.reload();});
    expect(result.current).toMatchObject({data: previous, loading: true, fresh: false});
    await act(async () => {next.reject(new Error('H:/private/secret.sqlite')); await pending;});
    expect(result.current.data).toBe(previous); expect(result.current.fresh).toBe(false); expect(result.current.error).not.toContain('private');
    await act(async () => {await result.current.reload();});
    expect(result.current.data?.works[0].title).toBe('最新'); expect(result.current.error).toBe('');
});

it.each([null, {works: [], tags: []}, fixture('変換できない', '9007199254740992')])('部分応答や数値変換不能を成功として採用しない (%j)', async (value) => {
    const load = vi.fn<() => Promise<unknown>>().mockResolvedValueOnce(fixture()).mockResolvedValueOnce(value);
    const {result} = renderHook(() => useLibrarySnapshot(load)); await waitFor(() => expect(result.current.fresh).toBe(true));
    const previous = result.current.data;
    await act(async () => {await result.current.reload();});
    expect(result.current.data).toBe(previous); expect(result.current.fresh).toBe(false); expect(result.current.error).not.toBe('');
});

it('初回失敗を空成功にせず、正当な空の成功は区別する', async () => {
    const load = vi.fn<() => Promise<unknown>>().mockRejectedValueOnce(new Error('失敗')).mockResolvedValueOnce({works: [], tags: [], links: []});
    const {result} = renderHook(() => useLibrarySnapshot(load)); await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.data).toBeNull(); expect(result.current.fresh).toBe(false);
    await act(async () => {await result.current.reload();});
    expect(result.current.data?.works).toEqual([]); expect(result.current.fresh).toBe(true);
});

it('取得順序が逆転しても古い成功・通常失敗は新しい一式を上書きしない', async () => {
    const old = deferred(), middle = deferred(), latest = deferred();
    const load = vi.fn<() => Promise<unknown>>().mockReturnValueOnce(old.promise).mockReturnValueOnce(middle.promise).mockReturnValueOnce(latest.promise);
    const {result} = renderHook(() => useLibrarySnapshot(load));
    act(() => {void result.current.reload(); void result.current.reload();});
    await act(async () => {latest.resolve(fixture('最新'));});
    const adopted = result.current.data;
    await act(async () => {old.resolve(fixture('古い')); middle.reject(new Error('古い失敗'));});
    expect(result.current.data).toBe(adopted); expect(result.current.error).toBe(''); expect(result.current.fresh).toBe(true);
});

it('変更確定で進行中の読取を失効し、同一ターンの変更通知を一回の再取得へまとめる', async () => {
    const old = deferred(), latest = deferred();
    const load = vi.fn<() => Promise<unknown>>().mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise);
    const {result} = renderHook(() => useLibrarySnapshot(load));
    await act(async () => {result.current.changed(); result.current.changed();});
    expect(load).toHaveBeenCalledTimes(2); expect(result.current.fresh).toBe(false);
    await act(async () => {old.resolve(fixture('変更前'));});
    expect(result.current.data).toBeNull(); expect(result.current.loading).toBe(true);
    await act(async () => {latest.resolve(fixture('変更後'));});
    expect(result.current.data?.works[0].title).toBe('変更後');
});

it('明示再取得が同一ターンの変更通知を取り込み、余分な予約取得を起こさない', async () => {
    const load = vi.fn().mockResolvedValue(fixture());
    const {result} = renderHook(() => useLibrarySnapshot(load)); await waitFor(() => expect(result.current.fresh).toBe(true));
    await act(async () => {result.current.changed(); await result.current.reload();});
    expect(load).toHaveBeenCalledTimes(2);
});

it('古い要求のDB停止も観測し、遅い成功や再取得・変更通知で停止を解除しない', async () => {
    const old = deferred(), latest = deferred();
    const load = vi.fn<() => Promise<unknown>>().mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise);
    const {result} = renderHook(() => useLibrarySnapshot(load)); act(() => {void result.current.reload();});
    await act(async () => {old.reject(stoppedError());});
    expect(result.current).toMatchObject({stopped: true, fresh: false, loading: false, data: null});
    await act(async () => {latest.resolve(fixture()); await result.current.reload(); result.current.changed();});
    expect(result.current.data).toBeNull(); expect(result.current.stopped).toBe(true); expect(load).toHaveBeenCalledTimes(2);
});

it('最新データを採用した後の遅い停止でもデータを残し、停止はloader切替でも解除しない', async () => {
    const old = deferred(), load = vi.fn<() => Promise<unknown>>().mockReturnValueOnce(old.promise).mockResolvedValueOnce(fixture());
    const replacement = vi.fn().mockResolvedValue(fixture('別loader'));
    const {result, rerender} = renderHook(({loader}) => useLibrarySnapshot(loader), {initialProps: {loader: load}});
    await act(async () => {await result.current.reload();}); const adopted = result.current.data;
    await act(async () => {old.reject(stoppedError());});
    rerender({loader: replacement});
    expect(result.current.data).toBe(adopted); expect(result.current.stopped).toBe(true); expect(replacement).not.toHaveBeenCalled();
});

it('外部の停止通知も同期で受け付け、同じターンの新しい読取を始めない', async () => {
    const load = vi.fn().mockResolvedValue(fixture());
    const {result} = renderHook(() => useLibrarySnapshot(load)); await waitFor(() => expect(result.current.fresh).toBe(true));
    const adopted = result.current.data;
    await act(async () => {result.current.stop(); await result.current.reload(); result.current.changed();});
    expect(result.current.data).toBe(adopted); expect(result.current.stopped).toBe(true); expect(load).toHaveBeenCalledTimes(1);
});

it('StrictModeとloader切替では古い成功を採用せず、unmount後の予約を実行しない', async () => {
    const old = deferred(), current = deferred(), replacement = vi.fn().mockResolvedValue(fixture('切替後'));
    const load = vi.fn<() => Promise<unknown>>().mockReturnValueOnce(old.promise).mockReturnValue(current.promise);
    const {result, rerender, unmount} = renderHook(({loader}) => useLibrarySnapshot(loader), {initialProps: {loader: load}, wrapper: StrictMode});
    expect(load).toHaveBeenCalledTimes(2);
    rerender({loader: replacement}); await waitFor(() => expect(result.current.data?.works[0].title).toBe('切替後'));
    await act(async () => {old.resolve(fixture('最初')); current.resolve(fixture('以前'));});
    expect(result.current.data?.works[0].title).toBe('切替後');
    await act(async () => {result.current.changed(); unmount();});
    expect(replacement).toHaveBeenCalledTimes(1);
});
