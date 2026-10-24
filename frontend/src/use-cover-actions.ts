import {useEffect, useRef, useState} from 'react';
import {RemoveWorkCoverWithOperation, SelectWorkCoverWithOperation} from '../wailsjs/go/main/App';
import {EventsOn} from '../wailsjs/runtime/runtime';
import {apiErrorMessage} from './work-display';
import type {WorkView} from './work-display';

export type CoverNotice = {error?: string; success?: string; warnings?: string[]};
export type CoverState = {work: WorkView; pending: boolean; operationID?: string; kind?: 'select' | 'remove'; notice?: CoverNotice};

const identity = (work: WorkView) => JSON.stringify([work.id, work.createdAt]);

const warningCodes = new Set(['cover_cleanup_failed', 'cover_unavailable_after_commit', 'cover_release_unconfirmed', 'database_stopped_after_commit']);

// Owned by App, not by cards: filtering never cancels an operation or its notice.
export function useCoverActions(ready: boolean, onSaved: (work: WorkView) => void, blocked?: (id: number) => boolean) {
    const [states, setStates] = useState<Record<string, CoverState>>({});
    const [selecting, setSelecting] = useState(false);
    const pending = useRef(new Set<number>());
    const chooser = useRef<{workID: number; operationID: string} | null>(null);
    const generation = useRef(0);
    useEffect(() => {
        const current = ++generation.current;
        const off = EventsOn('cover-selection-finished', (event: unknown) => {
            if (current !== generation.current) return;
            if (typeof event !== 'object' || event === null) return;
            if ('workID' in event && 'operationID' in event && chooser.current
                && event.workID === chooser.current.workID && event.operationID === chooser.current.operationID) {
                chooser.current = null;
                setSelecting(false);
            }
        });
        return () => { ++generation.current; off(); };
    }, []);

    async function run(work: WorkView, remove: boolean) {
        const id = work.id;
        const key = identity(work);
        // Refs close the same-tick gap before disabled controls are rendered.
        if (!ready || blocked?.(id) || pending.current.has(id) || (!remove && chooser.current)) return;
        const current = generation.current;
        const operationID = Array.from(crypto.getRandomValues(new Uint8Array(16)), (value) => value.toString(16).padStart(2, '0')).join('');
        pending.current.add(id);
        setStates((values) => ({...values, [key]: {work: {...work}, pending: true, operationID, kind: remove ? 'remove' : 'select'}}));
        if (!remove) { chooser.current = {workID: id, operationID}; setSelecting(true); }
        try {
            const result = remove ? await RemoveWorkCoverWithOperation(id, operationID) : await SelectWorkCoverWithOperation(id, operationID);
            if (current !== generation.current) return;
            if ('cancelled' in result && result.cancelled) return;
            if (!result.work || identity(result.work) !== key) throw new Error('Invalid cover result');
            onSaved(result.work);
            const warnings = result.warnings.map((warning) => warningCodes.has(warning.code) ? warning.message
                : '画像変更後の後処理を確認できませんでした。アプリを再起動して状態を確認してください。');
            setStates((values) => ({...values, [key]: {...values[key], pending: true, notice: {
                success: remove ? '画像を外しました。' : '画像を設定しました。', warnings,
            }}}));
        } catch (cause: unknown) {
            if (current !== generation.current) return;
            setStates((values) => ({...values, [key]: {...values[key], pending: true, notice: {
                error: apiErrorMessage(cause, '画像の操作結果を取得できませんでした。アプリを再起動して状態を確認してください。'),
            }}}));
        } finally {
            pending.current.delete(id);
            if (current === generation.current) {
                if (chooser.current?.operationID === operationID && chooser.current.workID === id) {
                    chooser.current = null; setSelecting(false);
                }
                setStates((values) => ({...values, [key]: {...values[key], pending: false}}));
            }
        }
    }
    return {states, selecting, stateFor: (work: WorkView) => states[identity(work)], isPending: (id: number) => pending.current.has(id),
        select: (work: WorkView) => { void run(work, false); }, remove: (work: WorkView) => { void run(work, true); },
        dismiss: (work: WorkView) => setStates((values) => {
            const key = identity(work);
            if (values[key]?.pending) return values;
            const next = {...values}; delete next[key]; return next;
        })};
}

export type CoverActions = ReturnType<typeof useCoverActions>;
