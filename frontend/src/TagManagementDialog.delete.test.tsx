import {StrictMode, useState} from 'react';
import {act, cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import {TagManagementDialog} from './TagManagementDialog';
import {DeleteTag, LoadTagDeletePreview, LoadTagUsage} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';
import {readTagDeletePreview} from './tag-management';
import type {TagNotice} from './TagManagementNotices';

vi.mock('../wailsjs/go/main/App', () => ({DeleteTag: vi.fn(), LoadTagDeletePreview: vi.fn(), LoadTagUsage: vi.fn()}));
const tag = {id: '9223372036854775807', name: 'B'};
const work = {id: '9007199254740993', title: '作品1', createdAt: '2026-10-31T00:00:00.123456789+09:00'};
const preview = {tag, works: [work, {...work, id: '2', title: '作品2', createdAt: '2026-10-31T00:00:00.000000001+09:00'}]};
const read = vi.mocked(LoadTagDeletePreview), remove = vi.mocked(DeleteTag), load = vi.mocked(LoadTagUsage);
const onNotice = vi.fn(), close = vi.fn(), stop = vi.fn();
function Harness() {
    const [stopped, setStopped] = useState(false), [notices, setNotices] = useState<TagNotice[]>([]);
    return <TagManagementDialog stopped={stopped} onStopped={() => {stop(); setStopped(true);}} onClose={close} notices={notices}
        onNotice={(value) => {onNotice(value); setNotices([value]);}} />;
}
function deferred<T>() {
    let resolve!: (value: T) => void, reject!: (reason: unknown) => void;
    const promise = new Promise<T>((yes, no) => {resolve = yes; reject = no;}); return {promise, resolve, reject};
}
const result = (warnings: {code: string; message: string}[] = []) => main.TagChangeResult.createFrom({tag, warnings});
const deleteButton = () => screen.getByRole('button', {name: '削除する'}) as HTMLButtonElement;
async function boot(query = '') {
    const view = render(<StrictMode><Harness /></StrictMode>);
    await screen.findByRole('button', {name: '「B」を削除'});
    if (query) await userEvent.type(screen.getByRole('searchbox'), query);
    await userEvent.click(screen.getByRole('button', {name: '「B」を削除'}));
    return view;
}
beforeEach(() => {
    vi.clearAllMocks();
    load.mockReset().mockResolvedValue([
        {tag: {id: '1', name: 'A'}, workCount: 0}, {tag, workCount: 2}, {tag: {id: '3', name: 'C'}, workCount: 1},
    ].map((value) => main.TagUsageDTO.createFrom(value)));
    read.mockReset().mockResolvedValue(main.TagDeletePreviewDTO.createFrom(preview)); remove.mockReset().mockResolvedValue(result());
    Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {configurable: true, value: function(this: HTMLDialogElement) {this.open = true;}});
    Object.defineProperty(HTMLDialogElement.prototype, 'close', {configurable: true, value: function(this: HTMLDialogElement) {this.open = false;}});
});
afterEach(() => {cleanup(); Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal'); Reflect.deleteProperty(HTMLDialogElement.prototype, 'close');});

it('対象名・全タイトルを表示し、IDとナノ秒の文字列をそのまま送って次のタグへ戻る', async () => {
    await boot(); await screen.findByText('作品2');
    expect(within(screen.getByRole('region', {name: 'タグを削除'})).getByText('2作品')).toBeTruthy();
    expect(screen.getByText('作品は残ります。このタグと、作品との関連だけを削除します。')).toBeTruthy();
    const calls = read.mock.calls.length, listCalls = load.mock.calls.length;
    await userEvent.click(deleteButton());
    const target = await screen.findByRole('button', {name: '「C」の名前変更'});
    expect(document.activeElement).toBe(target);
    expect(JSON.parse(JSON.stringify(remove.mock.calls[0][0]))).toEqual(preview);
    expect(read).toHaveBeenCalledTimes(calls); expect(load).toHaveBeenCalledTimes(listCalls);
    expect(screen.queryByRole('button', {name: '「B」を削除'})).toBeNull();
    expect(onNotice.mock.calls[0][0].message).toContain('作品は残っています');
});

it.each([false, true])('末尾なら前、検索結果がなくなれば検索へ戻す: filter=%s', async (filter) => {
    load.mockResolvedValue([{tag: {id: '1', name: 'A'}, workCount: 0}, {tag, workCount: 2}].map((value) => main.TagUsageDTO.createFrom(value)));
    await boot(filter ? 'B' : ''); await screen.findByText('作品1'); await userEvent.click(deleteButton());
    await waitFor(() => expect(document.activeElement).toBe(filter ? screen.getByRole('searchbox') : screen.getByRole('button', {name: '「A」の名前変更'})));
    if (filter) expect((screen.getByRole('searchbox') as HTMLInputElement).value).toBe('B');
});

it('未取得は削除不可、Escで検索を保って戻り、遅い応答を採用しない', async () => {
    const next = deferred<main.TagDeletePreviewDTO>(); read.mockReturnValue(next.promise);
    await boot('B'); expect(deleteButton().disabled).toBe(true);
    fireEvent(screen.getByRole('dialog'), new Event('cancel', {cancelable: true}));
    expect((screen.getByRole('searchbox') as HTMLInputElement).value).toBe('B');
    expect(remove).not.toHaveBeenCalled();
    await act(async () => {next.resolve(main.TagDeletePreviewDTO.createFrom(preview));});
    expect(screen.queryByText('作品1')).toBeNull();
});

it('未使用の明示的な空配列を確認して削除できる', async () => {
    read.mockResolvedValue(main.TagDeletePreviewDTO.createFrom({tag, works: []}));
    await boot(); await screen.findByText('使用している作品はありません。');
    await userEvent.click(deleteButton()); await screen.findByRole('button', {name: '「A」の名前変更'});
    expect(remove.mock.calls[0][0].works).toEqual([]);
});

it('確認で判明した現在名と件数を一覧へ戻っても古い表示へ戻さない', async () => {
    read.mockResolvedValue(main.TagDeletePreviewDTO.createFrom({...preview, tag: {...tag, name: '現在名'}, works: []}));
    await boot(); await screen.findByText('使用している作品はありません。');
    await userEvent.click(screen.getByRole('button', {name: '一覧に戻る'}));
    expect(screen.getByRole('button', {name: '「現在名」の名前変更'})).toBeTruthy();
    expect(screen.queryByRole('button', {name: '「B」の名前変更'})).toBeNull(); expect(remove).not.toHaveBeenCalled();
});

it('同件数の対象入替も最新タイトルを再確認し、クリックするまで再送しない', async () => {
    await boot(); await screen.findByText('作品2'); remove.mockRejectedValueOnce({code: 'tag_changed', message: '対象変更'});
    const changed = {...preview, tag: {...tag, name: '新しい名前'}, works: [work, {...work, id: '3', title: '作品3'}]};
    read.mockResolvedValue(main.TagDeletePreviewDTO.createFrom(changed));
    await userEvent.click(deleteButton()); await screen.findByText('作品3');
    expect(screen.queryByText('作品2')).toBeNull(); expect(within(screen.getByRole('region', {name: 'タグを削除'})).getByText('新しい名前')).toBeTruthy();
    expect(remove).toHaveBeenCalledTimes(1);
    remove.mockResolvedValue(main.TagChangeResult.createFrom({tag: changed.tag, warnings: []}));
    await userEvent.click(deleteButton()); await screen.findByRole('button', {name: '「C」の名前変更'});
    expect(JSON.parse(JSON.stringify(remove.mock.calls[1][0]))).toEqual(changed);
});

it('再取得失敗では前回の対象を残し、削除を止める', async () => {
    await boot(); await screen.findByText('作品2'); read.mockRejectedValue({code: 'storage_error', message: '再取得失敗'});
    await userEvent.click(screen.getByRole('button', {name: '最新情報を再読み込み'}));
    await screen.findByText('再取得失敗'); expect(screen.getByText('作品2')).toBeTruthy();
    expect(screen.getByText('前回の確認内容です。最新情報は未確認です。')).toBeTruthy(); expect(deleteButton().disabled).toBe(true);
});

it('取得失敗から一覧へ戻っても最新情報の再取得まで変更操作を再開しない', async () => {
    await boot(); await screen.findByText('作品2'); read.mockRejectedValue({code: 'storage_error', message: '再取得失敗'});
    await userEvent.click(screen.getByRole('button', {name: '最新情報を再読み込み'})); await screen.findByText('再取得失敗');
    await userEvent.click(screen.getByRole('button', {name: '一覧に戻る'}));
    const rename = screen.getByRole('button', {name: '「B」の名前変更'}) as HTMLButtonElement;
    expect(rename.disabled).toBe(true);
    await userEvent.click(screen.getByRole('button', {name: '再読み込み'}));
    await waitFor(() => expect(rename.disabled).toBe(false));
});

it('処理中は連打・Esc・戻る・閉じるで再送や画面離脱を起こさない', async () => {
    await boot(); await screen.findByText('作品1'); const next = deferred<main.TagChangeResult>(); remove.mockReturnValue(next.promise);
    const button = deleteButton(); fireEvent.click(button); fireEvent.click(button);
    fireEvent(screen.getByRole('dialog'), new Event('cancel', {cancelable: true}));
    expect(remove).toHaveBeenCalledTimes(1); expect(close).not.toHaveBeenCalled();
    expect((screen.getByRole('button', {name: '一覧に戻る'}) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('button', {name: 'タグ管理を閉じる'}) as HTMLButtonElement).disabled).toBe(true);
    await act(async () => {next.resolve(result());});
});

it('不存在を今回の成功とせず再送を止める', async () => {
    await boot(); await screen.findByText('作品1'); remove.mockRejectedValue({code: 'tag_not_found', message: '既に存在しません'});
    await userEvent.click(deleteButton()); await screen.findByText('既に存在しません');
    expect(deleteButton().disabled).toBe(true); expect(onNotice).not.toHaveBeenCalled(); expect(stop).not.toHaveBeenCalled();
});

it('確定後警告は成功通知に添えて親の停止を維持する', async () => {
    await boot(); await screen.findByText('作品1'); remove.mockResolvedValue(result([{code: 'database_stopped_after_commit', message: '削除確定・再起動してください'}]));
    await userEvent.click(deleteButton()); await screen.findByText('削除確定・再起動してください');
    expect(onNotice.mock.calls[0][0].message).toContain('削除しました'); expect(stop).toHaveBeenCalled();
    expect((screen.getByRole('button', {name: '閉じる'}) as HTMLButtonElement).disabled).toBe(false);
    expect(document.activeElement).toBe(screen.getByRole('button', {name: '閉じる'}));
});

it.each([
    {failure: {code: 'tag_change_outcome_unknown', message: '結果不明'}}, {failure: new Error('private diagnostic')},
    {success: {tag: {id: '1', name: 'B'}, warnings: []}}, {success: {tag, warnings: null}},
])('結果不明や不正成功で自動再送せず安全な親通知を残す: %j', async ({failure, success}) => {
    await boot(); await screen.findByText('作品1');
    if (failure) remove.mockRejectedValue(failure); else remove.mockResolvedValue(success as unknown as main.TagChangeResult);
    await userEvent.click(deleteButton()); await waitFor(() => expect(stop).toHaveBeenCalled());
    expect(onNotice).toHaveBeenCalled(); expect(deleteButton().disabled).toBe(true); expect(remove).toHaveBeenCalledTimes(1);
    expect(screen.queryByText('private diagnostic')).toBeNull();
});

it.each([
    {...preview, works: null}, {...preview, works: [work, work]}, {...preview, works: [work, {...work, id: '01'}]},
    {...preview, works: [{...work, createdAt: '2026-02-30T00:00:00+09:00'}]},
    {...preview, works: [{...work, createdAt: '2026-10-31T00:00:00.1234567891+09:00'}]},
    {...preview, works: [{...work, createdAt: '2026-10-31T00:00:00.100+09:00'}]},
    {...preview, works: [{...work, createdAt: '0001-01-01T09:00:00+09:00'}]},
    {...preview, works: [{...work, title: ''}]}, {...preview, tag: {...tag, id: '1'}},
])('不正な確認情報を部分採用・未使用扱いしない: %j', async (value) => {
    read.mockResolvedValue(value as main.TagDeletePreviewDTO);
    await boot(); await screen.findByRole('alert'); expect(deleteButton().disabled).toBe(true);
    expect(screen.queryByText('作品1')).toBeNull(); expect(remove).not.toHaveBeenCalled();
});

it('過去の日本の夏時間オフセットと1/9桁の精度を変えず検証する', () => {
    for (const createdAt of ['1949-07-01T12:00:00+10:00', '2026-10-31T00:00:00.1+09:00', '2026-10-31T00:00:00.000000001+09:00']) {
        expect(readTagDeletePreview({...preview, works: [{...work, createdAt}]}, tag.id).works[0].createdAt).toBe(createdAt);
    }
});

it('初回失敗は未使用と扱わず、旧要求の停止を最新表示へ重ねない', async () => {
    const old = deferred<main.TagDeletePreviewDTO>();
    read.mockReturnValueOnce(old.promise).mockResolvedValue(main.TagDeletePreviewDTO.createFrom(preview));
    await boot(); await screen.findByText('作品2');
    await act(async () => {old.reject({code: 'database_stopped', message: '旧応答'});});
    expect(stop).not.toHaveBeenCalled(); expect(deleteButton().disabled).toBe(false);
});

it('初回取得の失敗と停止を空の確認情報で補わない', async () => {
    read.mockRejectedValue({code: 'database_stopped', message: '停止中'});
    await boot(); await waitFor(() => expect(stop).toHaveBeenCalled());
    expect(deleteButton().disabled).toBe(true);
    expect(screen.queryByText('使用している作品はありません。')).toBeNull();
    expect(screen.getByText('未取得')).toBeTruthy(); expect(remove).not.toHaveBeenCalled();
});
