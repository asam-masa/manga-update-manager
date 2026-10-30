import {readTags} from './tag-edit';
import type {Tag} from './tag-edit';
import {titleOrder} from './work-sort';

export type TagUsage = {tag: Tag; workCount: number};

export function readTagUsage(value: unknown): TagUsage[] {
    if (!Array.isArray(value)) throw new Error('Invalid tag usage');
    const tags = readTags(value.map((item) => item?.tag));
    return value.map((item, index) => {
        if (!Number.isSafeInteger(item.workCount) || item.workCount < 0) throw new Error('Invalid work count');
        return {tag: tags[index], workCount: item.workCount};
    }).sort((a, b) => titleOrder(a.tag.name, b.tag.name)
        || a.tag.id.length - b.tag.id.length || (a.tag.id < b.tag.id ? -1 : a.tag.id > b.tag.id ? 1 : 0));
}
