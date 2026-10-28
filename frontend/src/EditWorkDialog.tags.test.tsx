import {StrictMode} from 'react';
import {act, cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import {EditWorkDialog} from './EditWorkDialog';
import {EditWork, LoadTags, LoadWorkTags} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';
import {readTags, tagName, validTagName} from './tag-edit';

vi.mock('../wailsjs/go/main/App', () => ({EditWork: vi.fn(), LoadTags: vi.fn(), LoadWorkTags: vi.fn()}));
const one = {id: '9007199254740993', name: '連載中'}, two = {id: '2', name: 'マンガ'};
const work = {id: 1, url: 'https://example.com/1', title: '作品', siteName: '', notes: 'メモ', thumbnailPath: '', createdAt: '2026-10-28T00:00:00+09:00', updatedAt: '2026-10-28T00:00:00+09:00'};
const stopped = vi.fn(), closed = vi.fn(), saved = vi.fn(), started = vi.fn();
const load = vi.mocked(LoadTags), current = vi.mocked(LoadWorkTags), edit = vi.mocked(EditWork);
function deferred<T>() {let resolve!: (v: T) => void, reject!: (e: unknown) => void; const promise = new Promise<T>((yes, no) => {resolve = yes; reject = no;}); return {promise, resolve, reject};}
const saveButton = () => screen.getByRole('button', {name: '保存する'}) as HTMLButtonElement;
const query = () => screen.getByLabelText('候補を検索／新しいタグ名を入力');
const submit = () => fireEvent.submit(screen.getByRole('dialog').querySelector('form')!);
async function boot() {const view = render(<StrictMode><EditWorkDialog work={work} onStopped={stopped} onClose={closed} onSaved={saved} onStart={started} /></StrictMode>); await waitFor(() => expect(saveButton().disabled).toBe(false)); return view;}
beforeEach(() => {
    vi.resetAllMocks(); load.mockResolvedValue([one, two]); current.mockResolvedValue([one]);
    edit.mockResolvedValue(new main.EditWorkResult({work, warnings: []}));
    Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {configurable: true, value: function(this: HTMLDialogElement) {this.open = true;}});
    Object.defineProperty(HTMLDialogElement.prototype, 'close', {configurable: true, value: function(this: HTMLDialogElement) {this.open = false;}});
});
afterEach(() => {cleanup(); Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal'); Reflect.deleteProperty(HTMLDialogElement.prototype, 'close');});

it('候補検索は半角を吸収し、チェックと複数仮追加を正確な文字列IDで一括送信する', async () => {
    await boot(); fireEvent.change(query(), {target: {value: 'ﾏﾝｶﾞ'}});
    expect(screen.queryByRole('checkbox', {name: '連載中'})).toBeNull();
    await userEvent.click(screen.getByRole('checkbox', {name: 'マンガ'}));
    // A width-equivalent spelling remains an explicitly different saved name.
    await userEvent.click(screen.getByRole('button', {name: '＋「ﾏﾝｶﾞ」を新規タグとして仮追加'}));
    fireEvent.change(query(), {target: {value: '　週末　'}});
    await userEvent.click(screen.getByRole('button', {name: '＋「週末」を新規タグとして仮追加'}));
    expect(document.activeElement).toBe(query());
    fireEvent.change(query(), {target: {value: '週末'}}); expect(screen.queryByRole('button', {name: /新規タグとして仮追加/})).toBeNull();
    await userEvent.click(saveButton());
    expect(edit).toHaveBeenCalledTimes(1);
    expect(JSON.parse(JSON.stringify(edit.mock.calls[0][1])).tags).toEqual({existing: [one, two], newNames: ['ﾏﾝｶﾞ', '週末']});
    expect(closed).toHaveBeenCalledOnce();
});
it('読込中の本文を保持し、失敗/不正値を空選択として送らず再取得成功後に保存できる', async () => {
    const pending = deferred<main.TagDTO[]>(); load.mockReturnValue(pending.promise);
    render(<EditWorkDialog work={work} onStopped={stopped} onClose={closed} onSaved={saved} onStart={started} />);
    fireEvent.change(screen.getByLabelText(/タイトル/), {target: {value: '途中入力'}}); submit(); expect(edit).not.toHaveBeenCalled();
    await act(async () => pending.reject(new Error('private-path')));
    expect(saveButton().disabled).toBe(true); expect(document.body.textContent).not.toContain('private-path');
    load.mockResolvedValueOnce([{id: '01', name: '不正'}]); await userEvent.click(screen.getByRole('button', {name: 'タグを再読み込み'}));
    expect(saveButton().disabled).toBe(true); submit(); expect(edit).not.toHaveBeenCalled();
    load.mockResolvedValue([one, two]); await userEvent.click(screen.getByRole('button', {name: 'タグを再読み込み'}));
    await waitFor(() => expect(saveButton().disabled).toBe(false));
    expect((screen.getByLabelText(/タイトル/) as HTMLInputElement).value).toBe('途中入力');
    await userEvent.click(saveButton()); expect(edit.mock.calls[0][1].tags?.existing).toEqual([one]);
});
it('改名/削除後の再取得が失敗しても本文・選択・仮追加を保持し、変更だけ明示確認する', async () => {
    await boot(); await userEvent.click(screen.getByRole('checkbox', {name: 'マンガ'}));
    fireEvent.change(screen.getByLabelText(/メモ/), {target: {value: '編集中'}});
    fireEvent.change(query(), {target: {value: '新規A'}}); await userEvent.click(screen.getByRole('button', {name: /新規タグとして仮追加/}));
    edit.mockRejectedValueOnce({code: 'tags_changed', message: 'タグが変更されました。'});
    load.mockRejectedValueOnce({code: 'storage_error', message: 'private diagnostic'});
    await userEvent.click(saveButton()); await screen.findByRole('button', {name: 'タグを再読み込み'});
    expect(saveButton().disabled).toBe(true); expect(screen.getByText('新規A')).toBeTruthy();
    const renamed = {...one, name: '連載'}; load.mockResolvedValue([renamed]); current.mockResolvedValue([renamed]);
    await userEvent.click(screen.getByRole('button', {name: 'タグを再読み込み'}));
    await screen.findByRole('button', {name: '「連載」を採用する'}); submit(); expect(edit).toHaveBeenCalledTimes(1);
    await userEvent.click(screen.getByRole('button', {name: '「連載」を採用する'})); expect(saveButton().disabled).toBe(true);
    await userEvent.click(screen.getByRole('button', {name: '「マンガ」を選択から外す'})); expect(saveButton().disabled).toBe(false);
    expect((screen.getByLabelText(/メモ/) as HTMLTextAreaElement).value).toBe('編集中');
    await userEvent.click(saveButton());
    expect(edit.mock.calls[1][1].tags).toMatchObject({existing: [renamed], newNames: ['新規A']});
});
it('保存中の連打/解除/取消を止め、既知失敗では途中状態を残す', async () => {
    await boot(); const pending = deferred<main.EditWorkResult>(); edit.mockReturnValue(pending.promise);
    fireEvent.change(query(), {target: {value: '仮追加'}}); await userEvent.click(screen.getByRole('button', {name: /新規タグとして仮追加/}));
    act(() => {submit(); submit(); fireEvent(screen.getByRole('dialog'), new Event('cancel', {cancelable: true}));});
    expect(edit).toHaveBeenCalledOnce(); expect(closed).not.toHaveBeenCalled();
    expect((screen.getByRole('button', {name: 'タグ「連載中」を外す'}) as HTMLButtonElement).disabled).toBe(true);
    await act(async () => pending.reject({code: 'duplicate_url', message: 'URLが重複しています。'}));
    expect(screen.getByText('仮追加')).toBeTruthy(); expect(saveButton().disabled).toBe(false);
});
it('全解除は明示空を送り、取消は仮追加も送信しない', async () => {
    await boot(); await userEvent.click(screen.getByRole('button', {name: 'タグ「連載中」を外す'}));
    await userEvent.click(saveButton()); expect(edit.mock.calls[0][1].tags).toMatchObject({existing: [], newNames: []});
    cleanup(); edit.mockClear(); await boot();
    fireEvent.change(query(), {target: {value: '取消用'}}); await userEvent.click(screen.getByRole('button', {name: /新規タグとして仮追加/}));
    await userEvent.click(screen.getByRole('button', {name: 'キャンセル'})); expect(edit).not.toHaveBeenCalled();
});
it('候補欄のIME/通常Enterで作品を送信せず、チェックをSpaceで選べる', async () => {
    await boot(); query().focus();
    const event = new KeyboardEvent('keydown', {key: 'Enter', bubbles: true, cancelable: true, isComposing: true});
    query().dispatchEvent(event); expect(event.defaultPrevented).toBe(true);
    await userEvent.keyboard('{Enter}'); expect(edit).not.toHaveBeenCalled();
    const checkbox = screen.getByRole('checkbox', {name: 'マンガ'}); checkbox.focus(); await userEvent.keyboard(' ');
    expect((checkbox as HTMLInputElement).checked).toBe(true);
});
it('StrictModeの古い取得結果と閉じた後の応答を反映しない', async () => {
    const stale = deferred<main.TagDTO[]>(); load.mockReturnValueOnce(stale.promise);
    const view = await boot(); await act(async () => stale.resolve([]));
    expect(screen.getByRole('button', {name: 'タグ「連載中」を外す'})).toBeTruthy();
    const late = deferred<main.TagDTO[]>(); load.mockReturnValue(late.promise);
    view.unmount(); render(<EditWorkDialog work={work} onStopped={stopped} onClose={closed} onSaved={saved} onStart={started} />);
    cleanup(); await act(async () => late.reject({code: 'database_stopped', message: '停止'}));
    expect(stopped).not.toHaveBeenCalled(); expect(edit).not.toHaveBeenCalled();
});
it('片方の通常失敗の後でも、もう片方のDB停止を観測し再取得・保存を止める', async () => {
    const late = deferred<main.TagDTO[]>(); load.mockRejectedValue({code: 'storage_error'}); current.mockReturnValue(late.promise);
    render(<EditWorkDialog work={work} onStopped={stopped} onClose={closed} onSaved={saved} onStart={started} />);
    await act(async () => late.reject({code: 'database_stopped', message: '停止'}));
    expect(stopped).toHaveBeenCalledOnce(); expect(saveButton().disabled).toBe(true);
    expect((screen.getByRole('button', {name: 'タグを再読み込み'}) as HTMLButtonElement).disabled).toBe(true);
    submit(); expect(edit).not.toHaveBeenCalled(); expect(screen.getByRole('button', {name: '閉じる'})).toBeTruthy();
});
it('タグ名のGo互換空白/コードポイントとID精度・不正保存値を検証する', () => {
    expect(tagName('\u0085　漫画\t')).toBe('漫画'); expect(tagName('\uFEFF漫画')).toBe('\uFEFF漫画');
    expect(validTagName('😀'.repeat(50))).toBe(true); expect(validTagName('😀'.repeat(51))).toBe(false);
    for (const name of ['', ' a', 'a\nb', 'a\u2028b', '\ud800']) expect(validTagName(name)).toBe(false);
    expect(readTags([{id: '9223372036854775807', name: '漫画'}])[0].id).toBe('9223372036854775807');
    for (const value of [null, [{id: 1, name: 'a'}], [{id: '9223372036854775808', name: 'a'}], [one, one], [{id: '1', name: ' a'}]]) expect(() => readTags(value)).toThrow();
});
