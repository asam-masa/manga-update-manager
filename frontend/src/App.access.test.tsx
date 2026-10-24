import {act, fireEvent, render, screen, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach, expect, it, vi} from 'vitest';
import App from './App';
import {ListWorks, OpenWork} from '../wailsjs/go/main/App';
import type {main} from '../wailsjs/go/models';

vi.mock('../wailsjs/go/main/App', () => ({ListWorks: vi.fn(), OpenWork: vi.fn(), CreateWork: vi.fn(), LoadDisplaySettings: vi.fn().mockResolvedValue({registrationFormVisible: true}), SetRegistrationFormVisible: vi.fn(), LoadCoverWarnings: vi.fn().mockResolvedValue([])}));

const work: main.WorkDTO = {id: 1, title: '作品', url: 'https://example.com', siteName: '', notes: '', thumbnailPath: '', createdAt: '2026-10-01T00:00:00+09:00', updatedAt: '2026-10-01T00:00:00+09:00', lastAccessedAt: '2026-10-08T12:34:56+09:00'};
beforeEach(() => { vi.mocked(ListWorks).mockReset(); vi.mocked(OpenWork).mockReset(); });

it('一覧の古い応答でもアクセス日時を戻さず、追加作品は表示する', async () => {
    const work: main.WorkDTO = {id: 1, title: '作品', url: 'https://example.com', siteName: '', notes: '', thumbnailPath: '', createdAt: '2026-10-01T00:00:00+09:00', updatedAt: '2026-10-01T00:00:00+09:00'};
    let reply!: (value: main.WorkDTO[]) => void;
    vi.mocked(ListWorks).mockResolvedValueOnce([work]).mockReturnValueOnce(new Promise((resolve) => { reply = resolve; }));
    vi.mocked(OpenWork).mockResolvedValue({...work, lastAccessedAt: '2026-10-09T12:00:00+09:00'});
    render(<App />);
    await screen.findByRole('heading', {name: '作品'});
    await userEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    await userEvent.click(screen.getByRole('button', {name: '作品のURLを開く'}));
    await screen.findByText('2026/10/09 12:00:00');
    await act(async () => { reply([work, {...work, id: 2, title: '追加作品'}]); });
    expect(screen.getByText('2026/10/09 12:00:00')).toBeTruthy();
    expect(screen.getByRole('heading', {name: '追加作品'})).toBeTruthy();
});

it.each(['browser_open_failed', 'access_save_failed', 'database_stopped'])('失敗 %s の通知をAppで保持し、日時を戻さず自動再試行しない', async (code) => {
    vi.mocked(ListWorks).mockResolvedValue([work]);
    vi.mocked(OpenWork).mockRejectedValue({code, message: '起動または保存を確認してください。'});
    render(<App />);
    await userEvent.click(await screen.findByRole('button', {name: '作品のURLを開く'}));
    const notice = await screen.findByRole('region', {name: '作品のURL操作'});
    expect(within(notice).getByRole('alert').textContent).toContain('確認してください');
    expect(screen.getByText('2026/10/08 12:34:56')).toBeTruthy();
    const search = screen.getByRole('searchbox');
    await userEvent.type(search, '別');
    expect(screen.queryByRole('heading', {name: '作品'})).toBeNull();
    expect(screen.getByRole('region', {name: '作品のURL操作'})).toBe(notice);
    await userEvent.click(within(notice).getByRole('button'));
    expect(document.activeElement).toBe(search);
    expect(OpenWork).toHaveBeenCalledTimes(1);
});

it('カードの非表示・再表示をまたぐ同時tick二重起動を防ぎ、完了時は通知だけ消す', async () => {
    let reply!: (value: main.WorkDTO) => void;
    vi.mocked(ListWorks).mockResolvedValue([work]);
    vi.mocked(OpenWork).mockReturnValue(new Promise((resolve) => {reply = resolve;}));
    render(<App />);
    const button = await screen.findByRole('button', {name: '作品のURLを開く'});
    act(() => {fireEvent.click(button); fireEvent.click(button);});
    const search = screen.getByRole('searchbox');
    fireEvent.change(search, {target: {value: '別'}});
    expect(screen.getByText('「作品」のページを開いています…')).toBeTruthy();
    fireEvent.change(search, {target: {value: ''}});
    expect((screen.getByRole('button', {name: '作品のURLを開く'}) as HTMLButtonElement).disabled).toBe(true);
    expect(OpenWork).toHaveBeenCalledTimes(1);
    search.focus();
    await act(async () => {reply({...work, lastAccessedAt: '2026-10-09T12:00:00+09:00'});});
    expect(screen.queryByRole('region', {name: '作品のURL操作'})).toBeNull();
    expect(document.activeElement).toBe(search);
    expect(screen.getByText('2026/10/09 12:00:00')).toBeTruthy();
});

