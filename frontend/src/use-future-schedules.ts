import {useEffect, useRef, useState} from 'react';
import {LoadShowFutureSchedules, SetShowFutureSchedules} from '../wailsjs/go/main/App';
import {apiErrorMessage} from './work-display';

export function useFutureSchedules(queue: {current: Promise<void>}) {
    const [visible, setVisible] = useState(true);
    const [ready, setReady] = useState(false);
    const [saving, setSaving] = useState(false);
    const [warning, setWarning] = useState('');
    const generation = useRef(0), revision = useRef(0);
    const selected = useRef(true), loaded = useRef(false);
    useEffect(() => {
        const current = ++generation.current;
        loaded.current = false;
        void (async () => {
            try {
                const value: unknown = await LoadShowFutureSchedules();
                if (current !== generation.current) return;
                if (typeof value !== 'boolean') throw new Error('Unknown future visibility');
                selected.current = value; setVisible(value);
            } catch (cause: unknown) {
                if (current !== generation.current) return;
                selected.current = true; setVisible(true);
                setWarning(apiErrorMessage(cause, '未来の予定の表示設定を読み込めませんでした。未来の予定を表示して続行します。アプリを再起動して状態を確認してください。'));
            } finally {
                if (current === generation.current) { loaded.current = true; setReady(true); }
            }
        })();
        return () => { ++generation.current; loaded.current = false; };
    }, []);
    function select(value: boolean) {
        if (!loaded.current || selected.current === value) return false;
        selected.current = value; setVisible(value); setSaving(true);
        const current = generation.current, latest = ++revision.current;
        queue.current = queue.current.then(async () => {
            if (current !== generation.current) return;
            try {
                await SetShowFutureSchedules(value);
                if (current === generation.current && latest === revision.current) setWarning('');
            } catch (cause: unknown) {
                if (current === generation.current && latest === revision.current) {
                    setWarning(apiErrorMessage(cause, '未来の予定の表示設定を保存できませんでした。再起動すると以前の状態に戻る場合があります。保存先を確認してください。'));
                }
            } finally {
                if (current === generation.current && latest === revision.current) setSaving(false);
            }
        });
        return true;
    }
    return {visible, ready, saving, warning, select};
}
