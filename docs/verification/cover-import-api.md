# 画像取り込みAPIの結合検証

Issue #143。正本は[画像保存契約](../cover-persistence.md#現在の段階実装)と[ADR-0013](../adr/0013-define-cover-operation-boundaries.md)。利用者データではなく、一時SQLite・一時領域・合成PNGで確認する。

## 接続と成功境界

`App.SelectWorkCover`は作品IDだけを受け、Applicationが処理順序を決める。OS選択はWails、元画像の読み取りと管理領域保持・新規保存はplatform、画像変換はthumbnail、管理記録と作品参照はStorageが担当する。新しい依存は追加しない。

選択のキャンセルは`cancelled: true, work: null, warnings: []`。正常完了は更新作品と警告配列を返す。日時は既存DTOの日本時間RFC 3339を使用する。失敗は固定した`code`と日本語文言でrejectし、元パスや内部診断を返さない。

元画像は読み取り専用で開き、生成とCloseが成功するまで管理領域を準備しない。祖先の外側の保持を予約から参照更新・最後の解放まで継続する。各段階前に受付停止を確認し、戻ったエラーを段階終了前に通知する。操作票の完了は最終解放の後である。

旧画像の削除・失敗出力の回収は行わない。旧参照があれば`cover_cleanup_deferred`を返す。Commit不明時に自動再試行・確認読取・書戻しはしない。確定後の解放失敗で成功済み参照を取り消さない。別作品の既存処理を強制中断せず、安全な段階境界で止める。

## 自動確認

- `TestCoverImportRealFileAndDB`: 実PNG生成・Windows保存・識別情報確認・ready記録・参照更新・DB再接続。
- `TestCoverImportCancelAndSourceFailureDoNotPrepare`: キャンセルと読取失敗で準備なし、旧参照保持。
- `TestCoverImportSourceReleaseStopsBeforePrepare`: 合成の元画像Close失敗で画像停止、保存なし、DB読取は継続。
- `TestCoverImportReleaseOutcomesAndStageStop`: 停止後の保存拒否、書込解放失敗と確定後フォルダー解放失敗の分類、解放中の終了拒否。
- `TestCoverImportRealCommitBusyPreservesOutput`: 管理記録の実確定後に別接続の読取を保持し、実参照CommitをBUSYにする。成功を返さず新規PNGを保持し、Storeと画像受付が停止する。
- `TestCoverImportCommittedWarningDoesNotBecomeFailure`: 実参照確定後に返却失敗の印を合成し、成功と警告を維持する。実接続返却故障の再現ではない。
- `TestCoverImportSelectionExclusionAndClose`: 同作品と選択枠の多重要求を拒否し、キャンセル後に終了できる。
- `TestSelectWorkCoverAPI`: 選択部分だけを差し替え、実元ファイルからAPI結果まで確認する。元画像の内容保持、再設定時の旧生成画像保持、空配列・null、停止通知を確認する。
- `TestBeforeCloseWithImageOperation`、`TestCoverAPIErrorSafety`、`TestOpenCoverSource`: 終了拒否と再操作、秘密を含む内部診断の非公開、通常ファイル・キャンセルを確認する。

## 限界と後続

実OSダイアログ、WebView2からのAPI操作と終了案内、電源断・実I/O故障、全ドライブ種別は未確認。選択と案内はテストで差し替えている。[Wails終了コールバック](https://wails.io/docs/reference/options/#onbeforeclose)に加え、採用版v2.16.0の`Frontend.Quit`でtrueが終了を防ぐことを静的確認した。実画面での確認は#43の接続時に行う。

共有参照と削除の排他、物理削除、配信、起動時整理、物理DB隔離、長時間処理の表示は未実装。画像UIと配信がないため、現画面で画像を設定・表示できる段階ではない。残置で容量が増える制約を解消したとは扱わず、ADR-0013はPendingを維持する。
