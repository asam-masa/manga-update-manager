import {StrictMode} from 'react';
import {act, cleanup, renderHook} from '@testing-library/react';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import {useBackupObservation} from './use-backup-observation';

const idle = {executionId: '', state: 'idle', stage: 'unobserved', elapsedSeconds: 0, canCancel: false, result: null};
const running = (id = '1') => ({...idle, executionId: id, state: 'running', stage: 'database', canCancel: true});
const finished = (id = '1') => ({...running(id), state: 'finished', canCancel: false,
    result: {publication: 'confirmed', hasWarnings: false, omittedImageCount: 0, workNamesState: 'available', workCount: 0}});
const saved = (id = '1') => ({executionId: id, outcome: 'saved', message: '保存しました。', cancellationObserved: false, retryAllowed: true, notices: []});
function deferred() {
    let resolve!: (value: unknown) => void, reject!: (value: unknown) => void;
    const promise = new Promise<unknown>((yes, no) => {resolve = yes; reject = no;});
    return {promise, resolve, reject};
}
beforeEach(() => vi.useFakeTimers());
afterEach(() => {cleanup(); vi.useRealTimers();});
const flush = () => act(async () => {});
const tick = (ms = 1000) => act(async () => {await vi.advanceTimersByTimeAsync(ms);});

it('直後に接続し、完了から1秒待って取得し、遅い要求を重ねない', async () => {
    const slow = deferred(), status = vi.fn().mockReturnValueOnce(slow.promise).mockResolvedValue(idle), resultAPI = vi.fn();
    const {result} = renderHook(() => useBackupObservation(status, resultAPI));
    expect(result.current).toMatchObject({status: null, fresh: false, loading: true});
    await tick(5000); expect(status).toHaveBeenCalledTimes(1);
    await act(async () => {slow.resolve(running());});
    expect(result.current.status?.state).toBe('running');
    await tick(999); expect(status).toHaveBeenCalledTimes(1);
    await tick(1); expect(status).toHaveBeenCalledTimes(2);
    expect(resultAPI).not.toHaveBeenCalled();
});

it('完了結果を同じIDで取得し、通常の定期取得で保持結果を繰り返し送らせない', async () => {
    const status = vi.fn().mockResolvedValue(finished('9007199254740993'));
    const resultAPI = vi.fn().mockResolvedValue(saved('9007199254740993'));
    const {result} = renderHook(() => useBackupObservation(status, resultAPI)); await flush();
    expect(resultAPI).toHaveBeenCalledWith('9007199254740993');
    expect(result.current).toMatchObject({fresh: true, result: {outcome: 'saved'}});
    await tick(); expect(status).toHaveBeenCalledTimes(2); expect(resultAPI).toHaveBeenCalledTimes(1);
});

it('失敗では前回表示を残して未確認とし、診断本文を出さず次回に回復する', async () => {
    const status = vi.fn().mockResolvedValueOnce(running()).mockRejectedValueOnce(new Error('H:/private/secret.sqlite')).mockResolvedValue(idle);
    const resultAPI = vi.fn();
    const {result} = renderHook(() => useBackupObservation(status, resultAPI)); await flush();
    await tick();
    expect(result.current).toMatchObject({fresh: false, status: {state: 'running'}});
    expect(result.current.error).not.toContain('private');
    await tick(); expect(result.current).toMatchObject({fresh: true, error: '', status: {state: 'idle'}});
});

it('初回の不正応答をidle成功に変えず明示再読込で回復する', async () => {
    const status = vi.fn().mockResolvedValueOnce({}).mockResolvedValue(idle), resultAPI = vi.fn();
    const {result} = renderHook(() => useBackupObservation(status, resultAPI)); await flush();
    expect(result.current).toMatchObject({status: null, fresh: false, loading: false});
    await act(async () => {result.current.reload();});
    expect(result.current).toMatchObject({fresh: true, status: {state: 'idle'}});
});

it('取得中の再読込を1回へまとめ、同時取得しない', async () => {
    const slow = deferred(), status = vi.fn().mockReturnValueOnce(slow.promise).mockResolvedValue(idle), resultAPI = vi.fn();
    const {result} = renderHook(() => useBackupObservation(status, resultAPI));
    act(() => {result.current.reload(); result.current.reload();});
    expect(status).toHaveBeenCalledTimes(1);
    await act(async () => {slow.resolve(running());});
    expect(status).toHaveBeenCalledTimes(2);
    await tick(999); expect(status).toHaveBeenCalledTimes(2);
});

