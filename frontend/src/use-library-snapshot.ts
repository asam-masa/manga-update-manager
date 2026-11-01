import {useCallback, useEffect, useRef, useState} from 'react';
import {LoadLibrarySnapshot} from '../wailsjs/go/main/App';
import {libraryWorkView, readLibrarySnapshot} from './library-snapshot';
import type {LibrarySnapshot} from './library-snapshot';
import {apiErrorMessage, isDatabaseStopped} from './work-display';
import type {WorkView} from './work-display';

export type LibraryData = {snapshot: LibrarySnapshot; works: WorkView[]};
type ReadState = {data: LibraryData | null; loading: boolean; fresh: boolean; stopped: boolean; error: string};

// Own one complete read view. Mutation results/notices and filter selection are
// owned by the caller; changed() invalidates reads, never resends a mutation.
export function useLibrarySnapshot(load: () => Promise<unknown> = LoadLibrarySnapshot) {
    const [state, setState] = useState<ReadState>({data: null, loading: true, fresh: false, stopped: false, error: ''});
    const active = useRef(false), stopped = useRef(false);
    const generation = useRef(0), lifetime = useRef(0);
    const queued = useRef<number | null>(null);

    const stop = useCallback((error?: unknown) => {
        if (!active.current) return;
        stopped.current = true;
        ++generation.current;
        queued.current = null;
        setState((old) => ({...old, loading: false, fresh: false, stopped: true,
            error: apiErrorMessage(error, 'データの読み書きを停止しています。アプリを再起動して状態を確認してください。')}));
    }, []);

    const reload = useCallback(async (): Promise<LibraryData | undefined> => {
        if (!active.current || stopped.current) return;
        queued.current = null; // An explicit reload also covers queued changes.
        const current = ++generation.current;
        setState((old) => ({...old, loading: true, fresh: false, error: ''}));
        try {
            const value = await load();
            if (!active.current || current !== generation.current || stopped.current) return;
            const snapshot = readLibrarySnapshot(value);
            const data = {snapshot, works: snapshot.works.map(libraryWorkView)};
            setState({data, loading: false, fresh: true, stopped: false, error: ''});
            return data;
        } catch (error: unknown) {
            if (!active.current) return;
            // A stale read cannot replace data, but its DB-stop observation must
            // still prevent later successful reads from re-enabling operations.
            if (isDatabaseStopped(error)) { stop(error); return; }
            if (current === generation.current && !stopped.current) {
                setState((old) => ({...old, loading: false, fresh: false,
                    error: apiErrorMessage(error, '作品とタグの一覧を取得できません。再読み込みをお試しください。')}));
            }
        }
    }, [load, stop]);

    const changed = useCallback(() => {
        if (!active.current || stopped.current) return;
        ++generation.current; // Invalidate now, before the queued read starts.
        setState((old) => ({...old, loading: true, fresh: false}));
        const epoch = lifetime.current;
        if (queued.current === epoch) return;
        queued.current = epoch;
        queueMicrotask(() => {
            if (queued.current !== epoch) return;
            queued.current = null;
            if (active.current && lifetime.current === epoch && !stopped.current) void reload();
        });
    }, [reload]);

    useEffect(() => {
        active.current = true;
        ++lifetime.current;
        void reload();
        return () => {
            active.current = false;
            ++generation.current;
            queued.current = null;
        };
    }, [reload]);

    return {...state, reload, changed, stop};
}
