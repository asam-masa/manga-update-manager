import {act, fireEvent, render, screen, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach, expect, it, vi} from 'vitest';
import App from './App';
import {CreateWork, ListWorks} from '../wailsjs/go/main/App';
import type {main} from '../wailsjs/go/models';

vi.mock('../wailsjs/go/main/App', () => ({CreateWork: vi.fn(), ListWorks: vi.fn(), OpenWork: vi.fn()}));
const first: main.WorkDTO = {id: 1, title: 'ＡＢＣ物語', url: 'https://example.com/1', siteName: '', notes: '', thumbnailPath: '', createdAt: '2026-10-10T00:00:00+09:00', updatedAt: '2026-10-10T00:00:00+09:00'};
const second = {...first, id: 2, title: 'マンガ物語', url: 'https://example.com/2'};
beforeEach(() => { vi.resetAllMocks(); vi.mocked(ListWorks).mockResolvedValue([first, second]); vi.mocked(CreateWork).mockResolvedValue(second); });

it('フォーム開閉で入力を保持し、フォーカスを移動する', async () => {
    render(<App />);
    await screen.findByRole('heading', {name: first.title});
    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/作品URL/), first.url);
    await user.type(screen.getByLabelText(/タイトル/), '途中の入力');
    await user.click(screen.getByRole('button', {name: '登録フォームを閉じる'}));
    const toggle = screen.getByRole('button', {name: '作品を登録'});
    expect(document.activeElement).toBe(toggle);
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByRole('button', {name: '登録する'})).toBeNull();
    expect(screen.getByRole('region', {name: '登録した作品2件 / 全2件'}).closest('.workspace')?.classList.contains('registration-hidden')).toBe(true);
    await user.click(toggle);
    expect(document.activeElement).toBe(screen.getByLabelText(/作品URL/));
    expect((screen.getByLabelText(/タイトル/) as HTMLInputElement).value).toBe('途中の入力');
});

it('検索だけで絞り込み、0件と未登録を区別し、空欄で元の順序へ戻す', async () => {
    render(<App />);
    await screen.findByRole('heading', {name: first.title});
    const user = userEvent.setup();
    const search = screen.getByRole('searchbox', {name: '漫画名で検索'});
    await user.type(search, 'abc');
    expect(screen.queryByRole('heading', {name: second.title})).toBeNull();
    expect(screen.getByText('1件 / 全2件')).toBeTruthy();
    await user.clear(search);
    await user.type(search, '一致しない文字');
    expect(screen.getByText('検索条件に一致する作品がありません')).toBeTruthy();
    expect(screen.queryByText('まだ作品が登録されていません')).toBeNull();
    await user.clear(search);
    const cards = screen.getAllByRole('listitem');
    expect(within(cards[0]).getByRole('heading').textContent).toBe(first.title);
    expect(vi.mocked(ListWorks)).toHaveBeenCalledTimes(1);
});

it('IME変換中の一覧を維持し、確定後に絞り込む', async () => {
    render(<App />);
    await screen.findByRole('heading', {name: first.title});
    const search = screen.getByRole('searchbox');
    fireEvent.compositionStart(search);
    fireEvent.change(search, {target: {value: 'マンガ'}});
    expect(screen.getAllByRole('listitem')).toHaveLength(2);
    fireEvent.compositionEnd(search, {data: 'マンガ'});
    expect(screen.getAllByRole('listitem')).toHaveLength(1);
    expect(screen.getByRole('heading', {name: second.title})).toBeTruthy();
    fireEvent.change(search, {target: {value: 'マンガ'}});
    expect(screen.getAllByRole('listitem')).toHaveLength(1);
});

