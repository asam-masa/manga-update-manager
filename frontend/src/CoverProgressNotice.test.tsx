import {StrictMode} from 'react';
import {act, render, screen} from '@testing-library/react';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import {CoverProgressNotice} from './CoverProgressNotice';
import type {CoverProgressTarget} from './CoverProgressNotice';
import {LoadCoverProgress} from '../wailsjs/go/main/App';
import type {main} from '../wailsjs/go/models';

vi.mock('../wailsjs/go/main/App', () => ({LoadCoverProgress: vi.fn()}));
const a: CoverProgressTarget = {workID: 1, operationID: 'a'.repeat(32), kind: 'select'};
const b: CoverProgressTarget = {workID: 2, operationID: 'b'.repeat(32), kind: 'remove'};
const cleanup: CoverProgressTarget = {workID: 0, operationID: 'startup-cleanup', kind: 'cleanup'};
const sample = (target = a, elapsedMilliseconds = 10000, stage = 'saving') => ({...target, elapsedMilliseconds, stage, timed: true});
function deferred<T>() {
    let resolve!: (value: T) => void;
    const promise = new Promise<T>((yes) => {resolve = yes;});
    return {promise, resolve};
}
const tick = async (milliseconds = 1000) => { await act(async () => {await vi.advanceTimersByTimeAsync(milliseconds);}); };
beforeEach(() => {vi.useFakeTimers(); vi.mocked(LoadCoverProgress).mockReset().mockResolvedValue([]);});
afterEach(() => {vi.useRealTimers();});

it('1秒未満で完了した操作では取得も長時間案内も行わない', async () => {
    const view = render(<CoverProgressNotice targets={[a]} works={null} />);
    await tick(999);
    view.rerender(<CoverProgressNotice targets={[]} works={null} />);
    await tick(2000);
    expect(LoadCoverProgress).not.toHaveBeenCalled();
    expect(screen.queryByRole('status')).toBeNull();
});

it('処理中だけ取得し、選択待ちと10秒未満では詳細を出さず、段階・確認時の経過を表示する', async () => {
    const view = render(<CoverProgressNotice targets={[]} works={null} />);
    await tick(2000);
    expect(LoadCoverProgress).not.toHaveBeenCalled();
    view.rerender(<CoverProgressNotice targets={[a]} works={null} />);
    vi.mocked(LoadCoverProgress).mockResolvedValueOnce([{...sample(), timed: false, elapsedMilliseconds: 0, stage: 'choosing'}]);
    await tick();
    expect(screen.queryByRole('status')).toBeNull();
    vi.mocked(LoadCoverProgress).mockResolvedValueOnce([sample(a, 9999)]);
    await tick();
    expect(screen.queryByRole('status')).toBeNull();
    vi.mocked(LoadCoverProgress).mockResolvedValueOnce([sample(a)]);
    await tick();
    expect(screen.getByText(/画像を保存（最終確認時の経過 10秒）/)).toBeTruthy();
    expect(screen.getByText(/正常動作やフリーズの有無を保証/)).toBeTruthy();
    vi.mocked(LoadCoverProgress).mockResolvedValueOnce([sample(a, 12000, 'releasing')]);
    await tick();
    expect(screen.getByText(/画像・フォルダーを解放（最終確認時の経過 12秒）/)).toBeTruthy();
    view.rerender(<CoverProgressNotice targets={[]} works={null} />);
    const calls = vi.mocked(LoadCoverProgress).mock.calls.length;
    await tick(5000);
    expect(screen.queryByRole('status')).toBeNull();
    expect(LoadCoverProgress).toHaveBeenCalledTimes(calls);
});

it('取得を重ねず、開始後の別要求や完了済み要求へ遅い応答を適用しない', async () => {
    const pending = deferred<main.CoverProgressDTO[]>();
    vi.mocked(LoadCoverProgress).mockReturnValueOnce(pending.promise);
    const view = render(<CoverProgressNotice targets={[a]} works={null} />);
    await tick(5000);
    expect(LoadCoverProgress).toHaveBeenCalledTimes(1);
    view.rerender(<CoverProgressNotice targets={[b]} works={null} />);
    await act(async () => pending.resolve([sample(a), sample(b)]));
    expect(screen.queryByRole('status')).toBeNull();
    vi.mocked(LoadCoverProgress).mockResolvedValueOnce([sample(b)]);
    await tick();
    expect(screen.getByText(/作品ID 2/)).toBeTruthy();
    view.rerender(<CoverProgressNotice targets={[]} works={null} />);
    expect(screen.queryByRole('status')).toBeNull();
});

