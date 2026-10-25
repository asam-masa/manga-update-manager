import {useState} from 'react';
import type {WorkListMode} from './WorkList';

export const worksPerPage: Record<WorkListMode, number> = {compact: 20, rows: 20, cards: 12};

export function pageNumbers(page: number, total: number): (number | 'gap')[] {
    if (total <= 7) return Array.from({length: total}, (_, i) => i + 1);
    const selected = new Set([1, total, page - 1, page, page + 1]);
    if (page <= 3) [2, 3, 4].forEach((n) => selected.add(n));
    if (page >= total - 2) [total - 3, total - 2, total - 1].forEach((n) => selected.add(n));
    const numbers = [...selected].filter((n) => n >= 1 && n <= total).sort((a, b) => a - b);
    return numbers.flatMap((n, i) => i > 0 && n > numbers[i - 1] + 1 ? ['gap' as const, n] : [n]);
}

// The position belongs to the current filter and layout, never to persistence.
export function useWorkPagination(mode: WorkListMode, query: string, count: number, order = 'registration_asc') {
    const size = worksPerPage[mode];
    const total = Math.ceil(count / size);
    const [position, setPosition] = useState({mode, query, order, page: 1});
    const page = Math.min(position.mode === mode && position.query === query && position.order === order ? position.page : 1, Math.max(1, total));
    // Adjust this component's state before rendering children. Remember a clamp
    // so a later count increase cannot resurrect an abandoned last page.
    if (position.mode !== mode || position.query !== query || position.order !== order || position.page !== page) setPosition({mode, query, order, page});
    return {page, total, size, start: (page - 1) * size,
        setPage: (page: number) => setPosition({mode, query, order, page})};
}
