# 画像参照変更のCommit結果調査

Issue #125。2026-10-18、Windows・Go 1.26.9・modernc.org/sqlite v1.60.1・CGO無効。1作業セッションのSpikeとして、採用版ソースと合成DBを確認した。本番のStorage・画像APIを変更するものではない。

## 結論

Commitエラーだけで「必ず旧参照に戻った」とは扱わない。採用ドライバーは失敗後にトランザクションが残っていればRollbackを試すが、そのRollbackエラーは返さない。今回の2種類の拒否では未確定と接続再利用を観測できたが、全故障へ一般化しない。結果不明時の保全・停止は[承認済み契約](../cover-persistence.md#db更新の結果不明)に従う。

## 固定版ソースと公開仕様

- [SQLite Transaction](https://www.sqlite.org/lang_transaction.html)の2.3と3節では、BUSYによるCommit失敗でトランザクションが残ること、FULL・IOERRなどで文またはトランザクションが取り消される場合があることを説明している。SQLiteのSQL操作とGoのTxの寿命を同一視しない。
- [Go 1.26.9 database/sql](https://github.com/golang/go/blob/go1.26.9/src/database/sql/sql.go)の`Tx.Commit`（2287行以降）は、ドライバーのCommitを呼ぶ前にdoneを設定する。ドライバー呼び出し後に失敗しても同じTxのCommit再試行やRollbackは`ErrTxDone`になる。事前context確認で返る場合は別経路であり、すべてのCommitエラーがこの経路とは限らない。
- [modernc.org/sqlite v1.60.1 tx.go](https://gitlab.com/cznic/sqlite/-/blob/v1.60.1/tx.go)の`Commit`（35行以降）はCOMMIT失敗後にautocommit状態を調べ、トランザクションが残ればROLLBACKを試す。そのRollbackの結果は返さず元のCommitエラーを返す。
- 同版の[conn.go](https://gitlab.com/cznic/sqlite/-/blob/v1.60.1/conn.go)の`ResetSession`・`IsValid`（1043行以降）は接続・割込状態を検査する。Rollback成功や画像参照の確定を証明するAPIではない。

ソースはローカルのGo 1.26.9ツールチェーンとgo.sum対象のモジュールキャッシュを読んで確認した。現在の`storage.changeWorkCover`はCommitエラーをラップして返し、deferしたRollbackの結果を消費しない。画像操作へ接続した時の結果分類・接続健全性は未実装である。

## 実行確認

`tools/spikes/commit-outcome/outcome_test.go`を試作用テストとして保持する。本番コードから呼ばず、そのまま回復処理へ転用しない。一時ファイルに合成の親子テーブルを作り、DELETE journal・busy_timeout=0・foreign_keys=ONで確認する。本番スキーマや画像ファイルは使わない。

| ケース | Commit結果 | その後に確認すること |
| --- | --- | --- |
| 正常な追加 | nil | 追加行をwriter・独立pool・再オープンで確認 |
| 別接続の読取Txを保持して追加 | SQLITE_BUSY | 追加行が残らず、既存行を維持 |
| 存在しない親への追加を遅延制約で検査 | SQLITE_CONSTRAINT_FOREIGNKEY | 同上 |

各ケースでCommit後の明示Rollbackが`ErrTxDone`となること、writerで新しいTxを開始・Rollbackできることも確認する。独立読取は保持中の読取Txを終えてから行い、古いスナップショットを確定状態の根拠にしない。単一接続poolであっても物理接続の同一性を計測した試験ではない。

```powershell
$env:GOTOOLCHAIN='go1.26.9'
$env:CGO_ENABLED='0'
go test ./tools/spikes/commit-outcome -v -count=20
```

上記3ケースを各20回実行し、すべて成功した。元画像・利用者DB・ネットワーク保存先は使用していない。

## 限界と後続

- 同じ接続での値の再読取だけでは、未終了Tx内の値を見ている可能性を排除できない。今回の別pool・再オープンの成功も、故障時の一般的な判別手順を採用する根拠には足りない。
- Commit成功後に応答だけ失われる故障、内部Rollback失敗、実容量不足、I/O故障、電源断、キャンセルとの競合は未検証。正常な一時DBの再オープン成功を、故障後の接続健全性や耐久性の証明にしない。
- エラーコードだけでの確定・未確定分類、危険な接続の切り離し、別接続からの確認、再読取失敗時の扱いは本番接続前に設計・検証する。独立poolは試験の観測用であり、アプリへ二つ目のStoreを導入する決定ではない。
- 当面の推奨は、本番接続時に「確定済み・未確定・不明」を明示できる結果契約を先に定めること。判別根拠が不足する経路を不明へ残し、変更の自動再試行・削除を許可しない。方式の採用は別途承認を得る。
- 再起動は復旧成功の保証ではなく状態確認の契機である。既存の安全な整理条件は緩和しない。画像機能全体とADR-0013はPending。
