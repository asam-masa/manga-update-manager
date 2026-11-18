import {useCallback, useEffect, useState} from 'react';
import {GetBackupLocations} from '../wailsjs/go/main/App';
import {readBackupLocations} from './backup-locations';
import type {BackupLocations} from './backup-locations';

export function useBackupLocations(id: string | null, load: (id: string) => Promise<unknown> = GetBackupLocations) {
    const [revision, setRevision] = useState(0);
    const [state, setState] = useState<{id: string | null; data: BackupLocations | null; loading: boolean; error: string}>(
        {id: null, data: null, loading: false, error: ''});
    const reload = useCallback(() => setRevision((old) => old + 1), []);
    useEffect(() => {
        let active = true;
        if (id === null) {setState({id, data: null, loading: false, error: ''}); return;}
        setState({id, data: null, loading: true, error: ''});
        void (async () => {
            try {
                const value = await load(id);
                if (active) setState({id, data: readBackupLocations(value, id), loading: false, error: ''});
            } catch {
                if (active) setState({id, data: null, loading: false, error: '保存先・保全候補を取得できません。詳細を再読み込みしてください。'});
            }
        })();
        return () => {active = false;};
    }, [id, load, revision]);
    // Do not expose a previous run for even one render before effect cleanup.
    return {...(state.id === id ? state : {id, data: null, loading: id !== null, error: ''}), reload};
}
