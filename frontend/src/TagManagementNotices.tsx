export type TagNotice = {id: string; message: string; warning?: string; error?: boolean};

// The parent owns these results; closing the dialog must not remove them.
export function TagManagementNotices({notices}: {notices: TagNotice[]}) {
    return <>{notices.map((notice) => <section key={notice.id} aria-label="タグ操作の結果"
        className={`message ${notice.error || notice.warning ? 'error-message' : 'cover-result'}`}>
        <p role={notice.error ? 'alert' : 'status'}>{notice.message}</p>
        {notice.warning && <p role="alert">{notice.warning}</p>}
    </section>)}</>;
}
