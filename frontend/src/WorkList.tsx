import type {main} from '../wailsjs/go/models';
import {registrationDate} from './work-display';

export function WorkList({works, showEmpty}: {works: main.WorkDTO[]; showEmpty: boolean}) {
    if (works.length === 0) {
        return showEmpty ? <div className="empty-state">
            <div className="empty-book" aria-hidden="true">本</div>
            <h3>まだ作品が登録されていません</h3>
            <p>作品URLとタイトルを入力して、最初の作品を登録しましょう。</p>
        </div> : null;
    }
    return <ul className="work-list" aria-label="作品一覧">
        {works.map((work) => <li key={work.id} className="work-card">
            <div className="cover-placeholder" aria-hidden="true">本</div>
            <div className="work-details">
                <h3>{work.title}</h3>
                {work.siteName && <p className="site-name">{work.siteName}</p>}
                <p className="work-url">{work.url}</p>
                {work.notes && <p className="work-notes">{work.notes}</p>}
                <p className="registered-at">登録 <time dateTime={work.createdAt}>{registrationDate(work.createdAt)}</time>（日本時間）</p>
            </div>
        </li>)}
    </ul>;
}
