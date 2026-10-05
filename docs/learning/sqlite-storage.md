# 作品モデルとSQLite保存を分離する

## 学ぶこと

作品の入力規則とSQLite固有の処理を分離すると、重要な規則をデータベースなしでUnit Testしながら、保存境界だけをIntegration Testできます。

## 責務の分け方

`internal/manga`は、作品URL、タイトル、サムネイル相対パス、日時の規則を担当します。SQLite、SQL、マイグレーションを参照しません。

`internal/storage`は、`database/sql`、SQLite接続、SQL、マイグレーション、行とGo型の変換を担当します。Wailsと画面を参照しません。

```text
入力
  ↓
manga.NewWork: 検証と正規化
  ↓
storage.CreateWork: SQLiteへ保存
```

この分離により、不正URLや絶対画像パスのテストは高速なUnit Testで行えます。UNIQUE制約、マイグレーション、DBを閉じて再度開いた後のデータは、一時SQLiteファイルを使うIntegration Testで確認します。

## マイグレーション

マイグレーションは番号付きSQLファイルとして実行ファイルへ埋め込みます。適用済みのファイル名を`schema_migrations`へ記録し、同じマイグレーションを再実行しません。

マイグレーションと適用記録は同じトランザクションで処理します。SQLの実行に成功しても適用記録に失敗した場合は、両方をロールバックします。

初期テーブルへ将来の全項目を先に追加しない理由は、状態、更新予定、最終アクセスにはまだ確定していない規則があるためです。規則と列を同じIssueで追加すると、列の意味と`NULL`の扱いをテストで説明できます。

## 日時

SQLiteにはアプリ全体で共通するタイムゾーン付き日時型がないため、時刻はUTCのRFC 3339文字列へ統一します。小数秒を9桁へ固定すると、文字列の大小と時刻の前後関係を一致させられます。`RFC3339Nano`の可変桁のまま保存すると、末尾の`Z`と小数点の辞書順によって、後の時刻を前と判定する場合があります。

利用者向けの表示、入力、予定、アラート判定では`Asia/Tokyo`へ変換します。

Go標準ライブラリの`time/tzdata`を実行ファイルへ含めることで、Windows環境のタイムゾーンデータの有無に依存せず`Asia/Tokyo`を読み込めます。

## SQLiteドライバー

ADR-0004に従い、CGo不要の`modernc.org/sqlite`とGo標準の`database/sql`を使用します。プロジェクトのGo 1.25設定を維持するため、Go 1.25を要求するv1.58.0を固定します。v1.60.1はGo 1.26を要求するため、今回の更新対象にしません。

`database/sql`は接続プールを管理します。初期版ではローカルの単一SQLiteファイルを単純に扱うため、同時に開く接続数を1へ制限します。並行処理が必要になった場合は、計測とロック動作を確認して見直します。

## 正本と根拠

- [データ仕様](../データ仕様.md)
- [ADR-0002: 内部アーキテクチャを選定する](../adr/0002-select-application-architecture.md)
- [ADR-0004: SQLiteアクセス方式を選定する](../adr/0004-select-sqlite-access.md)
- [Issue #19: 作品モデルとSQLite保存基盤を実装する](https://github.com/asam-masa/manga-update-manager/issues/19)
- [`modernc.org/sqlite` package](https://pkg.go.dev/modernc.org/sqlite)
- [`modernc.org/sqlite` v1.58.0 `go.mod`](https://gitlab.com/cznic/sqlite/-/raw/v1.58.0/go.mod)
