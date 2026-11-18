import {useEffect, useRef, useState} from 'react';
import {CancelBackup, GetBackupResult, GetBackupStatus, SelectAndStartBackup} from '../wailsjs/go/main/App';
import {readBackupCancel, readBackupStart} from './backup-response';
import {useBackupObservation} from './use-backup-observation';

type BackupAPI = {
    status: () => Promise<unknown>; result: (id: string) => Promise<unknown>;
    start: () => Promise<unknown>; cancel: (id: string) => Promise<unknown>;
};
const backupAPI: BackupAPI = {status: GetBackupStatus, result: GetBackupResult, start: SelectAndStartBackup, cancel: CancelBackup};
type Command = {pending: 'selecting' | 'cancelling' | null; error: string; decision: ReturnType<typeof readBackupCancel>['decision'] | null};
const emptyCommand: Command = {pending: null, error: '', decision: null};

// Command lifetime is owned outside the dialog. Callers must keep this hook
// mounted while commands are pending; unmount never cancels backend work.
export function useBackupSession(api: BackupAPI = backupAPI) {
    const observation = useBackupObservation(api.status, api.result);
    const latest = useRef(observation);
    latest.current = observation;
    const [command, setCommand] = useState<Command>(emptyCommand);
    const active = useRef(false), lifetime = useRef(0), sending = useRef(false);
    const fence = useRef({revision: 0, executionId: null as string | null});
    useEffect(() => {
        active.current = true; ++lifetime.current; sending.current = false;
        fence.current = {revision: 0, executionId: null}; setCommand(emptyCommand);
        return () => {active.current = false; ++lifetime.current;};
    }, [api.status, api.result, api.start, api.cancel]);

    function reconciled() {
        const view = latest.current, boundary = fence.current;
        return boundary.revision === 0 || (view.fresh && view.revision >= boundary.revision
            && (boundary.executionId === null || view.status?.executionId === boundary.executionId));
    }
    function canStart() {
        const {fresh, status, result} = latest.current;
        return active.current && !sending.current && reconciled() && fresh
            && (status?.state === 'idle' || (status?.state === 'finished' && result?.executionId === status.executionId
                && result.retryAllowed && result.outcome !== 'unknown'));
    }
    function canCancel() {
        const {fresh, status} = latest.current;
        return active.current && !sending.current && reconciled() && fresh && status?.state === 'running' && status.canCancel;
    }
    function reobserve(executionId: string | null) {
        fence.current = {revision: latest.current.reload(), executionId};
    }
    async function start() {
        if (!canStart()) return;
        sending.current = true;
        const epoch = lifetime.current;
        const current = () => active.current && epoch === lifetime.current;
        setCommand({pending: 'selecting', error: '', decision: null});
        try {
            const value = await api.start();
            if (!current()) return;
            const response = readBackupStart(value);
            if (!response.cancelled) reobserve(response.executionId);
            setCommand(emptyCommand);
        } catch {
            if (!current()) return;
            reobserve(null);
            setCommand({pending: null, decision: null,
                error: '開始結果を確認できません。自動では再実行しません。最新の状況を確認してください。'});
        } finally { if (current()) sending.current = false; }
    }
    async function cancel() {
        if (!canCancel()) return;
        const id = latest.current.status!.executionId;
        sending.current = true;
        const epoch = lifetime.current;
        const current = () => active.current && epoch === lifetime.current;
        setCommand({pending: 'cancelling', error: '', decision: null});
        try {
            const value = await api.cancel(id);
            if (!current()) return;
            const response = readBackupCancel(value, id);
            reobserve(null);
            setCommand({pending: null, error: '', decision: response.decision});
        } catch {
            if (!current()) return;
            reobserve(null);
            setCommand({pending: null, decision: null,
                error: '取消結果を確認できません。自動では再送しません。最新の状況を確認してください。'});
        } finally { if (current()) sending.current = false; }
    }
    return {...observation, commandError: command.error, selecting: command.pending === 'selecting',
        cancelling: command.pending === 'cancelling', cancelDecision: command.decision,
        reconciling: !reconciled(), canStart: canStart(), canCancel: canCancel(), start, cancel};
}
