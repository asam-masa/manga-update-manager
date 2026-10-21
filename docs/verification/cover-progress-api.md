# 画像処理の状態取得API

Issue #186、親#183。[ADR-0017](../adr/0017-poll-cover-progress-snapshots.md)で承認した定期取得方式のWails接続を扱う。画面の定期取得と10秒表示はまだ追加しない。

## 契約

- `LoadCoverProgress()`は観測済みの進行中要求の配列を返す。未開始・完了済み・観測対象なしは`[]`。状態はメモリだけに保持する。
- 各要素は`operationID`・`workID`・`kind`（`select`/`remove`/`cleanup`）・`stage`（#184の固定コード）・`timed`・`elapsedMilliseconds`。パス、内部エラー、画像、絶対時刻は含めない。経過時間の負値は0へ丸める。
- 設定・変更は既存の`SelectWorkCoverWithOperation`、解除は追加の`RemoveWorkCoverWithOperation(workID, operationID)`を使う。操作IDは小文字16進32文字。進行中の同一IDは上書きせず`cover_busy`で拒否する。終わったIDの再利用は可能だが、画面は要求ごとに新規IDを作る。
- 起動時整理は`LoadCoverWarnings`の一回の実行に対応し、`operationID="startup-cleanup"`、`workID=0`。この固定IDは利用者指定のID形式と重ならない。未開始予約だけでは公開しない。呼出しの合流は従来のsync.Onceで行う。
- `select`の選択待ちは`timed=false`、経過時間0。生成段階から計測する。`remove`・`cleanup`は最初の実段階から計測する。途中の元画像解放やDB更新でもリセットしない。
- API処理終了時に記録を除く。空配列や要素の欠落は完了・成功・故障の証明ではない。APIのPromise完了前後に観測結果が空になる競合も許容し、結果と警告は操作APIで判断する。旧SelectWorkCover/RemoveWorkCoverは従来どおり動くが観測対象には追加しない。
- 状態のコピーは専用mutexだけを使い、Appの寿命ロック・DB・画像処理を待たない。I/Oや送信はmutex内でも段階コールバック内でも行わない。これはOSスケジューリングや短いmutex待ちまでゼロにする保証ではない。

## 自動テストと限界

`TestCoverProgressTimingIdentityAndCopies`は制御時計で選択待ち除外・累積時間・コピー・重複ID・ID再利用後の古い通知/終了の無効化・空配列を確認する。`TestCoverProgressReadDoesNotWaitForAppLifetime`はAppの書込みロック中も状態取得できることを確認する。

`TestCoverProgressAPICancelRejectAndLegacy`は実APIの取消・不在作品・不正ID・進行中IDの再使用・旧API・解除結果を確認する。`TestCoverProgressDuringRealReferenceUpdate`は実PNGと一時SQLiteの参照更新中に安全な状態だけを取得し、合成DB更新失敗後も記録を除くことを確認する。`TestCoverProgressConcurrentReadAndFinish`は並行更新・取得・終了を行う。既存の起動時整理テストでも、処理をチャネルで待機させて`cleanup`の取得と終了後の削除を確認する。race detectorを実行した結果とは扱わない。

後続画面では処理中だけ約1秒ごとに取得し、要求を重ねず、古い応答を捨てる。作品IDと操作IDを照合し、最終結果後の遅い応答で案内を復活させない。取得失敗を成功・停止・フリーズと扱わず、確認できない状態を推測しない。画面と実WebView2の確認は親#183に残す。ADR-0013・0017はPendingを維持する。
