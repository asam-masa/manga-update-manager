import {useEffect, useLayoutEffect, useRef, useState} from 'react';
import type {BackupResult, BackupStatus} from './backup-response';
import './BackupDialog.css';
import {BackupLocationsPanel} from './BackupLocationsPanel';
import type {useBackupLocations} from './use-backup-locations';
import {useBackupOmissionPage} from './use-backup-omission-page';

export type BackupDialogProps = {
    status: BackupStatus | null; result: BackupResult | null; fresh: boolean;
    selecting: boolean; cancelling: boolean; error: string;
    confirming?: boolean;
    locations?: ReturnType<typeof useBackupLocations>;
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
    const body = useRef<HTMLDivElement>(null), detailHeading = useRef<HTMLHeadingElement>(null);
    const detailButton = useRef<HTMLButtonElement>(null);
    const positions = useRef({summary: 0, images: 0});
    const pendingFocus = useRef<'heading' | 'image' | 'summary' | null>(null);
    const [navigation, setNavigation] = useState<{id: string | null; mode: 'summary' | 'images' | 'works'; imagePage: number; workPage: number; imageId: string | null}>(
        {id: null, mode: 'summary', imagePage: 1, workPage: 1, imageId: null});
    const result = status?.state === 'finished' && props.result?.executionId === status.executionId ? props.result : null;
    const detail = navigation.id === result?.executionId ? navigation
        : {id: result?.executionId ?? null, mode: 'summary' as const, imagePage: 1, workPage: 1, imageId: null};
    const images = useBackupOmissionPage(detail.mode !== 'summary' && detail.id ? {executionId: detail.id, page: detail.imagePage} : null);
    const works = useBackupOmissionPage(detail.mode === 'works' && detail.id && detail.imageId
        ? {executionId: detail.id, page: detail.workPage, imageId: detail.imageId} : null);
    const page = detail.mode === 'works' ? works : images;
    function back() {
        pendingFocus.current = detail.mode === 'works' ? 'image' : 'summary';
        setNavigation({...detail, mode: detail.mode === 'works' ? 'images' : 'summary'});
    }
    function changePage(next: number) {
        if (page.loading || !page.data || next < 1 || next > page.data.pages) return;
        pendingFocus.current = 'heading';
        setNavigation({...detail, ...(detail.mode === 'works' ? {workPage: next} : {imagePage: next})});
    }
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
    useLayoutEffect(() => {
        const target = pendingFocus.current;
        if (target === 'heading') {detailHeading.current?.focus(); pendingFocus.current = null;}
        else if (target === 'image' && detail.mode === 'images' && images.data) {
            document.getElementById(`backup-image-${detail.imageId}`)?.focus({preventScroll: true});
            if (body.current) body.current.scrollTop = positions.current.images;
            pendingFocus.current = null;
        } else if (target === 'summary' && detail.mode === 'summary') {
            detailButton.current?.focus({preventScroll: true});
            if (body.current) body.current.scrollTop = positions.current.summary;
            pendingFocus.current = null;
        }
    });

    return <dialog ref={dialog} className="edit-dialog backup-dialog" aria-labelledby="backup-dialog-title"
        onCancel={(event) => {event.preventDefault(); if (canClose) {if (detail.mode !== 'summary') back(); else onClose();}}}>
        <header><h2 id="backup-dialog-title" ref={heading} tabIndex={-1}>バックアップ</h2></header>
        <div ref={body} className="backup-dialog-body">
            <div hidden={detail.mode !== 'summary'}>
            <p>作品情報・更新予定日・タグ・最終アクセス・設定と、現在の作品が使う管理画像を1つのZIPファイルに保存します。</p>
            <ul>
                <li>暗号化されません。共有せず、安全な場所へ保管してください。</li>
                <li>既存ファイルは上書きしません。新しい名前を選んでください。</li>
                <li>保存処理中は登録・編集などの通常操作を一時的に止めます。</li>
            </ul>
            </div>
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
                {detail.mode === 'summary' && props.locations && props.locations.id === result.executionId && <BackupLocationsPanel view={props.locations} />}
                {!result.retryAllowed && <p className="warning-message">再保存せず、保存先や保全対象を確認してください。再起動だけで保存の成否は確定しません。</p>}
                {detail.mode === 'summary' ? <button ref={detailButton} type="button" className="secondary-button"
                    disabled={!fresh || busy || status?.result?.omittedImageCount == null || status.result.omittedImageCount === 0}
                    onClick={() => {positions.current.summary = body.current?.scrollTop ?? 0; pendingFocus.current = 'heading'; setNavigation({...detail, mode: 'images'});}}>除外画像の詳細</button>
                    : <section aria-label="除外詳細">
                        <h4 ref={detailHeading} tabIndex={-1}>{detail.mode === 'works' ? '対象作品の一覧' : '除外画像の一覧'}</h4>
                        {page.loading && <p role="status">詳細を読み込んでいます…</p>}
                        {page.error && <p role="alert">{page.error}</p>}
                        {detail.mode === 'images' && status?.result?.workNamesState !== 'available' && <p>対象作品名の詳細は未確認または利用不可です。</p>}
                        <div hidden={detail.mode !== 'images'}>{images.data?.kind === 'images' && <ol className="backup-omission-list" start={(detail.imagePage - 1) * 50 + 1}>
                            {images.data.items.map((image, index) => <li key={image.imageId}>
                                <span>{({missing: '欠損', corrupt: '変更または破損', unavailable: '理由を確認できません'})[image.reason]}</span>
                                <button id={`backup-image-${image.imageId}`} type="button" className="secondary-button"
                                    disabled={status?.result?.workNamesState !== 'available'} aria-label={`除外画像${(detail.imagePage - 1) * 50 + index + 1}の対象作品`}
                                    onClick={() => {positions.current.images = body.current?.scrollTop ?? 0; pendingFocus.current = 'heading'; setNavigation({...detail, mode: 'works', imageId: image.imageId, workPage: 1});}}>対象作品を表示</button>
                            </li>)}
                        </ol>}</div>
                        {detail.mode === 'works' && works.data?.kind === 'works' && <ul className="backup-omission-list">{works.data.items.map((work) => <li key={work.workId}>{work.title}</li>)}</ul>}
                        {page.data && <nav aria-label={detail.mode === 'works' ? '対象作品のページ' : '除外画像のページ'}>
                            <p>{page.data.page} / {page.data.pages}ページ・全{page.data.total}{detail.mode === 'works' ? '作品' : '画像'}</p>
                            <button type="button" className="secondary-button" disabled={page.loading || page.data.page === 1} onClick={() => changePage(page.data!.page - 1)}>前へ</button>
                            <button type="button" className="secondary-button" disabled={page.loading || page.data.page === page.data.pages} onClick={() => changePage(page.data!.page + 1)}>次へ</button>
                        </nav>}
                    </section>}
            </section>}
        </div>
        <footer className="backup-dialog-footer">
            <button type="button" className="secondary-button" disabled={selecting || (detail.mode !== 'summary' && page.loading)} onClick={detail.mode === 'summary' ? onReload : page.reload}>{detail.mode === 'summary' ? '再読み込み' : '詳細を再読み込み'}</button>
            {detail.mode !== 'summary' && <button type="button" className="secondary-button" onClick={back}>戻る</button>}
            {canCancel && <button type="button" className="secondary-button" onClick={onCancel}>バックアップをキャンセル</button>}
            <button type="button" className="secondary-button" disabled={!canClose} onClick={() => {if (canClose) onClose();}}>閉じる</button>
            {canStart && detail.mode === 'summary' && <button type="button" className="primary-button" onClick={onStart}>{result ? '保存先を選び直す' : '保存先を選んで開始'}</button>}
        </footer>
    </dialog>;
}
