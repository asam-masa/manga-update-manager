import {useRef, useState} from 'react';
import {apiErrorMessage, registrationDate} from './work-display';
import type {WorkView} from './work-display';

export function WorkList({works, showEmpty, onOpen}: {works: WorkView[]; showEmpty: boolean; onOpen: (id: number) => Promise<void>}) {
    if (works.length === 0) {
        return showEmpty ? <div className="empty-state">
            <div className="empty-book" aria-hidden="true">本</div>
            <h3>まだ作品が登録されていません</h3>
            <p>作品URLとタイトルを入力して、最初の作品を登録しましょう。</p>
        </div> : null;
    }
    return <ul className="work-list" aria-label="作品一覧">
        {works.map((work) => <WorkCard key={work.id} work={work} onOpen={onOpen} />)}
    </ul>;
}

function WorkCard({work, onOpen}: {work: WorkView; onOpen: (id: number) => Promise<void>}) {
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
            <div className="cover-placeholder" aria-hidden="true">本</div>
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
                {error && <p className="message error-message" role="alert">{error}</p>}
            </div>
        </li>;
}
