import {act, fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach, describe, expect, it, vi} from 'vitest';
import {StrictMode} from 'react';
import App from './App';
import {CreateWork, ListWorks, LoadDisplaySettings} from '../wailsjs/go/main/App';
import type {main} from '../wailsjs/go/models';

vi.mock('../wailsjs/go/main/App', () => ({CreateWork: vi.fn(), ListWorks: vi.fn(), OpenWork: vi.fn(), LoadDisplaySettings: vi.fn(), SetRegistrationFormVisible: vi.fn()}));
const list = vi.mocked(ListWorks);
const create = vi.mocked(CreateWork);
const work: main.WorkDTO = {
    id: 1, url: 'https://example.com/manga/1', title: '最初の作品', siteName: '漫画サイト',
    notes: '一行目\n二行目', thumbnailPath: '',
    createdAt: '2026-10-06T03:30:00+09:00', updatedAt: '2026-10-06T03:30:00+09:00',
};

function deferred<T>() {
    let resolve!: (value: T) => void;
    const promise = new Promise<T>((done) => { resolve = done; });
    return {promise, resolve};
}

async function fillRequired() {
    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/作品URL/), work.url);
    await user.type(screen.getByLabelText(/タイトル/), work.title);
    return user;
}

beforeEach(() => {
    vi.resetAllMocks();
    vi.mocked(LoadDisplaySettings).mockResolvedValue({registrationFormVisible: true});
    list.mockResolvedValue([]);
    create.mockResolvedValue(work);
});

