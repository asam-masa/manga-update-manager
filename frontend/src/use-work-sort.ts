import {useEffect, useRef, useState} from 'react';
import {LoadWorkSort, SetWorkSort} from '../wailsjs/go/main/App';
import {apiErrorMessage} from './work-display';
import {isWorkSort} from './work-sort';
import type {WorkSort} from './work-sort';

export function useWorkSort(queue: {current: Promise<void>}) {
    const [order, setOrder] = useState<WorkSort>('registration_asc');
    const [ready, setReady] = useState(false);
    const [saving, setSaving] = useState(false);
    const [warning, setWarning] = useState('');
    const generation = useRef(0), revision = useRef(0);
    const selected = useRef<WorkSort>('registration_asc');
    const loaded = useRef(false);
    useEffect(() => {
        const current = ++generation.current;
        loaded.current = false;
        void (async () => {
            try {
                const value: unknown = await LoadWorkSort();
                if (current !== generation.current) return;
                if (!isWorkSort(value)) throw new Error('Unknown sort');
                selected.current = value; setOrder(value);
            } catch (cause: unknown) {
                if (current !== generation.current) return;
                selected.current = 'registration_asc'; setOrder('registration_asc');
                setWarning(apiErrorMessage(cause, '一覧の並び順を読み込めませんでした。登録の古い順で続行します。アプリを再起動して状態を確認してください。'));
            } finally {
                if (current === generation.current) { loaded.current = true; setReady(true); }
            }
        })();
        return () => { ++generation.current; loaded.current = false; };
    }, []);
    function select(value: WorkSort) {
        if (!loaded.current || selected.current === value) return false;
        selected.current = value; setOrder(value); setSaving(true);
        const current = generation.current, latest = ++revision.current;
        queue.current = queue.current.then(async () => {
            if (current !== generation.current) return;
            try {
                await SetWorkSort(value);
                if (current === generation.current && latest === revision.current) setWarning('');
            } catch (cause: unknown) {
                if (current === generation.current && latest === revision.current) {
                    setWarning(apiErrorMessage(cause, '一覧の並び順を保存できませんでした。再起動すると以前の順番に戻る場合があります。保存先を確認してください。'));
                }
            } finally {
                if (current === generation.current && latest === revision.current) setSaving(false);
            }
        });
        return true;
    }
    return {order, ready, saving, warning, select};
}
