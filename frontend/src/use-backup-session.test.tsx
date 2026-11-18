import {StrictMode} from 'react';
import {act, cleanup, renderHook} from '@testing-library/react';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import {useBackupSession} from './use-backup-session';

beforeEach(() => vi.useFakeTimers());
afterEach(() => {cleanup(); vi.useRealTimers();});
const flush = () => act(async () => {});
const tick = () => act(async () => {await vi.advanceTimersByTimeAsync(1000);});
const idle = {executionId: '', state: 'idle', stage: 'unobserved', elapsedSeconds: 0, canCancel: false};
const running = (id = '1') => ({...idle, executionId: id, state: 'running', canCancel: true});
const finished = (id = '1') => ({...running(id), state: 'finished', canCancel: false, result: {
    publication: 'confirmed', hasWarnings: false, omittedImageCount: 0, workNamesState: 'available', workCount: 0}});
const saved = (id = '1', retryAllowed = true) => ({executionId: id, outcome: 'saved', retryAllowed,
    cancellationObserved: false, message: '保存しました。', notices: []});
function deferred() {
    let resolve!: (v: unknown) => void, reject!: (v: unknown) => void;
    const promise = new Promise<unknown>((yes, no) => {resolve = yes; reject = no;});
    return {promise, resolve, reject};
}
function api() {
    return {status: vi.fn<() => Promise<unknown>>().mockResolvedValue(idle),
        result: vi.fn<(id: string) => Promise<unknown>>().mockResolvedValue(saved()),
        start: vi.fn<() => Promise<unknown>>().mockResolvedValue({cancelled: false, executionId: '1'}),
        cancel: vi.fn<(id: string) => Promise<unknown>>().mockResolvedValue({executionId: '1', decision: 'accepted'})};
}

it('マウントと定期取得だけでは開始・取消を送らない', async () => {
    const a = api(); const {result} = renderHook(() => useBackupSession(a), {wrapper: StrictMode}); await flush(); await tick();
    expect(result.current.canStart).toBe(true); expect(a.start).not.toHaveBeenCalled(); expect(a.cancel).not.toHaveBeenCalled();
});

it('選択中の同一ターン連打を同期的に止め、選択取消で前回結果を保持する', async () => {
    const a = api(), pending = deferred(); a.status.mockResolvedValue(finished()); a.start.mockReturnValue(pending.promise);
    const {result} = renderHook(() => useBackupSession(a)); await flush(); const previous = result.current.result;
    act(() => {void result.current.start(); void result.current.start(); void result.current.cancel();});
    expect(a.start).toHaveBeenCalledTimes(1); expect(a.cancel).not.toHaveBeenCalled(); expect(result.current.selecting).toBe(true);
    await act(async () => {pending.resolve({cancelled: true, executionId: ''});});
    expect(result.current.result).toBe(previous); expect(result.current.canStart).toBe(true);
});

it('開始受付後は応答後の同ID観測を待ち、前回のidleで二重開始しない', async () => {
    const a = api(), current = deferred(); a.status.mockResolvedValueOnce(idle).mockReturnValue(current.promise);
    const {result} = renderHook(() => useBackupSession(a)); await flush();
    await act(async () => {await result.current.start(); await result.current.start();});
    expect(a.start).toHaveBeenCalledTimes(1); expect(result.current).toMatchObject({reconciling: true, canStart: false, canCancel: false});
    await act(async () => {current.resolve(running());});
    expect(result.current).toMatchObject({reconciling: false, canStart: false, canCancel: true});
});

it('開始中の古い取得を応答後に採用せず、その後の取得へ直列に進む', async () => {
    const a = api(), old = deferred(), latest = deferred(), start = deferred();
    a.start.mockReturnValue(start.promise); a.status.mockResolvedValueOnce(idle).mockReturnValueOnce(old.promise).mockReturnValue(latest.promise);
    const {result} = renderHook(() => useBackupSession(a)); await flush();
    act(() => {void result.current.start();}); await tick();
    await act(async () => {start.resolve({cancelled: false, executionId: '1'});});
    expect(a.status).toHaveBeenCalledTimes(2);
    await act(async () => {old.resolve(idle);});
    expect(result.current.canStart).toBe(false); expect(result.current.fresh).toBe(false); expect(a.status).toHaveBeenCalledTimes(3);
    await act(async () => {latest.resolve(running());}); expect(result.current.canCancel).toBe(true);
});

it.each(['reject', 'malformed'])('開始の%sは診断を出さず状況を確認し、自動再送しない', async (mode) => {
    const a = api(), latest = deferred(); a.status.mockResolvedValueOnce(idle).mockReturnValue(latest.promise);
    if (mode === 'reject') a.start.mockRejectedValue(new Error('H:/private/secret.sqlite')); else a.start.mockResolvedValue({});
    const {result} = renderHook(() => useBackupSession(a)); await flush();
    await act(async () => {await result.current.start();});
    expect(result.current.canStart).toBe(false); expect(result.current.commandError).not.toContain('private');
    await act(async () => {latest.resolve(finished());});
    expect(result.current.result?.outcome).toBe('saved'); expect(a.start).toHaveBeenCalledTimes(1);
});

