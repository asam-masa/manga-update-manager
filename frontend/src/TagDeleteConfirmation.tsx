import {useEffect, useRef, useState} from 'react';
import {DeleteTag, LoadTagDeletePreview} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';
import {apiErrorCode, apiErrorMessage, isDatabaseStopped} from './work-display';
import {readTagChange, readTagDeletePreview} from './tag-management';
import type {TagDeletePreview} from './tag-management';
import type {Tag} from './tag-edit';
import type {TagNotice} from './TagManagementNotices';

export function TagDeleteConfirmation({tag, stopped, onStopped, onBusy, onBack, onRemoved, onNotice, onPreview, onUnconfirmed}: {
    tag: Tag; stopped: boolean; onStopped: () => void; onBusy: (busy: boolean) => void;
    onBack: () => void; onRemoved: (tag: Tag, warning?: string) => void; onNotice: (notice: TagNotice) => void;
    onPreview: (preview: TagDeletePreview) => void;
    onUnconfirmed: () => void;
}) {
    const [preview, setPreview] = useState<TagDeletePreview | null>(null);
    const [reading, setReading] = useState(!stopped);
    const [busy, setBusy] = useState(false);
    const [blocked, setBlocked] = useState(stopped);
    const [message, setMessage] = useState('');
    const [confirmed, setConfirmed] = useState(false);
    const fresh = useRef(false), pendingRead = useRef(false), pendingWrite = useRef(false);
    const active = useRef(false), stop = useRef(stopped), generation = useRef(0);
    const back = useRef<HTMLButtonElement>(null);
    function stopWrites() {stop.current = true; fresh.current = false; setBlocked(true); setConfirmed(false); onStopped();}
    async function reload(changed = false) {
        if (pendingRead.current || stop.current || (pendingWrite.current && !changed)) return;
        pendingRead.current = true; fresh.current = false; onUnconfirmed();
        const request = ++generation.current;
        const current = () => active.current && generation.current === request;
        setReading(true); setConfirmed(false); setMessage('');
        try {
            const result = await LoadTagDeletePreview(tag.id);
            if (!current() || stop.current) return;
            const snapshot = readTagDeletePreview(result, tag.id);
            setPreview(snapshot); onPreview(snapshot); fresh.current = true; setConfirmed(true);
            if (changed) setMessage('確認後にタグ名または対象作品が変わりました。最新情報を確認し、削除する場合はもう一度「削除する」を押してください。');
        } catch (cause: unknown) {
            if (!current() || stop.current) return;
            if (isDatabaseStopped(cause) || apiErrorCode(cause) === 'database_unavailable') stopWrites();
            setMessage(apiErrorMessage(cause, '削除対象を取得できません。最新情報を再読み込みするか、一覧に戻ってください。'));
        } finally {
            if (current()) {pendingRead.current = false; setReading(false);}
        }
    }
    useEffect(() => {
        active.current = true; back.current?.focus(); void reload();
        return () => {active.current = false; ++generation.current; pendingRead.current = false; fresh.current = false;};
    }, []);
    useEffect(() => {
        if (stopped) {stop.current = true; fresh.current = false; setBlocked(true); setConfirmed(false); setReading(false); ++generation.current;}
    }, [stopped]);
    useEffect(() => {if (!busy) back.current?.focus();}, [busy]);

    async function remove() {
        if (stop.current || pendingWrite.current || pendingRead.current || !fresh.current || !preview) return;
        const expected = preview;
        pendingWrite.current = true; setBusy(true); onBusy(true); setMessage('');
        try {
            const result = await DeleteTag(main.TagDeletePreviewDTO.createFrom(expected));
            if (!active.current) return;
            const deleted = readTagChange(result, expected.tag.id, expected.tag.name);
            if (deleted.warning) stopWrites();
            // Release the parent's close guard before this child is unmounted.
            onBusy(false); onRemoved(deleted.tag, deleted.warning);
        } catch (cause: unknown) {
            if (!active.current) return;
            const code = apiErrorCode(cause);
            if (!code || isDatabaseStopped(cause) || code === 'database_unavailable') {
                stopWrites();
                const message = apiErrorMessage(cause, 'タグの削除結果を確認できません。削除を繰り返さず、アプリを再起動して状態を確認してください。');
                setMessage(message); onNotice({id: tag.id, message, error: true});
            } else if (code === 'tag_changed') {
                await reload(true);
            } else {
                if (code === 'tag_not_found') {fresh.current = false; setConfirmed(false);}
                setMessage(apiErrorMessage(cause, 'タグを削除できません。状態を確認してください。'));
            }
        } finally {
            pendingWrite.current = false;
            if (active.current) {setBusy(false); onBusy(false);}
        }
    }
    return <section className="tag-delete-confirmation" aria-labelledby="tag-delete-heading">
        <h3 id="tag-delete-heading">タグを削除</h3>
        <p>作品は残ります。このタグと、作品との関連だけを削除します。</p>
        <dl><dt>タグ名</dt><dd>{preview?.tag.name ?? tag.name}</dd><dt>使用作品数</dt><dd>{preview ? `${preview.works.length}作品` : '未取得'}</dd></dl>
        <div className="tag-delete-targets" role="region" aria-label="タグが付いた作品" tabIndex={0} aria-busy={reading}>
            {preview && (preview.works.length ? <ul>{preview.works.map((work) => <li key={work.id}>{work.title}</li>)}</ul> : <p>使用している作品はありません。</p>)}
        </div>
        {preview && !confirmed && <p>前回の確認内容です。最新情報は未確認です。</p>}
        {reading && <p role="status">削除対象を読み込んでいます…</p>}
        {busy && <p role="status">削除または最新情報の確認を処理中です。結果が表示されるまでお待ちください。</p>}
        {blocked && <p role="alert">データの読み書きを停止しています。アプリを再起動して状態を確認してください。</p>}
        {message && <p className="message error-message" role="alert">{message}</p>}
        <div className="tag-management-actions">
            <button ref={back} type="button" className="secondary-button" disabled={busy} onClick={() => {if (!pendingWrite.current) onBack();}}>一覧に戻る</button>
            <button type="button" className="secondary-button" disabled={reading || busy || blocked} onClick={() => {void reload();}}>最新情報を再読み込み</button>
            <button type="button" className="danger-button" disabled={!confirmed || reading || busy || blocked} onClick={() => {void remove();}}>削除する</button>
        </div>
    </section>;
}
