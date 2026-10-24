import {useEffect, useRef, useState} from 'react';
import {LoadWorkListMode, SetWorkListMode} from '../wailsjs/go/main/App';
import {apiErrorMessage} from './work-display';
import type {WorkListMode} from './WorkList';

const validMode = (value: unknown): value is WorkListMode => value === 'compact' || value === 'rows' || value === 'cards';

export function useWorkListMode(queue: {current: Promise<void>}) {
    const [mode, setMode] = useState<WorkListMode>('cards');
    const [ready, setReady] = useState(false);
    const [saving, setSaving] = useState(false);
    const [warning, setWarning] = useState('');
    const generation = useRef(0);
    const revision = useRef(0);
    const selected = useRef<WorkListMode>('cards');
    const loaded = useRef(false);
    useEffect(() => {
        const current = ++generation.current;
        loaded.current = false;
        void (async () => {
            try {
                const value: unknown = await LoadWorkListMode();
                if (current !== generation.current) return;
                if (!validMode(value)) throw new Error('Unknown mode');
                selected.current = value; setMode(value);
            } catch (cause: unknown) {
                if (current !== generation.current) return;
                selected.current = 'cards'; setMode('cards');
                setWarning(apiErrorMessage(cause, '表示形式を読み込めませんでした。メモなしカードで続行します。アプリを再起動して状態を確認してください。'));
            } finally {
                if (current === generation.current) { loaded.current = true; setReady(true); }
            }
        })();
        return () => { ++generation.current; loaded.current = false; };
    }, []);
    function select(value: WorkListMode) {
        if (!loaded.current || selected.current === value) return false;
        selected.current = value; setMode(value); setSaving(true);
        const current = generation.current;
        const latest = ++revision.current;
        // Share the form's queue; each API still updates only its own column.
        queue.current = queue.current.then(async () => {
            if (current !== generation.current) return;
            try {
                await SetWorkListMode(value);
                if (current === generation.current && latest === revision.current) setWarning('');
            } catch (cause: unknown) {
                if (current === generation.current && latest === revision.current) {
                    setWarning(apiErrorMessage(cause, '表示形式を保存できませんでした。再起動すると以前の形式に戻る場合があります。保存先を確認してください。'));
                }
            } finally {
                if (current === generation.current && latest === revision.current) setSaving(false);
            }
        });
        return true;
    }
    return {mode, ready, saving, warning, select};
}
