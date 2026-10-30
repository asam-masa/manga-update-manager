import {StrictMode, useState} from 'react';
import {act, cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import {TagManagementDialog} from './TagManagementDialog';
import {LoadTagUsage, RenameTag} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';
import type {TagNotice} from './TagManagementNotices';

vi.mock('../wailsjs/go/main/App', () => ({LoadTagUsage: vi.fn(), RenameTag: vi.fn()}));
const old = {id: '9223372036854775807', name: 'マンガ'};
const initial = [{tag: old, workCount: 2}, {tag: {id: '1', name: '別タグ'}, workCount: 0}].map((row) => main.TagUsageDTO.createFrom(row));
const load = vi.mocked(LoadTagUsage), rename = vi.mocked(RenameTag);
const close = vi.fn(), notice = vi.fn(), stop = vi.fn();
const result = (name: string, warnings: {code: string; message: string}[] = []) => main.TagChangeResult.createFrom({tag: {...old, name}, warnings});
function Harness() {
    const [stopped, setStopped] = useState(false), [notices, setNotices] = useState<TagNotice[]>([]);
    return <TagManagementDialog stopped={stopped} onStopped={() => {stop(); setStopped(true);}} onClose={close} notices={notices}
        onNotice={(value) => {notice(value); setNotices([value]);}} />;
}
function deferred<T>() {
    let resolve!: (value: T) => void, reject!: (reason: unknown) => void;
    const promise = new Promise<T>((yes, no) => {resolve = yes; reject = no;}); return {promise, resolve, reject};
}
async function boot(query = '') {
    const view = render(<StrictMode><Harness /></StrictMode>);
    await screen.findByRole('button', {name: '「マンガ」の名前変更'});
    if (query) await userEvent.type(screen.getByRole('searchbox'), query);
    await userEvent.click(screen.getByRole('button', {name: '「マンガ」の名前変更'}));
    return view;
}
const input = () => screen.getByRole('textbox', {name: '新しいタグ名'}) as HTMLInputElement;
const save = () => screen.getByRole('button', {name: '保存'}) as HTMLButtonElement;
async function typeName(name: string) {fireEvent.change(input(), {target: {value: name}});}
beforeEach(() => {
    vi.clearAllMocks(); load.mockReset().mockResolvedValue(initial); rename.mockReset().mockResolvedValue(result('新しいタグ'));
    Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {configurable: true, value: function(this: HTMLDialogElement) {this.open = true;}});
    Object.defineProperty(HTMLDialogElement.prototype, 'close', {configurable: true, value: function(this: HTMLDialogElement) {this.open = false;}});
});
afterEach(() => {cleanup(); Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal'); Reflect.deleteProperty(HTMLDialogElement.prototype, 'close');});

it('1タグずつ編集し、確認IDを丸めず送り、成功後は検索を保ち対象外なら検索へ戻す', async () => {
    await boot('ﾏﾝｶﾞ'); await typeName(' 新しいタグ ');
    expect((screen.getByRole('searchbox') as HTMLInputElement).disabled).toBe(true);
    const calls = load.mock.calls.length;
    await userEvent.click(save());
    await screen.findByText('検索条件に一致するタグがありません。');
    expect(JSON.parse(JSON.stringify(rename.mock.calls[0][0]))).toEqual(old);
    expect(rename.mock.calls[0][1]).toBe(' 新しいタグ ');
    expect(load).toHaveBeenCalledTimes(calls);
    expect(notice.mock.calls[0][0].message).toContain('新しいタグ');
    expect(document.activeElement).toBe(screen.getByRole('searchbox'));
});

it('表示に残る改名後は同じタグの名前変更へ戻り、他タグ操作は編集中無効', async () => {
    await boot();
    expect((screen.getByRole('button', {name: '「別タグ」の名前変更'}) as HTMLButtonElement).disabled).toBe(true);
    await typeName('新しいタグ'); await userEvent.click(save());
    const target = await screen.findByRole('button', {name: '「新しいタグ」の名前変更'});
    expect(document.activeElement).toBe(target);
    expect(screen.getByText('2作品')).toBeTruthy();
});

it('保存中の二重送信・閉じる・Escを防ぎ、IME確定Enterをフォーム送信しない', async () => {
    await boot(); await typeName('新しいタグ');
    const form = document.getElementById('tag-rename-form')!;
    expect(fireEvent.keyDown(input(), {key: 'Enter', isComposing: true})).toBe(false);
    const next = deferred<main.TagChangeResult>(); rename.mockReturnValue(next.promise);
    fireEvent.submit(form); fireEvent.submit(form);
    fireEvent(screen.getByRole('dialog'), new Event('cancel', {cancelable: true}));
    expect(rename).toHaveBeenCalledTimes(1); expect(close).not.toHaveBeenCalled();
    expect((screen.getByRole('button', {name: '閉じる'}) as HTMLButtonElement).disabled).toBe(true);
    await act(async () => {next.resolve(result('新しいタグ'));});
});

it('変更なしなら閉じ、変更した入力は破棄確認から戻れる', async () => {
    await boot(); fireEvent(screen.getByRole('dialog'), new Event('cancel', {cancelable: true})); expect(close).toHaveBeenCalledTimes(1);
    await typeName('途中入力');
    await userEvent.click(screen.getByRole('button', {name: '閉じる'}));
    expect(close).toHaveBeenCalledTimes(1);
    expect(screen.getByText('変更を破棄して閉じますか？')).toBeTruthy();
    await userEvent.click(screen.getByRole('button', {name: '編集に戻る'}));
    expect(input().value).toBe('途中入力'); expect(document.activeElement).toBe(input());
    await userEvent.click(screen.getByRole('button', {name: '閉じる'}));
    await userEvent.click(screen.getByRole('button', {name: '変更を破棄して閉じる'}));
    expect(close).toHaveBeenCalledTimes(2); expect(rename).not.toHaveBeenCalled();
});

it('破棄確認中に再取得が終わっても選択中のボタンからフォーカスを奪わない', async () => {
    await boot(); await typeName('途中入力');
    const next = deferred<main.TagUsageDTO[]>(); load.mockReturnValue(next.promise);
    await userEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    await userEvent.click(screen.getByRole('button', {name: '閉じる'}));
    const discard = screen.getByRole('button', {name: '変更を破棄して閉じる'});
    discard.focus(); expect(document.activeElement).toBe(discard);
    await act(async () => {next.resolve(initial);});
    expect(document.activeElement).toBe(discard);
});

it('競合後は入力を残し、現在名の明示確認と再保存を別操作にする', async () => {
    await boot('ﾏﾝｶﾞ'); await typeName('自分の入力');
    rename.mockRejectedValueOnce({code: 'tag_changed', message: '名前が変わりました'});
    load.mockResolvedValue([{tag: {...old, name: '現在名'}, workCount: 3}].map((row) => main.TagUsageDTO.createFrom(row)));
    await userEvent.click(save());
    const confirm = await screen.findByRole('button', {name: '現在の名前を確認して編集を続ける'});
    expect(input().value).toBe('自分の入力'); expect(save().disabled).toBe(true);
    await userEvent.click(confirm); expect(rename).toHaveBeenCalledTimes(1); expect(input().value).toBe('自分の入力');
    rename.mockResolvedValue(result('自分の入力'));
    await userEvent.click(save());
    await screen.findByText('検索条件に一致するタグがありません。');
    expect(rename.mock.calls[1][0].name).toBe('現在名');
});

it('競合の再取得失敗で入力と前回表示を残し、成功まで保存を止める', async () => {
    await boot(); await typeName('自分の入力');
    rename.mockRejectedValue({code: 'tag_changed', message: '変更あり'}); load.mockRejectedValue({code: 'storage_error', message: '読込失敗'});
    await userEvent.click(save()); await screen.findByText('読込失敗');
    expect(input().value).toBe('自分の入力'); expect(save().disabled).toBe(true);
    expect(screen.getByText('前回の表示です。最新情報は未確認です。')).toBeTruthy();
    load.mockResolvedValue(initial); await userEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    await waitFor(() => expect(save().disabled).toBe(false)); expect(rename).toHaveBeenCalledTimes(1);
});

it('最後のタグがなくなっても入力を隠さず、再作成や再送しない', async () => {
    await boot(); await typeName('残す入力'); rename.mockRejectedValue({code: 'tag_not_found', message: '不存在'}); load.mockResolvedValue([]);
    await userEvent.click(save()); await screen.findByText('このタグは存在しません。入力を控えてキャンセルしてください。');
    expect(input().value).toBe('残す入力'); expect(save().disabled).toBe(true);
    expect(rename).toHaveBeenCalledTimes(1); expect(screen.queryByText('タグはまだありません。')).toBeNull();
});

it('同名エラーは入力を残して手動修正でき、勝手に統合・再取得しない', async () => {
    await boot(); await typeName('別タグ'); rename.mockRejectedValue({code: 'tag_duplicate_name', message: '同じ名前'});
    const calls = load.mock.calls.length; await userEvent.click(save()); await screen.findByText('同じ名前');
    expect(input().value).toBe('別タグ'); expect(stop).not.toHaveBeenCalled(); expect(load).toHaveBeenCalledTimes(calls);
});

it.each([' ', 'あ'.repeat(51), 'a\u0001b'])('不正な入力を送信しない: %j', async (name) => {
    await boot(); await typeName(name); expect(save().disabled).toBe(true);
    fireEvent.submit(document.getElementById('tag-rename-form')!); expect(rename).not.toHaveBeenCalled();
});

it('再取得開始と保存が同じイベントで重なっても古い確認を送らない', async () => {
    await boot(); await typeName('新しいタグ');
    const next = deferred<main.TagUsageDTO[]>(); load.mockReturnValue(next.promise);
    act(() => {
        fireEvent.click(screen.getByRole('button', {name: '再読み込み'}));
        fireEvent.submit(document.getElementById('tag-rename-form')!);
    });
    expect(rename).not.toHaveBeenCalled();
    await act(async () => {next.resolve(initial);});
});

it('保存成功の新名と古い一覧の別タグ名が一致しても確定を取り消さない', async () => {
    await boot(); await typeName('別タグ'); rename.mockResolvedValue(result('別タグ'));
    const calls = load.mock.calls.length; await userEvent.click(save());
    await screen.findByText('タグ「マンガ」を「別タグ」に変更しました。');
    expect(stop).not.toHaveBeenCalled(); expect(notice).toHaveBeenCalled();
    expect(screen.getByText('前回の表示です。最新情報は未確認です。')).toBeTruthy();
    expect(load).toHaveBeenCalledTimes(calls);
    expect((screen.getAllByRole('button', {name: '「別タグ」の名前変更'})[0] as HTMLButtonElement).disabled).toBe(true);
});

it('確定後の停止警告は成功として通知し、再取得や追加保存を行わない', async () => {
    await boot(); await typeName('新しいタグ');
    rename.mockResolvedValue(result('新しいタグ', [{code: 'database_stopped_after_commit', message: '確定済み・再起動してください'}]));
    const calls = load.mock.calls.length; await userEvent.click(save());
    await screen.findByText('確定済み・再起動してください');
    expect(notice.mock.calls[0][0].message).toContain('変更しました'); expect(stop).toHaveBeenCalled();
    expect(load).toHaveBeenCalledTimes(calls); expect(screen.queryByRole('textbox')).toBeNull();
    expect(document.activeElement).toBe(screen.getByRole('button', {name: '閉じる'}));
});

it.each([
    {label: '結果不明', failure: {code: 'tag_change_outcome_unknown', message: '結果不明'}},
    {label: '生の診断', failure: new Error('private diagnostic')},
    {label: '別IDの成功', success: {tag: {id: '1', name: '新しいタグ'}, warnings: []}},
    {label: '別名の成功', success: {tag: old, warnings: []}},
    {label: '不正な警告', success: {tag: {...old, name: '新しいタグ'}, warnings: [{code: 'unknown', message: 'private diagnostic'}]}},
])('$labelを再送可能な通常失敗とせず、入力と安全な通知を残す', async ({failure, success}) => {
    await boot(); await typeName('新しいタグ');
    if (failure) rename.mockRejectedValue(failure); else rename.mockResolvedValue(success as main.TagChangeResult);
    await userEvent.click(save()); await waitFor(() => expect(stop).toHaveBeenCalled());
    expect(input().value).toBe('新しいタグ'); expect(save().disabled).toBe(true);
    expect(notice).toHaveBeenCalled(); expect(screen.queryByText('private diagnostic')).toBeNull();
    fireEvent.submit(document.getElementById('tag-rename-form')!); expect(rename).toHaveBeenCalledTimes(1);
});