it('別IDの結果を混ぜず、結果取得だけの失敗も回復する', async () => {
    const status = vi.fn().mockResolvedValueOnce(finished()).mockResolvedValue(finished('2'));
    const resultAPI = vi.fn().mockResolvedValueOnce(saved()).mockResolvedValueOnce(saved()).mockResolvedValue(saved('2'));
    const {result} = renderHook(() => useBackupObservation(status, resultAPI)); await flush();
    await tick();
    expect(result.current).toMatchObject({status: {executionId: '2'}, result: null, fresh: false});
    await tick(); expect(result.current).toMatchObject({result: {executionId: '2'}, fresh: true});
});

it('結果取得が終わるまで定期取得を重ねない', async () => {
    const slow = deferred(), status = vi.fn().mockResolvedValue(finished()), resultAPI = vi.fn().mockReturnValue(slow.promise);
    const {result} = renderHook(() => useBackupObservation(status, resultAPI)); await flush();
    await tick(5000); expect(status).toHaveBeenCalledTimes(1); expect(result.current.fresh).toBe(false);
    await act(async () => {slow.resolve(saved());}); expect(result.current.fresh).toBe(true);
});

it('結果取得の通信失敗は完了成功にせず、明示再読込で結果を回復する', async () => {
    const status = vi.fn().mockResolvedValue(finished());
    const resultAPI = vi.fn().mockRejectedValueOnce(new Error('private diagnostic')).mockResolvedValue(saved());
    const {result} = renderHook(() => useBackupObservation(status, resultAPI)); await flush();
    expect(result.current).toMatchObject({fresh: false, status: {state: 'finished'}, result: null});
    expect(result.current.error).not.toContain('diagnostic');
    await act(async () => {result.current.reload();});
    expect(result.current).toMatchObject({fresh: true, result: {executionId: '1'}});
});

it('完了結果を保持したまま状況取得失敗を未確認とし、復帰時に別実行へ切り替える', async () => {
    const status = vi.fn().mockResolvedValueOnce(finished()).mockRejectedValueOnce(new Error('失敗')).mockResolvedValue(running('2'));
    const resultAPI = vi.fn().mockResolvedValue(saved());
    const {result} = renderHook(() => useBackupObservation(status, resultAPI)); await flush();
    await tick(); expect(result.current).toMatchObject({fresh: false, result: {executionId: '1'}});
    await tick(); expect(result.current).toMatchObject({fresh: true, status: {executionId: '2'}, result: null});
});

it('アンマウント後はタイマーも結果取得も進めず、新規マウントで再接続する', async () => {
    const slow = deferred(), status = vi.fn().mockReturnValueOnce(slow.promise).mockResolvedValue(finished()), resultAPI = vi.fn().mockResolvedValue(saved());
    const old = renderHook(() => useBackupObservation(status, resultAPI)); old.unmount();
    await act(async () => {slow.resolve(finished());}); await tick(5000);
    expect(status).toHaveBeenCalledTimes(1); expect(resultAPI).not.toHaveBeenCalled();
    const next = renderHook(() => useBackupObservation(status, resultAPI)); await flush();
    expect(next.result.current).toMatchObject({fresh: true, result: {outcome: 'saved'}});
});

it('取得元変更後に返る古い結果を採用しない', async () => {
    const slow = deferred(), oldStatus = vi.fn().mockResolvedValue(finished()), nextStatus = vi.fn().mockResolvedValue(running('2'));
    const resultAPI = vi.fn().mockReturnValue(slow.promise);
    const {result, rerender} = renderHook(({load}) => useBackupObservation(load, resultAPI), {initialProps: {load: oldStatus}});
    await flush(); rerender({load: nextStatus}); await flush();
    await act(async () => {slow.resolve(saved());});
    expect(result.current).toMatchObject({fresh: true, status: {executionId: '2'}, result: null});
    await tick(); expect(oldStatus).toHaveBeenCalledTimes(1); expect(nextStatus).toHaveBeenCalledTimes(2);
});

it('StrictModeの古い応答を無視し、現行のタイマーだけ維持する', async () => {
    const slow = deferred(), status = vi.fn().mockReturnValueOnce(slow.promise).mockResolvedValue(running('2')), resultAPI = vi.fn();
    const {result} = renderHook(() => useBackupObservation(status, resultAPI), {wrapper: StrictMode}); await flush();
    await act(async () => {slow.resolve(finished());});
    expect(result.current.status?.executionId).toBe('2'); expect(resultAPI).not.toHaveBeenCalled();
    await tick(); expect(status).toHaveBeenCalledTimes(3);
});
