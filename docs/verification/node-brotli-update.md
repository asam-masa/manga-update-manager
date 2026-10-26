# 開発用Node.jsのBrotli修正と確認範囲

Issue #274。2026-10-26、基準main `c13bea972fb78b2c7d7e52b066509a5ce946b2b8`のコード・設定とローカル環境を照合した。対象はCVE-2026-111497であり、アプリ全体の安全性監査ではない。

## 公式情報と判断

[Node.js公式の24.22.1リリース](https://nodejs.org/en/blog/release/v24.22.1)は、BrotliデコーダーのCVE-2026-111497の修正を含む。[セキュリティ告知](https://nodejs.org/en/blog/vulnerability/october-2026-security-releases)では、攻撃者が供給するBrotliデータをnode:zlibで展開する場合のメモリ破壊とコード実行の可能性を説明している。

開発PCのNode.jsは24.18.0で、修正前の版だった。jsdomのリソース取得部品にはcreateBrotliDecompressの呼び出しがある。ただし、現在のテストから攻撃者の応答が到達する経路は確定していない。古い部品の存在と、このプロジェクトで実際に攻撃が成立することを区別し、開発用ランタイムを修正版へ更新した。

## 更新と検証結果

- 既存のwingetパッケージ`OpenJS.NodeJS.LTS`を24.18.0から24.22.1へ更新した。公式配布元のインストーラーに対するハッシュ検証とインストール完了を確認した。
- 新しく起動したプロセスで`node --version`はv24.22.1、`npm --version`は11.19.1だった。
- `npm --prefix frontend test -- --reporter=dot`: 19ファイル・207テスト成功。
- `npm --prefix frontend run build`: TypeScriptの静的検査とViteビルド成功。
- 上記の検証は更新後に新しく起動したプロセスで行った。他アプリの内蔵Node.jsや、他の場所で動作中のプロセスを更新したとは扱わない。
- リポジトリのコード・依存宣言・lockfile・CI設定は変更していない。Goテスト、Wails全体の再ビルド、実画面操作、攻撃実証は今回実施していない。

Node.jsにはBrotliの修正がバックポートされるため、process.versions.brotliが1.2.0でも、それだけで未修正とは判定しない。今回はNode.js本体の公式修正リリースとの一致を根拠にした。

## 配布アプリ・CI・未確認の範囲

配布アプリはmain.goでビルド済み画面を埋め込み、GoとWebView2で動作する。Node.jsサーバーを配布する構成ではない。現行の作品URL起動はOSのブラウザーへ渡し、アプリ自身が漫画ページを取得・Brotli展開する経路は見つからなかった。これだけでWebView2自体の安全性を保証しない。

直近の[Windows CI実行40252482394](https://github.com/asam-masa/manga-update-manager/actions/runs/40252482394)のセットアップログはNode.js 24.22.1だった。CI設定の24系指定と実際に使われた修正版を区別して確認した。

ローカルWebView2の導入フォルダーは155.0.4283.62だった。このCVEに対するMicrosoftの修正状況と実際の更新設定は未確認である。[Evergreen Runtimeは自動更新される方式](https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/evergreen-vs-fixed-version)だが、このPCで更新が正常に適用されることまで検証したとは扱わない。

Node.js 20以前・Bun・PythonのBrotliパッケージ・Docker・Vercelを使用する構成は、このリポジトリの依存宣言と配布設定では見つからなかった。PC全体の他プロジェクトやアプリは調査対象外である。過去のADRや検証記録にあるNode.js 24.18.0は、当時の実施環境として変更しない。
