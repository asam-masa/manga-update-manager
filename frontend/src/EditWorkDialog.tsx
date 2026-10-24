import {useEffect, useRef, useState} from 'react';
import type {FormEvent} from 'react';
import {EditWork} from '../wailsjs/go/main/App';
import {apiErrorCode, apiErrorMessage, isDatabaseStopped} from './work-display';
import type {WorkView} from './work-display';

export function EditWorkDialog({work, onSaved, onClose, onStopped, onStart}: {
    work: WorkView; onSaved: (work: WorkView, warnings: string[]) => void; onClose: () => void; onStopped: () => void; onStart: (work: WorkView) => void;
}) {
    // Snapshot only once per opening: background responses must never replace input.
    const [form, setForm] = useState(() => ({url: work.url, title: work.title, siteName: work.siteName, notes: work.notes}));
    const [saving, setSaving] = useState(false);
    const [stopped, setStopped] = useState(false);
    const [error, setError] = useState('');
    const pending = useRef(false);
    const blocked = useRef(false);
    const dialog = useRef<HTMLDialogElement>(null);
    const active = useRef(false);
    useEffect(() => {
        active.current = true;
        const node = dialog.current!;
        node.showModal();
        return () => { active.current = false; node.close(); };
    }, []);

    function cancel() { if (!pending.current) onClose(); }
    async function save(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        if (pending.current || blocked.current) return;
        pending.current = true;
        onStart(work);
        setSaving(true); setError('');
        try {
            const result = await EditWork(work.id, {...form});
            if (!active.current) return;
            if (!result?.work || result.work.id !== work.id || !Array.isArray(result.warnings)
                || !['url', 'title', 'siteName', 'notes', 'updatedAt'].every((key) => typeof result.work[key as keyof WorkView] === 'string')) {
                // A missing/invalid success result is not permission to send another write.
                blocked.current = true; setStopped(true); onStopped();
                setError('保存結果を確認できません。入力を控え、アプリを再起動して状態を確認してください。');
                return;
            }
            const warnings = result.warnings.map((warning) => warning?.code === 'database_stopped_after_commit'
                && typeof warning.message === 'string' && warning.message.trim() && warning.message.length <= 8192
                ? warning.message : '保存後の状態を確認できません。アプリを再起動して状態を確認してください。');
            if (warnings.length) onStopped();
            onSaved(result.work, warnings);
            onClose();
        } catch (cause: unknown) {
            if (!active.current) return;
            if (isDatabaseStopped(cause) || !apiErrorCode(cause) || apiErrorCode(cause) === 'database_unavailable') {
                blocked.current = true; setStopped(true); onStopped();
            }
            setError(apiErrorMessage(cause, '保存結果を取得できませんでした。入力を控え、アプリを再起動して状態を確認してください。'));
        } finally {
            pending.current = false;
            if (active.current) setSaving(false);
        }
    }

    return <dialog ref={dialog} className="edit-dialog" aria-labelledby="edit-heading" onCancel={(event) => { event.preventDefault(); cancel(); }}>
        <h2 id="edit-heading">作品を編集</h2>
        <p className="section-description">URLとタイトルは必須です。画像と日時は変更しません。</p>
        <form onSubmit={(event) => { void save(event); }} aria-busy={saving} onKeyDown={(event) => {
            if (event.key === 'Enter' && (event.nativeEvent.isComposing || event.keyCode === 229)) event.preventDefault();
        }}>
            <fieldset disabled={saving}>
                <label htmlFor="edit-url">作品URL <span className="required">必須</span></label>
                <input id="edit-url" autoFocus type="url" required value={form.url} readOnly={stopped} onChange={(event) => setForm({...form, url: event.target.value})} />
                <label htmlFor="edit-title">タイトル <span className="required">必須</span></label>
                <input id="edit-title" required value={form.title} readOnly={stopped} onChange={(event) => setForm({...form, title: event.target.value})} />
                <label htmlFor="edit-site">サイト名 <span className="optional">任意</span></label>
                <input id="edit-site" value={form.siteName} readOnly={stopped} onChange={(event) => setForm({...form, siteName: event.target.value})} />
                <label htmlFor="edit-notes">メモ <span className="optional">任意</span></label>
                <textarea id="edit-notes" rows={4} value={form.notes} readOnly={stopped} onChange={(event) => setForm({...form, notes: event.target.value})} />
                <button type="submit" className="primary-button" disabled={stopped}>{saving ? '保存中…' : '保存する'}</button>
            </fieldset>
            {saving && <p role="status">作品情報を保存しています。結果が表示されるまでお待ちください。</p>}
            {error && <p className="message error-message" role="alert">{error}</p>}
            <button type="button" className="secondary-button edit-cancel" disabled={saving} onClick={cancel}>{stopped ? '閉じる' : 'キャンセル'}</button>
        </form>
    </dialog>;
}
