# 作品編集の入力とSQLite保存基盤

Issue #209、親#205。`manga.EditWorkInput.Normalize`と`Store.EditWork`だけを追加する。API・画面・画像/URL起動との共通受付は未接続であり、利用者向け編集機能の完成ではない。正本は[ADR-0018](../adr/0018-edit-and-delete-works.md)。スキーマ・保存形式・依存の変更なし。

## 保存契約

URL・タイトル・サイト名・メモの前後空白を除去し、URL/タイトルは登録と同じ検証関数を使う。Storeも正規化し、共通のDB実行受付の下でTxから現在行を取得する。4項目とupdatedAt以外はUPDATEしない。日時はUTC固定形式を維持し、updatedAtを既存値より戻さない。正規化後に変更なしならUPDATE/Commitせず読取TxをRollbackで解放する。

重複はSQLiteのUNIQUE制約から`manga.ErrDuplicateURL`へ変換する。不正入力・不存在・通常のUPDATE失敗は成功作品を返さず、正常に解放できればStoreを継続する。Commitエラーは`ErrWorkEditOutcomeUnknown`と`ErrDBStopped`、Rollback/返却確認不能もDB停止とする。結果不明で成功作品を返さず、再試行・書戻し・再接続を行わない。Commit成功後の返却失敗は`ErrWorkEditCommitted`を付け、確定済みの作品と停止エラーを区別できる形で返す。後続APIはこの結果を通常失敗へ潰さない必要がある。

このStore操作の直列化はDB内部だけであり、OS選択・ブラウザー起動を含む操作全体の排他ではない。呼出し側の共通受付、停止通知・通常終了との統合は#205の後続で行う。

## 確認する条件

- Unit Test: 4項目の正規化、必須・scheme/host拒否。既存の登録/モデルテストも再実行する。
- 一時SQLite: 変更と再接続、画像参照/ready記録/ID/登録日時/入力作成後に更新した最終アクセスの保持、時計巻戻り、任意項目の消去、重複/不存在/不正入力/零時刻、更新拒否・0行更新、取消・停止の拒否。
- 変更なしはUPDATE拒否トリガーと、Commitすると合成エラーを返すdriverの両方で、UPDATE/Commitしないことを確認する。
- 既存のテスト専用driverは、実SQLite Commit/Rollbackを実行した後で合成エラーを返す。実Commit済みでも結果不明として新規DB操作を拒否し、独立の再接続による観測では変更が保存されていることを確認する。Rollback失敗印でも停止し、成功作品を返さない。

Commit成功後の接続返却失敗の識別は実装の分岐を静的に確認し、共通Storeの返却エラーテストと区別する。本編集経路の実返却故障や実OS故障を再現したものではない。実Commit競合・電源断・全競合順序・新規GUI/実利用者データの試験は未実施。実行した自動テスト・ビルドの対象版と結果、通常/独立レビューはPRへ記録する。ADR-0018はPendingを維持する。
