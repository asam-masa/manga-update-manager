import {useEffect, useRef, useState} from 'react';
import {LoadTags, LoadWorkTags} from '../wailsjs/go/main/App';
import {apiErrorCode, isDatabaseStopped} from './work-display';
import {readTags} from './tag-edit';
import type {Tag} from './tag-edit';

type Draft = {candidates: Tag[]; existing: Tag[]; newNames: string[]; initialized: boolean; loading: boolean; error: string};
export function useWorkTagEdit(workID: number, onStopped: () => void) {
    const [draft, setDraft] = useState<Draft>({candidates: [], existing: [], newNames: [], initialized: false, loading: true, error: ''});
    const generation = useRef(0), alive = useRef(false), fatal = useRef(false), readable = useRef(false);
    const stoppedCallback = useRef(onStopped); stoppedCallback.current = onStopped;

    async function reload() {
        const request = ++generation.current;
        readable.current = false;
        setDraft((old) => ({...old, loading: true, error: ''}));
        const current = () => alive.current && generation.current === request;
        // Observe each read independently: a fast ordinary failure must not hide
        // a slower database_stopped result from the other read.
        async function observe(read: () => Promise<unknown>) {
            try { return await read(); }
            catch (cause) {
                if (current() && !fatal.current && (isDatabaseStopped(cause) || apiErrorCode(cause) === 'database_unavailable')) {
                    fatal.current = true; stoppedCallback.current();
                }
                throw cause;
            }
        }
        const results = await Promise.allSettled([observe(LoadTags), observe(() => LoadWorkTags(workID))]);
        if (!current()) return;
        try {
            if (fatal.current || results.some((result) => result.status === 'rejected')) throw new Error('Tag read failed');
            const candidates = readTags((results[0] as PromiseFulfilledResult<unknown>).value);
            const existing = readTags((results[1] as PromiseFulfilledResult<unknown>).value);
            // These APIs are separate reads. A change between them needs a retry,
            // rather than silently choosing one of two different names.
            if (existing.some((tag) => !candidates.some((candidate) => candidate.id === tag.id && candidate.name === tag.name))) throw new Error('Inconsistent tags');
            readable.current = true;
            setDraft((old) => ({...old, candidates, existing: old.initialized ? old.existing : existing,
                initialized: true, loading: false, error: ''}));
        } catch {
            setDraft((old) => ({...old, loading: false, error: fatal.current
                ? 'タグの取得を続けられません。入力を控え、アプリを再起動して状態を確認してください。'
                : 'タグを読み込めませんでした。他の入力は残っています。タグを再読み込みするか、キャンセルしてください。'}));
        }
    }
    useEffect(() => {
        alive.current = true;
        void reload();
        return () => {alive.current = false; generation.current++; readable.current = false;};
    }, [workID]);

    const conflicts = draft.existing.filter((tag) => !draft.candidates.some((candidate) => candidate.id === tag.id && candidate.name === tag.name));
    const ready = draft.initialized && !draft.loading && !draft.error && !conflicts.length && !fatal.current;
    return {
        ...draft, conflicts, ready, reload,
        input: () => readable.current && ready ? {existing: draft.existing, newNames: draft.newNames} : undefined,
        update: (existing: Tag[], newNames = draft.newNames) => setDraft((old) => ({...old, existing, newNames})),
    };
}
