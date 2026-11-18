import {act, cleanup, fireEvent, render, renderHook, screen, waitFor} from '@testing-library/react';
import {afterEach, expect, it, vi} from 'vitest';
import {readBackupLocations} from './backup-locations';
import {useBackupLocations} from './use-backup-locations';
import {BackupLocationsPanel} from './BackupLocationsPanel';

afterEach(cleanup);
const value = (id = '1') => ({executionId: id, destination: 'H:\\選択\\saved.zip', publication: 'confirmed',
    confirmedPath: 'H:\\選択\\saved.zip', candidates: [{kind: 'database_copy', path: 'H:\\作業\\copy.sqlite'}]});
function deferred() {
    let resolve!: (v: unknown) => void, reject!: (v: unknown) => void;
    const promise = new Promise<unknown>((yes, no) => {resolve = yes; reject = no;}); return {promise, resolve, reject};
}

it('uint64のIDとパスを数値化・補正せず保持し、返却配列を分離する', () => {
    const v = value('18446744073709551615'), parsed = readBackupLocations(v, v.executionId);
    expect(parsed).toEqual(v); parsed.candidates[0].path = '変更'; expect(v.candidates[0].path).not.toBe('変更');
});
it.each([null, {executionId: undefined}, {executionId: '2'}, {destination: null}, {confirmedPath: null}, {publication: 'future'},
    {publication: 'unknown'}, {candidates: null}, {candidates: [{kind: 'future', path: 'x'}]}, {candidates: [{kind: 'temporary_zip', path: ''}]}])('不正・別IDの場所を採用しない: %j', (patch) => {
    expect(() => readBackupLocations(patch === null ? null : {...value(), ...patch}, '1')).toThrow();
});
it.each(['not_attempted', 'collision', 'unknown'])('未確認の公開状態 %s に確認済み場所を付けない', (publication) => {
    expect(readBackupLocations({...value(), publication, confirmedPath: ''}, '1').confirmedPath).toBe('');
});
it('完了IDがなければ場所を読まず、ID切替の旧応答を採用しない', async () => {
    const old = deferred(), load = vi.fn().mockReturnValueOnce(old.promise).mockResolvedValue(value('2'));
    const {result, rerender} = renderHook(({id}: {id: string | null}) => useBackupLocations(id, load), {initialProps: {id: null as string | null}});
    expect(load).not.toHaveBeenCalled(); rerender({id: '1'}); rerender({id: '2'});
    await waitFor(() => expect(result.current.data?.executionId).toBe('2'));
    await act(async () => {old.resolve(value());}); expect(result.current.data?.executionId).toBe('2');
});
it('取得失敗は診断を表示せず、詳細の再読込だけで回復する', async () => {
    const load = vi.fn().mockRejectedValueOnce(new Error('H:/private/secret.sqlite')).mockResolvedValue(value());
    function View() { return <BackupLocationsPanel view={useBackupLocations('1', load)} />; }
    render(<View />); await screen.findByRole('alert'); expect(screen.getByRole('alert').textContent).not.toContain('private');
    fireEvent.click(screen.getByRole('button', {name: '場所の詳細を再読み込み'}));
    await screen.findByText('H:\\作業\\copy.sqlite'); expect(load).toHaveBeenCalledTimes(2);
});
it('場所の別ID応答を取得失敗とし、unmount後は採用しない', async () => {
    const load = vi.fn().mockResolvedValueOnce(value('2')), late = deferred();
    const first = renderHook(() => useBackupLocations('1', load)); await waitFor(() => expect(first.result.current.error).not.toBe(''));
    expect(first.result.current.data).toBeNull(); first.unmount();
    load.mockReturnValue(late.promise); const second = renderHook(() => useBackupLocations('1', load)); second.unmount();
    await act(async () => {late.reject(new Error('遅い失敗'));});
});
it('空の記録と候補なしを不存在・成功と読み替えない', () => {
    const data = readBackupLocations({...value(), destination: '', publication: 'unknown', confirmedPath: '', candidates: []}, '1');
    render(<BackupLocationsPanel view={{id: '1', data, loading: false, error: '', reload: vi.fn()}} />);
    expect(screen.getByText('未記録')).toBeTruthy(); expect(screen.getByText('未確認')).toBeTruthy();
    expect(screen.getByText(/存在しないことの保証ではありません/)).toBeTruthy();
    expect(screen.queryByRole('link')).toBeNull();
});
