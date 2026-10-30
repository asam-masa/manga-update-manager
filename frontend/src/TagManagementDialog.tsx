import {useEffect, useRef, useState} from 'react';
import {LoadTagUsage} from '../wailsjs/go/main/App';
import {apiErrorCode, apiErrorMessage, isDatabaseStopped} from './work-display';
import {titleMatches} from './work-search';
import {readTagUsage} from './tag-management';
import type {TagUsage} from './tag-management';

export function TagManagementDialog({stopped, onStopped, onClose}: {
    stopped: boolean; onStopped: () => void; onClose: () => void;
}) {
    const [usage, setUsage] = useState<TagUsage[] | null>(null);
    const [query, setQuery] = useState('');
    const [reading, setReading] = useState(!stopped);
    const [fresh, setFresh] = useState(false);
    const [error, setError] = useState('');
    const [blocked, setBlocked] = useState(stopped);
    const stop = useRef(stopped);
    const pending = useRef(false);
    const active = useRef(false);
    const generation = useRef(0);
    const dialog = useRef<HTMLDialogElement>(null);
    const search = useRef<HTMLInputElement>(null);

    async function reload() {
        if (pending.current || stop.current) return;
        pending.current = true;
        const request = ++generation.current;
        const current = () => active.current && generation.current === request;
        setReading(true); setFresh(false); setError('');
        try {
            const result = await LoadTagUsage();
            if (!current() || stop.current) return;
            const values = readTagUsage(result);
            setUsage(values); setFresh(true);
        } catch (cause: unknown) {
            if (!current() || stop.current) return;
            if (isDatabaseStopped(cause) || apiErrorCode(cause) === 'database_unavailable') {
                stop.current = true; setBlocked(true); onStopped();
            } else {
                setError(apiErrorMessage(cause, 'タグ情報を取得できません。再読み込みをお試しください。'));
            }
        } finally {
            if (current()) { pending.current = false; setReading(false); }
        }
    }

    useEffect(() => {
        active.current = true;
        const node = dialog.current!;
        node.showModal(); search.current?.focus({preventScroll: true});
        void reload();
        return () => { active.current = false; ++generation.current; pending.current = false; node.close(); };
    }, []);
    useEffect(() => {
        if (stopped) {
            stop.current = true; setBlocked(true); setFresh(false); setReading(false);
            ++generation.current;
        }
    }, [stopped]);

    const visible = usage?.filter(({tag}) => titleMatches(tag.name, query));
    return <dialog ref={dialog} className="edit-dialog tag-management-dialog" aria-labelledby="tag-management-heading"
        onCancel={(event) => { event.preventDefault(); onClose(); }}>
        <header className="tag-management-header">
            <h2 id="tag-management-heading">タグ管理</h2>
            <button type="button" className="secondary-button" aria-label="タグ管理を閉じる" onClick={onClose}>×</button>
        </header>
        <div className="tag-management-search">
            <label htmlFor="tag-management-search">タグ名で検索</label>
            <input ref={search} id="tag-management-search" type="search" value={query} onChange={(event) => setQuery(event.target.value)} />
            {usage !== null && <p role="status">{visible?.length}件 / 全{usage.length}件</p>}
        </div>
        <div className="tag-management-feedback">
            {reading && <p role="status">タグ情報を読み込んでいます…</p>}
            {!fresh && usage !== null && <p>前回の表示です。最新情報は未確認です。</p>}
            {blocked && <p className="message error-message" role="alert">データの読み書きを停止しています。アプリを再起動して状態を確認してください。</p>}
            {error && <p className="message error-message" role="alert">{error}</p>}
        </div>
        <div className="tag-management-list" aria-busy={reading} tabIndex={0} role="region" aria-label="タグと使用作品数">
            {usage !== null && (usage.length === 0 ? <p>タグはまだありません。</p>
                : visible?.length === 0 ? <p>検索条件に一致するタグがありません。</p>
                : <table><thead><tr><th scope="col">タグ名</th><th scope="col">使用作品数</th></tr></thead>
                    <tbody>{visible?.map(({tag, workCount}) => <tr key={tag.id}><th scope="row">{tag.name}</th><td>{workCount}作品</td></tr>)}</tbody></table>)}
        </div>
        <footer className="tag-management-footer">
            <p className="section-description">使用数は全作品への付与件数です。</p>
            <div><button type="button" className="secondary-button" disabled={reading || blocked} onClick={() => { void reload(); }}>再読み込み</button>
                <button type="button" className="secondary-button" onClick={onClose}>閉じる</button></div>
        </footer>
    </dialog>;
}
