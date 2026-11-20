import {act, cleanup, renderHook, waitFor} from '@testing-library/react';
import {afterEach, expect, it, vi} from 'vitest';
import {readOmissionPage} from './backup-omission-page';
import type {OmissionRequest} from './backup-omission-page';
import {useBackupOmissionPage} from './use-backup-omission-page';

afterEach(cleanup);
const image = 'a'.repeat(32), other = 'b'.repeat(32);
const request: OmissionRequest = {executionId: '18446744073709551615', page: 1};
function images(page = 1, total = 51) {
    return {executionId: request.executionId, page, pageSize: 50, pages: Math.max(1, Math.ceil(total / 50)), total,
        items: Array.from({length: Math.min(50, total - (page - 1) * 50)}, (_, i) => ({imageId: (i + 1 + (page - 1) * 50).toString(16).padStart(32, '0'), reason: 'missing'}))};
}
const works = () => ({...images(1, 1), imageId: image, items: [{workId: '9223372036854775807', title: '保存時の作品名'}]});
function deferred() {
    let resolve!: (value: unknown) => void, reject!: (error: unknown) => void;
    const promise = new Promise<unknown>((yes, no) => {resolve = yes; reject = no;}); return {promise, resolve, reject};
}

it('50件の先頭と1件の末尾を検証し、画像IDと実行IDを文字列で保持する', () => {
    expect(readOmissionPage(images(), request).items).toHaveLength(50);
    expect(readOmissionPage(images(2), {...request, page: 2})).toMatchObject({executionId: request.executionId, page: 2, total: 51, items: [expect.anything()]});
});
it('観測済み空ページを未取得と区別し、1ページ・0件を保持する', () => {
    expect(readOmissionPage(images(1, 0), request)).toMatchObject({page: 1, pages: 1, total: 0, items: []});
});
it.each([{executionId: '1'}, {page: 2}, {pageSize: 49}, {pages: 3}, {total: -1}, {total: 0.5},
    {total: Number.MAX_SAFE_INTEGER + 1}, {items: null}, {items: []}, {items: Array(51).fill({imageId: image, reason: 'missing'})}])('不整合なページを成功として採用しない: %j', (patch) => {
    expect(() => readOmissionPage({...images(), ...patch}, request)).toThrow();
});
it.each(['missing', 'corrupt', 'unavailable'])('公開理由を保持する: %s', (reason) => {
    const parsed = readOmissionPage({...images(1, 1), items: [{imageId: image, reason}]}, request);
    expect(parsed.items[0]).toMatchObject({imageId: image, reason});
});
it.each([{imageId: 'x', reason: 'missing'}, {imageId: image.toUpperCase(), reason: 'missing'},
    {imageId: image, reason: 'private diagnostic'}])('不正な画像ID・理由を拒否する: %j', (item) => {
    expect(() => readOmissionPage({...images(1, 1), items: [item]}, request)).toThrow();
});
it('作品ページは要求画像ID・作品ID・タイトルを照合し、返却後に元の値を書き換えない', () => {
    const raw = works(), r = {...request, imageId: image}, parsed = readOmissionPage(raw, r);
    expect(parsed).toMatchObject({kind: 'works', imageId: image, items: [{workId: '9223372036854775807', title: '保存時の作品名'}]});
    expect(() => readOmissionPage(raw, {...r, imageId: other})).toThrow();
    if (parsed.kind === 'works') parsed.items[0].title = '表示変更';
    expect(raw.items[0].title).toBe('保存時の作品名');
});
it.each([{workId: '01', title: '作品'}, {workId: '9223372036854775808', title: '作品'}, {workId: '1', title: ''}, {workId: '1', title: '\ud800'}])('不正作品を拒否する: %j', (item) => {
    expect(() => readOmissionPage({...works(), items: [item]}, {...request, imageId: image})).toThrow();
});
it('画像・作品のページ内重複を拒否する', () => {
    expect(() => readOmissionPage({...images(1, 2), items: [{imageId: image, reason: 'missing'}, {imageId: image, reason: 'corrupt'}]}, request)).toThrow();
    expect(() => readOmissionPage({...works(), total: 2, items: [works().items[0], works().items[0]]}, {...request, imageId: image})).toThrow();
});
it('Goが保持するU+FEFFは失わず、Goの空白だけのタイトルは拒否する', () => {
    const r = {...request, imageId: image};
    expect(readOmissionPage({...works(), items: [{workId: '1', title: '\ufeff'}]}, r).items[0]).toMatchObject({title: '\ufeff'});
    expect(() => readOmissionPage({...works(), items: [{workId: '1', title: '\u0085'}]}, r)).toThrow();
});
it.each([{...request, page: 0}, {...request, page: 1.5}, {...request, executionId: '01'}, {...request, imageId: ''}])('不正な要求を送信しない: %j', async (selected) => {
    const api = {images: vi.fn(), works: vi.fn()};
    const {result} = renderHook(() => useBackupOmissionPage(selected, api)); await waitFor(() => expect(result.current.error).not.toBe(''));
    expect(api.images).not.toHaveBeenCalled(); expect(api.works).not.toHaveBeenCalled();
});
it('未選択は読まず、ページ切替後の古い成功を採用しない', async () => {
    const old = deferred(), api = {images: vi.fn().mockReturnValueOnce(old.promise).mockResolvedValue(images(2)), works: vi.fn()};
    const {result, rerender} = renderHook(({selected}: {selected: OmissionRequest | null}) => useBackupOmissionPage(selected, api), {initialProps: {selected: null as OmissionRequest | null}});
    expect(api.images).not.toHaveBeenCalled(); rerender({selected: request}); rerender({selected: {...request, page: 2}});
    await waitFor(() => expect(result.current.data?.page).toBe(2));
    await act(async () => {old.resolve(images());}); expect(result.current.data?.page).toBe(2);
});
it('画像切替後の古い失敗を採用せず、別実行の応答は取得失敗にする', async () => {
    const old = deferred(), api = {images: vi.fn(), works: vi.fn().mockReturnValueOnce(old.promise).mockResolvedValue({...works(), imageId: other})};
    const {result, rerender} = renderHook(({selected}) => useBackupOmissionPage(selected, api), {initialProps: {selected: {...request, imageId: image}}});
    rerender({selected: {...request, imageId: other}}); await waitFor(() => expect(result.current.data?.kind).toBe('works'));
    await act(async () => {old.reject(new Error('private path'));}); expect(result.current.error).toBe('');
    rerender({selected: {...request, executionId: '1', imageId: other}}); await waitFor(() => expect(result.current.error).not.toBe(''));
    expect(result.current.data).toBeNull();
});
it('通信失敗は固定案内とし、詳細だけの再取得で回復する', async () => {
    const api = {images: vi.fn().mockRejectedValueOnce(new Error('H:/private/secret.sqlite')).mockResolvedValue(images(1, 0)), works: vi.fn()};
    const {result} = renderHook(() => useBackupOmissionPage(request, api)); await waitFor(() => expect(result.current.error).not.toBe(''));
    expect(result.current.error).not.toContain('private'); expect(result.current.data).toBeNull();
    act(() => {result.current.reload();}); await waitFor(() => expect(result.current.data?.total).toBe(0)); expect(api.images).toHaveBeenCalledTimes(2);
});
it('アンマウント後の応答は採用しない', async () => {
    const late = deferred(), api = {images: vi.fn().mockReturnValue(late.promise), works: vi.fn()};
    const {unmount} = renderHook(() => useBackupOmissionPage(request, api)); unmount();
    await act(async () => {late.resolve(images());}); expect(api.images).toHaveBeenCalledTimes(1);
});
