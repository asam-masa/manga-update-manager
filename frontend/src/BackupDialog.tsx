import {useEffect, useLayoutEffect, useRef} from 'react';
import type {BackupResult, BackupStatus} from './backup-response';
import './BackupDialog.css';

export type BackupDialogProps = {
    status: BackupStatus | null; result: BackupResult | null; fresh: boolean;
    selecting: boolean; cancelling: boolean; error: string;
    confirming?: boolean;
    onStart: () => void; onCancel: () => void; onReload: () => void; onClose: () => void;
};
const stageNames: Record<BackupStatus['stage'], string> = {
    unobserved: '処理状況を確認中', waiting: '開始済み操作の完了待ち', preparing: '開始前の確認',
    database: 'DBコピー', database_check: 'DBコピーの検証', images: '画像確認',
    metadata: '目録作成', zip: 'ZIP作成', publishing: '保存確定', cleanup: '片付け',
};

// Receives validated observations; commands and their request lifetimes belong
// to the caller. Mounting or observing a result never sends a save/cancel.
export function BackupDialog(props: BackupDialogProps) {
    const {status, fresh, selecting, cancelling, error, onStart, onCancel, onReload, onClose} = props;
    const dialog = useRef<HTMLDialogElement>(null);
    const heading = useRef<HTMLHeadingElement>(null);
    const result = status?.state === 'finished' && props.result?.executionId === status.executionId ? props.result : null;
    const busy = selecting || cancelling || props.confirming === true || (status !== null && status.state !== 'idle' && status.state !== 'finished');
    const canClose = !busy;
    const canStart = fresh && !busy && (status?.state === 'idle'
        || (status?.state === 'finished' && result?.retryAllowed === true && result.outcome !== 'unknown'));
    const canCancel = fresh && !selecting && !cancelling && !props.confirming && status?.state === 'running' && status.canCancel;

    useEffect(() => {
        const node = dialog.current!;
        node.showModal(); heading.current?.focus();
        return () => node.close();
    }, []);
    useLayoutEffect(() => {
        // A focused action can disappear after cancellation or completion.
        if (dialog.current?.open && document.activeElement === document.body) heading.current?.focus();
    });

    return <dialog ref={dialog} className="edit-dialog backup-dialog" aria-labelledby="backup-dialog-title"
        onCancel={(event) => {event.preventDefault(); if (canClose) onClose();}}>
        <header><h2 id="backup-dialog-title" ref={heading} tabIndex={-1}>バックアップ</h2></header>
        <div className="backup-dialog-body">
            <p>作品情報・更新予定日・タグ・最終アクセス・設定と、現在の作品が使う管理画像を1つのZIPファイルに保存します。</p>
            <ul>
                <li>暗号化されません。共有せず、安全な場所へ保管してください。</li>
                <li>既存ファイルは上書きしません。新しい名前を選んでください。</li>
                <li>保存処理中は登録・編集などの通常操作を一時的に止めます。</li>
            </ul>
            {!fresh && <p className="warning-message" role="status">最新情報は未確認です。保存を開始せず、状態を確認してください。</p>}
            {error && <p className="error-message" role="alert">{error}</p>}
            {selecting && <p role="status">保存先を選択中です。選択画面でキャンセルすると元の画面に戻ります。</p>}
            {props.confirming && <p role="status">操作後の状況を確認しています。再操作せずお待ちください。</p>}
            {status && status.state !== 'idle' && <section aria-label="バックアップの状況">
                <p>処理段階：{stageNames[status.stage]}</p>
                <p>経過時間：{status.elapsedSeconds}秒</p>
                <p className="section-description">最後に確認した状況です。残り時間や正確な進捗率ではありません。</p>
                {(cancelling || status.state === 'cancelling') && <p role="status">取消を要求しました。片付け・解放が終わるまでお待ちください。</p>}
                {status.state === 'unknown' && <p className="warning-message">処理状態を確認できません。再保存せず、状態を確認してください。</p>}
            </section>}
            {status?.state === 'finished' && !result && <p role="status">結果の案内を確認できていません。再読み込みをお試しください。</p>}
            {result && <section aria-label="バックアップの結果">
                <h3>{result.outcome === 'saved' || result.outcome === 'saved_with_warnings' ? '保存を確認しました' : result.outcome === 'unknown' ? '保存の成否は不明です' : '保存されていません'}</h3>
                <p role="status">{result.message}</p>
                {result.notices.map((notice, index) => <p className="warning-message" key={`${notice.code}-${index}`}>{notice.message}</p>)}
                <p>除外した画像：{status?.result?.omittedImageCount == null ? '未確認' : `${status.result.omittedImageCount}件`}</p>
                {!result.retryAllowed && <p className="warning-message">再保存せず、保存先や保全対象を確認してください。再起動だけで保存の成否は確定しません。</p>}
            </section>}
        </div>
        <footer className="backup-dialog-footer">
            <button type="button" className="secondary-button" disabled={selecting} onClick={onReload}>再読み込み</button>
            {canCancel && <button type="button" className="secondary-button" onClick={onCancel}>バックアップをキャンセル</button>}
            <button type="button" className="secondary-button" disabled={!canClose} onClick={() => {if (canClose) onClose();}}>閉じる</button>
            {canStart && <button type="button" className="primary-button" onClick={onStart}>{result ? '保存先を選び直す' : '保存先を選んで開始'}</button>}
        </footer>
    </dialog>;
}
