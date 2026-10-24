import type {WorkView} from './work-display';

export type WorkNotice = {key: string; work: WorkView; kind: 'access' | 'cover' | 'edit' | 'delete';
    pending?: boolean; success?: string; error?: string; warnings?: string[]};

// This stays mounted independently of the filtered list and future layouts.
export function WorkNotices({notices, onDismiss}: {notices: WorkNotice[]; onDismiss: (notice: WorkNotice) => void}) {
    if (!notices.length) return null;
    return <div className="work-notices">
        {notices.map((notice) => {
            const label = notice.kind === 'access' ? 'URL操作' : notice.kind === 'cover' ? '画像操作' : notice.kind === 'edit' ? '編集結果' : '削除結果';
            return <section key={notice.key} role={notice.kind === 'cover' ? 'group' : undefined}
                aria-label={`${notice.work.title}の${label}`} className={`message ${notice.error ? 'error-message' : 'cover-result'}`}>
                <h3>「{notice.work.title}」の{label}</h3>
                {notice.pending && <p role="status">{notice.kind === 'access' ? `「${notice.work.title}」のページを開いています…` : '画像を処理しています…'}</p>}
                {notice.success && <p role="status">{notice.success}</p>}
                {notice.error && <p role="alert">{notice.error}</p>}
                {notice.warnings?.map((warning, index) => <p role="alert" key={index}>{warning}</p>)}
                {!notice.pending && <button type="button" className="secondary-button" onClick={() => onDismiss(notice)}>
                    {notice.kind === 'cover' ? '画像操作のお知らせを閉じる' : `「${notice.work.title}」の${label}のお知らせを閉じる`}
                </button>}
            </section>;
        })}
    </div>;
}
