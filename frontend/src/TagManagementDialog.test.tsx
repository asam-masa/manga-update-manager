import {StrictMode} from 'react';
import {act, cleanup, fireEvent, render, screen, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import {TagManagementDialog} from './TagManagementDialog';
import {LoadTagUsage} from '../wailsjs/go/main/App';
import {readTagUsage} from './tag-management';
import {main} from '../wailsjs/go/models';

vi.mock('../wailsjs/go/main/App', () => ({LoadTagUsage: vi.fn()}));
const load = vi.mocked(LoadTagUsage);
const usage = [
    {tag: {id: '9007199254740993', name: 'シリーズ10'}, workCount: 1},
    {tag: {id: '2', name: 'マンガ'}, workCount: 12},
    {tag: {id: '9007199254740992', name: 'シリーズ2'}, workCount: 0},
].map((value) => main.TagUsageDTO.createFrom(value));
const close = vi.fn(), stop = vi.fn();
function deferred() {
    let resolve!: (value: main.TagUsageDTO[]) => void, reject!: (reason: unknown) => void;
    const promise = new Promise<main.TagUsageDTO[]>((yes, no) => {resolve = yes; reject = no;});
    return {promise, resolve, reject};
}
function view(stopped = false) { return <StrictMode><TagManagementDialog stopped={stopped} onStopped={stop} onClose={close} /></StrictMode>; }
beforeEach(() => {
    vi.clearAllMocks(); load.mockReset().mockResolvedValue(usage);
    Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {configurable: true, value: function(this: HTMLDialogElement) {this.open = true;}});
    Object.defineProperty(HTMLDialogElement.prototype, 'close', {configurable: true, value: function(this: HTMLDialogElement) {this.open = false;}});
});
afterEach(() => {cleanup(); Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal'); Reflect.deleteProperty(HTMLDialogElement.prototype, 'close');});

it('未使用も自然順に表示し、表記差を吸収した検索でも全作品の使用数を保つ', async () => {
    render(view());
    await screen.findByText('シリーズ2');
    const names = screen.getAllByRole('rowheader').map((node) => node.textContent);
    expect(names.indexOf('シリーズ2')).toBeLessThan(names.indexOf('シリーズ10'));
    expect(screen.getByText('0作品')).toBeTruthy();
    expect(document.activeElement).toBe(screen.getByRole('searchbox'));
    await userEvent.type(screen.getByRole('searchbox'), 'ﾏﾝｶﾞ');
    expect(screen.getByText('マンガ')).toBeTruthy(); expect(screen.getByText('12作品')).toBeTruthy();
    expect(screen.getByText('1件 / 全3件')).toBeTruthy();
    expect(screen.queryByText('シリーズ2')).toBeNull();
    fireEvent.change(screen.getByRole('searchbox'), {target: {value: 'なし'}});
    expect(screen.getByText('検索条件に一致するタグがありません。')).toBeTruthy();
});

it('空一覧と初期失敗を区別し、診断を表示せず手動で再取得できる', async () => {
    load.mockRejectedValue(new Error('private diagnostic'));
    render(view());
    await screen.findByRole('alert');
    expect(screen.queryByText('タグはまだありません。')).toBeNull();
    expect(screen.queryByText('private diagnostic')).toBeNull();
    load.mockResolvedValue([]);
    await userEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    expect(await screen.findByText('タグはまだありません。')).toBeTruthy();
    expect(screen.queryByRole('alert')).toBeNull();
});

it('再取得の処理中・失敗も前回表示と検索を残し、二重送信しない', async () => {
    render(view()); await screen.findByText('マンガ');
    await userEvent.type(screen.getByRole('searchbox'), 'ﾏﾝｶﾞ');
    const next = deferred(); load.mockReturnValue(next.promise);
    const reload = screen.getByRole('button', {name: '再読み込み'});
    const count = load.mock.calls.length;
    fireEvent.click(reload); fireEvent.click(reload);
    expect(load).toHaveBeenCalledTimes(count + 1);
    expect(screen.getByText('マンガ')).toBeTruthy();
    expect(screen.getByText('前回の表示です。最新情報は未確認です。')).toBeTruthy();
    await act(async () => {next.reject({code: 'storage_error', message: '取得失敗'});});
    expect(screen.getByRole('alert').textContent).toBe('取得失敗');
    expect((screen.getByRole('searchbox') as HTMLInputElement).value).toBe('ﾏﾝｶﾞ');
    expect(screen.getByText('12作品')).toBeTruthy();
});

it('StrictModeの失効応答が最新表示を上書きせず、停止を誤通知しない', async () => {
    const old = deferred(); load.mockReturnValueOnce(old.promise).mockResolvedValue(usage);
    render(view()); await screen.findByText('シリーズ2');
    await act(async () => {old.reject({code: 'database_stopped', message: '旧応答'});});
    expect(stop).not.toHaveBeenCalled();
    expect(screen.getByText('シリーズ2')).toBeTruthy();
});

it('取得中でも閉じられ、閉じた画面の遅い応答は次の画面へ反映しない', async () => {
    const old = deferred(); load.mockReturnValue(old.promise);
    const first = render(view());
    fireEvent(screen.getByRole('dialog'), new Event('cancel', {bubbles: false, cancelable: true})); expect(close).toHaveBeenCalledTimes(1);
    first.unmount(); load.mockResolvedValue([]); render(view());
    await screen.findByText('タグはまだありません。');
    await act(async () => {old.resolve(usage);});
    expect(screen.queryByText('マンガ')).toBeNull();
});

it('停止を親へ通知し、取得再試行や閉じる操作で解除しない', async () => {
    load.mockRejectedValue({code: 'database_stopped', message: '停止'});
    const {rerender} = render(view());
    await screen.findByRole('alert'); expect(stop).toHaveBeenCalledTimes(1);
    expect((screen.getByRole('button', {name: '再読み込み'}) as HTMLButtonElement).disabled).toBe(true);
    rerender(view(false));
    expect((screen.getByRole('button', {name: '再読み込み'}) as HTMLButtonElement).disabled).toBe(true);
    await userEvent.click(screen.getByRole('button', {name: '閉じる'})); expect(close).toHaveBeenCalledTimes(1);
});

it('他の操作で停止したら読み取り途中の成功も採用しない', async () => {
    const old = deferred(); load.mockReturnValue(old.promise);
    const {rerender} = render(view()); rerender(view(true));
    await act(async () => {old.resolve(usage);});
    expect(screen.queryByText('マンガ')).toBeNull();
    expect(screen.getByRole('alert').textContent).toContain('再起動');
});

it('タグIDを丸めず、同順位は作成IDの昇順で揃える', () => {
    const values = readTagUsage([
        {tag: {id: '9007199254740993', name: 'シリーズ02'}, workCount: 0},
        {tag: {id: '9007199254740992', name: 'シリーズ2'}, workCount: 1},
    ]);
    expect(values.map(({tag}) => tag.id)).toEqual(['9007199254740992', '9007199254740993']);
});

it.each([
    null, [{tag: {id: '1', name: 'タグ'}, workCount: -1}],
    [{tag: {id: '1', name: 'タグ'}, workCount: 0.5}],
    [{tag: {id: '1', name: 'タグ'}, workCount: Number.MAX_SAFE_INTEGER + 1}],
    [{tag: {id: '01', name: 'タグ'}, workCount: 0}],
    [{tag: {id: '1', name: 'タグ'}, workCount: 1}, {tag: {id: '2', name: 'タグ'}, workCount: 0}],
    [usage[0], {tag: {id: '9007199254740993', name: '別名'}, workCount: 0}],
].map((value) => ({value})))('不正な応答を部分一覧や未使用として採用しない: $value', async ({value}) => {
    load.mockResolvedValue(value as main.TagUsageDTO[]);
    render(view()); await screen.findByRole('alert');
    expect(screen.queryByRole('table')).toBeNull(); expect(screen.queryByText('タグはまだありません。')).toBeNull();
    expect(within(screen.getByRole('dialog')).getByRole('button', {name: '閉じる'})).toBeTruthy();
});
