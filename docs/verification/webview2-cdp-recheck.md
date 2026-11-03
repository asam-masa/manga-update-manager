# 現行WailsのCDP接続再調査

## 結果

2026-11-03、Issue [#362](https://github.com/asam-masa/manga-update-manager/issues/362)を利用者承認の30分上限で調査した。Wails v2.17.1 / go-webview2 v1.0.22では、確認した公開設定からPlaywright用のCDP引数を渡す経路は見つからなかった。同じ阻害条件の起動試験は反復せず、ソース調査で終了した。実接続・実アプリの操作・終了検証は新たに実施していない。

## 確認した経路

- [Playwright公式手順](https://playwright.dev/docs/webview2)は環境変数、または環境作成時の追加引数からデバッグポートを指定し、connectOverCDPで接続する。
- [loaderのenv_create.go](https://github.com/wailsapp/go-webview2/blob/v1.0.22/webviewloader/env_create.go)はinitと環境作成時にpreventEnvAndRegistryOverridesを呼び、WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTSとWEBVIEW2_USER_DATA_FOLDERを空にする。起動後の環境変数復元だけでは環境作成時の消去を回避できない。
- [WailsのWindows Options](https://github.com/wailsapp/wails/blob/v2.17.1/pkg/options/windows/windows.go)はWebviewUserDataPathとWebviewBrowserPathを公開するが、任意の追加引数やCDPポートの設定は公開していない。
- [Wails内部frontend](https://github.com/wailsapp/wails/blob/v2.17.1/internal/frontend/desktop/windows/frontend.go)は内部ChromiumへGPU等の限定された引数を渡す。loaderにWithAdditionalBrowserArgumentsが存在することは、Wailsアプリから公開Optionsで利用できることを意味しない。

go.modとローカルの対象版モジュールソースを照合した。[過去#35](./webview2-playwright-spike.md)はWails v2.14.0で環境変数方式を実測し接続不可だった。今回Wailsはv2.17.1へ変わったが、loader版と上記の消去処理・公開引数設定の欠如は同じだった。DevToolsの表示とCDP接続口の公開は別である。

## 実施しなかったことと後続案

製品・依存・OS・レジストリを変更せず、GUI、ポート、利用者DB、通常プロファイルへアクセスしていない。新しい調査コード・合成DB・プロファイルは生成していない。過去のスクリプトは変更せず再現資料として保持する。すべての接続方式が不可能と証明した結果ではない。

現時点では導入済みのブラウザーPlaywrightとGo/SQLite試験を維持し、実WebView2・IME・ネイティブ選択・実保存に影響する変更は理由を添えて実機確認する。実Wails接続を再度優先する場合は、上流の公開設定追加の調査・提案などを別Issueで判断する。fork・loaderの直接利用・フレームワーク変更は採用も試行もしていない。本Spikeからそれらの変更を承認済みとは扱わない。
