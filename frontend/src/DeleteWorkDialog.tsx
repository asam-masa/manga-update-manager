import {useEffect, useRef, useState} from 'react';
import {DeleteWork} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';
import {apiErrorCode, apiErrorMessage, isDatabaseStopped} from './work-display';
import type {WorkView} from './work-display';

const warningCodes = new Set(['cover_cleanup_failed', 'cover_release_unconfirmed', 'cover_unavailable_after_commit', 'database_stopped_after_commit']);

export function DeleteWorkDialog({work, onReload, onRemoved, onClose, onStopped, onImagesStopped, onStart}: {
    work: WorkView; onReload: () => Promise<WorkView | undefined>;
    onRemoved: (work: WorkView, missing: boolean, warnings: string[]) => void;
    onClose: () => void; onStopped: () => void; onImagesStopped: () => void; onStart: (work: WorkView) => void;
}) {
    const [snapshot, setSnapshot] = useState(work);
    const [busy, setBusy] = useState(false);
    const [stopped, setStopped] = useState(false);
    const [needsReload, setNeedsReload] = useState(false);
    const [message, setMessage] = useState('');
    const pending = useRef(false);
    const blocked = useRef(false);
    const fresh = useRef(true);
    const active = useRef(false);
    const dialog = useRef<HTMLDialogElement>(null);
    const cancelButton = useRef<HTMLButtonElement>(null);
    useEffect(() => {
        active.current = true;
        const node = dialog.current!;
        node.showModal(); cancelButton.current?.focus();
        return () => { active.current = false; node.close(); };
    }, []);
    useEffect(() => { if (!busy) cancelButton.current?.focus(); }, [busy]);
    function stop() { blocked.current = true; setStopped(true); onStopped(); }
    function close() { if (!pending.current) onClose(); }
    async function refresh() {
        fresh.current = false; setNeedsReload(true);
        try {
            const current = await onReload();
            if (!active.current) return;
            if (!current) { onRemoved(snapshot, true, []); onClose(); return; }
            if (current.id !== work.id || !['url', 'title', 'siteName', 'notes', 'thumbnailPath', 'createdAt'].every((key) => typeof current[key as keyof WorkView] === 'string')
                || (current.scheduledDate != null && typeof current.scheduledDate !== 'string')) throw new Error('Invalid confirmation');
            setSnapshot({...current}); fresh.current = true; setNeedsReload(false);
            setMessage('作品情報が変わりました。最新の作品名とURLを確認し、削除する場合はもう一度「削除する」を押してください。');
            cancelButton.current?.focus();
        } catch (cause: unknown) {
            if (!active.current) return;
            if (isDatabaseStopped(cause) || apiErrorCode(cause) === 'database_unavailable') stop();
            setMessage(apiErrorMessage(cause, '最新の作品情報を取得できません。再読み込みするか、キャンセルしてください。'));
        }
    }
    async function remove() {
        if (pending.current || blocked.current || !fresh.current) return;
        pending.current = true; setBusy(true); setMessage('');
        onStart(snapshot);
        try {
            const result = await DeleteWork(snapshot.id, main.DeleteWorkInput.createFrom({url: snapshot.url, title: snapshot.title, siteName: snapshot.siteName, notes: snapshot.notes, thumbnailPath: snapshot.thumbnailPath, scheduledDate: snapshot.scheduledDate ?? ''}));
            if (!active.current) return;
            if (result?.workID !== snapshot.id || !Array.isArray(result.warnings)) {
                stop(); setMessage('削除結果を確認できません。削除を繰り返さず、アプリを再起動して状態を確認してください。'); return;
            }
            const warnings = result.warnings.map((warning) => {
                if (!warningCodes.has(warning?.code) || typeof warning.message !== 'string' || !warning.message.trim() || warning.message.length > 8192) {
                    stop(); return '削除後の状態を確認できません。アプリを再起動して状態を確認してください。';
                }
                if (warning.code === 'database_stopped_after_commit') stop();
                if (warning.code === 'cover_release_unconfirmed' || warning.code === 'cover_unavailable_after_commit') onImagesStopped();
                return warning.message;
            });
            onRemoved(snapshot, false, warnings); onClose();
        } catch (cause: unknown) {
            if (!active.current) return;
            const code = apiErrorCode(cause);
            if (isDatabaseStopped(cause) || !code || code === 'database_unavailable') stop();
            if (code === 'work_changed') { await refresh(); return; }
            if (code === 'work_not_found') { onRemoved(snapshot, true, []); onClose(); return; }
            setMessage(apiErrorMessage(cause, '削除結果を取得できません。削除を繰り返さず、アプリを再起動して状態を確認してください。'));
        } finally {
            pending.current = false;
            if (active.current) setBusy(false);
        }
    }
    async function reload() {
        if (pending.current || blocked.current) return;
        pending.current = true; setBusy(true);
        try { await refresh(); } finally { pending.current = false; if (active.current) setBusy(false); }
    }
    return <dialog ref={dialog} className="edit-dialog delete-dialog" aria-labelledby="delete-heading"
        onCancel={(event) => { event.preventDefault(); close(); }} aria-busy={busy}>
        <h2 id="delete-heading">作品を削除</h2>
        <p>この作品を完全に削除します。元に戻すことはできません。元画像は削除しません。</p>
        <dl><dt>作品名</dt><dd>{snapshot.title}</dd><dt>作品URL</dt><dd>{snapshot.url}</dd></dl>
        {busy && <p role="status">削除または最新情報の確認を処理中です。結果が表示されるまでお待ちください。</p>}
        {message && <p className="message error-message" role="alert">{message}</p>}
        <div className="delete-buttons">
            <button ref={cancelButton} autoFocus type="button" className="secondary-button" disabled={busy} onClick={close}>{stopped ? '閉じる' : 'キャンセル'}</button>
            {needsReload && !stopped && <button type="button" className="secondary-button" disabled={busy} onClick={() => { void reload(); }}>最新情報を再読み込み</button>}
            <button type="button" className="danger-button" disabled={busy || stopped || needsReload} onClick={() => { void remove(); }}>削除する</button>
        </div>
    </dialog>;
}
