import {act, fireEvent, render, screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach, afterEach, expect, it, vi} from 'vitest';
import {StrictMode} from 'react';
import App from './App';
import {LoadCoverWarnings, ListWorks, LoadCoverProgress} from '../wailsjs/go/main/App';

vi.mock('../wailsjs/go/main/App', () => ({LoadWorkListMode: async () => 'cards', SetWorkListMode: async () => {}, CreateWork: vi.fn(), ListWorks: vi.fn().mockResolvedValue([]), OpenWork: vi.fn(), LoadDisplaySettings: vi.fn().mockResolvedValue({registrationFormVisible: true}), SetRegistrationFormVisible: vi.fn(), LoadCoverWarnings: vi.fn(), LoadCoverProgress: vi.fn().mockResolvedValue([])}));
let frames: Map<number, FrameRequestCallback>;
beforeEach(() => {
    frames = new Map();
    let id = 0;
    vi.stubGlobal('requestAnimationFrame', (fn: FrameRequestCallback) => {frames.set(++id, fn); return id;});
    vi.stubGlobal('cancelAnimationFrame', (id: number) => {frames.delete(id);});
    vi.mocked(LoadCoverWarnings).mockReset();
    vi.mocked(LoadCoverProgress).mockReset().mockResolvedValue([]);
});
afterEach(() => {vi.useRealTimers(); vi.unstubAllGlobals();});
async function frame() {await act(async () => {const pending = [...frames.values()]; frames.clear(); pending.forEach((fn) => fn(0));});}

it('開始済み整理の長時間案内を表示し、整理の完了後は案内を消して画像を利用できる', async () => {
    let complete!: (warnings: []) => void;
    vi.mocked(LoadCoverWarnings).mockReturnValue(new Promise((resolve) => {complete = resolve;}));
    vi.useFakeTimers({toFake: ['setInterval', 'clearInterval']});
    render(<App />);
    await frame(); await frame();
    vi.mocked(LoadCoverProgress).mockResolvedValue([{operationID: 'startup-cleanup', workID: 0, kind: 'cleanup', stage: 'checking', timed: true, elapsedMilliseconds: 15000}]);
    await act(async () => {await vi.advanceTimersByTimeAsync(1000);});
    expect(screen.getByText(/起動時の画像整理：情報を確認/)).toBeTruthy();
    await act(async () => complete([]));
    expect(screen.queryByRole('status', {name: '画像処理の状況'})).toBeNull();
});

it('初回描画機会の後に整理を要求し、StrictModeでも予約を一つにして警告を閉じられる', async () => {
    vi.mocked(LoadCoverWarnings).mockResolvedValue([{code: 'cover_cleanup_failed', message: '画像整理を完了できませんでした。'}]);
    render(<StrictMode><App /></StrictMode>);
    expect(LoadCoverWarnings).not.toHaveBeenCalled();
    await frame();
    expect(LoadCoverWarnings).not.toHaveBeenCalled();
    await frame();
    expect(LoadCoverWarnings).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('alert', {name: '画像整理のお知らせ'})).toBeTruthy();
    await userEvent.click(screen.getByRole('button', {name: '画像整理のお知らせを閉じる'}));
    expect(screen.queryByRole('alert', {name: '画像整理のお知らせ'})).toBeNull();
    expect(screen.getByRole('heading', {name: /登録した作品/})).toBeTruthy();
});

it('結果取得失敗では内部診断を表示しない', async () => {
    vi.mocked(LoadCoverWarnings).mockRejectedValue(new Error('C:\\private\\secret.sqlite'));
    render(<App />);
    await frame(); await frame();
    expect(screen.getByText(/画像整理の結果を取得できませんでした/)).toBeTruthy();
    expect(document.body.textContent).not.toContain('private');
});

it('アンマウント後には整理を要求しない', async () => {
    const view = render(<App />);
    await frame(); view.unmount(); await frame();
    expect(LoadCoverWarnings).not.toHaveBeenCalled();
});

it('整理API完了後に画像取得を開始し、一覧再読み込み成功後に取得をやり直す', async () => {
    let complete!: (warnings: []) => void;
    vi.mocked(LoadCoverWarnings).mockReturnValue(new Promise((resolve) => { complete = resolve; }));
    vi.mocked(ListWorks).mockResolvedValue([{id: 1, title: '画像作品', url: 'https://example.com/', siteName: '', notes: '', thumbnailPath: 'covers/managed.png', createdAt: '', updatedAt: ''}]);
    const view = render(<App />);
    await screen.findByRole('heading', {name: '画像作品'});
    await frame(); await frame();
    expect(view.container.querySelector('img')).toBeNull();
    await act(async () => complete([]));
    const old = view.container.querySelector('img')!;
    fireEvent.error(old);
    expect(screen.getByText('画像を表示できません')).toBeTruthy();
    await userEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    expect(screen.queryByText('画像を表示できません')).toBeNull();
    expect(view.container.querySelector('img')).not.toBe(old);
    vi.mocked(ListWorks).mockResolvedValue([]);
});
