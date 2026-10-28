import {StrictMode} from 'react';
import {act, cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import App from './App';
import {CreateWork, EditWork, ListWorks, LoadWorkSort, SetWorkSort, LoadWorkListMode, SetWorkListMode,
    LoadShowFutureSchedules, SetShowFutureSchedules, LoadDisplaySettings, SetRegistrationFormVisible, LoadCoverWarnings, LoadCoverProgress, OpenWork} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';

const clock = vi.hoisted(() => ({today: '2028-02-29'}));
vi.mock('./use-japan-today', () => ({useJapanToday: () => clock.today}));
vi.mock('../wailsjs/go/main/App', () => ({LoadTags: async () => [], LoadWorkTags: async () => [], CreateWork: vi.fn(), EditWork: vi.fn(), DeleteWork: vi.fn(), ListWorks: vi.fn(),
    LoadWorkSort: vi.fn(), SetWorkSort: vi.fn(), LoadWorkListMode: vi.fn(), SetWorkListMode: vi.fn(),
    LoadShowFutureSchedules: vi.fn(), SetShowFutureSchedules: vi.fn(), LoadDisplaySettings: vi.fn(), SetRegistrationFormVisible: vi.fn(),
    LoadCoverWarnings: vi.fn(), LoadCoverProgress: vi.fn(), OpenWork: vi.fn(), SelectWorkCoverWithOperation: vi.fn(), RemoveWorkCoverWithOperation: vi.fn()}));
const work = (id: number, scheduledDate?: string): main.WorkDTO => ({id, scheduledDate, title: `作品${id}`, url: `https://example.com/${id}`,
    siteName: '', notes: '', thumbnailPath: '', createdAt: '2028-02-28T00:00:00+09:00', updatedAt: '2028-02-28T00:00:00+09:00'});
const fixtures = () => [work(1, '2028-02-29'), work(2, '2028-02-28'), work(3, '0001-01-01'), work(4, '2028-03-01'), work(5, '9999-12-31'),
    ...Array.from({length: 20}, (_, i) => work(i + 6))];
const select = () => screen.getByRole('combobox', {name: '並び順'}) as HTMLSelectElement;
const future = () => screen.getByRole('checkbox', {name: '未来の予定を表示'}) as HTMLInputElement;
const choose = (value: string) => fireEvent.change(select(), {target: {value}});
const page = () => screen.queryByRole('navigation', {name: '一覧上部のページ切替'})?.querySelector('[aria-current="page"]')?.textContent;
const second = () => fireEvent.click(within(screen.getByRole('navigation', {name: '一覧上部のページ切替'})).getByRole('button', {name: '2ページ'}));
const titles = () => within(screen.getByRole('region', {name: '登録した作品'})).getAllByRole('heading', {level: 3}).map((h) => h.textContent).filter((title) => /^作品\d+$/.test(title ?? ''));
function deferred<T>() { let resolve!: (value: T) => void, reject!: (cause: unknown) => void; const promise = new Promise<T>((yes, no) => {resolve = yes; reject = no;}); return {promise, resolve, reject}; }
beforeEach(() => {
    vi.resetAllMocks(); clock.today = '2028-02-29';
    vi.mocked(ListWorks).mockResolvedValue(fixtures()); vi.mocked(LoadWorkSort).mockResolvedValue('scheduled_date'); vi.mocked(SetWorkSort).mockResolvedValue();
    vi.mocked(LoadShowFutureSchedules).mockResolvedValue(true); vi.mocked(SetShowFutureSchedules).mockResolvedValue();
    vi.mocked(LoadWorkListMode).mockResolvedValue('cards'); vi.mocked(SetWorkListMode).mockResolvedValue();
    vi.mocked(LoadDisplaySettings).mockResolvedValue({registrationFormVisible: false}); vi.mocked(SetRegistrationFormVisible).mockResolvedValue();
    vi.mocked(LoadCoverWarnings).mockResolvedValue([]); vi.mocked(LoadCoverProgress).mockResolvedValue([]);
    Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {configurable: true, value: vi.fn()});
    Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {configurable: true, value: function(this: HTMLDialogElement) {this.open = true;}});
    Object.defineProperty(HTMLDialogElement.prototype, 'close', {configurable: true, value: function(this: HTMLDialogElement) {this.open = false;}});
});
afterEach(() => { cleanup(); Reflect.deleteProperty(HTMLElement.prototype, 'scrollIntoView'); Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal'); Reflect.deleteProperty(HTMLDialogElement.prototype, 'close'); });
async function boot() { const view = render(<App />); await waitFor(() => expect(select().disabled).toBe(false)); await waitFor(() => expect(future().disabled).toBe(false)); await screen.findByRole('heading', {name: '作品1'}); return view; }

it.each(['小型表示', '行一覧', 'メモなしカード'])('%sで検索→予定条件→全件整列→ページ分割し、未来OFFは他順序に影響しない', async (mode) => {
    await boot(); fireEvent.click(screen.getByRole('button', {name: mode}));
    expect(titles().slice(0, 5)).toEqual(['作品1', '作品2', '作品3', '作品4', '作品5']);
    second(); fireEvent.click(future()); expect(page()).toBe('1');
    expect(titles().slice(0, 4)).toEqual(['作品1', '作品2', '作品3', '作品6']);
    fireEvent.change(screen.getByRole('searchbox'), {target: {value: '作品2'}});
    expect(titles()).toEqual(['作品2', '作品20', '作品21', '作品22', '作品23', '作品24', '作品25']);
    choose('registration_desc'); expect(screen.queryByRole('checkbox')).toBeNull();
    fireEvent.change(screen.getByRole('searchbox'), {target: {value: ''}});
    expect(titles()[0]).toBe('作品25'); expect(ListWorks).toHaveBeenCalledTimes(1);
    choose('scheduled_date'); expect(future().checked).toBe(false);
    await waitFor(() => expect(SetShowFutureSchedules).toHaveBeenCalledWith(false));
});
it('予定日見出しから同じ順序を選び、フォーカスを保持して逆順を作らない', async () => {
    await boot(); choose('registration_desc'); fireEvent.click(screen.getByRole('button', {name: '行一覧'}));
    const header = screen.getByRole('button', {name: '更新予定日で今日を先頭に並べる'}); header.focus(); fireEvent.click(header);
    expect(select().value).toBe('scheduled_date'); expect(document.activeElement).toBe(header);
    fireEvent.click(header); expect(select().value).toBe('scheduled_date'); expect(header.getAttribute('aria-pressed')).toBe('true');
});

it('予定順への変更で0件になっても行見出しとフォーカスを残す', async () => {
    vi.mocked(ListWorks).mockResolvedValue([work(1, '2028-03-01')]);
    vi.mocked(LoadWorkListMode).mockResolvedValue('rows'); vi.mocked(LoadWorkSort).mockResolvedValue('registration_asc');
    vi.mocked(LoadShowFutureSchedules).mockResolvedValue(false); render(<App />);
    await screen.findByRole('heading', {name: '作品1'}); await waitFor(() => expect(select().disabled).toBe(false));
    await act(async () => {});
    const header = screen.getByRole('button', {name: '更新予定日で今日を先頭に並べる'}); header.focus(); fireEvent.click(header);
    expect(screen.getByText('表示条件に一致する作品がありません')).toBeTruthy();
    expect(header.isConnected).toBe(true); expect(document.activeElement).toBe(header);
    fireEvent.click(future()); expect(screen.getByRole('heading', {name: '作品1'})).toBeTruthy();
});
it.each(['unknown', 'failure'])('未来設定の読込%sはONと警告で続行し、自動保存しない', async (kind) => {
    if (kind === 'unknown') vi.mocked(LoadShowFutureSchedules).mockResolvedValue('false' as unknown as boolean);
    else vi.mocked(LoadShowFutureSchedules).mockRejectedValue('private diagnostic');
    await boot(); expect(future().checked).toBe(true); expect(screen.getByRole('alert').textContent).toContain('未来の予定');
    expect(document.body.textContent).not.toContain('private diagnostic'); expect(SetShowFutureSchedules).not.toHaveBeenCalled();
});
it('StrictModeの古い復元を破棄し、復元だけでは保存しない', async () => {
    const old = deferred<boolean>(), latest = deferred<boolean>(); vi.mocked(LoadShowFutureSchedules).mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise);
    render(<StrictMode><App /></StrictMode>);
    await act(async () => {latest.resolve(false);}); await act(async () => {old.resolve(true);});
    expect(future().checked).toBe(false); expect(SetShowFutureSchedules).not.toHaveBeenCalled();
});
it('未来設定と並び順を共有列で保存し、古い保存結果が最新の表示や警告を戻さない', async () => {
    const first = deferred<void>(); vi.mocked(SetShowFutureSchedules).mockReturnValueOnce(first.promise).mockRejectedValueOnce('private diagnostic');
    await boot(); fireEvent.click(future()); await waitFor(() => expect(SetShowFutureSchedules).toHaveBeenCalledTimes(1));
    fireEvent.click(future()); choose('title_desc'); expect(SetWorkSort).not.toHaveBeenCalled();
    await act(async () => {first.resolve();}); await waitFor(() => expect(SetWorkSort).toHaveBeenCalledWith('title_desc'));
    expect(screen.getByRole('alert').textContent).toContain('未来の予定の表示設定を保存できません');
    choose('scheduled_date'); expect(future().checked).toBe(true);
    expect(document.body.textContent).not.toContain('private diagnostic');
});
it('日付変更で現在ページと検索・編集中の内容を保ち、未来OFFの今日を戻す', async () => {
    vi.mocked(LoadShowFutureSchedules).mockResolvedValue(false);
    const view = await boot(); fireEvent.change(screen.getByRole('searchbox'), {target: {value: '作品'}}); second();
    const title = titles()[0]!;
    fireEvent.click(screen.getByRole('button', {name: `${title}の作品操作`})); fireEvent.click(screen.getByRole('button', {name: `${title}を編集`}));
    const input = within(screen.getByRole('dialog')).getByRole('textbox', {name: /タイトル/}) as HTMLInputElement;
    fireEvent.change(input, {target: {value: '編集中'}});
    fireEvent.click(future()); expect(future().checked).toBe(false);
    clock.today = '2028-03-01'; view.rerender(<App />);
    expect(page()).toBe('2'); expect(input.value).toBe('編集中'); expect((screen.getByRole('searchbox') as HTMLInputElement).value).toBe('作品'); expect(ListWorks).toHaveBeenCalledTimes(1);
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', {name: 'キャンセル'}));
    fireEvent.click(within(screen.getByRole('navigation', {name: '一覧上部のページ切替'})).getByRole('button', {name: '1ページ'}));
    expect(titles()[0]).toBe('作品4'); expect(SetShowFutureSchedules).not.toHaveBeenCalled();
});
it('編集で未来へ移った作品が隠れても現在ページと通知を残し、遅い一覧で戻さない', async () => {
    vi.mocked(LoadShowFutureSchedules).mockResolvedValue(false); await boot();
    const stale = deferred<main.WorkDTO[]>(); vi.mocked(ListWorks).mockReturnValueOnce(stale.promise);
    fireEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    fireEvent.click(screen.getByRole('button', {name: '作品1の作品操作'})); fireEvent.click(screen.getByRole('button', {name: '作品1を編集'}));
    vi.mocked(EditWork).mockResolvedValue(new main.EditWorkResult({work: work(1, '2028-03-02'), warnings: []}));
    fireEvent.change(screen.getByLabelText(/更新予定日/), {target: {value: '2028-03-02'}});
    await waitFor(() => expect((within(screen.getByRole('dialog')).getByRole('button', {name: '保存する'}) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', {name: '保存する'}));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(screen.getByText(/未来の予定を非表示にしているため/)).toBeTruthy(); expect(page()).toBe('1');
    await act(async () => {stale.resolve(fixtures());}); expect(titles()[0]).toBe('作品2');
});
it('未来だけなら説明を出し、時計巻戻しで件数が減ったときだけページを補正する', async () => {
    vi.mocked(ListWorks).mockResolvedValue(Array.from({length: 25}, (_, i) => work(i + 1, '2028-02-29')));
    vi.mocked(LoadShowFutureSchedules).mockResolvedValue(false); const view = await boot(); second();
    clock.today = '2028-02-28'; view.rerender(<App />);
    expect(screen.getByText('表示条件に一致する作品がありません')).toBeTruthy(); expect(page()).toBeUndefined();
    clock.today = '2028-02-29'; view.rerender(<App />); expect(page()).toBe('1');
});

it('遅い未来設定の復元後に登録作品のページへ移り、初期値で移動を確定しない', async () => {
    const restore = deferred<boolean>(); vi.mocked(LoadShowFutureSchedules).mockReturnValue(restore.promise);
    const values = Array.from({length: 40}, (_, i) => work(i + 1, i < 25 ? '2028-03-01' : undefined));
    vi.mocked(ListWorks).mockResolvedValue(values); vi.mocked(CreateWork).mockResolvedValue(work(41));
    render(<App />); await screen.findByRole('button', {name: '作品を登録'});
    fireEvent.click(screen.getByRole('button', {name: '作品を登録'}));
    fireEvent.change(screen.getByRole('textbox', {name: /作品URL/}), {target: {value: 'https://example.com/41'}});
    fireEvent.change(screen.getByRole('textbox', {name: /タイトル/}), {target: {value: '作品41'}});
    vi.mocked(ListWorks).mockResolvedValue([...values, work(41)]);
    fireEvent.click(screen.getByRole('button', {name: '登録する'}));
    await waitFor(() => expect(ListWorks).toHaveBeenCalledTimes(2));
    await act(async () => {restore.resolve(false);});
    await waitFor(() => expect(page()).toBe('2')); expect(titles()).toContain('作品41');
});

it('未来OFFで隠れた作品のURL処理を止めず、保存成功の通知を残す', async () => {
    const pending = deferred<main.WorkDTO>(); vi.mocked(OpenWork).mockReturnValue(pending.promise); await boot();
    fireEvent.click(screen.getByRole('button', {name: '作品4のURLを開く'})); fireEvent.click(future());
    expect(screen.getByText('「作品4」のページを開いています…')).toBeTruthy();
    await act(async () => {pending.resolve({...work(4, '2028-03-01'), lastAccessedAt: '2028-02-29T12:00:00+09:00'});});
    expect(screen.getByRole('region', {name: '作品4のURL操作'}).textContent).toContain('最終アクセス日時を保存しました');
    expect(screen.queryByRole('button', {name: '作品4のURLを開く'})).toBeNull();
});

it('アンマウントで古い未送信の未来設定保存を破棄する', async () => {
    const pending = deferred<void>(); vi.mocked(SetShowFutureSchedules).mockReturnValueOnce(pending.promise);
    const view = await boot(); fireEvent.click(future()); await waitFor(() => expect(SetShowFutureSchedules).toHaveBeenCalledTimes(1));
    fireEvent.click(future()); view.unmount(); await act(async () => {pending.resolve();});
    expect(SetShowFutureSchedules).toHaveBeenCalledTimes(1);
    vi.mocked(LoadShowFutureSchedules).mockResolvedValue(false); await boot(); expect(future().checked).toBe(false);
});
