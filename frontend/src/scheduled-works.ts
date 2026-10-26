import type {WorkView} from './work-display';

// API dates are validated YYYY-MM-DD calendar dates, not timestamps.
const dateOf = (work: WorkView): string | null => typeof work.scheduledDate === 'string' && work.scheduledDate ? work.scheduledDate : null;

export function scheduledWorks(works: readonly WorkView[], today: string, showFuture: boolean): WorkView[] {
    const group = (date: string | null) => date === null ? 3 : date === today ? 0 : date < today ? 1 : 2;
    return works.filter((work) => {
        const date = dateOf(work);
        return showFuture || date === null || date <= today;
    }).sort((a, b) => {
        const left = dateOf(a), right = dateOf(b);
        const category = group(left) - group(right);
        if (category) return category;
        if (left === right) return a.id - b.id;
        // Within a category both dates are set. Past is newest first, future
        // is nearest first; equal dates and unset entries use registration ID.
        const comparison = left! < right! ? -1 : 1;
        return group(left) === 1 ? -comparison : comparison;
    });
}
