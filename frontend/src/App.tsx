import {useCallback, useEffect, useRef, useState} from 'react';
import type {FormEvent} from 'react';
import {CreateWork, ListWorks, OpenWork, LoadDisplaySettings, SetRegistrationFormVisible, LoadCoverWarnings} from '../wailsjs/go/main/App';
import type {WorkView} from './work-display';
import {WorkList} from './WorkList';
import {apiErrorMessage, isDatabaseStopped} from './work-display';
import {titleMatches} from './work-search';
import {useCoverActions} from './use-cover-actions';
import {CoverProgressNotice} from './CoverProgressNotice';
import type {CoverProgressTarget} from './CoverProgressNotice';
import {EditWorkDialog} from './EditWorkDialog';
import {DeleteWorkDialog} from './DeleteWorkDialog';
import {WorkNotices} from './WorkNotices';
import type {WorkNotice} from './WorkNotices';
import './App.css';

const emptyForm = {url: '', title: '', siteName: '', notes: ''};

function App() {
    const [form, setForm] = useState(emptyForm);
    const [works, setWorks] = useState<WorkView[] | null>(null);
    const [loading, setLoading] = useState(true);
    const [listError, setListError] = useState('');
    const [listStopped, setListStopped] = useState(false);
    const [saving, setSaving] = useState(false);
    const [saveError, setSaveError] = useState('');
    const [registeredTitle, setRegisteredTitle] = useState<string | null>(null);
    const [formVisible, setFormVisible] = useState(true);
    const [settingsReady, setSettingsReady] = useState(false);
    const [settingsSaving, setSettingsSaving] = useState(false);
    const [settingsWarning, setSettingsWarning] = useState('');
    const [coverWarnings, setCoverWarnings] = useState<{code: string; message: string}[]>([]);
    const [coversReady, setCoversReady] = useState(false);
    const [coverRevision, setCoverRevision] = useState(0);
    const [cleanupPending, setCleanupPending] = useState(false);
    const [editing, setEditing] = useState<WorkView | null>(null);
    const [editStopped, setEditStopped] = useState(false);
    const [editNotices, setEditNotices] = useState<Record<string, {work: WorkView; warnings: string[]}>>({});
    const editOrigin = useRef<HTMLButtonElement | null>(null);
    const searchElement = useRef<HTMLInputElement>(null);
    const opening = useRef(new Set<number>());
    const [accessStates, setAccessStates] = useState<Record<string, {work: WorkView; pending: boolean; error?: string}>>({});
    const editRevision = useRef(0);
    const savedEdits = useRef(new Map<number, {revision: number; work: WorkView}>());
    const [deleting, setDeleting] = useState<WorkView | null>(null);
    const [deleteStopped, setDeleteStopped] = useState(false);
    const [deleteNotices, setDeleteNotices] = useState<Record<string, {work: WorkView; missing: boolean; warnings: string[]}>>({});
    const deleteOrigin = useRef<HTMLButtonElement | null>(null);
    const modal = useRef(false);
    // SQLite can reuse IDs. Tombstones identify the deleted registration, not
    // every future work that receives the same numeric ID.
    const removedWorks = useRef(new Set<string>());

    useEffect(() => {
        let active = true;
        // A second frame leaves an initial paint opportunity before cleanup.
        // Backend sync.Once also joins requests after remounts or reloads.
        let frame = requestAnimationFrame(() => {
            frame = requestAnimationFrame(() => {
                setCleanupPending(true);
                void LoadCoverWarnings().then((warnings) => {
                    if (active) { setCoverWarnings(warnings); setCoversReady(true); }
                }).catch(() => {
                    if (active) setCoverWarnings([{code: 'cover_cleanup_unavailable', message: '画像整理の結果を取得できませんでした。アプリを再起動して状態を確認してください。'}]);
                }).finally(() => { if (active) setCleanupPending(false); });
            });
        });
        return () => { active = false; cancelAnimationFrame(frame); };
    }, []);
    const visibleRef = useRef(true);
    const settingsRequest = useRef(0);
    const settingsRevision = useRef(0);
    const settingsQueue = useRef<Promise<void>>(Promise.resolve());
    const [searchInput, setSearchInput] = useState('');
    const [searchQuery, setSearchQuery] = useState('');
    const composingSearch = useRef(false);
    const focusAfterToggle = useRef(false);
    const urlInput = useRef<HTMLInputElement>(null);
    const toggleButton = useRef<HTMLButtonElement>(null);
    const submitting = useRef(false);
    const request = useRef(0);
    const accessRevision = useRef(0);
    const savedAccess = useRef(new Map<number, {revision: number; value: WorkView['lastAccessedAt']; createdAt: string}>());
    const coverChangeRevision = useRef(0);
    const savedCovers = useRef(new Map<number, {revision: number; work: WorkView}>());
    const coverActions = useCoverActions(coversReady, (updated) => {
        savedCovers.current.set(updated.id, {revision: ++coverChangeRevision.current, work: updated});
        setWorks((current) => current?.map((work) => identity(work) === identity(updated)
            ? {...work, thumbnailPath: updated.thumbnailPath, updatedAt: later(work.updatedAt, updated.updatedAt)} : work) ?? null);
        setCoverRevision((value) => value + 1);
    }, (id) => modal.current || opening.current.has(id));

    const loadWorks = useCallback(async (throwOnError = false) => {
        const current = ++request.current;
        const revision = accessRevision.current;
        const coverStart = coverChangeRevision.current;
        const editStart = editRevision.current;
        setLoading(true);
        setListError('');
        setListStopped(false);
        try {
            const result = await ListWorks();
            if (current === request.current) {
                setCoverRevision((value) => value + 1);
                const values = result.filter((work) => !removedWorks.current.has(identity(work))).map((work) => {
                    const saved = savedAccess.current.get(work.id);
                    const cover = savedCovers.current.get(work.id);
                    const edit = savedEdits.current.get(work.id);
                    const edited = edit && edit.work.createdAt === work.createdAt && edit.revision > editStart ? edit.work : null;
                    return {...work,
                        ...(saved && saved.createdAt === work.createdAt && saved.revision > revision ? {lastAccessedAt: saved.value} : {}),
                        ...(cover && cover.work.createdAt === work.createdAt && cover.revision > coverStart ? {thumbnailPath: cover.work.thumbnailPath} : {}),
                        ...(edited ? {url: edited.url, title: edited.title, siteName: edited.siteName, notes: edited.notes} : {}),
                        updatedAt: later(work.updatedAt, later(cover && cover.work.createdAt === work.createdAt && cover.revision > coverStart ? cover.work.updatedAt : work.updatedAt, edited?.updatedAt ?? work.updatedAt))};
                });
                setWorks(values);
                return values;
            }
            if (throwOnError) throw new Error('Superseded confirmation read');
        } catch (error: unknown) {
            if (current === request.current) {
                const stopped = isDatabaseStopped(error);
                setListError(apiErrorMessage(error, stopped ? 'データの読み書きを停止しています。アプリを再起動して状態を確認してください。' : '作品一覧を取得できません。再読み込みをお試しください。'));
                setListStopped(stopped);
            }
            if (throwOnError) throw error;
        } finally {
            if (current === request.current) setLoading(false);
        }
    }, []);

    useEffect(() => {
        void loadWorks();
        // Ignore results from the previous mount, including StrictMode's first effect.
        return () => { ++request.current; };
    }, [loadWorks]);

    useEffect(() => {
        const current = ++settingsRequest.current;
        void (async () => {
            try {
                const value = await LoadDisplaySettings();
                if (current !== settingsRequest.current) return;
                visibleRef.current = value.registrationFormVisible;
                setFormVisible(value.registrationFormVisible);
            } catch (error: unknown) {
                if (current !== settingsRequest.current) return;
                visibleRef.current = true;
                setFormVisible(true);
                setSettingsWarning(apiErrorMessage(error, '表示設定を読み込めませんでした。登録フォームを表示して続行します。'));
            } finally {
                if (current === settingsRequest.current) setSettingsReady(true);
            }
        })();
        return () => { ++settingsRequest.current; };
    }, []);

    useEffect(() => {
        if (!focusAfterToggle.current) return;
        focusAfterToggle.current = false;
        if (formVisible) urlInput.current?.focus();
        else toggleButton.current?.focus();
    }, [formVisible]);

    function toggleForm() {
        if (submitting.current || !settingsReady) return;
        const visible = !visibleRef.current;
        visibleRef.current = visible;
        focusAfterToggle.current = true;
        setFormVisible(visible);
        setSettingsSaving(true);
        const current = settingsRequest.current;
        const revision = ++settingsRevision.current;
        // Serialize explicit actions; never save defaults just because the component mounted.
        settingsQueue.current = settingsQueue.current.then(async () => {
            try {
                await SetRegistrationFormVisible(visible);
                if (current === settingsRequest.current && revision === settingsRevision.current) setSettingsWarning('');
            } catch (error: unknown) {
                if (current === settingsRequest.current && revision === settingsRevision.current) {
                    setSettingsWarning(apiErrorMessage(error, '表示設定を保存できませんでした。再起動すると以前の状態に戻る場合があります。'));
                }
            } finally {
                if (current === settingsRequest.current && revision === settingsRevision.current) setSettingsSaving(false);
            }
        });
    }

    async function register(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        // Close the gap before React renders the disabled submit button.
        if (modal.current || submitting.current || !settingsReady || !formVisible) return;
        submitting.current = true;
        setSaving(true);
        setSaveError('');
        setRegisteredTitle(null);
        try {
            const created = await CreateWork({...form, thumbnailPath: ''});
            setForm(emptyForm);
            setRegisteredTitle(created.title);
            // A list failure must not invite a second insert after a successful save.
            await loadWorks();
        } catch (error: unknown) {
            setSaveError(apiErrorMessage(error, '作品を登録できません。入力内容を確認して再度お試しください。'));
        } finally {
            submitting.current = false;
            setSaving(false);
        }
    }

    async function openWork(id: number) {
        if (modal.current || opening.current.has(id) || coverActions.isPending(id)) return;
        const work = works?.find((value) => value.id === id);
        if (!work) return;
        const key = identity(work);
        opening.current.add(id);
        setAccessStates((current) => ({...current, [key]: {work, pending: true}}));
        try {
        const updated = await OpenWork(id);
        savedAccess.current.set(id, {revision: ++accessRevision.current, value: updated.lastAccessedAt, createdAt: updated.createdAt});
        // OpenWork owns lastAccessedAt, never an image snapshot from an older operation.
        setWorks((current) => current?.map((work) => identity(work) === identity(updated) ? {...work, lastAccessedAt: updated.lastAccessedAt} : work) ?? null);
        setAccessStates((current) => { const next = {...current}; delete next[key]; return next; });
        } catch (cause: unknown) {
            setAccessStates((current) => ({...current, [key]: {work, pending: false,
                error: apiErrorMessage(cause, '作品を開く処理に失敗しました。再度お試しください。')}}));
        } finally { opening.current.delete(id); }
    }

    function clearResult(kind: 'edit' | 'delete', work: WorkView) {
        // Both maps delete an entry without rewriting any other operation.
        if (kind === 'edit') setEditNotices((current) => {const next = {...current}; delete next[identity(work)]; return next;});
        else setDeleteNotices((current) => {const next = {...current}; delete next[identity(work)]; return next;});
    }
    function dismissNotice(notice: WorkNotice) {
        const key = identity(notice.work);
        if (notice.kind === 'access') setAccessStates((current) => {const next = {...current}; delete next[key]; return next;});
        else if (notice.kind === 'cover') coverActions.dismiss(notice.work);
        else clearResult(notice.kind, notice.work);
        const target = document.getElementById(`work-actions-${key}`);
        if (target instanceof HTMLButtonElement && !target.disabled) target.focus();
        else searchElement.current?.focus();
    }

    function startEdit(work: WorkView, button: HTMLButtonElement) {
        if (modal.current || editStopped || deleteStopped || opening.current.has(work.id) || coverActions.isPending(work.id)) return;
        modal.current = true;
        editOrigin.current = button;
        setEditing({...work});
    }
    function closeEdit() {
        modal.current = false;
        setEditing(null);
        requestAnimationFrame(() => {
            const button = editOrigin.current;
            // A notice may have already moved focus to another work or search.
            if (document.activeElement !== document.body && document.activeElement !== button) return;
            if (button?.isConnected && !button.disabled) button.focus();
            else searchElement.current?.focus();
        });
    }
    function edited(work: WorkView, warnings: string[]) {
        savedEdits.current.set(work.id, {revision: ++editRevision.current, work});
        setWorks((current) => current?.map((value) => identity(value) === identity(work) ? {...value,
            url: work.url, title: work.title, siteName: work.siteName, notes: work.notes,
            updatedAt: later(value.updatedAt, work.updatedAt)} : value) ?? null);
        setEditNotices((current) => ({...current, [identity(work)]: {work: {...work}, warnings}}));
    }
    function startDelete(work: WorkView, button: HTMLButtonElement) {
        if (modal.current || deleteStopped || editStopped || opening.current.has(work.id) || coverActions.isPending(work.id)) return;
        modal.current = true; deleteOrigin.current = button; setDeleting({...work});
    }
    function closeDelete() {
        modal.current = false; setDeleting(null);
        requestAnimationFrame(() => {
            const button = deleteOrigin.current;
            // Do not steal focus if the user already moved to another control.
            if (document.activeElement !== document.body && document.activeElement !== button) return;
            if (button?.isConnected && !button.disabled) button.focus();
            else searchElement.current?.focus();
        });
    }
    function removed(work: WorkView, missing: boolean, warnings: string[]) {
        removedWorks.current.add(identity(work));
        savedAccess.current.delete(work.id); savedCovers.current.delete(work.id); savedEdits.current.delete(work.id);
        setWorks((current) => current?.filter((value) => identity(value) !== identity(work)) ?? null);
        setDeleteNotices((current) => ({...current, [identity(work)]: {work: {...work}, missing, warnings}}));
    }

    const visibleWorks = works?.filter((work) => titleMatches(work.title, searchQuery));
    const progressTargets: CoverProgressTarget[] = Object.values(coverActions.states).flatMap((state) =>
        state.pending && state.operationID && state.kind ? [{operationID: state.operationID, workID: state.work.id, kind: state.kind, title: state.work.title}] : []);
    if (cleanupPending) progressTargets.push({operationID: 'startup-cleanup', workID: 0, kind: 'cleanup'});
    const success = registeredTitle === null ? '' : titleMatches(registeredTitle, searchQuery)
        ? '作品を登録しました。' : '作品を登録しました。検索条件に一致しないため非表示です。';
    const notices: WorkNotice[] = [
        ...Object.entries(accessStates).map(([key, state]): WorkNotice => ({key: `access:${key}`, work: state.work, kind: 'access', pending: state.pending, error: state.error})),
        ...Object.entries(coverActions.states).filter(([, state]) => state.pending || state.notice).map(([key, state]): WorkNotice =>
            ({key: `cover:${key}`, work: state.work, kind: 'cover', pending: state.pending, ...state.notice})),
        ...Object.entries(editNotices).map(([key, state]): WorkNotice => ({key: `edit:${key}`, work: state.work, kind: 'edit', warnings: state.warnings,
            success: `「${state.work.title}」を保存しました。${!titleMatches(state.work.title, searchQuery) ? '検索条件に一致しないため非表示です。' : ''}`})),
        ...Object.entries(deleteNotices).map(([key, state]): WorkNotice => ({key: `delete:${key}`, work: state.work, kind: 'delete', warnings: state.warnings,
            success: state.missing ? `「${state.work.title}」は既に存在しません。` : `「${state.work.title}」を削除しました。`})),
    ];

    return (
        <main className="app-shell">
            <header className="app-header">
                <div>
                    <p className="eyebrow">Manga Update Manager</p>
                    <h1>作品を管理</h1>
                    <p className="description">漫画を登録して、自分の一覧にまとめましょう。</p>
                </div>
                <div className="header-actions">
                    <span className="local-badge">端末内に保存</span>
                    <button ref={toggleButton} type="button" className="secondary-button" disabled={saving || !settingsReady}
                        aria-expanded={settingsReady && formVisible} aria-controls="registration-panel" onClick={toggleForm}>
                        {!settingsReady ? '表示設定を読み込み中…' : formVisible ? '登録フォームを閉じる' : '作品を登録'}
                    </button>
                </div>
            </header>
            {deleteStopped && <p className="message error-message" role="alert">削除結果またはDBの安全性を確認できないため、追加の編集・削除を停止しています。アプリを再起動して状態を確認してください。</p>}
            {deleting && <DeleteWorkDialog key={identity(deleting)} work={deleting} onReload={async () => (await loadWorks(true))?.find((work) => work.id === deleting.id)}
                onRemoved={removed} onClose={closeDelete} onStopped={() => { setDeleteStopped(true); setEditStopped(true); }} onImagesStopped={() => setCoversReady(false)} onStart={(work) => clearResult('delete', work)} />}
            {editStopped && <p className="message error-message" role="alert">編集の保存結果またはDBの安全性を確認できないため、追加の編集を停止しています。アプリを再起動して状態を確認してください。</p>}
            {editing && <EditWorkDialog key={editing.id} work={editing} onSaved={edited} onClose={closeEdit} onStopped={() => setEditStopped(true)} onStart={(work) => clearResult('edit', work)} />}
            {coverWarnings.length > 0 && <section className="message error-message" role="alert" aria-label="画像整理のお知らせ">
                <h2>画像整理のお知らせ</h2>
                {coverWarnings.map((warning) => <p key={warning.code}>{warning.message}</p>)}
                <button type="button" className="secondary-button" onClick={() => setCoverWarnings([])}>画像整理のお知らせを閉じる</button>
            </section>}
            {!settingsReady && <p role="status">表示設定を読み込んでいます…</p>}
            {settingsSaving && <p role="status">表示設定を保存しています…</p>}
            {settingsWarning && <p className="message error-message" role="alert">{settingsWarning}</p>}
            <div className={`workspace${settingsReady && formVisible ? '' : ' registration-hidden'}`}>
                <section id="registration-panel" className="registration-panel" hidden={!settingsReady || !formVisible} aria-labelledby="registration-heading">
                    <h2 id="registration-heading">作品を登録</h2>
                    <p className="section-description">URLとタイトルは必須です。</p>
                    <form onSubmit={register} aria-busy={saving} onKeyDown={(event) => {
                        if (event.key === 'Enter' && (event.nativeEvent.isComposing || event.keyCode === 229)) event.preventDefault();
                    }}>
                        <fieldset disabled={saving}>
                            <label htmlFor="work-url">作品URL <span className="required">必須</span></label>
                            <input ref={urlInput} id="work-url" type="url" required value={form.url}
                                placeholder="https://example.com/manga/1"
                                onChange={(event) => setForm({...form, url: event.target.value})} />
                            <label htmlFor="work-title">タイトル <span className="required">必須</span></label>
                            <input id="work-title" required value={form.title}
                                onChange={(event) => setForm({...form, title: event.target.value})} />
                            <label htmlFor="work-site">サイト名 <span className="optional">任意</span></label>
                            <input id="work-site" value={form.siteName}
                                onChange={(event) => setForm({...form, siteName: event.target.value})} />
                            <label htmlFor="work-notes">メモ <span className="optional">任意</span></label>
                            <textarea id="work-notes" rows={4} value={form.notes}
                                onChange={(event) => setForm({...form, notes: event.target.value})} />
                            <button className="primary-button" type="submit">{saving ? '登録中…' : '登録する'}</button>
                        </fieldset>
                        {saveError && <p className="message error-message" role="alert">{saveError}</p>}
                        <p className="success-message" role="status">{success}</p>
                    </form>
                </section>
                <section className="list-panel" aria-labelledby="list-heading">
                    <div className="list-heading">
                        <div>
                            <h2 id="list-heading">登録した作品{works !== null && <span className="work-count">{visibleWorks?.length}件 / 全{works.length}件</span>}</h2>
                            <p className="section-description">登録順に表示しています。</p>
                        </div>
                        <button className="secondary-button" disabled={loading || saving}
                            onClick={() => { void loadWorks(); }}>再読み込み</button>
                    </div>
                    <div className="search-controls">
                        <label htmlFor="title-search">漫画名で検索</label>
                        <input ref={searchElement} id="title-search" type="search" value={searchInput}
                            onCompositionStart={() => { composingSearch.current = true; }}
                            onCompositionEnd={(event) => {
                                composingSearch.current = false;
                                setSearchInput(event.currentTarget.value);
                                setSearchQuery(event.currentTarget.value);
                            }}
                            onChange={(event) => {
                                setSearchInput(event.target.value);
                                if (!composingSearch.current) setSearchQuery(event.target.value);
                            }} />
                    </div>
                    <CoverProgressNotice targets={progressTargets} works={works} />
                    <WorkNotices notices={notices} onDismiss={dismissNotice} />
                    {loading && <p className="loading-message" role="status">一覧を読み込んでいます…</p>}
                    {listError && <div className="message error-message" role="alert">
                        <p>{listError}</p>
                        {!listStopped && <p>一覧を再読み込みしてください。登録した作品は再登録する必要はありません。</p>}
                    </div>}
                    <div aria-busy={loading}>
                        {works !== null && visibleWorks !== undefined && <>
                            {works.length > 0 && visibleWorks.length === 0 && !loading && !listError &&
                                <div className="empty-state" role="status">
                                    <h3>検索条件に一致する作品がありません</h3>
                                    <p>検索文字を変更するか、空欄にして全作品を表示してください。</p>
                                </div>}
                            <WorkList works={visibleWorks} showEmpty={works.length === 0 && !loading && !listError} onOpen={openWork} isOpening={(work) => accessStates[identity(work)]?.pending ?? false}
                                coversReady={coversReady} coverRevision={coverRevision} coverActions={coverActions} editing={{onEdit: startEdit, stopped: editStopped || deleteStopped}}
                                deleting={{onDelete: startDelete, stopped: editStopped || deleteStopped}} />
                        </>}
                    </div>
                </section>
            </div>
        </main>
    );
}

export default App;

function later(a: string, b: string): string { return Date.parse(a) >= Date.parse(b) ? a : b; }
function identity(work: WorkView): string { return JSON.stringify([work.id, work.createdAt]); }
