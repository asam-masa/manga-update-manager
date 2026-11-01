import {act, cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import App from './App';
import {CreateWork, DeleteWork, EditWork, LoadLibrarySnapshot, LoadTagUsage, OpenWork, RenameTag} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';

vi.mock('../wailsjs/go/main/App', () => ({
    LoadLibrarySnapshot: vi.fn(), ListWorks: () => { throw new Error('Legacy reader must not be used'); },
    CreateWork: vi.fn(), EditWork: vi.fn(), DeleteWork: vi.fn(), OpenWork: vi.fn(),
    LoadTagUsage: vi.fn(), RenameTag: vi.fn(),
    LoadTags: async () => [], LoadWorkTags: async () => [], LoadCoverWarnings: async () => [], LoadCoverProgress: async () => [],
    LoadDisplaySettings: async () => ({registrationFormVisible: true}), SetRegistrationFormVisible: async () => {},
    LoadWorkListMode: async () => 'cards', SetWorkListMode: async () => {}, LoadWorkSort: async () => 'registration_asc', SetWorkSort: async () => {},
    LoadShowFutureSchedules: async () => true, SetShowFutureSchedules: async () => {},
}));
const work = {id: 1, title: '旧名', url: 'https://example.com/1', siteName: '', notes: '', thumbnailPath: '',
    createdAt: '2026-11-01T00:00:00+09:00', updatedAt: '2026-11-01T00:00:00+09:00', lastAccessedAt: null, scheduledDate: null};
const snapshot = (works = [work]) => new main.LibrarySnapshotDTO({works: works.map((w) => ({...w, id: String(w.id)})), tags: [], links: []});
const read = vi.mocked(LoadLibrarySnapshot);
function deferred<T>() { let resolve!: (value: T) => void, reject!: (cause: unknown) => void;
    const promise = new Promise<T>((yes, no) => {resolve = yes; reject = no;}); return {promise, resolve, reject}; }
beforeEach(() => {
    vi.resetAllMocks(); read.mockResolvedValue(snapshot());
    Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {configurable: true, value: function(this: HTMLDialogElement) {this.open = true;}});
    Object.defineProperty(HTMLDialogElement.prototype, 'close', {configurable: true, value: function(this: HTMLDialogElement) {this.open = false;}});
});
afterEach(() => {cleanup(); Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal'); Reflect.deleteProperty(HTMLDialogElement.prototype, 'close');});
async function boot() {render(<App />); await screen.findByRole('heading', {name: '旧名'});}
async function action(name: string) {await userEvent.click(screen.getByRole('button', {name: '旧名の作品操作'}));
    await userEvent.click(screen.getByRole('button', {name})); return screen.getByRole('dialog');}

it('部分応答を初回の空成功にせず、一式の再取得成功だけを採用する', async () => {
    read.mockResolvedValueOnce({works: [], tags: []} as unknown as main.LibrarySnapshotDTO);
    render(<App />); await screen.findByRole('alert');
    expect(screen.queryByText('まだ作品が登録されていません')).toBeNull();
    await userEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    await screen.findByRole('heading', {name: '旧名'}); expect(read).toHaveBeenCalledTimes(2);
});

it('編集成功と入力を閉じる動作を保持し、再取得中/失敗は旧名を残して再送しない', async () => {
    await boot(); const next = deferred<main.LibrarySnapshotDTO>(); read.mockReturnValueOnce(next.promise);
    vi.mocked(EditWork).mockResolvedValue(new main.EditWorkResult({work: {...work, title: '新名'}, warnings: []}));
    const dialog = await action('旧名を編集');
    await userEvent.clear(within(dialog).getByRole('textbox', {name: /タイトル/}));
    await userEvent.type(within(dialog).getByRole('textbox', {name: /タイトル/}), '新名');
    await userEvent.click(within(dialog).getByRole('button', {name: '保存する'}));
    await screen.findByText(/「新名」を保存しました/);
    expect(screen.getByRole('heading', {name: '旧名'})).toBeTruthy(); expect(screen.queryByRole('dialog')).toBeNull();
    expect(screen.getByText(/最新情報は未確認/)).toBeTruthy();
    await act(async () => {next.reject(new Error('private.sqlite'));});
    expect(screen.getByRole('heading', {name: '旧名'})).toBeTruthy(); expect(document.body.textContent).not.toContain('private.sqlite');
    read.mockResolvedValue(snapshot([{...work, title: '新名'}]));
    await userEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    await screen.findByRole('heading', {name: '新名'}); expect(EditWork).toHaveBeenCalledTimes(1);
    expect(screen.getByText(/「新名」を保存しました/)).toBeTruthy();
});

it('削除確定後の読取失敗は旧表示と成功通知を残すが、旧登録の再操作を止める', async () => {
    await boot(); const next = deferred<main.LibrarySnapshotDTO>(); read.mockReturnValueOnce(next.promise);
    vi.mocked(DeleteWork).mockResolvedValue(new main.DeleteWorkResult({workID: 1, warnings: []}));
    const dialog = await action('旧名を削除');
    await waitFor(() => expect((within(dialog).getByRole('button', {name: '削除する'}) as HTMLButtonElement).disabled).toBe(false));
    await userEvent.click(within(dialog).getByRole('button', {name: '削除する'}));
    await screen.findByText(/「旧名」を削除しました/);
    await act(async () => {next.reject(new Error('read failed'));});
    expect(screen.getByRole('heading', {name: '旧名'})).toBeTruthy();
    expect((screen.getByRole('button', {name: '旧名のURLを開く'}) as HTMLButtonElement).disabled).toBe(true);
    await userEvent.click(screen.getByRole('button', {name: '旧名の作品操作'}));
    expect((screen.getByRole('button', {name: '旧名を削除'}) as HTMLButtonElement).disabled).toBe(true);
    read.mockResolvedValue(snapshot([])); await userEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    await screen.findByText('まだ作品が登録されていません'); expect(DeleteWork).toHaveBeenCalledTimes(1);
});

it('登録確定後の取得失敗で空表示を最新扱いせず、成功とフォームクリアを残す', async () => {
    read.mockResolvedValueOnce(snapshot([])).mockRejectedValueOnce(new Error('read failed')); vi.mocked(CreateWork).mockResolvedValue(new main.WorkDTO(work));
    render(<App />); await screen.findByText('まだ作品が登録されていません');
    fireEvent.change(screen.getByRole('textbox', {name: /作品URL/}), {target: {value: work.url}});
    fireEvent.change(screen.getByRole('textbox', {name: /^タイトル/}), {target: {value: work.title}});
    await userEvent.click(screen.getByRole('button', {name: '登録する'}));
    await screen.findByText('作品を登録しました。'); await screen.findByRole('alert');
    expect((screen.getByRole('textbox', {name: /^タイトル/}) as HTMLInputElement).value).toBe('');
    expect(screen.queryByText('まだ作品が登録されていません')).toBeNull(); expect(CreateWork).toHaveBeenCalledTimes(1);
});

it('背景読取の停止を編集中にも反映し、入力を保持して保存と追加読取を止める', async () => {
    await boot(); const pending = deferred<main.LibrarySnapshotDTO>(); read.mockReturnValueOnce(pending.promise);
    await userEvent.click(screen.getByRole('button', {name: '再読み込み'})); const dialog = await action('旧名を編集');
    await userEvent.type(within(dialog).getByRole('textbox', {name: /タイトル/}), '入力');
    await act(async () => {pending.reject({code: 'database_stopped', message: '停止しました'});});
    expect((screen.getByRole('button', {name: '再読み込み'}) as HTMLButtonElement).disabled).toBe(true);
    expect((within(dialog).getByRole('textbox', {name: /タイトル/}) as HTMLInputElement).value).toBe('旧名入力');
    fireEvent.submit(dialog.querySelector('form')!); expect(EditWork).not.toHaveBeenCalled();
    expect(read).toHaveBeenCalledTimes(2);
});

it('アクセス保存後の再取得失敗でも成功通知を残し、前回日時を勝手に更新しない', async () => {
    await boot(); read.mockRejectedValueOnce(new Error('read failed'));
    vi.mocked(OpenWork).mockResolvedValue(new main.WorkDTO({...work, lastAccessedAt: '2026-11-01T12:00:00+09:00'}));
    await userEvent.click(screen.getByRole('button', {name: '旧名のURLを開く'}));
    await screen.findByText(/ページを開き、最終アクセス日時を保存しました/); await screen.findByRole('alert');
    expect(screen.getByText(/最終アクセス 未アクセス/)).toBeTruthy(); expect(OpenWork).toHaveBeenCalledTimes(1);
});

it('アクセス結果の登録同一性や保存日時を確認できなければ成功通知や変更後読取を出さない', async () => {
    await boot(); vi.mocked(OpenWork).mockResolvedValue(new main.WorkDTO({...work, lastAccessedAt: 'not-a-time'}));
    await userEvent.click(screen.getByRole('button', {name: '旧名のURLを開く'}));
    await screen.findByText('作品を開く処理に失敗しました。再度お試しください。');
    expect(screen.queryByText(/ページを開き、最終アクセス日時を保存しました/)).toBeNull(); expect(read).toHaveBeenCalledTimes(1);
});

it.each(['success', 'failure', 'stop'])('タグ管理の%sを通知と区別し、確定変更だけを再取得する', async (kind) => {
    const tag = {id: '1', name: '元のタグ'};
    vi.mocked(LoadTagUsage).mockResolvedValue([new main.TagUsageDTO({tag, workCount: 1})]);
    if (kind === 'failure') vi.mocked(RenameTag).mockRejectedValue({code: 'storage_error', message: '保存できません'});
    else vi.mocked(RenameTag).mockResolvedValue(new main.TagChangeResult({tag: {...tag, name: '新しいタグ'},
        warnings: kind === 'stop' ? [{code: 'database_stopped_after_commit', message: '保存済みですが再起動してください'}] : []}));
    await boot(); await userEvent.click(screen.getByRole('button', {name: 'タグ管理'})); const dialog = screen.getByRole('dialog');
    await userEvent.click(await within(dialog).findByRole('button', {name: '「元のタグ」の名前変更'}));
    fireEvent.change(within(dialog).getByRole('textbox', {name: '新しいタグ名'}), {target: {value: '新しいタグ'}});
    await userEvent.click(within(dialog).getByRole('button', {name: '保存'}));
    if (kind === 'failure') await within(dialog).findByText('保存できません');
    else await within(dialog).findByText('タグ「元のタグ」を「新しいタグ」に変更しました。');
    expect(read).toHaveBeenCalledTimes(kind === 'success' ? 2 : 1);
});
