import {useRef, useState} from 'react';
import {apiErrorMessage, registrationDate} from './work-display';
import type {WorkView} from './work-display';
import type {CoverActions} from './use-cover-actions';

export function WorkList({works, showEmpty, onOpen, coversReady, coverRevision, coverActions}: {works: WorkView[]; showEmpty: boolean; onOpen: (id: number) => Promise<void>; coversReady: boolean; coverRevision: number; coverActions?: CoverActions}) {
    if (works.length === 0) {
        return showEmpty ? <div className="empty-state">
            <div className="empty-book" aria-hidden="true">本</div>
            <h3>まだ作品が登録されていません</h3>
            <p>作品URLとタイトルを入力して、最初の作品を登録しましょう。</p>
        </div> : null;
    }
    return <ul className="work-list" aria-label="作品一覧">
        {works.map((work) => <WorkCard key={work.id} work={work} onOpen={onOpen} coversReady={coversReady} coverRevision={coverRevision} coverActions={coverActions} />)}
    </ul>;
}

function WorkCard({work, onOpen, coversReady, coverRevision, coverActions}: {work: WorkView; onOpen: (id: number) => Promise<void>; coversReady: boolean; coverRevision: number; coverActions?: CoverActions}) {
    const pending = useRef(false);
    const [opening, setOpening] = useState(false);
    const [error, setError] = useState('');
    async function open() {
        if (pending.current) return;
        pending.current = true;
        setOpening(true);
        setError('');
        try { await onOpen(work.id); }
        catch (cause: unknown) { setError(apiErrorMessage(cause, '作品を開く処理に失敗しました。再度お試しください。')); }
        finally { pending.current = false; setOpening(false); }
    }
    return <li className="work-card">
            <WorkCover key={`${work.thumbnailPath}:${coverRevision}`} id={work.id} enabled={coversReady && work.thumbnailPath !== ''} />
            <div className="work-details">
                <h3>{work.title}</h3>
                {work.siteName && <p className="site-name">{work.siteName}</p>}
                <button type="button" className="work-url" title={work.url} disabled={opening}
                    aria-label={`${work.title}のURLを開く`} onClick={() => { void open(); }}>{work.url}</button>
                {opening && <p role="status">ページを開いています…</p>}
                {work.notes && <p className="work-notes">{work.notes}</p>}
                <p className="registered-at">登録 <time dateTime={work.createdAt}>{registrationDate(work.createdAt)}</time>（日本時間）</p>
                <p className="registered-at">最終アクセス {work.lastAccessedAt == null ? '未アクセス' :
                    <><time dateTime={work.lastAccessedAt}>{registrationDate(work.lastAccessedAt)}</time>（日本時間）</>}</p>
                {coverActions && <CoverControls work={work} ready={coversReady} actions={coverActions} />}
                {error && <p className="message error-message" role="alert">{error}</p>}
            </div>
        </li>;
}

function CoverControls({work, ready, actions}: {work: WorkView; ready: boolean; actions: CoverActions}) {
    const state = actions.states[work.id];
    const notice = state?.notice;
    return <div className="cover-controls" role="group" tabIndex={-1} aria-label={`${work.title}の画像操作`}>
        <div className="cover-buttons">
            <button type="button" className="secondary-button" disabled={!ready || state?.pending || actions.selecting}
                onClick={() => actions.select(work.id)}>{work.thumbnailPath ? '画像を変更' : '画像を設定'}</button>
            {work.thumbnailPath && <button type="button" className="secondary-button" disabled={!ready || state?.pending}
                onClick={() => actions.remove(work.id)}>画像を外す</button>}
        </div>
        {state?.pending && <p role="status">画像を処理しています…</p>}
        {notice && <div className={`message ${notice.error ? 'error-message' : 'cover-result'}`}>
            {notice.error && <p role="alert">{notice.error}</p>}
            {notice.success && <p role="status">{notice.success}</p>}
            {notice.warnings?.map((warning, index) => <p role="alert" key={index}>{warning}</p>)}
            <button type="button" className="secondary-button" onClick={(event) => {
                // The notice button is about to disappear; keep keyboard position in this card.
                const group = event.currentTarget.closest<HTMLElement>('.cover-controls');
                actions.dismiss(work.id);
                const target = group?.querySelector<HTMLButtonElement>('.cover-buttons button:not(:disabled)') ?? group;
                target?.focus();
            }}>画像操作のお知らせを閉じる</button>
        </div>}
    </div>;
}

function WorkCover({id, enabled}: {id: number; enabled: boolean}) {
    const [failed, setFailed] = useState(false);
    const [loaded, setLoaded] = useState(false);
    return <div className="work-cover">
        <div className="cover-placeholder" aria-hidden="true">
            {(!enabled || !loaded || failed) && <span>本</span>}
            {enabled && !failed && <img src={`/__covers/works/${id}`} alt="" className={loaded ? '' : 'cover-loading'}
                onLoad={() => setLoaded(true)} onError={() => setFailed(true)} />}
        </div>
        {failed && <p className="cover-error" role="status">画像を表示できません</p>}
    </div>;
}
