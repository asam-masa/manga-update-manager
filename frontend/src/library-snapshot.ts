import {readTags, validStringID} from './tag-edit';
import type {Tag} from './tag-edit';
import {validCreatedAt} from './tag-management';
import type {WorkView} from './work-display';

export type LibraryWork = Omit<WorkView, 'id'> & {id: string};
export type LibrarySnapshot = {works: LibraryWork[]; tags: Tag[]; links: {workID: string; tagID: string}[]};
const idOrder = (a: string, b: string) => a.length - b.length || (a < b ? -1 : a > b ? 1 : 0);
const invalid = (): never => { throw new Error('Invalid library snapshot'); };
const record = (value: unknown): Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : invalid();
const text = (value: unknown): string => typeof value === 'string' && !/\p{Cs}/u.test(value) ? value : invalid();

function validDate(value: string): boolean {
    const parts = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
    if (!parts) return false;
    const [year, month, day] = parts.slice(1).map(Number);
    const days = [31, year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
    return year >= 1 && month >= 1 && month <= 12 && day >= 1 && day <= days[month - 1];
}

// Compare seconds and nine fractional digits separately. Never round stored
// instants through Date or recreate their confirmation strings from a timestamp.
function timeOrder(a: string, b: string): number {
    const parts = (value: string) => {
        const match = /^(.*T\d{2}:\d{2}:\d{2})(?:\.(\d+))?(Z|[+-]\d{2}:\d{2})$/.exec(value)!;
        return {seconds: Date.parse(match[1] + match[3]), fraction: (match[2] ?? '').padEnd(9, '0')};
    };
    const left = parts(a), right = parts(b);
    return left.seconds - right.seconds || (left.fraction < right.fraction ? -1 : left.fraction > right.fraction ? 1 : 0);
}

export function readLibrarySnapshot(value: unknown): LibrarySnapshot {
    const source = record(value);
    if (!Array.isArray(source.works) || !Array.isArray(source.tags) || !Array.isArray(source.links)) return invalid();
    const workIDs = new Set<string>(), urls = new Set<string>();
    let previous = '';
    const works = Array.from(source.works, (raw): LibraryWork => {
        const work = record(raw);
        if (!validStringID(work.id) || (previous && idOrder(previous, work.id) >= 0)) return invalid();
        previous = work.id;
        const url = text(work.url), title = text(work.title), siteName = text(work.siteName), thumbnailPath = text(work.thumbnailPath), notes = text(work.notes);
        // Go owns URL parsing. Browser URL would repair missing hosts and reject
        // numeric ports Go permits, so check the wire shape without rewriting it.
        const authority = /^https?:\/\/([^/?#]*)/i.exec(url)?.[1];
        const host = authority?.slice(authority.lastIndexOf('@') + 1);
        if (!title || !host || /[\u0000-\u001f\u007f]/.test(url) || urls.has(url)) return invalid();
        if (thumbnailPath && (thumbnailPath.startsWith('/') || thumbnailPath === '.' || thumbnailPath === '..'
            || thumbnailPath.startsWith('../') || thumbnailPath.split('/')[0].includes(':'))) return invalid();
        if (!validCreatedAt(work.createdAt) || !validCreatedAt(work.updatedAt) || timeOrder(work.updatedAt, work.createdAt) < 0
            || (work.lastAccessedAt !== null && !validCreatedAt(work.lastAccessedAt))
            || (work.scheduledDate !== null && (typeof work.scheduledDate !== 'string' || !validDate(work.scheduledDate)))) return invalid();
        workIDs.add(work.id); urls.add(url);
        return {id: work.id, url, title, siteName, thumbnailPath, notes, createdAt: work.createdAt, updatedAt: work.updatedAt,
            lastAccessedAt: work.lastAccessedAt as string | null, scheduledDate: work.scheduledDate as string | null};
    });
    const tags = readTags(source.tags);
    previous = '';
    for (const tag of tags) {
        if (previous && idOrder(previous, tag.id) >= 0) return invalid();
        previous = tag.id;
    }
    const tagIDs = new Set(tags.map((tag) => tag.id));
    let lastWork = '', lastTag = '';
    const links = Array.from(source.links, (raw) => {
        const link = record(raw);
        if (!validStringID(link.workID) || !validStringID(link.tagID) || !workIDs.has(link.workID) || !tagIDs.has(link.tagID)
            || (lastWork && (idOrder(lastWork, link.workID) > 0 || (lastWork === link.workID && idOrder(lastTag, link.tagID) >= 0)))) return invalid();
        lastWork = link.workID; lastTag = link.tagID;
        return {workID: link.workID, tagID: link.tagID};
    });
    return {works, tags, links};
}

// Existing mutation APIs take numeric IDs. Conversion is explicit and rejects
// an otherwise valid large read ID instead of addressing a rounded neighbour.
export function libraryWorkView(work: LibraryWork): WorkView {
    const id = Number(work.id);
    if (!validStringID(work.id) || !Number.isSafeInteger(id) || id <= 0 || String(id) !== work.id) throw new Error('Work ID cannot be represented safely');
    return {...work, id};
}
