# Wailsアプリの画面とブラウザーの境界

## 学ぶこと

Wailsアプリの独立した画面、WebView2、開発用URL、漫画ページを開く外部ブラウザーは、それぞれ役割が異なります。この境界を理解すると、画面表示の問題とFirefox連携の問題を分けて調査できます。

## 実行時の構成

利用者から見える画面は、独立したWindowsアプリのウィンドウです。アドレスバーやタブは表示されません。内部ではReactが画面を構成し、WebView2がHTMLとCSSを描画します。GoはWailsのバインディングを通じてアプリケーション処理を提供します。

```text
Manga Update Managerのウィンドウ
├─ WebView2: Reactで作った画面を描画
└─ Go: データ管理やOS連携を担当
```

WebView2は画面の描画部品であり、漫画ページを閲覧するブラウザーではありません。作品URLは既定ブラウザーで開く方針のため、Windowsの既定ブラウザーがFirefoxならFirefoxのタブで開きます。

## 開発モード

`wails dev`では、完成版にはない開発用処理も動きます。

- GoとTypeScriptのコンパイル
- Vite開発サーバー
- ファイル変更の監視
- Wails開発サーバー
- WebView2によるアプリ画面

このため、起動直後は完成版よりCPU負荷が高くなる場合があります。Viteの既定ポートが使用中の場合は、5173番から5174番などへ自動的に切り替わります。

Wailsが表示する開発用URLをFirefoxで開くと、同じReact画面をブラウザーから確認できます。このURLは開発時だけ使用し、完成版アプリの利用には必要ありません。

## 今回の切り分け

Codex環境から`wails dev`を起動したときは、WebView2が`AppData`配下へデータディレクトリを作成できない警告が表示されました。一方、通常のPowerShellから同じコマンドを実行すると、WebView2環境の作成に成功しました。

この結果から、アプリの設定ではなくCodex実行環境の書き込み制限が原因と判断しました。通常環境でも再現する場合に限り、Wailsの`WebviewUserDataPath`設定を検討します。

Firefoxで開いた開発用画面に「Goバックエンド: 接続済み」と表示されたため、Reactの表示だけでなく、TypeScriptからGoの`App.Status`を呼び出せることも確認できました。

## 正本と根拠

- [ADR-0001: デスクトップアプリケーション基盤を選定する](../adr/0001-select-desktop-application-foundation.md)
- [ADR-0003: フロントエンド構成を選定する](../adr/0003-select-frontend-foundation.md)
- [PR #16: Wailsアプリの雛形を生成する](https://github.com/asam-masa/manga-update-manager/pull/16)
- [Wails v2 Windows Guide](https://v2.wails.io/docs/guides/windows/)
