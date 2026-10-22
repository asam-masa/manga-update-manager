# 編集保存中の通常終了と再起動

Issue #219、親#205。2026-10-22、基準main `a663021cf9057e5d324cce0d4179579e01285fd8`で確認した。正本は[ADR-0018](../adr/0018-edit-and-delete-works.md)、先行の[編集API](./work-edit-api.md)と[編集画面](./work-edit-dialog.md)の検証を補う。

## 方法と通常版との差

通常の保存は短いため、Goの`-overlay`で確認用の起動入口だけを置き換えた計測用exeを使った。起動時に既存WorkEditorの保存Repositoryを一度だけラップし、APIが`BeginEdit`を取得した後、実Storeへ入る前で待機させる。`entered`の新規作成と`release`の未作成を確認し、終了要求後に解除する。最大60秒で保存せず失敗する制限を設け、今回は期限切れではなく解除後の成功まで観測した。

通常版との差は、専用領域の起動前確認、ウィンドウタイトル、startup後のRepository差替え、起動エラーの出力である。画面・入力正規化・共通受付・`OnBeforeClose`・案内・`OnShutdown`・API結果変換・実SQLite保存は既存経路を使う。成功作品を合成して返さない。これは無改変のproduction exeで長時間保存を再現した試験でも、実OS遅延・物理故障の試験でもない。

新規GUIDの専用APPDATA/TEMPと合成3作品・画像だけを使用した。補助コード・マーカー・exe・データ・画像はGit対象外の`build/bin`に保持し、通常ソース、普段の保存先・exe、元画像は変更しない。パスの文字列検査は敵対的なjunction差替え等への防御を実証したものではない。

## 観測結果

UI Automationで一つの合成作品のタイトルを変更し、次を順に確認した。人による目視・マウス操作の検証とは区別する。

| 操作・状態 | 結果 |
| --- | --- |
| 保存境界へ到達、未解除、保存中ボタン | 新規enteredとrelease不存在、保存ボタンの無効化を確認 |
| 保存中に通常終了要求 | 「作品情報を保存中です」と「処理結果を確認してから、もう一度閉じてください」の案内。プロセスは生存 |
| OK後に待機解除 | 実Storeでタイトルを保存し、成功通知と編集済みカードを表示。成功後の画面も撮影・確認 |
| 終了要求が取消・自動終了にならない | 解除後に成功し、画面が操作可能。再度編集を開いて保存値を確認できた |
| 結果後にもう一度通常終了 | 計測用プロセスの終了を確認 |
| 待機点なしの通常ビルドで同じ専用保存先を再起動 | 編集済みタイトル・URL・サイト名・メモをUIAで確認し、キャンセル後に通常終了。保持対象列全ての前後比較を今回新たに行ったとは扱わない |

計測用exeの再起動ではenteredが残るため最初の再編集が拒否される。再起動保持の確認は待機点のない通常ビルドで行い、この補助仕様を通常アプリの制約として扱わない。データは削除せず保持した。

## 対象版と補助物

- 計測用ビルド: `go build '-overlay=build/bin/219-overlay.json' '-tags=desktop,production' -ldflags '-H windowsgui' -o build/bin/edit-close-probe.exe .`
- 計測用exe SHA-256: `994918ee56eef9a95dca36d3a90c029cefa3ead820de2f592c82558954fb723e`
- overlay起動入口 SHA-256: `e87583d33ab4dc4f9194f043676cdd1cc628c1462fb02a144229bd80bfaadbce`
- overlay対応表 SHA-256: `d3a923897ad95c1e1d5722194aad898bdfc775663da4ae21093d8ec60389f38a`
- 専用起動スクリプト SHA-256: `e781cefaaeb79b7fe576bb3e1639fa88edee61928452cbbbaf48310f68f9ddc3`
- 通常ビルド: `go run github.com/wailsapp/wails/v2/cmd/wails@v2.16.0 build -s -m -nosyncgomod -o work-edit-close-normal-check.exe`
- 通常exe SHA-256: `59fd8e22c8e934f8fcfb598c3a23925631546bca425ab7b55f0583a81ae16975`

初回ビルドのPowerShellオプション分割でoverlayファイルを開けず失敗した。上記の引用した引数で再実行し成功した。終了検証は成功した計測用exeで実施している。補助物はローカル保持であり、新規checkoutだけで自動再実行できるテストの追加ではない。通常/独立レビューと最終文書版はPRに記録する。

## 残る限界

保存結果不明・確定後の解放失敗は先行の合成境界テストであり、今回実WebView2から物理Commit/Closeを故障させてはいない。電源断、強制終了、全競合順序、実OSの長時間遅延、利用者データは対象外。初期フォーカスの独立した観測も追加していない。作品削除は未実装で、ADR-0018全体のPendingを維持する。
