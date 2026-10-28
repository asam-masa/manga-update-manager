import type {main} from '../wailsjs/go/models';

export type Tag = Pick<main.TagDTO, 'id' | 'name'>;
// Match Go strings.TrimSpace, rather than JS trim (which also removes U+FEFF).
export function tagName(value: string): string {
    return value.replace(/^[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\u0009-\u000d\u0020\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$/g, '');
}
export function validTagName(value: string): boolean {
    return value === tagName(value) && [...value].length >= 1 && [...value].length <= 50
        && !/[\p{Cc}\p{Cs}\u2028\u2029]/u.test(value);
}
export function readTags(value: unknown): Tag[] {
    if (!Array.isArray(value)) throw new Error('Invalid tag list');
    const ids = new Set<string>(), names = new Set<string>();
    return value.map((tag) => {
        const id = tag?.id, name = tag?.name;
        if (typeof id !== 'string' || !/^[1-9][0-9]{0,18}$/.test(id)
            || (id.length === 19 && id > '9223372036854775807') || ids.has(id)
            || typeof name !== 'string' || !validTagName(name) || names.has(name)) throw new Error('Invalid tag');
        ids.add(id); names.add(name);
        return {id, name};
    });
}