describe('作品管理画面', () => {
    it('読み込み中と空一覧を区別し、必須入力がないと送信しない', async () => {
        const pending = deferred<main.WorkDTO[]>();
        list.mockReturnValue(pending.promise);
        render(<App />);
        expect(screen.getByText('一覧を読み込んでいます…')).toBeTruthy();
        expect(screen.queryByText('まだ作品が登録されていません')).toBeNull();
        expect((screen.getByRole('button', {name: '再読み込み'}) as HTMLButtonElement).disabled).toBe(true);
        await act(async () => { pending.resolve([]); });
        expect(screen.getByText('まだ作品が登録されていません')).toBeTruthy();
        await userEvent.click(screen.getByRole('button', {name: '登録する'}));
        expect(create).not.toHaveBeenCalled();
    });

    it('一覧をAPIの順序で表示し、作品文字列をHTMLとして解釈しない', async () => {
        list.mockResolvedValue([work, {...work, id: 2, title: '<img src=x onerror=alert(1)>', notes: '<script>bad()</script>'}]);
        render(<App />);
        const items = await screen.findAllByRole('listitem');
        expect(within(items[0]).getByRole('heading').textContent).toBe(work.title);
        expect(within(items[1]).getByRole('heading').textContent).toContain('<img');
        expect(items[1].querySelector('img, script, a')).toBeNull();
        expect(screen.getAllByText('2026/10/06 03:30:00')).toHaveLength(2);
        expect(within(items[0]).getByText('漫画サイト')).toBeTruthy();
    });

    it('登録成功で全入力をクリアして一覧を再取得する', async () => {
        render(<App />);
        await screen.findByText('まだ作品が登録されていません');
        const user = await fillRequired();
        await user.type(screen.getByLabelText(/サイト名/), '漫画サイト');
        await user.type(screen.getByLabelText(/メモ/), 'メモ');
        list.mockResolvedValue([work]);
        await user.click(screen.getByRole('button', {name: '登録する'}));
        await screen.findByRole('heading', {name: work.title});
        expect(create).toHaveBeenCalledExactlyOnceWith({url: work.url, title: work.title, siteName: '漫画サイト', notes: 'メモ', thumbnailPath: ''});
        for (const label of [/作品URL/, /タイトル/, /サイト名/, /メモ/]) {
            expect((screen.getByLabelText(label) as HTMLInputElement).value).toBe('');
        }
        expect(screen.getByText('作品を登録しました。')).toBeTruthy();
        expect(list).toHaveBeenCalledTimes(2);
    });

    it.each(['duplicate_url', 'invalid_input', 'database_unavailable'])('登録失敗 %s で入力を保持する', async (code) => {
        create.mockRejectedValue({code, message: '入力と保存先を確認してください'});
        render(<App />);
        await screen.findByText('まだ作品が登録されていません');
        const user = await fillRequired();
        await user.click(screen.getByRole('button', {name: '登録する'}));
        expect((await screen.findByRole('alert')).textContent).toContain('入力と保存先を確認してください');
        expect((screen.getByLabelText(/作品URL/) as HTMLInputElement).value).toBe(work.url);
        expect((screen.getByLabelText(/タイトル/) as HTMLInputElement).value).toBe(work.title);
        expect(list).toHaveBeenCalledTimes(1);
    });

    it('登録待ちの間は二重送信と入力変更を防ぐ', async () => {
        const pending = deferred<main.WorkDTO>();
        create.mockReturnValue(pending.promise);
        render(<App />);
        await screen.findByText('まだ作品が登録されていません');
        const user = await fillRequired();
        await user.click(screen.getByRole('button', {name: '登録する'}));
        const button = screen.getByRole('button', {name: '登録中…'}) as HTMLButtonElement;
        expect(button.closest('fieldset')?.disabled).toBe(true);
        fireEvent.submit(button.closest('form')!);
        expect(create).toHaveBeenCalledTimes(1);
        await act(async () => { pending.resolve(work); });
        expect(screen.getByRole('button', {name: '登録する'})).toBeTruthy();
    });

    it('一覧取得失敗を空一覧と区別し、再読み込みで復旧する', async () => {
        list.mockRejectedValueOnce({code: 'database_unavailable', message: '保存先を確認して再起動してください'});
        render(<App />);
        expect((await screen.findByRole('alert')).textContent).toContain('保存先を確認');
        expect(screen.queryByText('まだ作品が登録されていません')).toBeNull();
        await userEvent.click(screen.getByRole('button', {name: '再読み込み'}));
        await screen.findByText('まだ作品が登録されていません');
        expect(screen.queryByRole('alert')).toBeNull();
    });

    it('登録後の一覧失敗でも成功と入力クリアを維持し、既存一覧を残す', async () => {
        list.mockResolvedValueOnce([work]).mockRejectedValueOnce(new Error('private path')).mockResolvedValueOnce([work, {...work, id: 2, title: '追加作品'}]);
        render(<App />);
        await screen.findByRole('heading', {name: work.title});
        const user = await fillRequired();
        await user.click(screen.getByRole('button', {name: '登録する'}));
        const alert = await screen.findByRole('alert');
        expect(alert.textContent).toContain('再登録する必要はありません');
        expect(alert.textContent).not.toContain('private');
        expect(screen.getByText('作品を登録しました。')).toBeTruthy();
        expect((screen.getByLabelText(/タイトル/) as HTMLInputElement).value).toBe('');
        expect(screen.getByRole('heading', {name: work.title})).toBeTruthy();
        await user.click(screen.getByRole('button', {name: '再読み込み'}));
        await screen.findByRole('heading', {name: '追加作品'});
        expect(create).toHaveBeenCalledTimes(1);
    });

    it('StrictModeの古い一覧応答で新しい一覧を上書きしない', async () => {
        const stale = deferred<main.WorkDTO[]>();
        list.mockReturnValueOnce(stale.promise).mockResolvedValueOnce([work]);
        render(<StrictMode><App /></StrictMode>);
        await screen.findByRole('heading', {name: work.title});
        await act(async () => { stale.resolve([]); });
        expect(screen.getByRole('heading', {name: work.title})).toBeTruthy();
        expect(screen.queryByText('まだ作品が登録されていません')).toBeNull();
    });

    it('キーボードで必須入力から登録できる', async () => {
        render(<App />);
        await screen.findByText('まだ作品が登録されていません');
        const user = userEvent.setup();
        await user.tab();
        expect(document.activeElement).toBe(screen.getByRole('button', {name: '登録フォームを閉じる'}));
        await user.tab();
        expect(document.activeElement).toBe(screen.getByLabelText(/作品URL/));
        await user.type(document.activeElement as HTMLElement, work.url);
        await user.tab();
        expect(document.activeElement).toBe(screen.getByLabelText(/タイトル/));
        await user.type(document.activeElement as HTMLElement, work.title);
        await user.keyboard('{Enter}');
        await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
    });
});
