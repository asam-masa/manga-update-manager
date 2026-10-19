# Manga Update Manager

登録した漫画を表紙付きで一覧表示し、更新予定や最終アクセス日時を管理するローカルデスクトップアプリです。

## 基本方針

- 利用者の漫画一覧、閲覧履歴、画像キャッシュは端末内だけに保存する
- 公開リポジトリにはソースコードと仕様だけを置く
- 自動取得は、利用規約上問題のない公開情報に限定する
- 自動取得できない作品は、更新予定と最終アクセス日時から「要確認」を表示する
- アプリから作品を開いた日時を記録する

## 想定技術

- デスクトップ基盤: Wails v2
- バックエンド: Go
- 画面: React / TypeScript / HTML / CSS
- ローカルデータ: SQLite（`modernc.org/sqlite`＋`database/sql`）
- 対象OS: Windowsを優先

## 最初の実装範囲

1. 作品の登録・編集・削除
2. 表紙カード形式の一覧
3. 既定ブラウザーで作品ページを開く
4. 最終アクセス日時と要確認期間の管理
5. 定期更新情報の手入力
6. 起動時・手動・指定時刻の状態確認
7. 保持データの表形式表示
8. バックアップと復元

サイトからの自動取得は、この基本機能が安定してからサイト単位で追加します。

## 開発状況

作品モデル、SQLite保存基盤、Wails API、作品登録フォームと一覧画面を実装しました。一覧のURLから既定ブラウザーで作品を開き、起動要求の成功時刻を最終アクセスとして保存できます。ページ表示・読了は確認しません。登録済み作品の表紙を設定・変更・解除でき、未設定・表示失敗では仮表示にします。作品の編集・削除、更新予定は後続Issueで追加します。DBはWindowsの`%AppData%\MangaUpdateManager\manga.sqlite`へ保存します。[画面設計](./docs/work-screen.md)と[作品API設計](./docs/work-api.md)を参照してください。

登録フォームは画面上部のボタンで開閉でき、閉じても途中の入力を保持します。開閉状態はSQLiteへ保存し、再起動後も復元します。保存に失敗しても開閉は反映し、警告します。「漫画名で検索」で一覧を絞り込めます。検索条件は登録後も維持しますが、検索文字と途中入力はアプリ終了後に保持しません。

同じ保存先では一つのアプリだけを起動できます。二つ目の起動や保存先の排他を確認できない場合は、案内して終了します。終了後に`.instance.lock`が残るのは正常で、手動削除は不要です。旧版はこの排他に参加しないため、更新時は起動中の旧版を閉じてください。[起動排他の設計と制約](./docs/adr/0014-lock-storage-before-startup.md)を参照してください。

## 開発コマンド

JPEG・PNG・WebPの静止画像を検証し、PNGサムネイルを生成します。既存APIの参照変更・安全な旧画像削除、初回描画後の整理と警告、作品IDによる限定HTTP配信、カード内の画像表示・設定・変更・解除を接続しています。取消では変更せず、成功後の後処理警告は操作失敗と区別します。reserved・未知・確認不能な画像は残すため、容量を必ず回収できるわけではありません。[画像生成](./docs/thumbnail-processing.md)・[解除API](./docs/verification/cover-remove-api.md)・[起動時整理](./docs/verification/startup-cover-cleanup.md)・[HTTP配信](./docs/verification/cover-http-delivery.md)・[画像操作UIの検証と限界](./docs/verification/cover-actions-ui.md)を参照してください。配布時は[依存ライブラリの通知](./THIRD_PARTY_NOTICES.md)を同梱してください。

最低Go版は1.26.0です。テスト・ビルドには1.26系の最新修正版を使用します。その他の必要な環境はNode.js 24.15.0以降の24系、npm、Wails CLI v2.16.0、WebView2です。画面テストのjsdomと推移依存が要求するNode.jsバージョンを満たしてください。今回の検証ではNode.js 24.18.0を使用しました。

```powershell
wails doctor
wails dev
```

検証には次のコマンドを使用します。

```powershell
go test ./...
go vet ./...
npm --prefix frontend test
npm --prefix frontend run build
wails build
```

## 開発運用

- GitHub Issuesを唯一の作業管理手段とする
- GitHub Flowで短命な作業ブランチとPRを使用する
- 重要な設計判断はADRへ記録する
- Issueに目的と受け入れ条件を記載する
- テスト、静的解析、ビルド、手動確認の結果をPRへ記録する
- 個人データと秘密情報が含まれていないことをマージ前に確認する

詳細は[ドキュメント一覧](./docs/README.md)を参照してください。

コントリビューションの手順は[CONTRIBUTING](./CONTRIBUTING.md)、セキュリティ上の報告方法は[SECURITY](./SECURITY.md)を参照してください。

