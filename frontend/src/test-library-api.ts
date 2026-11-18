import type {WorkView} from './work-display';

export const idleBackupAPI = {
    GetBackupLocations: async () => {throw new Error('Unexpected backup locations request');},
    GetBackupStatus: async () => ({executionId: '', state: 'idle', stage: 'unobserved', elapsedSeconds: 0, canCancel: false}),
    GetBackupResult: async () => {throw new Error('Unexpected backup result request');},
    SelectAndStartBackup: async () => {throw new Error('Unexpected backup start');},
    CancelBackup: async () => {throw new Error('Unexpected backup cancellation');},
};

// Existing App tests control a works-only fixture source. Adapt that source to
// the new wire contract; invalid raw snapshot tests bypass this fixture helper.
export function withLibrarySnapshot<T extends {ListWorks: () => Promise<WorkView[]>}>(api: T) {
    return {...idleBackupAPI, ...api, LoadLibrarySnapshot: async () => ({
        works: (await api.ListWorks()).map((work) => ({...work, id: String(work.id),
            lastAccessedAt: work.lastAccessedAt ?? null, scheduledDate: work.scheduledDate ?? null})),
        tags: [], links: [],
    })};
}
