// Validate at the Wails boundary. These observations never grant permission to
// resume ordinary operations; the backend remains the owner of that decision.
type RecordValue = Record<string, unknown>;
function record(value: unknown): RecordValue {
    if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid backup response');
    return value as RecordValue;
}
function flag(value: unknown): boolean {
    if (typeof value !== 'boolean') throw new Error('Invalid backup flag');
    return value;
}
function text(value: unknown): string {
    if (typeof value !== 'string' || !value.trim()) throw new Error('Invalid backup text');
    return value;
}
function choice<T extends string>(value: unknown, values: readonly T[]): T {
    if (!values.includes(value as T)) throw new Error('Unsupported backup value');
    return value as T;
}
function count(value: unknown): number {
    if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0) throw new Error('Invalid backup count');
    return value;
}
function optionalCount(value: unknown): number | null {
    return value == null ? null : count(value);
}
export function validBackupID(value: unknown): value is string {
    return typeof value === 'string' && /^[1-9][0-9]{0,19}$/.test(value)
        && (value.length < 20 || value <= '18446744073709551615');
}
function executionID(value: unknown): string {
    if (!validBackupID(value)) throw new Error('Invalid backup execution');
    return value;
}
function matchingID(value: unknown, expected: string): string {
    if (!validBackupID(expected) || executionID(value) !== expected) {
        throw new Error('Mismatched backup execution');
    }
    return expected;
}

export function readBackupStart(value: unknown): {cancelled: boolean; executionId: string} {
    const v = record(value), cancelled = flag(v.cancelled);
    if (cancelled) {
        if (v.executionId !== '') throw new Error('Invalid cancelled selection');
        return {cancelled, executionId: ''};
    }
    return {cancelled, executionId: executionID(v.executionId)};
}

const states = ['idle', 'running', 'cancelling', 'publishing', 'finished', 'unknown'] as const;
const stages = ['unobserved', 'waiting', 'preparing', 'database', 'database_check', 'images', 'metadata', 'zip', 'publishing', 'cleanup'] as const;
const publications = ['not_attempted', 'collision', 'confirmed', 'unknown'] as const;
function observedResult(value: unknown) {
    const v = record(value);
    return {
        publication: choice(v.publication, publications), hasWarnings: flag(v.hasWarnings),
        omittedImageCount: optionalCount(v.omittedImageCount),
        workNamesState: choice(v.workNamesState, ['unobserved', 'available', 'unavailable'] as const),
        workCount: optionalCount(v.workCount),
    };
}
export function readBackupStatus(value: unknown) {
    const v = record(value), state = choice(v.state, states), stage = choice(v.stage, stages);
    const elapsedSeconds = count(v.elapsedSeconds), canCancel = flag(v.canCancel);
    if (canCancel && state !== 'running') throw new Error('Invalid backup cancellation permission');
    const result = v.result == null ? null : observedResult(v.result);
    if (state === 'idle') {
        if (v.executionId !== '' || stage !== 'unobserved' || elapsedSeconds !== 0 || result !== null) {
            throw new Error('Invalid idle backup');
        }
        return {executionId: '', state, stage, elapsedSeconds, canCancel, result};
    }
    const id = executionID(v.executionId);
    if ((state === 'finished') !== (result !== null) && state !== 'unknown') throw new Error('Invalid backup completion');
    return {executionId: id, state, stage, elapsedSeconds, canCancel, result};
}

export function readBackupCancel(value: unknown, expected: string) {
    const v = record(value);
    return {executionId: matchingID(v.executionId, expected),
        decision: choice(v.decision, ['accepted', 'already_accepted', 'publication_started', 'finished'] as const)};
}
export function readBackupResult(value: unknown, expected: string) {
    const v = record(value), id = matchingID(v.executionId, expected);
    const outcome = choice(v.outcome, ['saved', 'saved_with_warnings', 'not_saved', 'collision', 'unknown'] as const);
    const retryAllowed = flag(v.retryAllowed);
    if (outcome === 'unknown' && retryAllowed) throw new Error('Invalid backup retry permission');
    if (!Array.isArray(v.notices)) throw new Error('Invalid backup notices');
    return {executionId: id, outcome, message: text(v.message),
        cancellationObserved: flag(v.cancellationObserved), retryAllowed,
        notices: v.notices.map((value) => {
            const notice = record(value);
            return {code: text(notice.code), message: text(notice.message)};
        })};
}

export type BackupStatus = ReturnType<typeof readBackupStatus>;
export type BackupResult = ReturnType<typeof readBackupResult>;
