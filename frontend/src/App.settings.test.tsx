import {act, render, screen, waitFor, fireEvent} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach, expect, it, vi} from 'vitest';
import {StrictMode} from 'react';
import App from './App';
import {ListWorks, LoadDisplaySettings, SetRegistrationFormVisible} from '../wailsjs/go/main/App';
import type {main} from '../wailsjs/go/models';

vi.mock('../wailsjs/go/main/App', () => ({CreateWork: vi.fn(), ListWorks: vi.fn(), OpenWork: vi.fn(), LoadDisplaySettings: vi.fn(), SetRegistrationFormVisible: vi.fn()}));
const load = vi.mocked(LoadDisplaySettings);
const save = vi.mocked(SetRegistrationFormVisible);
const work: main.WorkDTO = {id: 1, title: '作品', url: 'https://example.com/1', siteName: '', notes: '', thumbnailPath: '', createdAt: '2026-10-10T00:00:00+09:00', updatedAt: '2026-10-10T00:00:00+09:00'};
function deferred<T>() {
    let resolve!: (value: T) => void;
    let reject!: (reason: unknown) => void;
    const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
    return {promise, resolve, reject};
}
beforeEach(() => {
    vi.resetAllMocks();
    vi.mocked(ListWorks).mockResolvedValue([work]);
    load.mockResolvedValue({registrationFormVisible: true});
    save.mockResolvedValue(undefined);
});

it('初回復元は自動保存せず、フォーカスも移動しない', async () => {
    render(<App />);
    await screen.findByRole('button', {name: '登録する'});
    expect(save).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(document.body);
});

it('設定取得中は開閉と登録を待ち、隠す設定を復元する', async () => {
    const pending = deferred<main.DisplaySettingsDTO>();
    load.mockReturnValue(pending.promise);
    render(<App />);
    expect((screen.getByRole('button', {name: '表示設定を読み込み中…'}) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.queryByRole('button', {name: '登録する'})).toBeNull();
    await screen.findByRole('heading', {name: '作品'});
    await act(async () => { pending.resolve({registrationFormVisible: false}); });
    expect(screen.getByRole('button', {name: '作品を登録'}).getAttribute('aria-expanded')).toBe('false');
    expect(save).not.toHaveBeenCalled();
});

it('読み込み失敗は初期値と警告で続行し、自動上書きしない', async () => {
    load.mockRejectedValue('private diagnostic');
    render(<App />);
    await screen.findByRole('button', {name: '登録する'});
    expect(screen.getByRole('alert').textContent).toContain('登録フォームを表示して続行');
    expect(screen.queryByText('private diagnostic')).toBeNull();
    expect(save).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole('button', {name: '登録フォームを閉じる'}));
    await waitFor(() => { expect(save).toHaveBeenCalledWith(false); expect(screen.queryByRole('alert')).toBeNull(); });
});

it('保存失敗でも操作を戻さず、次の保存を試せる', async () => {
    save.mockRejectedValueOnce('private diagnostic');
    render(<App />);
    await screen.findByRole('button', {name: '登録する'});
    await userEvent.click(screen.getByRole('button', {name: '登録フォームを閉じる'}));
    expect((await screen.findByRole('alert')).textContent).toContain('以前の状態に戻る場合');
    expect(screen.queryByRole('button', {name: '登録する'})).toBeNull();
    await userEvent.click(screen.getByRole('button', {name: '作品を登録'}));
    await waitFor(() => { expect(save).toHaveBeenLastCalledWith(true); expect(screen.queryByRole('alert')).toBeNull(); });
});

it('連続操作は即座に反映し、保存要求を一つずつ操作順に送る', async () => {
    const first = deferred<void>();
    save.mockReturnValueOnce(first.promise);
    render(<App />);
    await screen.findByRole('button', {name: '登録する'});
    await userEvent.click(screen.getByRole('button', {name: '登録フォームを閉じる'}));
    await userEvent.click(screen.getByRole('button', {name: '作品を登録'}));
    expect(save).toHaveBeenCalledTimes(1);
    expect(screen.getByText('表示設定を保存しています…')).toBeTruthy();
    expect(screen.getByRole('button', {name: '登録する'})).toBeTruthy();
    await act(async () => { first.reject('old save failure'); });
    await waitFor(() => expect(save).toHaveBeenCalledTimes(2));
    expect(save.mock.calls.map(([visible]) => visible)).toEqual([false, true]);
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.queryByText('表示設定を保存しています…')).toBeNull();
});

it('StrictModeで古い復元結果を反映せず、保存もしない', async () => {
    const old = deferred<main.DisplaySettingsDTO>();
    load.mockReturnValueOnce(old.promise).mockResolvedValueOnce({registrationFormVisible: false});
    render(<StrictMode><App /></StrictMode>);
    await screen.findByRole('button', {name: '作品を登録'});
    await act(async () => { old.resolve({registrationFormVisible: true}); });
    expect(screen.queryByRole('button', {name: '登録する'})).toBeNull();
    expect(save).not.toHaveBeenCalled();
});

it('再マウントで開閉だけ復元し、検索と途中入力は解除する', async () => {
    let visible = true;
    load.mockImplementation(async () => ({registrationFormVisible: visible}));
    save.mockImplementation(async (value) => { visible = value; });
    const first = render(<App />);
    await screen.findByRole('button', {name: '登録する'});
    fireEvent.change(screen.getByRole('searchbox'), {target: {value: '途中の検索'}});
    fireEvent.change(screen.getByLabelText(/タイトル/), {target: {value: '途中の入力'}});
    await userEvent.click(screen.getByRole('button', {name: '登録フォームを閉じる'}));
    await waitFor(() => expect(save).toHaveBeenCalledWith(false));
    first.unmount();
    render(<App />);
    await screen.findByRole('button', {name: '作品を登録'});
    expect((screen.getByRole('searchbox') as HTMLInputElement).value).toBe('');
    await userEvent.click(screen.getByRole('button', {name: '作品を登録'}));
    expect((screen.getByLabelText(/タイトル/) as HTMLInputElement).value).toBe('');
});
