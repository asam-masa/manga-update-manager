# 実WailsアプリへのPlaywright接続調査

## 結果

2026-10-10のIssue [#35](https://github.com/asam-masa/manga-update-manager/issues/35)では、現在のWailsで公式の環境変数方式によるCDP接続口を確認できませんでした。アプリは起動しましたが、15秒間の確認で接続口は開きませんでした。Playwrightによる実アプリE2Eは未実施です。

通常アプリと依存ライブラリは変更していません。接続の前提が満たされないため、テスト専用のGo実装、Playwright依存、E2Eテスト、CIも追加していません。これは調査結果であり、E2Eが不要という判断やPlaywrightの採用Decisionではありません。

## 調査範囲と環境

- この作業セッションを期限とし、既存のGo WebView2 loaderで環境変数方式を検証しました。
- Windows、Go設定1.25.0、Wails v2.14.0、go-webview2 v1.0.22を使用しました。
- 通常のproductionビルドを確認専用の名前で生成しました。テスト用タグやデバッグコードは加えていません。
- 登録から実Go API・SQLiteまでの一連の操作、実IME、既定ブラウザー連携は対象外です。

## 接続できなかった根拠

[Playwright公式のWebView2手順](https://playwright.dev/docs/webview2)は、`WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`へデバッグポートを指定し、`connectOverCDP`で接続する方式を説明しています。

現在の依存ソースでは、次の経路を確認しました。

1. go-webview2の[`webviewloader/env_create.go`](https://github.com/wailsapp/go-webview2/blob/v1.0.22/webviewloader/env_create.go)は、パッケージ初期化とWebView2環境作成時に`preventEnvAndRegistryOverrides`を呼びます。
2. この処理は`WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS`と`WEBVIEW2_USER_DATA_FOLDER`を空にします。アプリ起動後に環境変数を戻すだけでも、環境作成時に再び消去されます。
3. Wailsの[`pkg/options/windows/windows.go`](https://github.com/wailsapp/wails/blob/v2.14.0/pkg/options/windows/windows.go)には、任意のブラウザー引数やデバッグポートを指定する公開設定がありません。
4. Wailsの`internal/frontend/desktop/windows/frontend.go`は内部のChromiumへ限定された引数だけを渡します。go-webview2の`pkg/edge/chromium.go`には引数フィールドがありますが、アプリからWailsの公開設定を通じて指定できません。

したがって、現在の公開設定と環境変数だけでは公式方式を構成できないと判断できます。すべての接続方法が不可能という結論ではありません。依存ライブラリの直接変更、非公開実装への介入、別loaderや別フレームワークへの切り替えは試していません。

## 再現手順

リポジトリのルートでPowerShell 7を使用します。GUI起動、Goキャッシュへの書き込みなどには環境の承認が必要です。

```powershell
wails build -o issue35-check.exe
./tools/spikes/webview2-cdp.ps1
```

`tools/spikes/webview2-cdp.ps1`は接続口の事前確認用で、Playwrightテストそのものではありません。確認対象は`build/bin`内の確認専用exeです。同時に複数実行しません。

- 実行ごとに`build/bin/webview2-spike-<GUID>`を作ります。
- 子プロセスだけの`APPDATA`をこの領域へ向けます。親PowerShellの環境変数や通常のアプリデータは変更しません。
- DBは`appdata/MangaUpdateManager/manga.sqlite`、実際のWebView2データはWailsの既定動作により`appdata/issue35-check.exe`へ保存されます。消去される`WEBVIEW2_USER_DATA_FOLDER`には依存しません。
- ループバックの空きポートを確認し、公式のデバッグ引数を渡して`/json/version`を最大15秒間確認します。接続できた場合も、専用データのパスを持つプロセスがポートを使用していることを照合します。
- 終了処理はこの実行で起動したプロセスだけを対象にします。通常終了を試し、必要な場合はその子プロセスを含めて終了します。
- 対象データのパスを持つWebView2プロセスの残存数を確認します。他のアプリを名前だけで終了しません。
- 調査用DBとWebView2データは削除せず、Git対象外の`build/bin`へ残します。リポジトリへ追加しません。

CDP接続不可も有効な調査結果として終了コード0を返します。起動・データ分離・終了処理の確認に失敗した場合は2を返します。`cdpReachable: false`をE2E成功と解釈しないでください。

## 実測

| 確認項目 | 結果 |
| --- | --- |
| WebView2環境作成 | 成功ログを確認 |
| 15秒後のアプリプロセス | 起動中 |
| CDP接続口 | 接続不可 |
| 専用領域のDB | 作成済み |
| 専用領域のWebView2データ | 作成済み |
| 終了後の対象WebView2プロセス | 0件 |

最終版でも同じ結果を確認しました。PowerShell構文検査は成功し、`build/bin`外のファイルを指定すると起動前に拒否されることも確認しました。CDP接続成功後の所有者照合と、強制終了へ進む分岐は未実測です。

通常productionビルドへデバッグ設定は追加していません。今回の指定ポートが開かなかったことは確認していますが、OS全体の全ポートやすべての起動条件を検証したわけではありません。

## 検証コードの扱いと後続案

接続口の事前確認スクリプトだけを再現資料として残します。通常ビルドや既存テストからは呼びません。アプリやloaderを更新した際に同じ前提を確認できます。

後続のFeatureへ直ちに進むのではなく、次の選択をgrillingで確認することを推奨します。まだ採用していません。

- 実WailsアプリのE2Eを優先する場合、Wails側で公開されたブラウザー引数設定を利用できるようにする方法を別Spikeで調べます。依存更新や上流への提案は個別に範囲を決めます。
- 先に画面のブラウザーE2Eを追加する場合、API差し替えを使う検証だと明記します。実Wails APIとSQLiteの結合を検証したことにはしません。
- 今は既存のGo・SQLite・DOMテストと実機手動確認を維持し、機能追加を進める選択もあります。

テスト構成を採用する際は、後続Issueで受け入れ条件を決め、必要なADRを作成します。このSpikeでは承認前の推奨をDecisionへ記載しません。
