import {useEffect, useLayoutEffect, useRef, useState} from 'react';
import type {CSSProperties} from 'react';
import {registrationDate, scheduledDateLabel} from './work-display';
import type {WorkView} from './work-display';
import type {CoverActions} from './use-cover-actions';
import {WorkActionsMenu} from './WorkActionsMenu';

type Editing = {onEdit: (work: WorkView, button: HTMLButtonElement) => void; stopped: boolean};
type Deleting = {onDelete: (work: WorkView, button: HTMLButtonElement) => void; stopped: boolean};
export type WorkListMode = 'compact' | 'rows' | 'cards';

export function WorkList({works, showEmpty, onOpen, coversReady, coverRevision, coverActions, editing, deleting, isOpening, mode = 'cards'}: {works: WorkView[]; showEmpty: boolean; onOpen: (id: number) => Promise<void>; coversReady: boolean; coverRevision: number; coverActions?: CoverActions; editing?: Editing; deleting?: Deleting; isOpening?: (work: WorkView) => boolean; mode?: WorkListMode}) {
    const [menu, setMenu] = useState<{identity: string; mode: WorkListMode; width: number} | null>(null);
    const viewport = useRef<HTMLDivElement>(null);
    const [width, setWidth] = useState(0);
    const hasWorks = works.length > 0;
    useLayoutEffect(() => {
        const node = viewport.current;
        if (!node) return;
        setWidth(node.getBoundingClientRect().width);
        if (typeof ResizeObserver === 'undefined') return;
        const observer = new ResizeObserver(([entry]) => { if (entry) setWidth(entry.contentRect.width); });
        observer.observe(node);
        return () => observer.disconnect();
    }, [hasWorks]);
    useEffect(() => {
        if (menu && (menu.mode !== mode || menu.width !== width || !works.some((work) => JSON.stringify([work.id, work.createdAt]) === menu.identity))) setMenu(null);
    }, [menu, works, mode, width]);
    if (works.length === 0) {
        return showEmpty ? <div className="empty-state">
            <div className="empty-book" aria-hidden="true">本</div>
            <h3>まだ作品が登録されていません</h3>
            <p>作品URLとタイトルを入力して、最初の作品を登録しましょう。</p>
        </div> : null;
    }
    const items = works.map((work) => <WorkCard key={`${work.id}:${work.createdAt}`} mode={mode} work={work} onOpen={onOpen} coversReady={coversReady} coverRevision={coverRevision} coverActions={coverActions} editing={editing} deleting={deleting} opening={isOpening?.(work) ?? false}
        menuOpen={menu?.mode === mode && menu.width === width && menu.identity === JSON.stringify([work.id, work.createdAt])} onMenuChange={(open) => setMenu(open ? {identity: JSON.stringify([work.id, work.createdAt]), mode, width} : null)} />);
    const layout = mode === 'rows' ? <div className="work-rows-scroll" tabIndex={0} aria-label="作品一覧の横スクロール">
        <table className="work-rows" aria-label="作品一覧"><colgroup><col className="title-column" /><col /><col className="access-column" /><col className="schedule-column" /><col className="actions-column" /></colgroup>
            <thead><tr><th scope="col">タイトル</th><th scope="col">URL</th><th scope="col">最終アクセス</th><th scope="col">更新予定日</th><th scope="col">操作</th></tr></thead>
            <tbody>{items}</tbody>
        </table>
    </div> : <ul key={mode} className={`work-list work-list-${mode}`} aria-label="作品一覧">{items}</ul>;
    // Keep the existing menu's viewport coordinates; only pass width to CSS.
    return <div ref={viewport} className="work-layout" style={{'--compact-columns': width > 859 ? 5 : width > 700 ? 4 : 2, '--card-columns': width > 700 ? 2 : 1} as CSSProperties}>{layout}</div>;
}

