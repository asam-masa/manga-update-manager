import {readTags} from './tag-edit';
import type {Tag} from './tag-edit';
import {titleOrder} from './work-sort';

export type TagUsage = {tag: Tag; workCount: number};
export function sortTagUsage(values: TagUsage[]): TagUsage[] {
    return [...values].sort((a, b) => titleOrder(a.tag.name, b.tag.name)
        || a.tag.id.length - b.tag.id.length || (a.tag.id < b.tag.id ? -1 : a.tag.id > b.tag.id ? 1 : 0));
}

export function readRenamedTag(value: unknown, id: string, name: string): {tag: Tag; warning?: string} {
    if (typeof value !== 'object' || value === null || !('tag' in value) || !('warnings' in value)
        || !Array.isArray(value.warnings)) throw new Error('Invalid rename result');
    const [tag] = readTags([value.tag]);
    if (tag.id !== id || tag.name !== name) throw new Error('Mismatched rename result');
    const warnings = value.warnings.map((warning) => {
        if (warning?.code !== 'database_stopped_after_commit' || typeof warning.message !== 'string'
            || !warning.message.trim() || warning.message.length > 8192) throw new Error('Invalid rename warning');
        return warning.message as string;
    });
    return {tag, warning: warnings.length ? warnings.join('\n') : undefined};
}

export function readTagUsage(value: unknown): TagUsage[] {
    if (!Array.isArray(value)) throw new Error('Invalid tag usage');
    const tags = readTags(value.map((item) => item?.tag));
    return sortTagUsage(value.map((item, index) => {
        if (!Number.isSafeInteger(item.workCount) || item.workCount < 0) throw new Error('Invalid work count');
        return {tag: tags[index], workCount: item.workCount};
    }));
}
