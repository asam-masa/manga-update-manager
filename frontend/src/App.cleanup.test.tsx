import {act, render, screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach, afterEach, expect, it, vi} from 'vitest';
import {StrictMode} from 'react';
import App from './App';
import {LoadCoverWarnings} from '../wailsjs/go/main/App';

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
