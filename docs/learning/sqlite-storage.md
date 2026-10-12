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

ADR-0004に従い、CGo不要の`modernc.org/sqlite`とGo標準の`database/sql`を使用します。Issue #19ではGo 1.25設定を維持するため、Go 1.25を要求するv1.58.0を固定しました。当時はGo 1.26を要求するv1.60.1を対象外にしました。

[ADR-0015](../adr/0015-separate-go-minimum-and-validation-version.md)でGo設定を1.26へ更新した後、[PR #65](https://github.com/asam-masa/manga-update-manager/pull/65)でv1.60.1への更新を検証しました。ドライバーだけでなく、上流が指定する`modernc.org/libc` v1.77.1との一致も確認します。

同じドライバーでDBを再オープンするテストだけでは、更新前後の互換性を直接確認できません。今回は旧版で合成DBを作り、新版で作品・最終アクセス・表示設定を読み書きして再オープンし、旧版でも読み直しました。日時の小数秒、URL重複のエラー変換、適用済みマイグレーション数、`PRAGMA integrity_check`も確認しました。利用者のDBには触れていません。この結果は検証した合成DBに限り、任意の既存DBや将来の更新の互換性を保証しません。

`database/sql`は接続プールを管理します。初期版ではローカルの単一SQLiteファイルを単純に扱うため、同時に開く接続数を1へ制限します。並行処理が必要になった場合は、計測とロック動作を確認して見直します。

## 制約とNULLの関係

SQLiteのCHECKは条件がNULLになっただけでは拒否しません。例えば「保存確認済みならハッシュの長さが32」という条件だけでは、ハッシュ未設定を防げない場合があります。必須の状態では`IS NOT NULL`も確認し、不足した列ごとの異常系をテストします。STRICTの型検査と、複数列の状態整合性は別です。[生成画像管理テーブルのSpike](../verification/managed-cover-schema.md)で一時SQLiteを使って確認しました。列制約は、実ファイルの所有や安全性を証明するものではありません。

## 候補の取得と操作の許可を分ける

「保存確認済みで、どの作品からも参照されていない」というSQL条件は、ファイルを調べる候補を絞るためのものです。それだけで削除を許可すると、DB取得後に別の作品が参照を追加した場合や、同名ファイルが変わった場合を扱えません。実ファイルの照合と競合排他を合わせて考えます。[状態と候補取得のSpike](../verification/managed-cover-state.md)は、このうち状態遷移と共有参照の除外を確認しています。

状態と確認情報は一つのUPDATEで確定します。更新件数が0でもSQLの実行自体は成功するため、件数を確認して「変更できた」と区別します。また、DB取得失敗を「参照なし」と読み替えると危険です。エラーでは処理を止める条件を、参照が本当に0件の場合とは別に検証します。

## 正本と根拠

- [データ仕様](../データ仕様.md)
- [ADR-0002: 内部アーキテクチャを選定する](../adr/0002-select-application-architecture.md)
- [ADR-0004: SQLiteアクセス方式を選定する](../adr/0004-select-sqlite-access.md)
- [Issue #19: 作品モデルとSQLite保存基盤を実装する](https://github.com/asam-masa/manga-update-manager/issues/19)
- [`modernc.org/sqlite` package](https://pkg.go.dev/modernc.org/sqlite)
- [`modernc.org/sqlite` v1.58.0 `go.mod`](https://gitlab.com/cznic/sqlite/-/raw/v1.58.0/go.mod)
- [`modernc.org/sqlite` v1.60.1 `go.mod`](https://github.com/modernc-org/sqlite/blob/v1.60.1/go.mod)
- [`modernc.org/sqlite` v1.60.1変更履歴](https://github.com/modernc-org/sqlite/blob/v1.60.1/CHANGELOG.md)