function WorkCard({work, onOpen, coversReady, coverRevision, coverActions, editing, deleting, opening, menuOpen, onMenuChange, mode}: {work: WorkView; onOpen: (id: number) => Promise<void>; coversReady: boolean; coverRevision: number; coverActions?: CoverActions; editing?: Editing; deleting?: Deleting; opening: boolean; menuOpen: boolean; onMenuChange: (open: boolean) => void; mode: WorkListMode}) {
    const blocked = opening || coverActions?.stateFor(work)?.pending;
    const title = <h3 className="work-title" title={work.title}>{work.title}</h3>;
    const url = <button id={`work-url-${JSON.stringify([work.id, work.createdAt])}`} type="button" className="work-url" title={work.url} disabled={blocked}
        aria-label={`${work.title}のURLを開く`} onClick={() => { void onOpen(work.id); }}>{work.url}</button>;
    const accessText = work.lastAccessedAt == null ? '未アクセス' : `${registrationDate(work.lastAccessedAt)}（日本時間）`;
    const access = <p className="work-last-access" title={`最終アクセス ${accessText}`}>{mode !== 'rows' && '最終アクセス '}{work.lastAccessedAt == null ? '未アクセス' :
        <><time dateTime={work.lastAccessedAt}>{registrationDate(work.lastAccessedAt)}</time>（日本時間）</>}</p>;
    const schedule = <p className="work-scheduled-date" title={`更新予定 ${scheduledDateLabel(work.scheduledDate)}`}>{mode !== 'rows' && '更新予定 '}{typeof work.scheduledDate === 'string' && work.scheduledDate ?
        <time dateTime={work.scheduledDate}>{scheduledDateLabel(work.scheduledDate)}</time> : scheduledDateLabel(work.scheduledDate)}</p>;
    const cover = <WorkCover key={`${work.thumbnailPath}:${coverRevision}`} id={work.id} enabled={coversReady && work.thumbnailPath !== ''} />;
    const actions = (editing || deleting || coverActions) && <WorkActionsMenu identity={JSON.stringify([work.id, work.createdAt])} title={work.title} open={menuOpen} onChange={onMenuChange}>
                    {(run) => <>
                        {editing && <button type="button" disabled={opening || coverActions?.stateFor(work)?.pending || editing.stopped}
                            aria-label={`${work.title}を編集`} onClick={() => run((origin) => editing.onEdit(work, origin))}>編集</button>}
                        {deleting && <button type="button" disabled={opening || coverActions?.stateFor(work)?.pending || deleting.stopped}
                            aria-label={`${work.title}を削除`} onClick={() => run((origin) => deleting.onDelete(work, origin))}>削除</button>}
                        {coverActions && <>
                            <button type="button" disabled={!coversReady || opening || coverActions.stateFor(work)?.pending || coverActions.selecting}
                                onClick={() => run(() => coverActions.select(work))}>{work.thumbnailPath ? '画像を変更' : '画像を設定'}</button>
                            {work.thumbnailPath && <button type="button" disabled={!coversReady || opening || coverActions.stateFor(work)?.pending}
                                onClick={() => run(() => coverActions.remove(work))}>画像を外す</button>}
                        </>}
                    </>}
                </WorkActionsMenu>;
    if (mode === 'rows') return <tr className="work-row"><td>{title}</td><td>{url}</td><td>{access}</td><td>{schedule}</td><td>{actions}</td></tr>;
    if (mode === 'compact') return <li className="work-card work-compact">
        <button type="button" className="work-image-entry" disabled={blocked} aria-label={`${work.title}のURLを開く`} title={work.url} onClick={() => { void onOpen(work.id); }}>{cover}</button>
        {title}{actions}
    </li>;
    return <li className="work-card">{cover}<div className="work-details">{title}
        <p className="site-name" title={work.siteName}>{work.siteName || '\u00a0'}</p>{url}{access}{schedule}
    </div>{actions}</li>;
}

function WorkCover({id, enabled}: {id: number; enabled: boolean}) {
    // A new mount must also change src: remounting alone can reuse the document's image.
    const [requestToken] = useState(() => Array.from(crypto.getRandomValues(new Uint8Array(16)),
        (byte) => byte.toString(16).padStart(2, '0')).join(''));
    const [failed, setFailed] = useState(false);
    const [loaded, setLoaded] = useState(false);
    return <span className="work-cover">
        <span className="cover-placeholder" aria-hidden="true">
            {(!enabled || !loaded || failed) && <span>本</span>}
            {enabled && !failed && <img src={`/__covers/works/${id}?v=${requestToken}`} alt="" className={loaded ? '' : 'cover-loading'}
                onLoad={() => setLoaded(true)} onError={() => setFailed(true)} />}
        </span>
        {failed && <span className="cover-error" role="status">画像を表示できません</span>}
    </span>;
}
