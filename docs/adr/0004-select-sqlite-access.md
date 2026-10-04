# ADR-0004: SQLiteアクセス方式を選定する

- Status: Accepted
- Validation Status: Pending
- Date: 2026-10-04
- Related: [#13](https://github.com/asam-masa/manga-update-manager/issues/13)

## Context

本プロジェクトは、漫画作品、更新予定、最終アクセス日時、状態、バックアップ用情報を端末内のSQLiteへ保存する。StorageはApplicationやDomainから分離し、一時データベースを使うIntegration Testを行う方針である。

GoからSQLiteを利用する主な候補には、CGoでC言語版SQLiteを呼び出す`mattn/go-sqlite3`、CGo不要の`modernc.org/sqlite`、Wasmから変換したCGo不要の`ncruces/go-sqlite3`がある。いずれも`database/sql`から利用できる。

Windows版Wails v2が必要とする主な開発環境はGo、NPM、WebView2であり、GCCはWindowsの必須要件ではない。`mattn/go-sqlite3`を使用する場合は、SQLiteのためにGCCまたはMinGW-w64とCGoの設定を追加する必要がある。

このADRではSQLiteドライバー、Goからのアクセス方法、SQLとマイグレーションの管理方針、ドライバーを再検討する条件を決定する。具体的なテーブル、インデックス、マイグレーション内容、バックアップ形式は対象外とする。

## Decision Drivers

- Windowsのローカル環境とCIで再現しやすくビルドできること
- Go標準の`database/sql`、トランザクション、エラー処理を学べること
- SQLiteを一時データベースでIntegration Testできること
- ドライバー固有処理をStorage境界へ閉じ込められること
- 初期版の想定データ量で十分な性能があること
- 将来ドライバーを比較、変更できること
- 過度なORMや抽象化を導入しないこと

## Considered Options

### `modernc.org/sqlite`＋`database/sql`＋手書きSQL

- 利点:
  - CGoとCコンパイラーが不要である
  - 通常のGoモジュールとしてWindowsとCIでビルドしやすい
  - 標準の`database/sql`で接続、トランザクション、キャンセルを学べる
  - v1のタグ付きバージョンが公開されている
- 欠点:
  - 生成されたGoコードと依存パッケージが大きい
  - C言語版を直接呼び出すドライバーと性能特性が異なる
  - 対応する`modernc.org/libc`のバージョン整合に注意が必要である
- リスク:
  - データ量やクエリによっては、CPU、メモリ、ビルド時間が問題になる可能性がある

### `mattn/go-sqlite3`＋`database/sql`＋手書きSQL

- 利点:
  - 長期間利用されてきた実績がある
  - C言語版SQLiteを直接利用できる
  - 性能とSQLite固有機能で有利になる可能性がある
- 欠点:
  - CGo、GCCまたはMinGW-w64、`CGO_ENABLED=1`が必要である
  - Windows、CI、クロスビルドの環境構築が増える
- リスク:
  - GoではなくCコンパイラー、リンカー、CPUアーキテクチャの問題へ対応する必要が生じる

### `ncruces/go-sqlite3`＋`database/sql`＋手書きSQL

- 利点:
  - CGoが不要で、Goと`x/sys`だけを外部依存とする
  - `database/sql`とバックアップなどの高度な機能を利用できる
  - Windows AMD64とARM64を含む複数環境でテストされている
- 欠点:
  - 現時点ではv1未満である
  - 接続ごとにWasm環境を使うため、他の候補よりメモリ使用量が増える
- リスク:
  - APIと実装特性が安定版まで変わる可能性がある

### ORMまたはSQL生成ライブラリを使用する

- 利点:
  - 単純なCRUDを短く記述できる
  - 型やモデルから定型処理を生成できる場合がある
- 欠点:
  - SQL、トランザクション、マイグレーションの挙動が隠れやすい
  - 初期規模に対して依存と抽象化が増える
- リスク:
  - ライブラリ固有のモデルがApplicationやDomainへ漏れる可能性がある

## Decision

`modernc.org/sqlite`をSQLiteドライバーとして採用し、Go標準の`database/sql`と手書きSQLから利用する。初期版ではORMとSQL生成ライブラリを導入しない。

初期版のデータ量ではSQLiteドライバーの最高性能より、WindowsとCIでの再現可能なビルド、Goの標準的なDB操作の学習、環境構築の単純さを優先する。SQLiteだけを目的としてGCCとCGoを追加しない。

規模や作品数だけを理由にドライバーを変更しない。計測によってドライバーがボトルネックになった場合、必要なSQLite機能に対応できない場合、または互換性上の問題が発生した場合に、`mattn/go-sqlite3`を含む候補を同じIntegration Testとベンチマークで比較する。

## Consequences

- ドライバーのimport、接続文字列、PRAGMA、エラー変換をStorage内へ閉じ込める
- ApplicationとDomainからドライバー固有APIを参照しない
- 標準的なSQLを優先し、ドライバー固有機能を使用する場合は理由と影響を記録する
- 順番付きのSQLファイルを実行するマイグレーションを初期版から用意する
- マイグレーションSQLは実行ファイルへ埋め込み、適用済みバージョンをSQLite内へ記録する
- 一時データベースを使うIntegration Testで、マイグレーション、CRUD、トランザクションを確認する
- `modernc.org/sqlite`と必要な`modernc.org/libc`のバージョンを`go.mod`と`go.sum`で固定する
- SQLiteファイルの互換性だけでなく、DSN、PRAGMA、エラー、バックアップAPIの差をドライバー変更時に確認する

再検討では、最初にクエリ、インデックス、取得件数、トランザクション、画面再描画を計測する。次のいずれかを確認した場合にドライバー比較へ進む。

- 操作時間が定めた性能目標を超え、CPUプロファイルでドライバーが主要因になっている
- メモリ使用量、実行ファイルサイズ、ビルド時間が配布または開発の支障になっている
- 必要なSQLite拡張またはAPIを利用できない
- 継続できないドライバー固有の不具合または互換性問題がある
- 対象OSやCPUアーキテクチャなど、配布条件が変わる

## Validation

Wails雛形生成と最初のStorage実装で次を確認する。

- CGoとGCCなしでWindows向けにビルドできる
- `database/sql`から一時SQLiteデータベースを作成できる
- 順番付きマイグレーションを新規DBと更新DBへ適用できる
- CRUDとトランザクションのIntegration Testが成功する
- ドライバー固有処理がStorage外から参照されていない
- GitHub ActionsでGoのテストを実行できる

実装前で検証を完了していないため、Validation Statusは`Pending`とする。

## References

- [`modernc.org/sqlite` package](https://pkg.go.dev/modernc.org/sqlite)
- [`mattn/go-sqlite3` README](https://github.com/mattn/go-sqlite3/blob/master/README.md)
- [`ncruces/go-sqlite3` README](https://github.com/ncruces/go-sqlite3/blob/main/README.md)
- [Go: `cgo` command](https://pkg.go.dev/cmd/cgo)
- [Wails v2: Installation](https://v2.wails.io/docs/gettingstarted/installation/)
- [ADR-0002](./0002-select-application-architecture.md)
- [データ仕様](../データ仕様.md)
- [テスト方針](../develop/quality/testing.md)
