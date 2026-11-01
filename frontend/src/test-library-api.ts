import type {WorkView} from './work-display';

// Existing App tests control a works-only fixture source. Adapt that source to
// the new wire contract; invalid raw snapshot tests bypass this fixture helper.
export function withLibrarySnapshot<T extends {ListWorks: () => Promise<WorkView[]>}>(api: T) {
    return {...api, LoadLibrarySnapshot: async () => ({
        works: (await api.ListWorks()).map((work) => ({...work, id: String(work.id),
            lastAccessedAt: work.lastAccessedAt ?? null, scheduledDate: work.scheduledDate ?? null})),
        tags: [], links: [],
    })};
}
