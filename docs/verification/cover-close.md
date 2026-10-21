# 画像処理中の通常終了を拒否する

Issue #189、親#42。正本は[保存と通常終了の契約](../cover-persistence.md#close失敗と通常終了)。終了要求で画像処理を中断・完了させず、処理結果を確認した後、もう一度閉じる方式を検証する。終了予約や強制終了は追加しない。

## 自動テストと段階の対応

`TestCoverCloseRejectedAtDependencyBoundaries`は一時SQLiteと実PNGを使い、変更・解除・起動時整理を最後まで実行する。各依存呼出しから通常終了を2回要求し、ErrCoverBusyとnilの待機通知を確認する。段階通知から受付へ再入しない。元画像Readだけは呼出し回数がデコーダーに依存するため連続分をまとめ、それ以外は依存呼出し順序も照合する。

| 段階 | 確認する実処理境界 |
| --- | --- |
| 確認 | 変更・解除のWorkByID、整理のListManagedCoversとUnusedManagedCover |
| 選択待ち | 変更のchoose |
| 生成 | 元画像のOpen・Read |
| 保存準備 | 変更・解除・整理のprepare |
| 保存 | 変更のReserveManagedCover・write |
| 参照排他待ち | 別テストで変更・解除・整理の参照ゲートを保持 |
| DB更新 | 変更のConfirmManagedCover・SetWorkCover、解除のRemoveWorkCover |
| 後処理 | 変更・解除・整理のUnusedManagedCover・delete・ForgetUnusedManagedCover |
| 解放 | 元画像Close、変更・解除・整理の保持フォルダーClose |

`TestCoverCloseRejectedWhileWaitingForReferences`は参照ゲートを外から保持し、変更・解除・整理それぞれの待機段階を観測する。通知はバッファー付きチャネルへ記録するだけで、終了要求はテスト側から行う。拒否後も操作が未完了であること、ゲート解放後に処理が完了し、再要求が終了を許可することを確認する。依存境界テストでも最後に終了を許可し、その後の新規操作を拒否する。

`TestBeforeCloseKeepsRunningCoverAPIAndDatabase`は実Appの設定・解除APIとDB更新境界を接続する。正常・合成更新失敗で、beforeCloseの拒否と案内2回、ListWorksの利用継続、API結果の返却、観測状態の消去、結果後の終了許可を確認する。ここでは案内関数を置き換え、Wailsのモーダル表示自体は実画面で確認する。

既存の`TestStartupCleanupWaitsJoinsAndKeepsDatabaseUntilRelease`は開始済み整理中のbeforeClose拒否とshutdownのDB終了待ちを扱う。`TestCoverReadStagesStopAndClose`は表示読取だけの終了待ちを扱い、画像変更中の即時拒否と区別する。全テスト成功は競合の全順序や実OS故障の証明ではない。

## 実WebView2（2026-10-21）

Windows、Go 1.26.5、Wails CLI v2.16.0の開発モード。保存先・TEMP/TMPを`H:\appdata_local_tmp\mum-progress-5ad93268b15d423dba38e21df9fd9502`へ分離し、合成3作品と元画像だけを使用した。PID18224の所有ウィンドウだけを操作した。一時的なRepositoryラッパーで整理の一覧確認、画像変更・解除のDB参照更新を待機させ、終了制御・案内・長時間表示は本番コードを使った。

- 起動時整理の確認中、変更のDB更新中、解除のDB更新中に通常終了のWM_CLOSEを送り、アプリが終了せず「画像の処理中は終了できません。処理結果を確認してから、もう一度閉じてください。」とOKを表示することをUI Automationで確認した。長時間表示では整理80秒、変更28秒、解除68秒の観測値を確認した。
- OKを押しても待機中の処理は残り、検証専用の待機解除後に通常の結果へ進んだ。変更は「画像を設定しました」、解除は「画像を外しました」と未設定表示になり、長時間案内は消えた。拒否した終了要求が予約され、完了時に自動終了することもなかった。
- 解除完了後に再びWM_CLOSEを送るとプロセスが終了した。SQLite quick_check=ok、3作品、起動ロック再取得、元画像ハッシュを確認した。元画像SHA-256は解除待機中と終了後で`C97C8C8535E7B1C7DA5962C8C3C7ED19ADE26E9ABBA2C1E9665A88F9F1DFFF42`が一致した。

最初のUI Automation WindowPattern.Closeでは終了案内を観測できなかったため、所有プロセスのMainWindowHandleにWM_CLOSEを送る補助へ変更して再検証した。PrintWindowの親ウィンドウ撮影は別モーダルを含まないため、案内の確認根拠は所有UI要素の文言とOK操作であり、撮影だけとは扱わない。SetForegroundWindow、ACL変更、利用者データの操作は行っていない。一時ラッパー・起動呼出しは除去し、検証データ・補助・撮影はGitに含めない。

## 検証範囲と残件

全段階の依存境界・参照待機を自動確認し、実画面は整理と変更・解除の代表的な長時間待機で終了要求・案内・処理継続・結果後の終了を確認した。実画面で全段階・全競合順序を再現したものではない。選択ダイアログ中の無効な親の強制前面化は行わず、#176を解消したとは扱わない。未改変production exeの実I/Oフリーズ、電源断、OSによる強制終了、Close故障の全条件は未検証である。

終了拒否の検証条件について本記録を根拠とする。保存・削除・復旧の最終照合は別作業であり、ADR-0013全体はPendingを維持する。
