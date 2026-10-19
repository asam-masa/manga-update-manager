import {act, fireEvent, render, screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach, afterEach, expect, it, vi} from 'vitest';
import {StrictMode} from 'react';
import App from './App';
import {LoadCoverWarnings, ListWorks} from '../wailsjs/go/main/App';

vi.mock('../wailsjs/go/main/App', () => ({CreateWork: vi.fn(), ListWorks: vi.fn().mockResolvedValue([]), OpenWork: vi.fn(), LoadDisplaySettings: vi.fn().mockResolvedValue({registrationFormVisible: true}), SetRegistrationFormVisible: vi.fn(), LoadCoverWarnings: vi.fn()}));
let frames: Map<number, FrameRequestCallback>;
beforeEach(() => {
    frames = new Map();
    let id = 0;
    vi.stubGlobal('requestAnimationFrame', (fn: FrameRequestCallback) => {frames.set(++id, fn); return id;});
    vi.stubGlobal('cancelAnimationFrame', (id: number) => {frames.delete(id);});
    vi.mocked(LoadCoverWarnings).mockReset();
});
afterEach(() => vi.unstubAllGlobals());
async function frame() {await act(async () => {const pending = [...frames.values()]; frames.clear(); pending.forEach((fn) => fn(0));});}

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
