import {expect, it} from 'vitest';
import type {WorkView} from './work-display';
import {scheduledWorks} from './scheduled-works';

const work = (id: number, scheduledDate?: string | null): WorkView => ({id, scheduledDate, title: `作品${id}`,
    url: 'https://example.com', siteName: '', thumbnailPath: '', notes: '', createdAt: '', updatedAt: ''});
const ids = (works: WorkView[]) => works.map((w) => w.id);

it('今日・過去・未来・未設定を並べ、同日と未設定はID昇順にし、入力を変更しない', () => {
    const values = [work(8), work(7, '2028-03-02'), work(3, '2028-02-28'), work(2, '2028-02-29'),
        work(6, '2028-03-01'), work(4, '2028-02-28'), work(1, '2028-02-29'), work(5, '2028-02-27'), work(9, null)];
    values.forEach(Object.freeze); Object.freeze(values);
    expect(ids(scheduledWorks(values, '2028-02-29', true))).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9]);
    expect(ids(scheduledWorks(values, '2028-02-29', false))).toEqual([1, 2, 3, 4, 5, 8, 9]);
    expect(ids(values)).toEqual([8, 7, 3, 2, 6, 4, 1, 5, 9]);
    expect(values[1].scheduledDate).toBe('2028-03-02');
});

it('0001年・9999年・年末・うるう日も暦日のまま比較する', () => {
    const values = [work(5, '9999-12-31'), work(1, '0001-01-01'), work(3, '2028-02-29'), work(2, '2027-12-31'), work(4, '2028-03-01')];
    expect(ids(scheduledWorks(values, '2028-02-29', true))).toEqual([3, 2, 1, 4, 5]);
    expect(ids(scheduledWorks(values, '0001-01-01', false))).toEqual([1]);
    expect(ids(scheduledWorks(values, '9999-12-31', true))).toEqual([5, 4, 3, 2, 1]);
});

it('日付が変わると未来OFFの明日が戻り、既存の予定を消さない', () => {
    const values = [work(2, '2028-03-01'), work(1, '2028-02-29'), work(3, '')];
    expect(ids(scheduledWorks(values, '2028-02-29', false))).toEqual([1, 3]);
    expect(ids(scheduledWorks(values, '2028-03-01', false))).toEqual([2, 1, 3]);
    expect(values[0].scheduledDate).toBe('2028-03-01');
    expect(scheduledWorks([], '2028-03-01', true)).toEqual([]);
    expect(scheduledWorks([work(1, '2028-03-02')], '2028-03-01', false)).toEqual([]);
});
