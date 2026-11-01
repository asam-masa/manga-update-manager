import {StrictMode} from 'react';
import {act, cleanup, renderHook, waitFor} from '@testing-library/react';
import {afterEach, expect, it, vi} from 'vitest';
import {useTagSelection} from './use-tag-selection';
import type {LibraryData} from './use-library-snapshot';
import {filterLibrary} from './tag-filter';
import {useLibrarySnapshot} from './use-library-snapshot';

afterEach(cleanup);
const data = (tags = [{id: '1', name: 'マンガ'}, {id: '2', name: '連載中'}, {id: '3', name: '未使用'}]): LibraryData => ({
    snapshot: {works: [], tags, links: []}, works: [],
});
type Props = {value: LibraryData | null; fresh: boolean; stopped: boolean};
function boot(value: LibraryData | null = data(), fresh = true, stopped = false) {
    return renderHook((props: Props) => useTagSelection(props.value, props.fresh, props.stopped), {initialProps: {value, fresh, stopped}});
}

it('未取得では条件を変えず、空の完全結果でも未知IDを追加しない', () => {
    const {result, rerender} = boot(null);
    act(() => {result.current.select('1', true); result.current.clear();});
    expect(result.current.selectedIDs).toEqual([]); expect(result.current.enabled).toBe(false);
    rerender({value: data([]), fresh: true, stopped: false});
    act(() => {result.current.select('1', true);});
    expect(result.current.selectionRevision).toBe(0); expect(result.current.enabled).toBe(true);
});

it('同一ターンの複数選択と個別解除を順に反映し、重複や未知IDではページを戻さない', () => {
    const {result} = boot();
    act(() => {result.current.select('1', true); result.current.select('2', true); result.current.select('1', true); result.current.select('99', true);});
    expect(result.current.selectedIDs).toEqual(['1', '2']); expect(result.current.selectionRevision).toBe(2);
    act(() => {result.current.select('1', false); result.current.select('1', false);});
    expect(result.current.selectedIDs).toEqual(['2']); expect(result.current.selectionRevision).toBe(3);
});

it('全解除は選択がある場合だけページ変更番号を進める', () => {
    const {result} = boot(); act(() => {result.current.select('1', true); result.current.select('2', true); result.current.clear(); result.current.clear();});
    expect(result.current.selectedIDs).toEqual([]); expect(result.current.selectionRevision).toBe(3);
});

it('改名と候補の順序変更では選択IDとページを維持して名称を更新する', () => {
    const {result, rerender} = boot(); act(() => {result.current.select('2', true); result.current.select('1', true);});
    rerender({value: data([{id: '1', name: '新しい名前'}, {id: '2', name: '連載中'}]), fresh: true, stopped: false});
    expect(result.current.selectedIDs).toEqual(['2', '1']); expect(result.current.selectedTags[1].name).toBe('新しい名前');
    expect(result.current.selectionRevision).toBe(2); expect(result.current.notice).toBeNull();
});

it('完全結果で消えた選択だけ外し、同じ描画で計算へ未知IDを渡さず通知する', () => {
    const initial = data(); const next = data([{id: '1', name: 'マンガ'}]);
    const {result, rerender} = renderHook(({value}) => {
        const selection = useTagSelection(value, true, false);
        const filtered = filterLibrary(value.snapshot, {selectedTagIDs: selection.selectedIDs, titleQuery: '', candidateQuery: '',
            order: 'registration_asc', today: '2026-11-01', showFuture: true});
        return {selection, filtered};
    }, {initialProps: {value: initial}});
    act(() => {result.current.selection.select('1', true); result.current.selection.select('2', true); result.current.selection.select('3', true);});
    rerender({value: next});
    expect(result.current.selection.selectedIDs).toEqual(['1']); expect(result.current.selection.selectionRevision).toBe(4);
    expect(result.current.selection.notice?.message).toBe('削除されたタグを条件から外しました。');
    expect(result.current.filtered.candidates).toHaveLength(1);
});

it('作品数やタグの使用数が0でも、候補が残る限り選択を削除しない', () => {
    const {result, rerender} = boot(); act(() => {result.current.select('3', true);});
    rerender({value: data(), fresh: true, stopped: false});
    expect(result.current.selectedIDs).toEqual(['3']); expect(result.current.selectionRevision).toBe(1); expect(result.current.notice).toBeNull();
});

it.each([false, true])('未確認または停止は前回選択を保持し、古いcallbackからも変更しない: stopped=%s', (stopped) => {
    const value = data(), {result, rerender} = boot(value); act(() => {result.current.select('1', true);});
    const oldSelect = result.current.select, oldClear = result.current.clear;
    rerender({value, fresh: stopped, stopped});
    act(() => {oldSelect('1', false); oldSelect('2', true); oldClear();});
    expect(result.current.selectedIDs).toEqual(['1']); expect(result.current.selectionRevision).toBe(1); expect(result.current.enabled).toBe(false);
    expect(result.current.notice).toBeNull();
});

