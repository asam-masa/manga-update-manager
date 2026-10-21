# 画像保存・削除・復旧の検証根拠の最終照合

Issue #191、親#42。2026-10-21、[ADR-0013の完了判定](../adr/0013-define-cover-operation-boundaries.md#現在の完了判定issue-178)に対し、本番のテスト・検証記録・CIを照合した。新しい仕様や完了条件を追加するものではない。本記録は照合時点の証拠であり、進行状態はGitHub Issuesで管理する。

## 結論と対象の版

最終照合には未確認が残る。生成・保存失敗の取り込み全体の保全、画像処理途中のプロセス終了から次回整理への接続、破損画像の実WebView2表示を後続で確認する。ADR-0013はPendingのままとし、必須条件を既知制約へ付け替えない。

- 基準main: `7ac8d98603131d23773d45b04d0402dc44e4d1a6`（PR #190）。PR head `0ce3cab87449ab58f4e093958227709c05f9e480`とtree `d9b86f3e67f87272420ae29697b6817d4d7f1e2f`が一致する。
- [基準mainのWindows CI](https://github.com/asam-masa/manga-update-manager/actions/runs/39546227455)は上記main SHAでsuccess。ワークフローはGoテスト・vet、画面テスト・ビルド、Wails productionビルドを実行する。CIは実WebView2や実I/O故障の検証ではない。
- ローカル再確認: Windows、Go 1.26.5、CGO_ENABLED=0、Node 24.18.0。`go test ./... -json -count=1 -timeout=5m`は559件のテスト成功イベント（サブテスト込み）、失敗0、テスト単位のskip 0。テストファイルなしの7パッケージのskipイベントは、拒否条件の未実行と混同しない。`go vet ./...`、画面89件、TypeScript/Viteビルドも成功した。
- 実画面の根拠は下記の過去の分離データ検証である。今回の基準版で全GUI・過去の全条件を再実行したものではない。各記録の日時・環境・変更後の再確認・限界を引き継ぐ。

2026-10-21、後続#192で[生成・保存失敗時の取り込み全体の保全](./cover-failure-preservation.md)を追加検証した。以下の3件の不足は#191時点の照合結果であり、#192の条件は同記録の範囲で確認済み。残る検証は#193・#194で、ADR-0013全体はPendingを維持する。

## 必須四項目との対応

| 項目 | 現在の根拠と判断 |
| --- | --- |
| 停止・DB結果不明の案内と保全 | [#180/#181の記録](./cover-stop-ui.md)。実Commit競合と合成の画像解放失敗を区別し、新旧画像保持・自動再試行なし・DB停止/画像停止・確定後警告を確認。画面案内は本番Wails経路の制御注入であり、実Close故障ではない |
| 長時間案内 | [#183の記録](./cover-progress-ui.md)。自動テストと実WebView2の代表待機を対応付け、選択待ち除外・検索非表示・完了後消去を確認。最後の観測であり正常継続の保証ではない |
| 処理中の終了拒否・完了後の終了 | [#189の記録](./cover-close.md)。全段階の依存境界・参照待ちを自動確認し、整理・変更・解除の代表待機で拒否案内と再要求終了を実画面確認。読取だけの終了待ちとは別契約 |
| 保存・削除・復旧の最終照合 | 以下の対応表で確認済みと不足を区別する。未確認3件が残るため、この項目もADR全体も未完了 |

## 保存・削除・復旧の対応表

「確認」は記載した条件と検証方法に限定する。部品テスト、実SQLite/実ファイルの結合テスト、合成エラー、過去の実画面を同じ検証として数えない。過去のSpikeの成功は本番経路の代替にしない。

| 条件 | 本番のテスト・記録 | 判断と限界 |
| --- | --- | --- |
| 正常な設定・変更・解除・再接続 | [取り込み](./cover-import-api.md)の`TestCoverImportRealFileAndDB`、[解除](./cover-remove-api.md)と`TestSelectWorkCoverAPI`。元画像保持・ready記録・現在参照・旧管理画像削除を確認 | 結合確認。実画面の設定・取消・変更・解除・再起動は[操作UI](./cover-actions-ui.md)と[開発モード](./dev-cover-operations.md)を参照 |
| 生成失敗・元画像読取失敗 | `internal/thumbnail/thumbnail_test.go`の`TestInvalidInputs`・`TestReadFailureAndConcurrentCalls`、`cover_import_test.go`の`TestCoverImportCancelAndSourceFailureDoNotPrepare` | 部品の生成拒否と、取り込みの元画像Open失敗は確認済み。生成途中の拒否から旧画像・旧参照保持までの結合は不足（#192） |
| 新規保存・同期・容量不足相当 | [新規ファイル](./new-cover-file.md)の`TestWriteNewCoverAndReopen`・`TestCoverWriteFailures`はCREATE_NEW、再取得照合、短い書込、書込/同期エラーを確認 | エラー注入は書込部品のみ。取り込み全体で通常の保存/同期失敗・容量不足相当を起こした保全は不足（#192）。実ディスク満杯の確認ではない |
| Close・解放失敗と停止 | `TestCoverDirectoryCloseAttemptsAllAndCachesFailure`、`TestCoverReleaseClassification`、`TestCoverImportSourceReleaseStopsBeforePrepare`・`TestCoverImportReleaseOutcomesAndStageStop`、[読取](./current-cover-reader.md)の`TestCoverReaderRejectsWithoutChangingReference` | 合成Close/解放失敗の分類、段階停止、確定後成功と警告、失敗時データ破棄を確認。実OS Close故障・全ハンドルの実故障注入ではない |
| DB失敗・Commit結果不明・確定後失敗 | `TestWorkCoverDatabaseFailures`、`TestCoverImportRealCommitBusyPreservesOutput`、`TestCoverRemoveRealCommitBusyKeepsImage`、`TestCoverAPIRealCommitBusyPreservesBothImages`、[Store停止](./store-db-admission.md) | トリガー失敗・実SQLite読取競合・合成返却失敗を区別。結果不明時の保全と停止を確認。競合ケースの再接続結果を全Commit失敗の結果へ一般化しない |
| 削除拒否・共有参照・確定後後処理失敗 | [旧画像削除](./verified-cover-cleanup.md)の`TestDeleteVerifiedCoverRefusals`・`TestCoverCleanupPostCommitOutcomes`、Storageの未参照判定と条件付きDELETE | 変更と解除の両経路で、共有・大小文字/別表記・改変・reserved・記録なし・削除後DB失敗・停止を確認。新参照を戻さない。削除拒否は実ファイル条件と合成解放エラーを区別 |
| 起動時候補確認・参照取得失敗 | [起動時整理](./startup-cover-cleanup.md)の`TestStartupCleanupRealFilesAndFailures` | 全候補事前確認、後半の参照取得失敗でも削除なし、reserved/未知ファイル/参照中保持、作品全項目保持を結合確認 |
| ファイル削除後の記録整理失敗・再接続 | 上記の`forget-failure`と`TestCoverCleanupPostCommitOutcomes` | トリガーで記録DELETEを失敗させ、再接続後の安全な不存在確認・記録整理を確認。正常に戻ったDBの再接続であり、プロセス途中終了の再現ではない |
| 処理途中の異常終了と次回整理 | 上記の残存状態・再接続テスト、[起動排他](./storage-instance-lock.md)の`TestStorageLockAcrossProcesses`、[#171の終了](./webview-focus-exit.md) | 画像処理の予約・保存・参照切替・削除境界で子プロセスを終了し、次の本番整理へ渡す根拠が不足（#193）。ロックのみの強制終了や選択中終了で代替しない |
| 同作品・選択枠・別作品の競合 | `TestCoverImportSelectionExclusionAndClose`・`TestCoverImportReferenceWait`・`TestCoverRemoveReferenceWaitAndExclusion`、[共通参照排他](./cover-reference-exclusion.md)の反復テスト | 同作品拒否、選択枠、別作品受付、参照待機中の停止/取消、登録との排他を確認。全スケジューリング・待機公平性・race detectorの証明ではない |
| 同じ保存先の二重起動・別保存先・解放 | [起動排他](./storage-instance-lock.md)の子プロセステストと6観測のproduction exe記録 | 保存先排他、正常/強制終了後の再取得を確認。画像処理途中終了の保全まで広げない |
| 限定配信・任意パス拒否・リンク拒否 | [HTTP](./cover-http-delivery.md)の両Wailsハンドラー、[読取境界](./verified-cover-read.md)、[Application読取](./current-cover-reader.md) | 固定ID、エンコード/不正クエリ、no-store/nosniff、改変/リンク/サイズ拒否、上限と解放、送信中のApp寿命を自動確認。悪意ある同一ユーザーとの完全な攻防の保証ではない |
| 開発/productionの表示・キャッシュ・欠損 | [再取得修正](./cover-image-refresh.md)で両環境の青色↔グラデーションを確認。[表示](./display-current-cover.md)と操作UIで欠損・未設定・回復を確認 | 過去の旧画像残留は#174で修正・再確認済み。破損画像の拒否はHTTP/DOMまでで、実画面の仮表示・参照保持・回復は不足（#194） |

## 未確認の追跡と最終判断の条件

| 追跡 | 埋める根拠 |
| --- | --- |
| [#192](https://github.com/asam-masa/manga-update-manager/issues/192) | 生成・通常保存・容量不足相当・同期失敗から取り込み全体の保全と未確認残置の保持 |
| [#193](https://github.com/asam-masa/manga-update-manager/issues/193) | 自身が起動した検証子プロセスの画像処理境界での途中終了、再接続・次回整理・現在参照と元画像保持 |
| [#194](https://github.com/asam-masa/manga-update-manager/issues/194) | 破損管理画像の実WebView2仮表示・正常カード分離・参照保持・明示再読み込みでの回復 |

これらは現在の不具合を実証した指摘ではなく、完了条件の根拠不足である。後続で不具合を検出した場合は原因と変更範囲を別途判断する。今回の照合Issue完了を、後続検証や親#42の完了と扱わない。

#176は2026-10-21時点でOPEN。画像選択中の無効な親への強制前面化での終了は未修正であり、[再現条件・回避範囲](./webview-focus-exit.md)を維持する。通常操作で再現したと確認した場合、または保全・通常操作を阻害する新たな根拠が出た場合は、ADRの必須条件として再評価する。今回のテスト成功でこの問題を解消したとは扱わない。

上記3件の結果を照合し、四項目の根拠と残るリスクを通常レビュー・必要な独立レビューで確認してから、別PRでVerifiedへの変更を判断する。実OSディスク故障・電源断・全ファイルシステム・全競合を実証したとはしない。一方、通常の保存失敗や途中終了の確認を、その制約を理由に省略しない。
