import {useEffect, useState} from 'react';
import {registrationDate} from './work-display';
import type {WorkView} from './work-display';
import type {CoverActions} from './use-cover-actions';
import {WorkActionsMenu} from './WorkActionsMenu';

type Editing = {onEdit: (work: WorkView, button: HTMLButtonElement) => void; stopped: boolean};
type Deleting = {onDelete: (work: WorkView, button: HTMLButtonElement) => void; stopped: boolean};

export function WorkList({works, showEmpty, onOpen, coversReady, coverRevision, coverActions, editing, deleting, isOpening}: {works: WorkView[]; showEmpty: boolean; onOpen: (id: number) => Promise<void>; coversReady: boolean; coverRevision: number; coverActions?: CoverActions; editing?: Editing; deleting?: Deleting; isOpening?: (work: WorkView) => boolean}) {
    const [menu, setMenu] = useState<string | null>(null);
    useEffect(() => {
        if (menu && !works.some((work) => JSON.stringify([work.id, work.createdAt]) === menu)) setMenu(null);
    }, [menu, works]);
    if (works.length === 0) {
        return showEmpty ? <div className="empty-state">
            <div className="empty-book" aria-hidden="true">本</div>
            <h3>まだ作品が登録されていません</h3>
            <p>作品URLとタイトルを入力して、最初の作品を登録しましょう。</p>
        </div> : null;
    }
    return <ul className="work-list" aria-label="作品一覧">
        {works.map((work) => <WorkCard key={`${work.id}:${work.createdAt}`} work={work} onOpen={onOpen} coversReady={coversReady} coverRevision={coverRevision} coverActions={coverActions} editing={editing} deleting={deleting} opening={isOpening?.(work) ?? false}
            menuOpen={menu === JSON.stringify([work.id, work.createdAt])} onMenuChange={(open) => setMenu(open ? JSON.stringify([work.id, work.createdAt]) : null)} />)}
    </ul>;
}

function WorkCard({work, onOpen, coversReady, coverRevision, coverActions, editing, deleting, opening, menuOpen, onMenuChange}: {work: WorkView; onOpen: (id: number) => Promise<void>; coversReady: boolean; coverRevision: number; coverActions?: CoverActions; editing?: Editing; deleting?: Deleting; opening: boolean; menuOpen: boolean; onMenuChange: (open: boolean) => void}) {
    return <li className="work-card">
            <WorkCover key={`${work.thumbnailPath}:${coverRevision}`} id={work.id} enabled={coversReady && work.thumbnailPath !== ''} />
            <div className="work-details">
                <h3>{work.title}</h3>
                {work.siteName && <p className="site-name">{work.siteName}</p>}
                <button id={`work-url-${JSON.stringify([work.id, work.createdAt])}`} type="button" className="work-url" title={work.url} disabled={opening || coverActions?.states[work.id]?.pending}
                    aria-label={`${work.title}のURLを開く`} onClick={() => { void onOpen(work.id); }}>{work.url}</button>
                {work.notes && <p className="work-notes">{work.notes}</p>}
                <p className="registered-at">登録 <time dateTime={work.createdAt}>{registrationDate(work.createdAt)}</time>（日本時間）</p>
                <p className="registered-at">最終アクセス {work.lastAccessedAt == null ? '未アクセス' :
                    <><time dateTime={work.lastAccessedAt}>{registrationDate(work.lastAccessedAt)}</time>（日本時間）</>}</p>
                {(editing || deleting || coverActions) && <WorkActionsMenu identity={JSON.stringify([work.id, work.createdAt])} title={work.title} open={menuOpen} onChange={onMenuChange}>
                    {(run) => <>
                        {editing && <button type="button" disabled={opening || coverActions?.states[work.id]?.pending || editing.stopped}
                            aria-label={`${work.title}を編集`} onClick={() => run((origin) => editing.onEdit(work, origin))}>編集</button>}
                        {deleting && <button type="button" disabled={opening || coverActions?.states[work.id]?.pending || deleting.stopped}
                            aria-label={`${work.title}を削除`} onClick={() => run((origin) => deleting.onDelete(work, origin))}>削除</button>}
                        {coverActions && <>
                            <button type="button" disabled={!coversReady || opening || coverActions.states[work.id]?.pending || coverActions.selecting}
                                onClick={() => run(() => coverActions.select(work.id))}>{work.thumbnailPath ? '画像を変更' : '画像を設定'}</button>
                            {work.thumbnailPath && <button type="button" disabled={!coversReady || opening || coverActions.states[work.id]?.pending}
                                onClick={() => run(() => coverActions.remove(work.id))}>画像を外す</button>}
                        </>}
                    </>}
                </WorkActionsMenu>}
                {coverActions && <CoverControls work={work} actions={coverActions} />}
            </div>
        </li>;
}

function CoverControls({work, actions}: {work: WorkView; actions: CoverActions}) {
    const state = actions.states[work.id];
    const notice = state?.notice;
    if (!state?.pending && !notice) return null;
    return <div className="cover-controls" role="group" tabIndex={-1} aria-label={`${work.title}の画像操作`}>
        {state?.pending && <p role="status">画像を処理しています…</p>}
        {notice && <div className={`message ${notice.error ? 'error-message' : 'cover-result'}`}>
            {notice.error && <p role="alert">{notice.error}</p>}
            {notice.success && <p role="status">{notice.success}</p>}
            {notice.warnings?.map((warning, index) => <p role="alert" key={index}>{warning}</p>)}
            <button type="button" className="secondary-button" onClick={(event) => {
                // The notice button is about to disappear; keep keyboard position in this card.
                const group = event.currentTarget.closest<HTMLElement>('.cover-controls');
                actions.dismiss(work.id);
                const target = group?.parentElement?.querySelector<HTMLButtonElement>('.work-actions-trigger') ?? group;
                target?.focus();
            }}>画像操作のお知らせを閉じる</button>
        </div>}
    </div>;
}

function WorkCover({id, enabled}: {id: number; enabled: boolean}) {
    // A new mount must also change src: remounting alone can reuse the document's image.
    const [requestToken] = useState(() => Array.from(crypto.getRandomValues(new Uint8Array(16)),
        (byte) => byte.toString(16).padStart(2, '0')).join(''));
    const [failed, setFailed] = useState(false);
    const [loaded, setLoaded] = useState(false);
    return <div className="work-cover">
        <div className="cover-placeholder" aria-hidden="true">
            {(!enabled || !loaded || failed) && <span>本</span>}
            {enabled && !failed && <img src={`/__covers/works/${id}?v=${requestToken}`} alt="" className={loaded ? '' : 'cover-loading'}
                onLoad={() => setLoaded(true)} onError={() => setFailed(true)} />}
        </div>
        {failed && <p className="cover-error" role="status">画像を表示できません</p>}
    </div>;
}
