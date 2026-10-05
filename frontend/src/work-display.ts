const knownCodes = new Set(['database_unavailable', 'invalid_input', 'duplicate_url', 'storage_error', 'internal_error']);

// Raw runtime errors may contain diagnostics; only the structured contract is shown.
export function apiErrorMessage(error: unknown, fallback: string): string {
    if (typeof error !== 'object' || error === null) return fallback;
    if ('code' in error && typeof error.code === 'string' && knownCodes.has(error.code)
        && 'message' in error && typeof error.message === 'string' && error.message.trim()) {
        return error.message;
    }
    return fallback;
}

const japanDate = new Intl.DateTimeFormat('ja-JP', {
    timeZone: 'Asia/Tokyo', year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23',
});

export function registrationDate(value: string): string {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? '日時を確認できません' : japanDate.format(date);
}
