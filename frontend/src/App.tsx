import {useCallback, useEffect, useRef, useState} from 'react';
import type {FormEvent} from 'react';
import {CreateWork, ListWorks, OpenWork, LoadDisplaySettings, SetRegistrationFormVisible} from '../wailsjs/go/main/App';
import type {WorkView} from './work-display';
import {WorkList} from './WorkList';
import {apiErrorMessage} from './work-display';
import {titleMatches} from './work-search';
import './App.css';

const emptyForm = {url: '', title: '', siteName: '', notes: ''};

function App() {
    const [form, setForm] = useState(emptyForm);
    const [works, setWorks] = useState<WorkView[] | null>(null);
    const [loading, setLoading] = useState(true);
    const [listError, setListError] = useState('');
    const [saving, setSaving] = useState(false);
    const [saveError, setSaveError] = useState('');
    const [registeredTitle, setRegisteredTitle] = useState<string | null>(null);
    const [formVisible, setFormVisible] = useState(true);
    const [settingsReady, setSettingsReady] = useState(false);
    const [settingsSaving, setSettingsSaving] = useState(false);
    const [settingsWarning, setSettingsWarning] = useState('');
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
    const savedAccess = useRef(new Map<number, {revision: number; value: WorkView['lastAccessedAt']}>());

    const loadWorks = useCallback(async () => {
        const current = ++request.current;
        const revision = accessRevision.current;
        setLoading(true);
        setListError('');
        try {
            const result = await ListWorks();
            if (current === request.current) setWorks(result.map((work) => {
                const saved = savedAccess.current.get(work.id);
                return saved && saved.revision > revision ? {...work, lastAccessedAt: saved.value} : work;
            }));
        } catch (error: unknown) {
            if (current === request.current) {
                setListError(apiErrorMessage(error, '作品一覧を取得できません。再読み込みをお試しください。'));
            }
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
        if (submitting.current || !settingsReady || !formVisible) return;
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
        const updated = await OpenWork(id);
        savedAccess.current.set(id, {revision: ++accessRevision.current, value: updated.lastAccessedAt});
        setWorks((current) => current?.map((work) => work.id === id ? updated : work) ?? null);
    }

    const visibleWorks = works?.filter((work) => titleMatches(work.title, searchQuery));
    const success = registeredTitle === null ? '' : titleMatches(registeredTitle, searchQuery)
        ? '作品を登録しました。' : '作品を登録しました。検索条件に一致しないため非表示です。';

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
                        <input id="title-search" type="search" value={searchInput}
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
                    {loading && <p className="loading-message" role="status">一覧を読み込んでいます…</p>}
                    {listError && <div className="message error-message" role="alert">
                        <p>{listError}</p>
                        <p>一覧を再読み込みしてください。登録した作品は再登録する必要はありません。</p>
                    </div>}
                    <div aria-busy={loading}>
                        {works !== null && visibleWorks !== undefined && <>
                            {works.length > 0 && visibleWorks.length === 0 && !loading && !listError &&
                                <div className="empty-state" role="status">
                                    <h3>検索条件に一致する作品がありません</h3>
                                    <p>検索文字を変更するか、空欄にして全作品を表示してください。</p>
                                </div>}
                            <WorkList works={visibleWorks} showEmpty={works.length === 0 && !loading && !listError} onOpen={openWork} />
                        </>}
                    </div>
                </section>
            </div>
        </main>
    );
}

export default App;
