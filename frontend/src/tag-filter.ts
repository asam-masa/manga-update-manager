import type {Tag} from './tag-edit';
import {titleMatches} from './work-search';
import {titleOrder} from './work-sort';
import type {WorkSort} from './work-sort';
import {scheduledDateVisible} from './scheduled-works';

type FilterWork = {id: string; title: string; scheduledDate?: string | null};
type WorkTagLink = {workID: string; tagID: string};
export type TagCandidate = {tag: Tag; workCount: number};
export type TagFilterConditions = {
    selectedTagIDs: readonly string[];
    titleQuery: string;
    candidateQuery: string;
    order: WorkSort;
    today: string;
    showFuture: boolean;
};

// The caller supplies one completely validated snapshot and a Japanese calendar
// date. Fetch/decode, selection reconciliation, sorting and paging belong outside.
export function filterLibrary<T extends FilterWork>(snapshot: {
    works: readonly T[];
    tags: readonly Tag[];
    links: readonly WorkTagLink[];
}, conditions: TagFilterConditions): {works: T[]; candidates: TagCandidate[]} {
    const tagIDs = new Set(snapshot.tags.map((tag) => tag.id));
    const selected = [...new Set(conditions.selectedTagIDs)];
    for (const id of selected) {
        if (!tagIDs.has(id)) throw new Error('Unknown selected tag');
    }
    const workTags = new Map(snapshot.works.map((work) => [work.id, new Set<string>()]));
    for (const link of snapshot.links) workTags.get(link.workID)!.add(link.tagID);
    const works = snapshot.works.filter((work) => titleMatches(work.title, conditions.titleQuery)
        && (conditions.order !== 'scheduled_date' || scheduledDateVisible(work.scheduledDate, conditions.today, conditions.showFuture))
        && selected.every((id) => workTags.get(work.id)!.has(id)));

    // Adding candidate C to the current AND result means counting only current
    // matches which also carry C. Count each work once, before any page slicing.
    const counts = new Map<string, number>();
    for (const work of works) {
        for (const id of workTags.get(work.id)!) counts.set(id, (counts.get(id) ?? 0) + 1);
    }
    const candidates = snapshot.tags.filter((tag) => titleMatches(tag.name, conditions.candidateQuery))
        .map((tag) => ({tag, workCount: counts.get(tag.id) ?? 0}))
        .sort((a, b) => titleOrder(a.tag.name, b.tag.name) || a.tag.id.length - b.tag.id.length
            || (a.tag.id < b.tag.id ? -1 : a.tag.id > b.tag.id ? 1 : 0));
    return {works, candidates};
}
