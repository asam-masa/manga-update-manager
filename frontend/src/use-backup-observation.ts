import {useCallback, useEffect, useRef, useState} from 'react';
import {GetBackupResult, GetBackupStatus} from '../wailsjs/go/main/App';
import {readBackupResult, readBackupStatus} from './backup-response';
import type {BackupResult, BackupStatus} from './backup-response';

type Observation = {
    status: BackupStatus | null; result: BackupResult | null;
    loading: boolean; fresh: boolean; error: string; revision: number;
};

// Read-only: mounting/reloading must never start, cancel or resend a backup.
export function useBackupObservation(
    loadStatus: () => Promise<unknown> = GetBackupStatus,
    loadResult: (id: string) => Promise<unknown> = GetBackupResult,
) {
    const [view, setView] = useState<Observation>({status: null, result: null, loading: true, fresh: false, error: '', revision: 0});
    const generation = useRef(0);
    const refresh = useRef<() => number>(() => generation.current);
    const reload = useCallback(() => refresh.current(), []);

    useEffect(() => {
        ++generation.current;
        let active = true, reading = false, queued = false;
        let timer: ReturnType<typeof setTimeout> | undefined;
        let retained: BackupResult | null = null;
        setView((old) => ({...old, loading: true, fresh: false, error: ''}));

        async function read() {
            if (!active) return;
            if (reading) { queued = true; return; }
            clearTimeout(timer);
            reading = true;
            const revision = generation.current;
            const current = () => active && revision === generation.current;
            let status: BackupStatus | null = null;
            setView((old) => ({...old, loading: true}));
            try {
                const value = await loadStatus();
                if (!current()) return;
                status = readBackupStatus(value);
                if (retained?.executionId !== status.executionId || status.state !== 'finished') retained = null;
                if (status.state === 'finished' && !retained) {
                    const result = await loadResult(status.executionId);
                    if (!current()) return;
                    retained = readBackupResult(result, status.executionId);
                }
                setView({status, result: retained, loading: false, fresh: true, error: '', revision});
            } catch {
                if (!current()) return;
                // A valid status with a failed result read still belongs to its
                // own generation. Never pair it with the previous run's result.
                setView((old) => ({...old,
                    status: status ?? old.status,
                    result: status ? retained : old.result,
                    loading: false, fresh: false,
                    error: 'バックアップの最新情報を確認できません。再読み込みをお試しください。'}));
            } finally {
                reading = false;
                if (active) {
                    if (queued) { queued = false; void read(); }
                    else timer = setTimeout(() => { void read(); }, 1000);
                }
            }
        }
        // Return a fence: only reads started at/after this revision can confirm
        // a command's outcome. An already pending read is invalidated now.
        const request = () => {
            const revision = ++generation.current;
            setView((old) => ({...old, fresh: false}));
            void read();
            return revision;
        };
        refresh.current = request;
        void read();
        return () => {
            active = false;
            clearTimeout(timer);
            if (refresh.current === request) refresh.current = () => generation.current;
        };
    }, [loadStatus, loadResult]);

    return {...view, reload};
}
