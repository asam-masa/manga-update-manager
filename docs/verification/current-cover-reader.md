# 現在画像のApplication読み取り

Issue #161。正本は[画像保存・配信契約](../cover-persistence.md#ファイルと配信の安全条件)と[ADR-0013](../adr/0013-define-cover-operation-boundaries.md)。HTTP・App・画面には未接続である。

## 処理と保護範囲

`CoverReader.Read`は正の作品IDだけを受け取り、読み取り操作票、参照排他、作品の現在参照、ready管理記録、実ファイル検証読み取り、静止PNG検査の順で進む。任意パスを受け取らず、小文字32桁の固定管理パスと記録のID・パス一致を確認する。PNG検査は2 MiB以下・各辺600px以下を展開前に確認し、コンテナ/CRC/末尾・アニメーション・デコード失敗を拒否する。

読み取り専用の`BeginReadStage`は既存の変更段階受付と区別し、同じmutexでStopと順序付ける。参照排他の待機後も次の段階で停止を再確認する。通常終了は新規読み取りを拒否するが、開始済み読み取りをキャンセルせず最終解放まで待つ。Stopは後続段階を拒否し、実行中I/Oを強制中断しない。

`platform.ReadVerifiedCover`の全解放結果を段階内で通知し、解放失敗・DB停止・危険な保存先では画像受付を停止する。エラー時はデータを破棄する。通知後に参照排他を返し、操作票を終える。参照と管理記録、ファイル、作品日時は変更しない。PNGが破損していてもDB参照を自動解除しない。

## 自動確認

- `TestCoverReaderCurrentReferenceAndPreservation`: 実PNG・一時SQLiteで設定、現在参照の再取得、変更後の取得、作品全項目の保持、解除後の未設定を確認する。
- `TestCoverReaderRejectsWithoutChangingReference`: legacy参照、reserved、記録なし、欠損、改変、合成不正PNG、DBエラー/停止、解放失敗、危険な保存先、読取後停止、キャンセルでデータを返さず参照を保持する。停止対象では新規受付も拒否する。
- `TestCoverReaderCloseWaitsAndStopsAdmission`: 読み取りを待機させ、終了は解放前に完了せず、新規受付を拒否し、開始済み読み取りは終了要求だけで失敗しない。
- `TestCoverReaderStopsBeforeNextDBStage`: 作品取得後の停止で次の記録取得を開始しない。
- `TestCoverReadStagesStopAndClose`と既存段階テスト: 読み取り/変更票の区別、重複段階、実行中終了拒否、終了待ち、停止後段階の拒否を確認する。
- `TestValidateCover`: 正常/600px上限、幅/高さ超過、空、非PNG、サイズ超過、切断、CRC破損、末尾データ、APNGを確認する。

## 限界と後続

合成エラーは実OS/DB故障や電源断の再現ではない。実WebView2とHTTP経路、AppのDB寿命との接続、待機/同時リクエスト総量、画面の更新と仮表示は未検証である。今回の読み取りは共通参照排他をPNG検査完了まで保持するため、同時変更はその間待機する。返却後の通信は排他外とする。ADR-0013はPendingを維持する。
