# ADR-0006: 表示設定をSQLiteへ保存する

- Status: Accepted
- Validation Status: Pending
- Date: 2026-10-10
- Related: [#27](https://github.com/asam-masa/manga-update-manager/issues/27)

## Context

登録フォームの開閉状態を再起動後にも復元する。既存SQLiteの接続・マイグレーション・一時DBテストがある。ADR-0005は保存する状態を定めたが、保存方式と失敗時の扱いは未決定だった。

Issue #27のgrillingで、SQLite方式、操作ごとの保存、失敗時の継続と警告、初期値、自動上書きをしない方針を利用者が承認した。今回の保存対象はフォーム開閉だけである。未実装の表示形式・並び順・未来ON/OFF、検索文字、選択タグ、途中入力、バックアップは対象外とする。

## Decision Drivers

- 既存の保存・移行・テスト基盤を再利用できること
- 操作と保存の成功・失敗を分けて扱えること
- 初回と設定破損・読み書き失敗を区別できること
- 作品データを変更せず、明示した設定だけを更新できること
- 新しい依存と汎用設定機構を増やさないこと

## Considered Options

### 既存SQLiteの設定専用テーブル

- 利点: 保存先・接続・移行・テストを再利用でき、一文のSQLで設定を更新できる。
- 欠点: 設定も作品と同じDBへ依存し、スキーマ変更が必要になる。
- リスク: DB全体が利用不能になると作品と設定の両方を読み書きできない。設定だけの不正値と区別する必要がある。

### 別のJSONファイル

- 利点: 作品DBから分離でき、内容を直接確認できる。
- 欠点: ファイルの更新・失敗・互換性の処理を別途実装する必要がある。
- リスク: 安全な置き換えや破損への対応が不足すると設定を失う可能性がある。

## Decision

既存SQLiteの`display_settings`専用テーブルへ保存する。新しい依存と設定ファイルは導入しない。初回はフォームを表示し、設定未登録を正常扱いにする。読み込みだけでは行を挿入しない。

フォームを開閉するたびに保存する。画面は即座に操作を反映し、保存要求を操作順に一つずつ送る。失敗しても操作を戻さず、再起動すると以前の状態へ戻る場合があると警告する。終了時だけの保存には依存しない。

設定の読み込み失敗や未知値は初期値の表示で続行し、警告する。起動時に初期値で自動上書きしない。その後、利用者が明示的に開閉した場合は、その値の保存を試みる。DB全体の初期化失敗時は作品操作の既存エラー契約を維持し、DBを再作成・修復・削除しない。

## Consequences

- WailsはDTO・エラー変換、Applicationは設定操作、settingsは保存対象の型と初期値、storageはSQLを担当する。
- APIは読み取りとフォーム開閉の保存だけを公開する。任意キーや画面状態全体の保存APIは作らない。
- SQLは指定項目だけを更新し、作品テーブルには触れない。
- 起動時の設定取得中はフォームを隠して開閉・登録を待つ。復元による自動フォーカス移動は行わない。
- 検索・途中入力と保存対象は別の状態として扱う。後続設定は機能実装と同時にスキーマ・型・APIを拡張する。
- 保存応答前の終了、OS障害、複数アプリ間の競合で最新状態の保存を保証しない。保存順序の保証は一画面からの連続操作が対象である。
- E2E導入は当面保留し、既存のGo・SQLite・DOMテストと実機確認を維持する。Issue #35の調査結果を参照する。

## Validation

一時SQLiteで未登録、保存・再オープン、未知値を勝手に修復しないこと、書き込み失敗で以前の値と作品が変わらないこと、既存スキーマからの移行を確認した。ApplicationとWails境界で操作と安全なエラー変換を確認した。

DOMテストで復元・自動保存なし・初回フォーカス、読み書き失敗、連続操作の順序、StrictModeの古い応答の無視、検索・途中入力の非保存を確認した。実WebView2の終了・再起動後の復元は未確認であり、Validation StatusはPendingを維持する。

## References

- [Issue #27](https://github.com/asam-masa/manga-update-manager/issues/27)
- [ADR-0002](./0002-select-application-architecture.md)
- [ADR-0004](./0004-select-sqlite-access.md)
- [ADR-0005](./0005-define-library-behavior.md)
- [SQLite UPSERT](https://www.sqlite.org/lang_upsert.html)
- [SQLite STRICT tables](https://www.sqlite.org/stricttables.html)
- [Playwright接続調査](../verification/webview2-playwright-spike.md)
