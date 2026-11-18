import {validBackupID} from './backup-response';

const kinds = ['temporary_zip', 'publication_target', 'database_copy', 'work_directory'] as const;
export type BackupLocations = {
    executionId: string; destination: string; publication: 'not_attempted' | 'collision' | 'confirmed' | 'unknown';
    confirmedPath: string; candidates: {kind: typeof kinds[number]; path: string}[];
};
export function readBackupLocations(value: unknown, expected: string): BackupLocations {
    if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid backup locations');
    const v = value as Record<string, unknown>;
    if (!validBackupID(expected) || v.executionId !== expected || typeof v.destination !== 'string'
        || typeof v.confirmedPath !== 'string' || !['not_attempted', 'collision', 'confirmed', 'unknown'].includes(v.publication as string)
        || (v.publication !== 'confirmed' && v.confirmedPath !== '') || !Array.isArray(v.candidates)) throw new Error('Invalid backup locations');
    const candidates = v.candidates.map((item: unknown) => {
        if (!item || typeof item !== 'object' || !('kind' in item) || !kinds.includes(item.kind as typeof kinds[number])
            || !('path' in item) || typeof item.path !== 'string' || !item.path.length) throw new Error('Invalid backup candidate');
        return {kind: item.kind as typeof kinds[number], path: item.path};
    });
    return {executionId: expected, destination: v.destination, publication: v.publication as BackupLocations['publication'],
        confirmedPath: v.confirmedPath, candidates};
}
