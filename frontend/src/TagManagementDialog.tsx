import {useEffect, useLayoutEffect, useRef, useState} from 'react';
import type {FormEvent} from 'react';
import {LoadTagUsage, RenameTag} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';
import {apiErrorCode, apiErrorMessage, isDatabaseStopped} from './work-display';
import {titleMatches} from './work-search';
import {readRenamedTag, readTagUsage, sortTagUsage} from './tag-management';
import type {TagUsage} from './tag-management';
import {tagName, validTagName} from './tag-edit';
import type {Tag} from './tag-edit';
import {TagManagementNotices} from './TagManagementNotices';
import type {TagNotice} from './TagManagementNotices';

export function TagManagementDialog({stopped, onStopped, onClose, notices = [], onNotice = () => {}}: {
    stopped: boolean; onStopped: () => void; onClose: () => void;
    notices?: TagNotice[]; onNotice?: (notice: TagNotice) => void;
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
    const input = useRef<HTMLInputElement>(null);
    const closeButton = useRef<HTMLButtonElement>(null);
    const returnButton = useRef<HTMLButtonElement>(null);
    const [edit, setEdit] = useState<{expected: Tag; original: string; name: string} | null>(null);
    const [writing, setWriting] = useState(false);
    const saving = useRef(false);
    const [discard, setDiscard] = useState(false);
    const focus = useRef<string | null>(null);

    function stopWrites() { stop.current = true; setBlocked(true); setFresh(false); onStopped(); }
    function close() {
        if (saving.current) return;
        if (edit && edit.name !== edit.original) setDiscard(true);
        else onClose();
    }
    function cancelEdit() {
        if (saving.current) return;
        focus.current = edit?.expected.id ?? 'search'; setEdit(null); setError('');
    }

    async function reload(afterConflict = false) {
        if (pending.current || stop.current || (saving.current && !afterConflict)) return;
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
                stopWrites();
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

    useLayoutEffect(() => {
        if (discard) returnButton.current?.focus();
    }, [discard]);
    useLayoutEffect(() => {
        if (discard || writing || !focus.current) return;
        const target = focus.current; focus.current = null;
        if (stop.current) closeButton.current?.focus();
        else if (target === 'input') input.current?.focus();
        else {
            const button = document.getElementById(`rename-tag-${target}`);
            if (button instanceof HTMLButtonElement && !button.disabled) button.focus();
            else search.current?.focus();
        }
    });

    const currentTag = usage?.find(({tag}) => tag.id === edit?.expected.id)?.tag;
    const conflict = !!edit && !!currentTag && currentTag.name !== edit.expected.name;
    const missing = !!edit && fresh && !currentTag;
    const canSave = !!edit && fresh && !reading && !writing && !blocked && !conflict && !missing && validTagName(tagName(edit.name));
    async function save(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        if (saving.current || pending.current || stop.current || !canSave || !edit) return;
        const submitted = edit;
        saving.current = true; setWriting(true); setError('');
        try {
            const result = await RenameTag(main.TagDTO.createFrom(submitted.expected), submitted.name);
            if (!active.current) return;
            const renamed = readRenamedTag(result, submitted.expected.id, tagName(submitted.name));
            // Reflect the confirmed mutation directly; no post-commit read can revoke it.
            setUsage(sortTagUsage(usage!.map((row) => row.tag.id === renamed.tag.id ? {...row, tag: renamed.tag} : row)));
            if (usage!.some((row) => row.tag.id !== renamed.tag.id && row.tag.name === renamed.tag.name)) setFresh(false);
            onNotice({id: renamed.tag.id, message: `タグ「${submitted.expected.name}」を「${renamed.tag.name}」に変更しました。`, warning: renamed.warning});
            setEdit(null); focus.current = renamed.tag.id;
            if (renamed.warning) stopWrites();
        } catch (cause: unknown) {
            if (!active.current) return;
            const code = apiErrorCode(cause);
            if (!code || isDatabaseStopped(cause) || code === 'database_unavailable') {
                stopWrites();
                const message = apiErrorMessage(cause, 'タグの変更結果を確認できません。操作を繰り返さず、入力を控えてアプリを再起動し、状態を確認してください。');
                setError(message); onNotice({id: submitted.expected.id, message, error: true});
            } else {
                setError(apiErrorMessage(cause, '名前を変更できません。入力を控えて状態を確認してください。'));
                if (code === 'tag_changed' || code === 'tag_not_found') await reload(true);
            }
        } finally {
            saving.current = false;
            if (active.current) setWriting(false);
        }
    }

    const visible: {tag: Tag; workCount: number | null}[] | undefined = usage?.filter(({tag}) => tag.id === edit?.expected.id || titleMatches(tag.name, query));
    if (edit && usage && !currentTag) visible?.unshift({tag: edit.expected, workCount: null});
    return <dialog ref={dialog} className="edit-dialog tag-management-dialog" aria-labelledby="tag-management-heading"
        onCancel={(event) => { event.preventDefault(); if (discard) {setDiscard(false); focus.current = 'input';} else close(); }}>
        <header className="tag-management-header">
            <h2 id="tag-management-heading">タグ管理</h2>
            <button type="button" className="secondary-button" aria-label="タグ管理を閉じる" disabled={writing} onClick={close}>×</button>
        </header>
        {discard && <div role="group" aria-labelledby="tag-discard-heading" className="tag-discard">
            <p id="tag-discard-heading">変更を破棄して閉じますか？</p>
            <button ref={returnButton} type="button" className="secondary-button" onClick={() => {setDiscard(false); focus.current = 'input';}}>編集に戻る</button>
            <button type="button" className="danger-button" onClick={onClose}>変更を破棄して閉じる</button>
        </div>}
        <div className="tag-management-search" hidden={discard}>
            <label htmlFor="tag-management-search">タグ名で検索</label>
            <input ref={search} id="tag-management-search" type="search" value={query} disabled={edit !== null || writing} onChange={(event) => setQuery(event.target.value)} />
            {usage !== null && <p role="status">{visible?.filter((row) => row.workCount !== null).length}件 / 全{usage.length}件</p>}
        </div>
        <div className="tag-management-feedback">
            {reading && <p role="status">タグ情報を読み込んでいます…</p>}
            {!fresh && usage !== null && <p>前回の表示です。最新情報は未確認です。</p>}
            {blocked && <p className="message error-message" role="alert">データの読み書きを停止しています。アプリを再起動して状態を確認してください。</p>}
            {error && <p className="message error-message" role="alert">{error}</p>}
            {writing && <p role="status">タグ名を保存しています。結果が表示されるまでお待ちください。</p>}
            <TagManagementNotices notices={notices} />
        </div>
        <div className="tag-management-list" hidden={discard} aria-busy={reading || writing} tabIndex={0} role="region" aria-label="タグと使用作品数">
            {usage !== null && (usage.length === 0 && !edit ? <p>タグはまだありません。</p>
                : visible?.length === 0 ? <p>検索条件に一致するタグがありません。</p>
                : <table><thead><tr><th scope="col">タグ名</th><th scope="col">使用作品数</th><th scope="col">操作</th></tr></thead>
                    <tbody>{visible?.map(({tag, workCount}) => <tr key={tag.id}><th scope="row">
                        {edit?.expected.id === tag.id ? <form id="tag-rename-form" onSubmit={(event) => {void save(event);}}
                            onKeyDown={(event) => {if (event.key === 'Enter' && (event.nativeEvent.isComposing || event.keyCode === 229)) event.preventDefault();}}>
                            <label htmlFor="tag-rename-name">新しいタグ名</label>
                            <input ref={input} id="tag-rename-name" value={edit.name} readOnly={writing || blocked} onChange={(event) => setEdit({...edit, name: event.target.value})} />
                            <p className="section-description">前後の空白を除いて1〜50文字。改行・制御文字は使えません。</p>
                            {conflict && <div className="tag-conflict"><p>確認時の名前「{edit.expected.name}」→ 現在の名前「{currentTag!.name}」</p>
                                <button type="button" className="secondary-button" disabled={!fresh || reading || writing || blocked} onClick={() => {setEdit({...edit, expected: {...currentTag!}}); focus.current = 'input';}}>現在の名前を確認して編集を続ける</button></div>}
                            {missing && <p>このタグは存在しません。入力を控えてキャンセルしてください。</p>}
                        </form> : tag.name}</th><td>{workCount === null ? '未確認' : `${workCount}作品`}</td><td>
                        {edit?.expected.id === tag.id ? <div className="tag-management-actions">
                            <button type="submit" form="tag-rename-form" className="primary-button" disabled={!canSave}>保存</button>
                            <button type="button" className="secondary-button" disabled={writing} onClick={cancelEdit}>キャンセル</button>
                        </div> : <button id={`rename-tag-${tag.id}`} type="button" className="secondary-button" aria-label={`「${tag.name}」の名前変更`}
                            disabled={!fresh || reading || writing || blocked || edit !== null} onClick={() => {setEdit({expected: {...tag}, original: tag.name, name: tag.name}); setError(''); focus.current = 'input';}}>名前変更</button>}
                    </td></tr>)}</tbody></table>)}
        </div>
        <footer className="tag-management-footer" hidden={discard}>
            <p className="section-description">使用数は全作品への付与件数です。</p>
            <div><button type="button" className="secondary-button" disabled={reading || blocked || writing} onClick={() => { void reload(); }}>再読み込み</button>
                <button ref={closeButton} type="button" className="secondary-button" disabled={writing} onClick={close}>閉じる</button></div>
        </footer>
    </dialog>;
}
