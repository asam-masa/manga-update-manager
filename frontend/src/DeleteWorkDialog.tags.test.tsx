import {StrictMode} from 'react';
import {act, cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import {DeleteWorkDialog} from './DeleteWorkDialog';
import {DeleteWork, LoadWorkTags} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';

vi.mock('../wailsjs/go/main/App', () => ({DeleteWork: vi.fn(), LoadWorkTags: vi.fn()}));
const tag = {id: '9223372036854775807', name: '連載中'};
const work = {id: 1, url: 'https://example.com/1', title: '表示確認', siteName: '', notes: 'メモ', thumbnailPath: '', createdAt: '2026-10-28T00:00:00+09:00', updatedAt: '2026-10-28T00:00:00+09:00'};
const reload = vi.fn<() => Promise<typeof work | undefined>>(), removed = vi.fn(), closed = vi.fn(), stopped = vi.fn(), imagesStopped = vi.fn(), started = vi.fn();
const load = vi.mocked(LoadWorkTags), remove = vi.mocked(DeleteWork);
const button = () => screen.getByRole('button', {name: '削除する'}) as HTMLButtonElement;
function deferred<T>() {let resolve!: (v: T) => void, reject!: (e: unknown) => void; const promise = new Promise<T>((yes, no) => {resolve = yes; reject = no;}); return {promise, resolve, reject};}
function mount(strict = false) {const node = <DeleteWorkDialog work={work} onReload={reload} onRemoved={removed} onClose={closed} onStopped={stopped} onImagesStopped={imagesStopped} onStart={started} />; return render(strict ? <StrictMode>{node}</StrictMode> : node);}
async function ready() {await waitFor(() => expect(button().disabled).toBe(false));}
beforeEach(() => {
    vi.resetAllMocks(); load.mockResolvedValue([tag]); reload.mockResolvedValue(work);
    remove.mockResolvedValue(new main.DeleteWorkResult({workID: 1, warnings: []}));
    Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {configurable: true, value: function(this: HTMLDialogElement) {this.open = true;}});
    Object.defineProperty(HTMLDialogElement.prototype, 'close', {configurable: true, value: function(this: HTMLDialogElement) {this.open = false;}});
});
afterEach(() => {cleanup(); Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal'); Reflect.deleteProperty(HTMLDialogElement.prototype, 'close');});

it('未取得では削除を許可せず、読取完了後だけ正確な文字列ID/名前集合を送る', async () => {
    const pending = deferred<main.TagDTO[]>(); load.mockReturnValue(pending.promise); mount();
    expect(screen.getByText('未取得')).toBeTruthy(); expect(button().disabled).toBe(true);
    fireEvent.click(button()); expect(remove).not.toHaveBeenCalled(); expect(started).not.toHaveBeenCalled();
    await act(async () => pending.resolve([tag])); expect(screen.getByText('連載中')).toBeTruthy();
    await userEvent.click(button());
    expect(remove).toHaveBeenCalledOnce();
    expect(JSON.parse(JSON.stringify(remove.mock.calls[0][1])).tags).toEqual([tag]);
    expect(removed).toHaveBeenCalledWith(work, false, []); expect(closed).toHaveBeenCalledOnce();
});
it('タグなしは取得成功した空配列だけを表示し、省略せず送信する', async () => {
    load.mockResolvedValue([]); mount(); await ready(); expect(screen.getByText('タグなし')).toBeTruthy();
    await userEvent.click(button()); expect(remove.mock.calls[0][1].tags).toEqual([]);
});
it('初期読取中のキャンセルとEscを許可し、閉じた後の停止応答を反映しない', async () => {
    const pending = deferred<main.TagDTO[]>(); load.mockReturnValue(pending.promise); const view = mount();
    await userEvent.click(screen.getByRole('button', {name: 'キャンセル'}));
    fireEvent(screen.getByRole('dialog'), new Event('cancel', {cancelable: true})); expect(closed).toHaveBeenCalledTimes(2);
    view.unmount(); await act(async () => pending.reject({code: 'database_stopped', message: '停止'}));
    expect(stopped).not.toHaveBeenCalled(); expect(remove).not.toHaveBeenCalled();
});
it('読取失敗と不正値を空タグにせず、再取得は作品とタグの両方を読み直す', async () => {
    load.mockRejectedValueOnce(new Error('private diagnostic')); mount(); await screen.findByRole('alert');
    expect(document.body.textContent).not.toContain('private diagnostic'); expect(screen.queryByText('タグなし')).toBeNull();
    expect(button().disabled).toBe(true);
    load.mockResolvedValueOnce([{id: '01', name: '不正'}]);
    await userEvent.click(screen.getByRole('button', {name: '最新情報を再読み込み'}));
    expect(button().disabled).toBe(true); expect(reload).toHaveBeenCalledOnce();
    const latest = {...work, title: '最新名', url: 'https://example.com/2'}; reload.mockResolvedValue(latest);
    load.mockResolvedValue([tag]); await userEvent.click(screen.getByRole('button', {name: '最新情報を再読み込み'})); await ready();
    expect(screen.getByText('最新名')).toBeTruthy(); expect(screen.getByText('連載中')).toBeTruthy(); expect(remove).not.toHaveBeenCalled();
    await userEvent.click(button()); expect(remove.mock.calls[0][1]).toMatchObject({title: '最新名', url: latest.url, tags: [tag]});
});
it('タグ変更で作品情報とタグを両方再確認し、自動削除せず再度の明示操作を待つ', async () => {
    mount(); await ready(); const tags = deferred<main.TagDTO[]>(); load.mockReturnValueOnce(tags.promise);
    reload.mockResolvedValue({...work, title: '変更後'}); remove.mockRejectedValueOnce({code: 'work_changed', message: '変更あり'});
    await userEvent.click(button()); await screen.findByText(/前回の確認内容/);
    expect(button().disabled).toBe(true); expect(screen.getByText('表示確認')).toBeTruthy();
    expect((screen.getByRole('button', {name: 'キャンセル'}) as HTMLButtonElement).disabled).toBe(true);
    const renamed = {...tag, name: '連載'}; await act(async () => tags.resolve([renamed])); await ready();
    expect(screen.getByText('変更後')).toBeTruthy(); expect(screen.getByText('連載')).toBeTruthy();
    expect(screen.queryByText('連載中')).toBeNull(); expect(remove).toHaveBeenCalledOnce();
    expect(document.activeElement).toBe(screen.getByRole('button', {name: 'キャンセル'}));
    await userEvent.click(button()); expect(remove.mock.calls[1][1]).toMatchObject({title: '変更後', tags: [renamed]});
});
it('再確認に片方だけ成功しても確認情報を部分更新せず、失敗後は削除しない', async () => {
    mount(); await ready(); remove.mockRejectedValueOnce({code: 'work_changed', message: '変更あり'});
    reload.mockResolvedValue({...work, title: '先行した名前'}); load.mockRejectedValueOnce(new Error('private tag error'));
    await userEvent.click(button()); await screen.findByRole('button', {name: '最新情報を再読み込み'});
    expect(button().disabled).toBe(true); expect(screen.getByText('表示確認')).toBeTruthy(); expect(screen.getByText('連載中')).toBeTruthy();
    expect(screen.queryByText('先行した名前')).toBeNull(); expect(document.body.textContent).not.toContain('private tag error');
    load.mockResolvedValue([]); await userEvent.click(screen.getByRole('button', {name: '最新情報を再読み込み'})); await ready();
    expect(screen.getByText('タグなし')).toBeTruthy(); expect(remove).toHaveBeenCalledOnce();
});
it.each(['missing', 'failure'])('%sが先に返っても遅いタグ読取のDB停止を優先する', async (kind) => {
    mount(); await ready(); remove.mockRejectedValueOnce({code: 'work_changed', message: '変更あり'});
    if (kind === 'missing') reload.mockResolvedValue(undefined); else reload.mockRejectedValue(new Error('private read error'));
    const pending = deferred<main.TagDTO[]>(); load.mockReturnValueOnce(pending.promise);
    await userEvent.click(button()); await act(async () => pending.reject({code: 'database_stopped', message: '停止'}));
    expect(stopped).toHaveBeenCalledOnce(); expect(removed).not.toHaveBeenCalled(); expect(button().disabled).toBe(true);
    expect(screen.queryByRole('button', {name: '最新情報を再読み込み'})).toBeNull(); expect(screen.getByRole('button', {name: '閉じる'})).toBeTruthy();
});
it('取得で作品不存在を確認した場合も、今回削除したという成功にしない', async () => {
    load.mockRejectedValue({code: 'work_not_found', message: '存在しません'}); mount();
    await waitFor(() => expect(closed).toHaveBeenCalledOnce()); expect(removed).toHaveBeenCalledWith(work, true, []); expect(remove).not.toHaveBeenCalled();
});
it('StrictModeの古いタグ応答で最新の確認情報を置換しない', async () => {
    const stale = deferred<main.TagDTO[]>(); load.mockReturnValueOnce(stale.promise); mount(true); await ready();
    await act(async () => stale.resolve([])); expect(screen.getByText('連載中')).toBeTruthy();
    expect(screen.queryByText('タグなし')).toBeNull();
});