it('別作品の失敗通知を保持し、同じ作品の再操作だけで更新する', async () => {
    const other = {...work, id: 2, title: '別作品'};
    vi.mocked(ListWorks).mockResolvedValue([work, other]);
    vi.mocked(OpenWork).mockRejectedValue(new Error('private diagnostic'));
    render(<App />);
    for (const title of ['作品', '別作品']) {
        await userEvent.click(await screen.findByRole('button', {name: `${title}のURLを開く`}));
        await screen.findByRole('region', {name: `${title}のURL操作`});
    }
    expect(screen.getAllByRole('region', {name: /のURL操作/})).toHaveLength(2);
    expect(document.body.textContent).not.toContain('private diagnostic');
    vi.mocked(OpenWork).mockResolvedValue(work);
    await userEvent.click(screen.getByRole('button', {name: '作品のURLを開く'}));
    expect(screen.queryByRole('region', {name: '作品のURL操作'})).toBeNull();
    const remaining = screen.getByRole('region', {name: '別作品のURL操作'});
    await userEvent.click(within(remaining).getByRole('button'));
    expect(document.activeElement).toBe(screen.getByRole('button', {name: '別作品の作品操作'}));
});

it('再利用されたIDの新しい登録へ処理状態と遅い日時応答を混ぜない', async () => {
    let reply!: (value: main.WorkDTO) => void;
    const replacement = {...work, title: '別登録', createdAt: '2026-10-10T00:00:00+09:00', lastAccessedAt: undefined};
    vi.mocked(ListWorks).mockResolvedValueOnce([work]).mockResolvedValueOnce([replacement]);
    vi.mocked(OpenWork).mockReturnValue(new Promise((resolve) => {reply = resolve;}));
    render(<App />);
    await userEvent.click(await screen.findByRole('button', {name: '作品のURLを開く'}));
    await userEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    await screen.findByRole('heading', {name: '別登録'});
    await act(async () => {reply({...work, lastAccessedAt: '2026-10-09T12:00:00+09:00'});});
    expect(screen.getByText(/未アクセス/)).toBeTruthy();
    expect(screen.queryByText('2026/10/09 12:00:00')).toBeNull();
});

it('ページ起動済み・保存失敗の案内を保持し、通知を閉じても再起動しない', async () => {
    vi.mocked(ListWorks).mockResolvedValue([work]);
    vi.mocked(OpenWork).mockRejectedValue({code: 'access_save_failed', message: 'ページを開きましたが、最終アクセス日時の保存を確認できません。アプリを再起動して状態を確認してください。'});
    render(<App />);
    await userEvent.click(await screen.findByRole('button', {name: '作品のURLを開く'}));
    const notice = await screen.findByRole('region', {name: '作品のURL操作'});
    expect(within(notice).getByRole('alert').textContent).toContain('ページを開きました');
    expect(screen.getByText('2026/10/08 12:34:56')).toBeTruthy();
    await userEvent.click(within(notice).getByRole('button'));
    expect(OpenWork).toHaveBeenCalledTimes(1);
});

it('IDが再利用されても古い通知の閉鎖で別登録のURLへフォーカスを移さない', async () => {
    vi.mocked(ListWorks).mockResolvedValueOnce([work]).mockResolvedValueOnce([{...work, title: '別登録', createdAt: '2026-10-10T00:00:00+09:00'}]);
    vi.mocked(OpenWork).mockRejectedValue(new Error('private diagnostic'));
    render(<App />);
    await userEvent.click(await screen.findByRole('button', {name: '作品のURLを開く'}));
    const notice = await screen.findByRole('region', {name: '作品のURL操作'});
    await userEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    await screen.findByRole('heading', {name: '別登録'});
    await userEvent.click(within(notice).getByRole('button'));
    expect(document.activeElement).toBe(screen.getByRole('searchbox'));
});
