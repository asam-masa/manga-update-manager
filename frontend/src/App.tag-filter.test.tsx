import {act, cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import App from './App';
import {CreateWork, LoadLibrarySnapshot, LoadTagUsage, LoadTagDeletePreview, RenameTag, DeleteTag, OpenWork} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';

vi.mock('../wailsjs/go/main/App', () => ({
    LoadLibrarySnapshot: vi.fn(), CreateWork: vi.fn(), OpenWork: vi.fn(), RenameTag: vi.fn(), DeleteTag: vi.fn(), LoadTagUsage: vi.fn(), LoadTagDeletePreview: vi.fn(),
    LoadTags: async () => [], LoadWorkTags: async () => [], LoadCoverWarnings: async () => [], LoadCoverProgress: async () => [],
    LoadDisplaySettings: async () => ({registrationFormVisible: false}), SetRegistrationFormVisible: async () => {},
    LoadWorkListMode: async () => 'rows', SetWorkListMode: async () => {}, LoadWorkSort: async () => 'registration_asc', SetWorkSort: async () => {},
    LoadShowFutureSchedules: async () => true, SetShowFutureSchedules: async () => {},
}));
const stamp = '2026-11-01T00:00:00+09:00';
const work = (id: number) => ({id: String(id), title: `作品${id}`, url: `https://example.com/${id}`, siteName: '', notes: '', thumbnailPath: '',
    createdAt: stamp, updatedAt: stamp, scheduledDate: null, lastAccessedAt: null});
function fixture() {return {works: Array.from({length: 31}, (_, i) => work(i + 1)),
    tags: [{id: '1', name: 'マンガ'}, {id: '2', name: '連載中'}, {id: '3', name: '未使用'}],
    links: Array.from({length: 31}, (_, i) => [
        ...(i % 2 === 0 ? [{workID: String(i + 1), tagID: '1'}] : []),
        ...(i < 25 ? [{workID: String(i + 1), tagID: '2'}] : []),
    ]).flat()};}
let saved = fixture();
const read = vi.mocked(LoadLibrarySnapshot);
beforeEach(() => {
    vi.resetAllMocks(); saved = fixture(); read.mockImplementation(async () => new main.LibrarySnapshotDTO(saved));
    vi.mocked(LoadTagUsage).mockImplementation(async () => saved.tags.map((tag) => new main.TagUsageDTO({tag,
        workCount: saved.links.filter((link) => link.tagID === tag.id).length})));
    Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {configurable: true, value: function(this: HTMLDialogElement) {this.open = true;}});
    Object.defineProperty(HTMLDialogElement.prototype, 'close', {configurable: true, value: function(this: HTMLDialogElement) {this.open = false;}});
});
afterEach(() => {cleanup(); Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal'); Reflect.deleteProperty(HTMLDialogElement.prototype, 'close');});
async function boot() {const view = render(<App />); await screen.findByRole('heading', {name: '作品1'}); return view;}
const trigger = () => screen.getByRole('button', {name: /^タグで絞り込み/});
const popup = () => screen.getByRole('region', {name: 'タグ絞り込みの候補'});
const checkbox = (name: string) => within(popup()).getByRole('checkbox', {name: new RegExp(name)});
const current = () => screen.getAllByRole('button', {current: 'page'})[0]?.textContent;
const titles = () => screen.getAllByRole('heading', {level: 3}).map((node) => node.textContent);

it('全件から候補追加後のAND件数を数え、選択の即時反映と0件解除を行う', async () => {
    await boot(); await userEvent.click(screen.getAllByRole('button', {name: '次へ'})[0]); expect(current()).toBe('2');
    await userEvent.click(trigger()); expect(document.activeElement).toBe(within(popup()).getByRole('searchbox'));
    expect(checkbox('マンガ').parentElement?.textContent).toContain('16件');
    await userEvent.click(checkbox('連載中')); expect(current()).toBe('1'); expect(titles()).toHaveLength(20);
    expect(checkbox('マンガ').parentElement?.textContent).toContain('13件');
    await userEvent.click(checkbox('マンガ')); expect(titles()).toHaveLength(13); expect(titles()[1]).toBe('作品3');
    await userEvent.click(checkbox('未使用')); expect(screen.queryByRole('heading', {name: '作品1'})).toBeNull();
    expect(screen.getByText('表示条件に一致する作品がありません')).toBeTruthy();
    await userEvent.click(screen.getByRole('button', {name: 'タグ条件をすべて解除'}));
    expect(titles()).toHaveLength(20); expect(current()).toBe('1');
});

it('候補検索のIMEは確定まで反映せず、確定後も作品とページを変えない', async () => {
    await boot(); await userEvent.click(screen.getAllByRole('button', {name: '次へ'})[0]); await userEvent.click(trigger());
    const input = within(popup()).getByRole('searchbox'); fireEvent.compositionStart(input);
    fireEvent.change(input, {target: {value: 'ﾏﾝｶﾞ'}}); expect(within(popup()).getAllByRole('checkbox')).toHaveLength(3);
    fireEvent.compositionEnd(input, {data: 'ﾏﾝｶﾞ'}); expect(within(popup()).getAllByRole('checkbox')).toHaveLength(1);
    expect(checkbox('マンガ')).toBeTruthy(); expect(current()).toBe('2'); expect(titles()[0]).toBe('作品21');
});

it('Tab/SpaceでチェックしEscでボタンへ戻り、外側クリックは検索へ移動する', async () => {
    await boot(); await userEvent.click(trigger());
    await userEvent.tab(); expect(document.activeElement).toBe(within(popup()).getAllByRole('checkbox')[0]);
    await userEvent.tab({shift: true}); expect(document.activeElement).toBe(within(popup()).getByRole('searchbox'));
    const candidate = checkbox('マンガ'); candidate.focus(); await userEvent.keyboard(' ');
    expect(document.activeElement).toBe(candidate); expect((candidate as HTMLInputElement).checked).toBe(true);
    await userEvent.keyboard('{Escape}'); expect(screen.queryByRole('region', {name: 'タグ絞り込みの候補'})).toBeNull();
    expect(document.activeElement).toBe(trigger());
    await userEvent.click(trigger()); const search = screen.getByRole('searchbox', {name: '漫画名で検索'});
    await userEvent.click(search); expect(document.activeElement).toBe(search); expect(screen.queryByRole('region', {name: 'タグ絞り込みの候補'})).toBeNull();
    expect(screen.getByRole('group', {name: '選択中のタグ条件'}).textContent).toContain('マンガ');
});

it('登録後の遅い一式取得で候補検索中のIME・フォーカス・ページを奪わない', async () => {
    await boot(); let resolve!: (value: main.WorkDTO) => void;
    vi.mocked(CreateWork).mockReturnValueOnce(new Promise((yes) => {resolve = yes;}));
    await userEvent.click(screen.getByRole('button', {name: '作品を登録'}));
    fireEvent.change(screen.getByRole('textbox', {name: /作品URL/}), {target: {value: work(32).url}});
    fireEvent.change(screen.getByRole('textbox', {name: /^タイトル/}), {target: {value: '作品32'}});
    await userEvent.click(screen.getByRole('button', {name: '登録する'})); await userEvent.click(trigger());
    const input = within(popup()).getByRole('searchbox'); fireEvent.compositionStart(input); fireEvent.change(input, {target: {value: 'マン'}});
    await act(async () => {saved.works.push(work(32)); resolve(new main.WorkDTO({...work(32), id: 32}));});
    expect(current()).toBe('1'); expect(document.activeElement).toBe(input); expect((input as HTMLInputElement).value).toBe('マン');
    fireEvent.compositionEnd(input, {data: 'マン'}); expect(within(popup()).getAllByRole('checkbox')).toHaveLength(1);
});

it('未確認中は前回選択/候補を残して変更を止め、取得失敗でも解除しない', async () => {
    await boot(); await userEvent.click(trigger()); await userEvent.click(checkbox('マンガ'));
    let reject!: (error: unknown) => void; read.mockReturnValueOnce(new Promise((_, no) => {reject = no;}));
    await userEvent.click(screen.getByRole('button', {name: '再読み込み'})); await userEvent.click(trigger());
    expect((checkbox('マンガ') as HTMLInputElement).disabled).toBe(true);
    expect((screen.getByRole('button', {name: 'すべて解除'}) as HTMLButtonElement).disabled).toBe(true);
    await act(async () => {reject(new Error('private.sqlite'));});
    expect((checkbox('マンガ') as HTMLInputElement).checked).toBe(true); expect(titles()).toHaveLength(16);
    expect(document.body.textContent).not.toContain('private.sqlite');
});

it('改名では選択とページを保ち、削除では条件だけ外して通知・ページ1へ戻す', async () => {
    await boot(); await userEvent.click(trigger()); await userEvent.click(checkbox('連載中'));
    await userEvent.keyboard('{Escape}'); await userEvent.click(screen.getAllByRole('button', {name: '次へ'})[0]); expect(current()).toBe('2');
    vi.mocked(RenameTag).mockImplementation(async () => {saved.tags[1] = {id: '2', name: '改名済み'}; return new main.TagChangeResult({tag: saved.tags[1], warnings: []});});
    await userEvent.click(screen.getByRole('button', {name: 'タグ管理'})); let dialog = screen.getByRole('dialog');
    await userEvent.click(await within(dialog).findByRole('button', {name: '「連載中」の名前変更'}));
    fireEvent.change(within(dialog).getByRole('textbox', {name: '新しいタグ名'}), {target: {value: '改名済み'}});
    await userEvent.click(within(dialog).getByRole('button', {name: '保存'})); await within(dialog).findByText(/変更しました/);
    await userEvent.click(within(dialog).getByRole('button', {name: '閉じる'}));
    expect(current()).toBe('2'); expect(screen.getByRole('group', {name: '選択中のタグ条件'}).textContent).toContain('改名済み');
    vi.mocked(LoadTagDeletePreview).mockResolvedValue(new main.TagDeletePreviewDTO({tag: saved.tags[1], works: saved.works.slice(0, 25).map((w) => ({id: w.id, title: w.title, createdAt: w.createdAt}))}));
    vi.mocked(DeleteTag).mockImplementation(async () => {const tag = saved.tags[1]; saved.tags = saved.tags.filter((t) => t.id !== '2'); saved.links = saved.links.filter((l) => l.tagID !== '2'); return new main.TagChangeResult({tag, warnings: []});});
    await userEvent.click(screen.getByRole('button', {name: 'タグ管理'})); dialog = screen.getByRole('dialog');
    await userEvent.click(await within(dialog).findByRole('button', {name: '「改名済み」を削除'}));
    await userEvent.click(await within(dialog).findByRole('button', {name: '削除する'}));
    await within(dialog).findByText(/作品は残っています/); await userEvent.click(within(dialog).getByRole('button', {name: '閉じる'}));
    expect(current()).toBe('1'); expect(screen.getByRole('region', {name: 'タグ条件の変更'}).textContent).toContain('削除されたタグを条件から外しました');
    expect(screen.queryByRole('group', {name: '選択中のタグ条件'})).toBeNull(); expect(saved.works).toHaveLength(31);
});

it('形式切替でも選択を保ち、選択タグ解除のフォーカスと再mount時の解除を確認する', async () => {
    const view = await boot(); await userEvent.click(trigger()); await userEvent.click(checkbox('連載中')); await userEvent.click(checkbox('マンガ'));
    await userEvent.keyboard('{Escape}');
    for (const name of ['小型表示', 'メモなしカード', '行一覧']) {await userEvent.click(screen.getByRole('button', {name})); expect(screen.getByRole('group', {name: '選択中のタグ条件'})).toBeTruthy();}
    await userEvent.click(screen.getByRole('button', {name: '「連載中」の条件を解除'}));
    expect(document.activeElement).toBe(screen.getByRole('button', {name: '「マンガ」の条件を解除'}));
    await userEvent.click(screen.getByRole('button', {name: 'すべて解除'})); expect(document.activeElement).toBe(trigger());
    await userEvent.click(trigger()); await userEvent.click(checkbox('マンガ')); view.unmount(); await boot();
    expect(screen.queryByRole('group', {name: '選択中のタグ条件'})).toBeNull();
});

it('タグ条件で登録作品が隠れても登録成功と入力クリアを保持する', async () => {
    await boot(); await userEvent.click(trigger()); await userEvent.click(checkbox('マンガ')); await userEvent.keyboard('{Escape}');
    const created = {...work(32), id: 32}; vi.mocked(CreateWork).mockImplementation(async () => {saved.works.push(work(32)); return new main.WorkDTO(created);});
    await userEvent.click(screen.getByRole('button', {name: '作品を登録'}));
    fireEvent.change(screen.getByRole('textbox', {name: /作品URL/}), {target: {value: created.url}});
    fireEvent.change(screen.getByRole('textbox', {name: /^タイトル/}), {target: {value: created.title}});
    await userEvent.click(screen.getByRole('button', {name: '登録する'}));
    await screen.findByText('作品を登録しました。タグ条件に一致しないため非表示です。');
    expect((screen.getByRole('textbox', {name: /^タイトル/}) as HTMLInputElement).value).toBe(''); expect(CreateWork).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('heading', {name: '作品32'})).toBeNull();
});

it('タグ条件と検索で処理中作品が隠れてもアクセス結果を失わない', async () => {
    await boot(); let resolve!: (w: main.WorkDTO) => void; vi.mocked(OpenWork).mockReturnValueOnce(new Promise((yes) => {resolve = yes;}));
    await userEvent.click(screen.getByRole('button', {name: '作品2のURLを開く'}));
    await userEvent.click(trigger()); await userEvent.click(checkbox('マンガ')); expect(screen.queryByRole('heading', {name: '作品2'})).toBeNull();
    await act(async () => {resolve(new main.WorkDTO({...work(2), id: 2, lastAccessedAt: stamp}));});
    expect(screen.getByRole('region', {name: '作品2のURL操作'}).textContent).toContain('保存しました');
});
