import type {useBackupLocations} from './use-backup-locations';

const labels = {temporary_zip: 'ZIPの一時ファイル', publication_target: '成否不明の保存先',
    database_copy: 'DBコピー', work_directory: '作業フォルダー'};
export function BackupLocationsPanel({view}: {view: ReturnType<typeof useBackupLocations>}) {
    const {data, loading, error, reload} = view;
    return <section aria-label="保存先と保全候補" className="backup-locations">
        <h4>保存先と保全候補</h4>
        {loading && <p role="status">場所の詳細を読み込んでいます…</p>}
        {error && <p role="alert">{error}</p>}
        {error && <button type="button" className="secondary-button" onClick={reload}>場所の詳細を再読み込み</button>}
        {data && <>
            <dl><dt>選んだ保存先（保存成功の証明ではありません）</dt><dd>{data.destination || '未記録'}</dd>
                <dt>保存を確認した場所</dt><dd>{data.publication === 'confirmed' ? data.confirmedPath || '未記録' : '未確認'}</dd></dl>
            <p>保全候補は処理時点の記録です。現在の存在や同一性を確認したものではありません。自己判断で削除せず、状態を確認してください。</p>
            {data.candidates.length ? <dl>{data.candidates.map((item, index) => <div key={`${item.kind}-${index}`}>
                <dt>{labels[item.kind]}</dt><dd>{item.path}</dd></div>)}</dl>
                : <p>記録された保全候補はありません。作業物が存在しないことの保証ではありません。</p>}
        </>}
    </section>;
}
