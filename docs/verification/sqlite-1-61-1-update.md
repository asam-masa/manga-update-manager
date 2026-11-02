# SQLite 1.61.1への更新確認（PR #353）

## 対象と上流の変更

2026-11-02、modernc.org/sqliteを1.60.1から1.61.1へ更新する。[公式変更履歴](https://github.com/modernc-org/sqlite/blob/v1.61.1/CHANGELOG.md)と[go.mod](https://github.com/modernc-org/sqlite/blob/v1.61.1/go.mod)を照合した。Go要求は1.26.0を維持し、指定されたlibc 1.79.0へ揃える。go-humanizeとgo-strftimeも1.1.0へ変わる。

内蔵SQLiteは3.53.4から3.54.1になる。CommitとRowsのロック待ちがcontext終了を観測する変更を重点確認する。独自VFS・共有キャッシュ・新しい日時オプションはアプリへ導入しない。マイグレーションや製品の保存処理は変更しない。

## 旧版からの互換性

旧ドライバー1.60.1で、合成3作品・予定日・最終アクセス・タグと関連・表示設定・管理画像を含むDBを作った。全6テーブルの列名と値をJSONへ記録し、新ドライバーのStore.OpenとClose後に一致を確認した。ナノ秒を含む保存文字列も比較に含め、PRAGMA integrity_checkはokだった。

新ドライバーでURL重複が共通エラーへ変換されること、タグ・予定日・画像参照を保ったタイトル編集、再接続を確認した。更新後の内容を新旧のドライバーで読み直し、再び全テーブルの値とintegrity_checkを確認した。同じ合成保存先の実ファイルを使い、通常の利用者データやバックアップには触れていない。任意の旧DB・将来のダウングレードを保証するものではない。

## ロック待ち中の期限切れ

`TestWorkCoverCommitDeadlineStopsWithoutSuccess`はrollback journalと10秒のbusy_timeoutを設定し、別接続がread lockを持つ間に画像参照の更新をCommitする。context期限は1秒で、10秒の待機を続けず終了することを、余裕を持った5秒未満の上限で確認する。

期限切れはcontext.DeadlineExceeded・ErrWorkCoverOutcomeUnknown・ErrDBStoppedとして返り、作品・旧画像パスの成功データを返さない。以後の読取を拒否し、再接続すると元の作品とready状態の候補画像管理レコードが残ることを確認した。これはDB内の画像参照・管理情報の試験であり、物理ファイル削除や電源断を注入したものではない。

同じ試験は旧ドライバーを指定した一時modfileでは約10秒の待機で失敗し、新ドライバーでは成功した。旧版の失敗は、この新しいキャンセル動作を検出できることの確認として扱う。

## 実行結果と限界

最新main da3f9392970987aed0bc01ab73640a60c0e3bee7を取り込んだ版で、Go 1.26.5・CGO無効の`go test ./...`と`go vet ./...`が成功した。Commit結果・DB受付・画像境界の既存Spikeも含む。Wails 2.17.0でWindows productionビルドが成功した。Wails更新PR #352とは分けて検証する。

フロントエンドは#352検証時とソース・package/lockが同一であり、そこで成功した437試験・型検査・Viteビルドを利用する。新headのWindows CIでも改めて実行する。

旧版から引き継いだ専用APPDATA/TEMPの合成DBで実WebView2確認を行い、結果・通常/独立レビュー・更新後CIをPRへ記録する。未検証の全OS・故障・共有キャッシュ・独自VFSまで成功を保証せず、既存ADRの検証状態を一括変更しない。
