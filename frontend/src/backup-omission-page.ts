import {validBackupID} from './backup-response';
import {tagName as trimGoSpace, validStringID as validWorkID} from './tag-edit';

export type OmissionRequest = {executionId: string; page: number; imageId?: string};
type ImageItem = {imageId: string; reason: 'missing' | 'corrupt' | 'unavailable'};
type WorkItem = {workId: string; title: string};
type PageInfo = {executionId: string; page: number; pageSize: 50; pages: number; total: number};
export type OmissionPage = PageInfo & ({kind: 'images'; items: ImageItem[]} | {kind: 'works'; imageId: string; items: WorkItem[]});
function imageID(value: unknown): value is string { return typeof value === 'string' && /^[0-9a-f]{32}$/.test(value); }
function integer(value: unknown, min: number): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value >= min; }
export function validateOmissionRequest(request: OmissionRequest) {
    if (!validBackupID(request.executionId) || !integer(request.page, 1)
        || (request.imageId !== undefined && !imageID(request.imageId))) throw new Error('Invalid omission request');
}
export function readOmissionPage(value: unknown, request: OmissionRequest): OmissionPage {
    validateOmissionRequest(request);
    if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid omission page');
    const v = value as Record<string, unknown>;
    if (v.executionId !== request.executionId || v.page !== request.page || v.pageSize !== 50
        || !integer(v.total, 0) || !integer(v.pages, 1) || v.pages !== Math.max(1, Math.ceil(v.total / 50))
        || request.page > v.pages || !Array.isArray(v.items)
        || v.items.length !== Math.min(50, v.total - (request.page - 1) * 50)) throw new Error('Inconsistent omission page');
    const info: PageInfo = {executionId: request.executionId, page: request.page, pageSize: 50, pages: v.pages, total: v.total};
    const ids = new Set<string>();
    if (request.imageId !== undefined) {
        if (v.imageId !== request.imageId) throw new Error('Mismatched omission image');
        const items = v.items.map((item: unknown) => {
            if (!item || typeof item !== 'object' || !('workId' in item) || !validWorkID(item.workId) || ids.has(item.workId)
                || !('title' in item) || typeof item.title !== 'string' || !trimGoSpace(item.title) || /\p{Cs}/u.test(item.title)) throw new Error('Invalid omission work');
            ids.add(item.workId); return {workId: item.workId, title: item.title};
        });
        return {...info, kind: 'works', imageId: request.imageId, items};
    }
    const items = v.items.map((item: unknown) => {
        if (!item || typeof item !== 'object' || !('imageId' in item) || !imageID(item.imageId) || ids.has(item.imageId)
            || !('reason' in item) || !['missing', 'corrupt', 'unavailable'].includes(item.reason as string)) throw new Error('Invalid omitted image');
        ids.add(item.imageId); return {imageId: item.imageId, reason: item.reason as ImageItem['reason']};
    });
    return {...info, kind: 'images', items};
}
