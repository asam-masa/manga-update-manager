import {useCallback, useEffect, useRef, useState} from 'react';
import type {FormEvent} from 'react';
import {CreateWork, ListWorks} from '../wailsjs/go/main/App';
import type {main} from '../wailsjs/go/models';
import {WorkList} from './WorkList';
import {apiErrorMessage} from './work-display';
import './App.css';

const emptyForm = {url: '', title: '', siteName: '', notes: ''};

function App() {
    const [form, setForm] = useState(emptyForm);
    const [works, setWorks] = useState<main.WorkDTO[] | null>(null);
    const [loading, setLoading] = useState(true);
    const [listError, setListError] = useState('');
    const [saving, setSaving] = useState(false);
    const [saveError, setSaveError] = useState('');
    const [success, setSuccess] = useState('');
    const submitting = useRef(false);
    const request = useRef(0);

    const loadWorks = useCallback(async () => {
        const current = ++request.current;
        setLoading(true);
        setListError('');
        try {
            const result = await ListWorks();
            if (current === request.current) setWorks(result);
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

    async function register(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        // Close the gap before React renders the disabled submit button.
        if (submitting.current) return;
        submitting.current = true;
        setSaving(true);
        setSaveError('');
        setSuccess('');
        try {
            await CreateWork({...form, thumbnailPath: ''});
            setForm(emptyForm);
            setSuccess('作品を登録しました。');
            // A list failure must not invite a second insert after a successful save.
            await loadWorks();
        } catch (error: unknown) {
            setSaveError(apiErrorMessage(error, '作品を登録できません。入力内容を確認して再度お試しください。'));
        } finally {
            submitting.current = false;
            setSaving(false);
        }
    }

    return (
        <main className="app-shell">
            <header className="app-header">
                <div>
                    <p className="eyebrow">Manga Update Manager</p>
                    <h1>作品を管理</h1>
                    <p className="description">漫画を登録して、自分の一覧にまとめましょう。</p>
                </div>
                <span className="local-badge">端末内に保存</span>
            </header>
            <div className="workspace">
                <section className="registration-panel" aria-labelledby="registration-heading">
                    <h2 id="registration-heading">作品を登録</h2>
                    <p className="section-description">URLとタイトルは必須です。</p>
                    <form onSubmit={register} aria-busy={saving}>
                        <fieldset disabled={saving}>
                            <label htmlFor="work-url">作品URL <span className="required">必須</span></label>
                            <input id="work-url" type="url" required value={form.url}
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
                            <h2 id="list-heading">登録した作品{works !== null && <span className="work-count">{works.length}件</span>}</h2>
                            <p className="section-description">登録順に表示しています。</p>
                        </div>
                        <button className="secondary-button" disabled={loading || saving}
                            onClick={() => { void loadWorks(); }}>再読み込み</button>
                    </div>
                    {loading && <p className="loading-message" role="status">一覧を読み込んでいます…</p>}
                    {listError && <div className="message error-message" role="alert">
                        <p>{listError}</p>
                        <p>一覧を再読み込みしてください。登録した作品は再登録する必要はありません。</p>
                    </div>}
                    <div aria-busy={loading}>
                        {works !== null && <WorkList works={works} showEmpty={!loading && !listError} />}
                    </div>
                </section>
            </div>
        </main>
    );
}

export default App;