it('再取得失敗中は操作を止め、次の成功で再開判断を取り直す', async () => {
    const a = api(); a.start.mockRejectedValue(new Error('応答欠落'));
    a.status.mockResolvedValueOnce(idle).mockRejectedValueOnce(new Error('読取失敗')).mockResolvedValue(idle);
    const {result} = renderHook(() => useBackupSession(a)); await flush();
    await act(async () => {await result.current.start();}); expect(result.current.canStart).toBe(false);
    await tick(); expect(result.current.canStart).toBe(true); expect(a.start).toHaveBeenCalledTimes(1);
});

it.each(['accepted', 'already_accepted', 'publication_started', 'finished'])('取消の%sを完了と置き換えず再観測する', async (decision) => {
    const a = api(), latest = deferred(); a.status.mockResolvedValueOnce(running()).mockReturnValue(latest.promise);
    a.cancel.mockResolvedValue({executionId: '1', decision});
    const {result} = renderHook(() => useBackupSession(a)); await flush();
    await act(async () => {await result.current.cancel(); await result.current.cancel();});
    expect(a.cancel).toHaveBeenCalledTimes(1); expect(a.cancel).toHaveBeenCalledWith('1');
    expect(result.current).toMatchObject({cancelDecision: decision, reconciling: true, canCancel: false});
    await act(async () => {latest.resolve({...running(), state: 'cancelling', canCancel: false});});
    expect(result.current.status?.state).toBe('cancelling'); expect(result.current.result).toBeNull();
});

it('別IDの取消応答を受付済みと扱わず、自動で取消を再送しない', async () => {
    const a = api(); a.status.mockResolvedValue(running()); a.cancel.mockResolvedValue({executionId: '2', decision: 'accepted'});
    const {result} = renderHook(() => useBackupSession(a)); await flush();
    await act(async () => {await result.current.cancel();}); await tick();
    expect(result.current.cancelDecision).toBeNull(); expect(result.current.commandError).toContain('取消結果を確認できません');
    expect(a.cancel).toHaveBeenCalledTimes(1);
});

it('取消の通信失敗でも自動再送せず、公開開始後の状況を確認する', async () => {
    const a = api(); a.status.mockResolvedValueOnce(running()).mockResolvedValue({...running(), state: 'publishing', canCancel: false});
    a.cancel.mockRejectedValue(new Error('private diagnostic'));
    const {result} = renderHook(() => useBackupSession(a)); await flush();
    await act(async () => {await result.current.cancel();});
    expect(result.current.commandError).not.toContain('diagnostic'); expect(result.current.canCancel).toBe(false);
    await tick(); expect(a.cancel).toHaveBeenCalledTimes(1); expect(result.current.status?.state).toBe('publishing');
});

it('API取得元の変更後に旧開始応答が返っても現行の取得を無効化しない', async () => {
    const oldAPI = api(), nextAPI = api(), pending = deferred(); oldAPI.start.mockReturnValue(pending.promise);
    const {result, rerender} = renderHook(({source}) => useBackupSession(source), {initialProps: {source: oldAPI}}); await flush();
    act(() => {void result.current.start();}); rerender({source: nextAPI}); await flush();
    const revision = result.current.revision;
    await act(async () => {pending.resolve({cancelled: false, executionId: '1'});});
    expect(result.current).toMatchObject({revision, canStart: true, selecting: false}); expect(nextAPI.status).toHaveBeenCalledTimes(1);
});

it('再実行不可と取得未確認からは開始できない', async () => {
    const a = api(); a.status.mockResolvedValue(finished()); a.result.mockResolvedValue(saved('1', false));
    const {result} = renderHook(() => useBackupSession(a)); await flush();
    await act(async () => {await result.current.start();}); expect(a.start).not.toHaveBeenCalled();
    a.status.mockRejectedValue(new Error('取得失敗')); await tick();
    await act(async () => {await result.current.cancel();}); expect(a.cancel).not.toHaveBeenCalled();
});

it('応答後に前回IDが返っても開始受付IDを確認するまで止める', async () => {
    const a = api(); const {result} = renderHook(() => useBackupSession(a)); await flush();
    await act(async () => {await result.current.start();});
    expect(result.current.reconciling).toBe(true); expect(result.current.canStart).toBe(false);
    a.status.mockResolvedValue(running()); await tick(); expect(result.current.reconciling).toBe(false);
});

it.each(['start', 'cancel'] as const)('unmount後の%s応答は再取得や次のmountへ作用しない', async (kind) => {
    const a = api(), pending = deferred(); a[kind].mockReturnValue(pending.promise);
    if (kind === 'cancel') a.status.mockResolvedValue(running());
    const old = renderHook(() => useBackupSession(a)); await flush();
    act(() => {void old.result.current[kind]();}); old.unmount();
    await act(async () => {pending.resolve(kind === 'start' ? {cancelled: false, executionId: '1'} : {executionId: '1', decision: 'accepted'});});
    expect(a.status).toHaveBeenCalledTimes(1);
    const next = renderHook(() => useBackupSession(a)); await flush(); expect(next.result.current.fresh).toBe(true);
});