it('再取得失敗でdataが同じ間は削除を推測せず、回復後に変更を再び受け付ける', () => {
    const value = data(), {result, rerender} = boot(value); act(() => {result.current.select('1', true);});
    rerender({value, fresh: false, stopped: false});
    rerender({value, fresh: false, stopped: false});
    expect(result.current.selectedIDs).toEqual(['1']); expect(result.current.notice).toBeNull();
    rerender({value, fresh: true, stopped: false}); act(() => {result.current.select('2', true);});
    expect(result.current.selectedIDs).toEqual(['1', '2']);
});

it('採用直後に別の読取が始まった場合も、採用された一式の更新を一度だけ照合する', () => {
    const {result, rerender} = boot(); act(() => {result.current.select('2', true);});
    const next = data([{id: '1', name: 'マンガ'}]);
    rerender({value: next, fresh: false, stopped: false});
    expect(result.current.selectedIDs).toEqual([]); expect(result.current.selectionRevision).toBe(2);
    const notice = result.current.notice;
    rerender({value: next, fresh: true, stopped: false});
    expect(result.current.selectionRevision).toBe(2); expect(result.current.notice).toEqual(notice);
});

it('data=nullへの移行をタグ削除と扱わず、完全結果が来るまで名前も残す', () => {
    const {result, rerender} = boot(); act(() => {result.current.select('1', true);});
    rerender({value: null, fresh: false, stopped: false});
    expect(result.current.selectedTags).toEqual([{id: '1', name: 'マンガ'}]); expect(result.current.selectionRevision).toBe(1);
    rerender({value: data([]), fresh: true, stopped: false});
    expect(result.current.selectedIDs).toEqual([]); expect(result.current.selectionRevision).toBe(2);
});

it('削除通知は再取得や別の条件変更で消さず、次の削除通知は別の識別子で返す', () => {
    const {result, rerender} = boot(); act(() => {result.current.select('1', true); result.current.select('2', true);});
    rerender({value: data([{id: '2', name: '連載中'}]), fresh: true, stopped: false});
    const first = result.current.notice;
    act(() => {result.current.select('2', false); result.current.select('2', true);});
    expect(result.current.notice).toEqual(first);
    rerender({value: data([]), fresh: true, stopped: false});
    expect(result.current.notice?.id).not.toBe(first?.id);
    act(() => {result.current.dismissNotice();}); expect(result.current.notice).toBeNull();
});

it('入力を変更せず、返却したタグを書き換えても選択内部を壊さない', () => {
    const value = data(); value.snapshot.tags.forEach(Object.freeze); Object.freeze(value.snapshot.tags);
    const {result} = boot(value); act(() => {result.current.select('1', true);});
    result.current.selectedTags[0].name = '外部の変更';
    act(() => {result.current.select('2', true);});
    expect(value.snapshot.tags[0].name).toBe('マンガ'); expect(result.current.selectedTags[0].name).toBe('マンガ');
});

it('StrictModeや同じdataの再描画ではページ番号を二重に進めず、新規mountでは条件を復元しない', () => {
    const value = data(); const first = renderHook(() => useTagSelection(value, true, false), {wrapper: StrictMode});
    act(() => {first.result.current.select('1', true);}); first.rerender();
    expect(first.result.current.selectionRevision).toBe(1); first.unmount();
    const second = boot(value); expect(second.result.current.selectedIDs).toEqual([]); expect(second.result.current.selectionRevision).toBe(0);
});

it('取得hookと組み合わせ、部分応答や古い成功では選択を外さず最新一式の採用だけで外す', async () => {
    let resolveOld!: (value: unknown) => void;
    const old = new Promise<unknown>((resolve) => {resolveOld = resolve;});
    const load = vi.fn<() => Promise<unknown>>().mockResolvedValueOnce(data().snapshot).mockReturnValueOnce(old)
        .mockResolvedValueOnce({works: [], tags: []}).mockResolvedValueOnce(data([]).snapshot);
    const {result} = renderHook(() => {
        const library = useLibrarySnapshot(load);
        return {library, selection: useTagSelection(library.data, library.fresh, library.stopped)};
    });
    await waitFor(() => expect(result.current.selection.enabled).toBe(true));
    act(() => {result.current.selection.select('1', true); void result.current.library.reload();});
    await act(async () => {await result.current.library.reload();});
    expect(result.current.selection.selectedIDs).toEqual(['1']); expect(result.current.selection.enabled).toBe(false);
    await act(async () => {resolveOld(data([]).snapshot);});
    expect(result.current.selection.selectedIDs).toEqual(['1']); expect(result.current.selection.notice).toBeNull();
    await act(async () => {await result.current.library.reload();});
    expect(result.current.selection.selectedIDs).toEqual([]); expect(result.current.selection.selectionRevision).toBe(2);
    expect(result.current.selection.notice).not.toBeNull(); expect(load).toHaveBeenCalledTimes(4);
});
