import {useEffect, useState} from 'react';
import './App.css';
import {Status} from '../wailsjs/go/main/App';

function App() {
    const [backendStatus, setBackendStatus] = useState('確認中');

    useEffect(() => {
        Status()
            .then((status) => setBackendStatus(status === 'ready' ? '接続済み' : '応答を確認できません'))
            .catch(() => setBackendStatus('接続できません'));
    }, []);

    return (
        <main className="app-shell">
            <section className="welcome-card">
                <p className="eyebrow">Manga Update Manager</p>
                <h1>漫画の更新状況を一画面で管理</h1>
                <p className="description">Wailsアプリの初期設定が完了しました。作品管理機能は次の開発工程で追加します。</p>
                <dl className="status-list">
                    <div>
                        <dt>Goバックエンド</dt>
                        <dd>{backendStatus}</dd>
                    </div>
                </dl>
            </section>
        </main>
    );
}

export default App;
