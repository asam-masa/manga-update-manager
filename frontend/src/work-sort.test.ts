import {expect, it} from 'vitest';
import {sortWorks} from './work-sort';
import type {WorkView} from './work-display';

const work = (id: number, title: string, lastAccessedAt?: string | null): WorkView => ({id, title, lastAccessedAt,
    url: 'https://example.com', siteName: '', thumbnailPath: '', notes: '', createdAt: '', updatedAt: ''});
const ids = (values: WorkView[]) => values.map((w) => w.id);
it('自然順・ゼロ埋め・全半角・大小・巨大な数字を比較し、元配列やタイトルは変更しない', () => {
    const values = [work(5, '作品10'), work(3, '作品02'), work(2, '作品２'), work(1, '作品1'), work(4, '作品0002')];
    expect(ids(sortWorks(values, 'title_asc'))).toEqual([1, 2, 3, 4, 5]);
    expect(ids(sortWorks(values, 'title_desc'))).toEqual([5, 2, 3, 4, 1]);
    expect(ids(values)).toEqual([5, 3, 2, 1, 4]); expect(values[2].title).toBe('作品２');
    expect(ids(sortWorks([work(2, 'ＡＢＣ'), work(1, 'abc')], 'title_desc'))).toEqual([1, 2]);
    expect(ids(sortWorks([work(2, '作品9007199254740993'), work(1, '作品9007199254740992')], 'title_asc'))).toEqual([1, 2]);
    expect(ids(sortWorks([work(2, `作品1${'0'.repeat(400)}`), work(1, `作品${'9'.repeat(400)}`)], 'title_asc'))).toEqual([1, 2]);
});
it('かなや互換記号の区別を残し、結合文字は検索と同じ正規化で揃える', () => {
    for (const [a, b] of [['かな', 'カナ'], ['①', '1']]) {
        const asc = ids(sortWorks([work(2, a), work(1, b)], 'title_asc'));
        expect(ids(sortWorks([work(2, a), work(1, b)], 'title_desc'))).toEqual([...asc].reverse());
    }
    expect(ids(sortWorks([work(2, 'カ\u3099'), work(1, 'ガ')], 'title_desc'))).toEqual([1, 2]);
});
it.each(['last_access_asc', 'last_access_desc'] as const)('%sの未設定は末尾、同時刻はID昇順、時差文字列は時刻で比較する', (order) => {
    const values = [work(5, '未設定', null), work(3, '同時刻', '2026-10-25T09:00:00+09:00'),
        work(2, '同時刻', '2026-10-25T00:00:00Z'), work(1, '過去', '2026-10-24T23:00:00Z'), work(4, '未設定')];
    expect(ids(sortWorks(values, order))).toEqual(order.endsWith('asc') ? [1, 2, 3, 4, 5] : [2, 3, 1, 4, 5]);
});
it('登録順は現在の配列順や登録日時によらずIDを使う', () => {
    const values = [work(3, ''), work(1, ''), work(2, '')];
    expect(ids(sortWorks(values, 'registration_asc'))).toEqual([1, 2, 3]);
    expect(ids(sortWorks(values, 'registration_desc'))).toEqual([3, 2, 1]);
});

it('同一ミリ秒内でもAPIのナノ秒精度と時差を保って比較する', () => {
    const values = [work(1, '後', '2026-10-25T00:00:00.000000200Z'), work(2, '前', '2026-10-25T09:00:00.0000001+09:00')];
    expect(ids(sortWorks(values, 'last_access_asc'))).toEqual([2, 1]);
    expect(ids(sortWorks(values, 'last_access_desc'))).toEqual([1, 2]);
    expect(ids(sortWorks([work(2, '', '2026-10-25T00:00:00.1Z'), work(1, '', '2026-10-25T09:00:00.100000000+09:00')], 'last_access_desc'))).toEqual([1, 2]);
});