it('登録中は開閉を止め、検索不一致の登録でも成功を案内する', async () => {
    let reply!: (work: main.WorkDTO) => void;
    vi.mocked(CreateWork).mockReturnValue(new Promise((resolve) => { reply = resolve; }));
    render(<App />);
    await screen.findByRole('heading', {name: first.title});
    const user = userEvent.setup();
    await user.type(screen.getByRole('searchbox'), 'abc');
    await user.type(screen.getByLabelText(/作品URL/), second.url);
    await user.type(screen.getByLabelText(/タイトル/), second.title);
    await user.click(screen.getByRole('button', {name: '登録する'}));
    const toggle = screen.getByRole('button', {name: '登録フォームを閉じる'}) as HTMLButtonElement;
    expect(toggle.disabled).toBe(true);
    fireEvent.click(toggle);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    await act(async () => { reply(second); });
    expect(screen.getByText('作品を登録しました。検索条件に一致しないため非表示です。')).toBeTruthy();
    expect((screen.getByRole('searchbox') as HTMLInputElement).value).toBe('abc');
    expect(screen.queryByRole('heading', {name: second.title})).toBeNull();
    expect((screen.getByLabelText(/タイトル/) as HTMLInputElement).value).toBe('');
});

it('閉じて開いても検索を維持し、再マウントで検索と途中入力をリセットする', async () => {
    const mounted = render(<App />);
    await screen.findByRole('heading', {name: first.title});
    const user = userEvent.setup();
    await user.type(screen.getByRole('searchbox'), 'abc');
    await user.type(screen.getByLabelText(/タイトル/), '途中');
    await user.click(screen.getByRole('button', {name: '登録フォームを閉じる'}));
    await user.click(screen.getByRole('button', {name: '作品を登録'}));
    expect((screen.getByRole('searchbox') as HTMLInputElement).value).toBe('abc');
    mounted.unmount();
    render(<App />);
    await screen.findByRole('heading', {name: second.title});
    expect((screen.getByRole('searchbox') as HTMLInputElement).value).toBe('');
    expect((screen.getByLabelText(/タイトル/) as HTMLInputElement).value).toBe('');
});

it('検索中の登録失敗でも入力と検索条件を維持する', async () => {
    vi.mocked(CreateWork).mockRejectedValue({code: 'duplicate_url', message: 'この作品URLは登録済みです'});
    render(<App />);
    await screen.findByRole('heading', {name: first.title});
    const user = userEvent.setup();
    await user.type(screen.getByRole('searchbox'), 'abc');
    await user.type(screen.getByLabelText(/作品URL/), first.url);
    await user.type(screen.getByLabelText(/タイトル/), '重複');
    await user.click(screen.getByRole('button', {name: '登録する'}));
    await screen.findByRole('alert');
    expect((screen.getByRole('searchbox') as HTMLInputElement).value).toBe('abc');
    expect((screen.getByLabelText(/タイトル/) as HTMLInputElement).value).toBe('重複');
    expect(screen.getByRole('button', {name: '登録フォームを閉じる'}).hasAttribute('disabled')).toBe(false);
});

it('登録待ちの間に変更した現在の検索条件に合わせて成功を案内する', async () => {
    let reply!: (work: main.WorkDTO) => void;
    vi.mocked(CreateWork).mockReturnValue(new Promise((resolve) => { reply = resolve; }));
    render(<App />);
    await screen.findByRole('heading', {name: first.title});
    const user = userEvent.setup();
    await user.type(screen.getByRole('searchbox'), 'abc');
    await user.type(screen.getByLabelText(/作品URL/), second.url);
    await user.type(screen.getByLabelText(/タイトル/), second.title);
    await user.click(screen.getByRole('button', {name: '登録する'}));
    await user.clear(screen.getByRole('searchbox'));
    await act(async () => { reply(second); });
    expect(screen.getByText('作品を登録しました。')).toBeTruthy();
    expect(screen.getByRole('heading', {name: second.title})).toBeTruthy();
});

it('フォームのIME確定用Enterを登録のEnterと混同しない', async () => {
    render(<App />);
    await screen.findByRole('heading', {name: first.title});
    const title = screen.getByLabelText(/タイトル/);
    expect(fireEvent.keyDown(title, {key: 'Enter', isComposing: true})).toBe(false);
    expect(CreateWork).not.toHaveBeenCalled();
});
