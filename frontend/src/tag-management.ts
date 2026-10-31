import {readTags, validStringID} from './tag-edit';
import type {Tag} from './tag-edit';
import {titleOrder} from './work-sort';

export type TagUsage = {tag: Tag; workCount: number};
export function sortTagUsage(values: TagUsage[]): TagUsage[] {
    return [...values].sort((a, b) => titleOrder(a.tag.name, b.tag.name)
        || a.tag.id.length - b.tag.id.length || (a.tag.id < b.tag.id ? -1 : a.tag.id > b.tag.id ? 1 : 0));
}

export function readTagChange(value: unknown, id: string, name: string): {tag: Tag; warning?: string} {
    if (typeof value !== 'object' || value === null || !('tag' in value) || !('warnings' in value)
        || !Array.isArray(value.warnings)) throw new Error('Invalid tag change result');
    const [tag] = readTags([value.tag]);
    if (tag.id !== id || tag.name !== name) throw new Error('Mismatched tag change result');
    const warnings = value.warnings.map((warning) => {
        if (warning?.code !== 'database_stopped_after_commit' || typeof warning.message !== 'string'
            || !warning.message.trim() || warning.message.length > 8192) throw new Error('Invalid tag change warning');
        return warning.message as string;
    });
    return {tag, warning: warnings.length ? warnings.join('\n') : undefined};
}

export type TagDeletePreview = {tag: Tag; works: {id: string; createdAt: string; title: string}[]};

// Validate canonical RFC3339Nano without Date conversion or fractional rounding.
// Tokyo's historical daylight saving time can use +10:00 as well as +09:00.
function validCreatedAt(value: unknown): value is string {
    if (typeof value !== 'string') return false;
    const parts = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{0,8}[1-9]))?(Z|([+-])(\d{2}):(\d{2}))$/.exec(value);
    if (!parts) return false;
    const [year, month, day, hour, minute, second] = parts.slice(1, 7).map(Number);
    const offsetHour = Number(parts[10] ?? 0), offsetMinute = Number(parts[11] ?? 0);
    if (offsetHour > 23 || offsetMinute > 59 || (parts[8] !== 'Z' && offsetHour === 0 && offsetMinute === 0)) return false;
    if (year === 1 && month === 1 && day === 1 && second === 0 && !parts[7]
        && ((parts[8] === 'Z' && hour === 0 && minute === 0) || (parts[9] === '+' && hour === offsetHour && minute === offsetMinute))) return false;
    const days = [31, year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
    return year >= 1 && month >= 1 && month <= 12 && day >= 1 && day <= days[month - 1]
        && hour < 24 && minute < 60 && second < 60;
}

export function readTagDeletePreview(value: unknown, id: string): TagDeletePreview {
    if (typeof value !== 'object' || value === null || !('tag' in value) || !('works' in value)
        || !Array.isArray(value.works)) throw new Error('Invalid delete preview');
    const [tag] = readTags([value.tag]);
    if (tag.id !== id) throw new Error('Mismatched preview');
    const ids = new Set<string>();
    const works = value.works.map((work) => {
        if (!validStringID(work?.id) || ids.has(work.id) || !validCreatedAt(work.createdAt)
            || typeof work.title !== 'string' || !work.title.trim() || /\p{Cs}/u.test(work.title)) throw new Error('Invalid preview work');
        ids.add(work.id);
        return {id: work.id, createdAt: work.createdAt, title: work.title};
    });
    return {tag, works};
}

export function readTagUsage(value: unknown): TagUsage[] {
    if (!Array.isArray(value)) throw new Error('Invalid tag usage');
    const tags = readTags(value.map((item) => item?.tag));
    return sortTagUsage(value.map((item, index) => {
        if (!Number.isSafeInteger(item.workCount) || item.workCount < 0) throw new Error('Invalid work count');
        return {tag: tags[index], workCount: item.workCount};
    }));
}