it('複数操作と整理を分け、異なる作品ID・操作ID・種別の応答を使わない', async () => {
    vi.mocked(LoadCoverProgress).mockResolvedValueOnce([sample({...a, workID: 99}), sample(b), sample(cleanup, 11000, 'cleaning')]);
    render(<CoverProgressNotice targets={[a, b, cleanup]} works={null} />);
    await tick();
    expect(screen.queryByText(/作品ID 1：/)).toBeNull();
    expect(screen.getByText(/作品ID 2：/)).toBeTruthy();
    expect(screen.getByText(/起動時の画像整理：/)).toBeTruthy();
});

it('操作完了後に取得応答が届いても案内を復活させない', async () => {
    const pending = deferred<main.CoverProgressDTO[]>();
    vi.mocked(LoadCoverProgress).mockReturnValueOnce(pending.promise);
    const view = render(<CoverProgressNotice targets={[a]} works={null} />);
    await tick();
    view.rerender(<CoverProgressNotice targets={[]} works={null} />);
    await act(async () => pending.resolve([sample(a)]));
    expect(screen.queryByRole('status')).toBeNull();
    view.rerender(<CoverProgressNotice targets={[b]} works={null} />);
    await tick();
    expect(screen.queryByRole('status')).toBeNull();
});

it('欠落・取得失敗で前回の情報を保持し、空配列を完了と扱わず、回復後は最新値を表示する', async () => {
    vi.mocked(LoadCoverProgress).mockResolvedValueOnce([sample()]);
    render(<CoverProgressNotice targets={[a]} works={null} />);
    await tick();
    await tick(); // default []
    expect(screen.getByText(/現在の処理状況を確認できません/)).toBeTruthy();
    expect(screen.getByText(/最終確認時の経過 10秒/)).toBeTruthy();
    vi.mocked(LoadCoverProgress).mockRejectedValueOnce(new Error('C:/private/source.png'));
    await tick();
    expect(document.body.textContent).not.toContain('private');
    vi.mocked(LoadCoverProgress).mockResolvedValueOnce([sample(a, 13000, 'updating')]);
    await tick();
    expect(screen.queryByText(/現在の処理状況を確認できません/)).toBeNull();
    expect(screen.getByText(/保存情報を更新（最終確認時の経過 13秒）/)).toBeTruthy();
});

it.each([null, {}, [{...sample(), stage: 'private/path'}], [{...sample(), stage: ['saving']}], [{...sample(), stage: {}}], [{...sample(), elapsedMilliseconds: -1}], [sample(), sample()]])('不正な取得値を安全な案内に置き換える: %j', async (value) => {
    vi.mocked(LoadCoverProgress).mockResolvedValueOnce(value as main.CoverProgressDTO[]);
    render(<CoverProgressNotice targets={[a]} works={null} />);
    await tick();
    expect(screen.getByText(/現在の処理状況を確認できません/)).toBeTruthy();
    expect(document.body.textContent).not.toContain('private/path');
});

it('同作品の新しい操作IDへ前回要求の取得応答を適用しない', async () => {
    const pending = deferred<main.CoverProgressDTO[]>();
    vi.mocked(LoadCoverProgress).mockReturnValueOnce(pending.promise);
    const next = {...a, operationID: 'c'.repeat(32)};
    const view = render(<CoverProgressNotice targets={[a]} works={null} />);
    await tick();
    view.rerender(<CoverProgressNotice targets={[next]} works={null} />);
    await act(async () => pending.resolve([sample(a), sample(next)]));
    expect(screen.queryByRole('status')).toBeNull();
    vi.mocked(LoadCoverProgress).mockResolvedValueOnce([sample(next)]);
    await tick();
    expect(screen.getByText(/作品ID 1/)).toBeTruthy();
});

it('StrictModeで取得を重ねず、アンマウント前の応答が新しい画面へ残らない', async () => {
    const pending = deferred<main.CoverProgressDTO[]>();
    vi.mocked(LoadCoverProgress).mockReturnValueOnce(pending.promise);
    const view = render(<StrictMode><CoverProgressNotice targets={[a]} works={null} /></StrictMode>);
    await tick(3000);
    expect(LoadCoverProgress).toHaveBeenCalledTimes(1);
    view.unmount();
    render(<CoverProgressNotice targets={[b]} works={null} />);
    await act(async () => pending.resolve([sample(a)]));
    expect(screen.queryByRole('status')).toBeNull();
    vi.mocked(LoadCoverProgress).mockResolvedValueOnce([sample(b)]);
    await tick();
    expect(screen.getByText(/作品ID 2/)).toBeTruthy();
});
