import type {main} from '../wailsjs/go/models';
export type WorkView = Omit<main.WorkDTO, 'lastAccessedAt'> & {lastAccessedAt?: string | null};
const knownCodes = new Set(['database_stopped', 'database_unavailable', 'invalid_input', 'duplicate_url', 'storage_error', 'internal_error', 'browser_open_failed', 'access_save_failed', 'work_not_found', 'work_open_in_progress', 'settings_read_failed', 'settings_save_failed', 'cover_outcome_unknown', 'cover_busy', 'cover_unavailable', 'invalid_cover', 'cover_import_failed', 'cover_remove_failed']);

const apiErrorWirePrefix = 'manga-update-manager:api-error:v1:';

function safeAPIError(error: unknown): {code: string; message: string} | null {
    let value: unknown = error;
    if (error instanceof Error) {
        if (error.message.length > 8192 || !error.message.startsWith(apiErrorWirePrefix)) return null;
        try { value = JSON.parse(error.message.slice(apiErrorWirePrefix.length)) as unknown; }
        catch { return null; }
    }
    if (typeof value !== 'object' || value === null || Array.isArray(value)) return null;
    if ('code' in value && typeof value.code === 'string' && knownCodes.has(value.code)
        && 'message' in value && typeof value.message === 'string' && value.message.trim() && value.message.length <= 8192) {
        return {code: value.code, message: value.message};
    }
    return null;
}

export function isDatabaseStopped(error: unknown): boolean {
    return safeAPIError(error)?.code === 'database_stopped';
}

// Raw runtime errors may contain diagnostics; only the structured contract is shown.
export function apiErrorMessage(error: unknown, fallback: string): string {
    return safeAPIError(error)?.message ?? fallback;
}

const japanDate = new Intl.DateTimeFormat('ja-JP', {
    timeZone: 'Asia/Tokyo', year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23',
});

export function registrationDate(value: string): string {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? '日時を確認できません' : japanDate.format(date);
}
