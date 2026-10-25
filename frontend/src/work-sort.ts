import type {WorkView} from './work-display';
import {searchText} from './work-search';

export const workSortOptions = [
    ['registration_asc', '登録：古い順'], ['registration_desc', '登録：新しい順'],
    ['title_asc', 'タイトル：昇順'], ['title_desc', 'タイトル：降順'],
    ['last_access_asc', '最終アクセス：古い順'], ['last_access_desc', '最終アクセス：新しい順'],
] as const;
export type WorkSort = typeof workSortOptions[number][0];
export const isWorkSort = (value: unknown): value is WorkSort => workSortOptions.some(([order]) => order === value);
const textOrder = new Intl.Collator('ja', {sensitivity: 'variant'});
const lexical = (a: string, b: string) => a < b ? -1 : a > b ? 1 : 0;

function titleOrder(a: string, b: string): number {
    const left = searchText(a).split(/(\d+)/), right = searchText(b).split(/(\d+)/);
    for (let i = 0; i < Math.min(left.length, right.length); i++) {
        let result: number;
        if (i % 2) {
            // Compare integer strings without Number's precision/range limit.
            const x = left[i].replace(/^0+/, '') || '0', y = right[i].replace(/^0+/, '') || '0';
            result = x.length - y.length || lexical(x, y);
        } else {
            // Keep kana distinctions even when Japanese collation equates them.
            result = textOrder.compare(left[i], right[i]) || lexical(left[i], right[i]);
        }
        if (result) return result;
    }
    return left.length - right.length;
}

function accessTime(value: WorkView['lastAccessedAt']): {seconds: number; fraction: string} | null {
    if (value == null) return null;
    const parts = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
    if (!parts) return null;
    // Date handles the offset; compare the fractional second separately so
    // RFC3339Nano's sub-millisecond precision is never rounded away.
    const seconds = Date.parse(parts[1] + parts[3]);
    return Number.isNaN(seconds) ? null : {seconds, fraction: (parts[2] ?? '').padEnd(9, '0')};
}

export function sortWorks(works: readonly WorkView[], order: WorkSort): WorkView[] {
    const direction = order.endsWith('_desc') ? -1 : 1;
    return [...works].sort((a, b) => {
        if (order.startsWith('registration_')) return direction * (a.id - b.id);
        if (order.startsWith('title_')) return direction * titleOrder(a.title, b.title) || a.id - b.id;
        const left = accessTime(a.lastAccessedAt), right = accessTime(b.lastAccessedAt);
        if ((left === null) !== (right === null)) return left === null ? 1 : -1;
        const result = left && right ? left.seconds - right.seconds || lexical(left.fraction, right.fraction) : 0;
        return direction * result || a.id - b.id;
    });
}
