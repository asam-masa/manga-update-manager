# 画像管理の技術確認

## 結果と範囲

2026-10-10、Issue #40で調査した。GoとWailsの既存APIで境界を分けられる見通しは得たが、画像管理の完成や実WebView2での表示は確認していない。承認済みの振る舞いは[ADR-0008](../adr/0008-manage-imported-cover-images.md)、現在の仕様は[一覧設計](../library-design.md)を参照する。

調査時のアプリ設定はGo 1.25.0、Wails v2.14.0、Vite 7系。追加依存や本番コード・DBの変更は行っていない。実行環境は`go version go1.26.5 windows/amd64`。設定のGo 1.25.0での実行は未確認。

## 入力と画像処理

- 標準ライブラリのJPEG・PNGデコーダーは`image.DecodeConfig`で形式と寸法を読み、`image.Decode`で全体を展開できる。ヘッダーが読めても本文が壊れている場合がある。
- 容量はファイル情報だけに頼らず、読み取りも20 MiB + 1 byteなどで制限する。寸法は正数を確認し、積のオーバーフローを避けて2,000万画素以下か検査する。同じ制限済みデータを寸法確認とデコードに使用する候補を検討する。
- PNGの`acTL`、WebPのアニメーションフラグ・`ANIM`・`ANMF`などをコンテナとして確認する必要がある。単なるバイト列検索では画像データ中の一致を誤検出するので、長さ・境界を検証してチャンクを読む。標準PNGデコーダーの成功を静止画判定に使わない。
- Goの`golang.org/x/image/webp`はWebPデコーダー、`golang.org/x/image/draw`は縮小処理の候補である。JPEG・PNG・WebPをGo側で一貫して制限でき、ブラウザーのデコードへ依存しないことが候補理由。今回go.modには追加しない。採用版、Go互換性、ライセンスと保守状況を実装前に確認し、採用理由を説明する。
- 保存をPNGへ統一する案は、透過保持とデコーダーの単純化に利点がある一方、JPEGより容量が増える場合がある。保存形式・品質は未採用で、実装前に判断する。
- JPEGのEXIF方向や色プロファイルは、画素デコード・縮小だけで正しく反映されると扱わない。方向補正の範囲、対応しない画像の案内、透過・色・縦横比・極端な寸法の検証は後続へ残す。

## ファイル・DB・配信

- 保存候補は`%AppData%\\MangaUpdateManager\\covers\\<アプリが生成した識別子>.<保存形式>`。元ファイル名や外部から指定された絶対パスを保存・配信のキーにしない。
- 新しいファイルを準備・確定してからDBの参照を更新し、成功後に旧ファイルを処理する候補を検討する。DB失敗時は旧参照を保持する。途中終了で残る未参照ファイル、旧ファイル削除失敗、解除後の物理削除、容量不足、同一作品の競合を検証する。後処理失敗を「旧画像を維持する失敗」と同一扱いできないため、操作成功と後処理の契約は着手前に決める。
- Wailsには`runtime.OpenFileDialog`がある。キャンセルを変更なしとして扱う契約と、利用者が選択した通常ファイルだけを読み取る境界を検証する。
- AssetServer Handlerは静的アセット未検出時の経路である。公式文書にはVite 5以降での制約があり、採用済みVite 7で動作を保証しない。
- Middlewareで限定した画像経路を先に処理する候補がある。v2.14.0の`pkg/assetserver/assethandler_external.go`では開発用プロキシにもMiddlewareを適用している。ソース上の可能性であり、実WebView2の開発・配布モードで未確認。
- 配信には作品IDからDBの現在の画像を解決する案と、アプリ生成IDを使う案がある。任意のパスを受け取るファイルサーバーは作らない。管理領域外へのアクセス、シンボリックリンク・Windowsのreparse point、二重エンコード、想定外メソッド、キャッシュ更新を実装前に検証する。

## 再現コマンドと観測

```powershell
go test ./tools/spikes/image-boundaries -v
```

4テストが成功した。

| 検証 | 観測 | 証明しないこと |
| --- | --- | --- |
| 合成JPEG・PNG | 寸法と完全デコードを取得できた | WebPや利用者画像すべての互換性 |
| 本文のないPNG | ヘッダー検査成功・完全デコード失敗 | 任意の破損をヘッダーだけで検出できること |
| acTL付き合成PNG | 標準デコーダーが拒否しなかった | 完全なAPNG適合性やアニメーション再生 |
| Wails公開Go API | Middlewareの専用経路、静的アセット、Handler fallbackが動いた | ViteプロキシとWebView2での画像表示 |

## 次の着手条件

画像処理を[#41](https://github.com/asam-masa/manga-update-manager/issues/41)、保存・配信を[#42](https://github.com/asam-masa/manga-update-manager/issues/42)、設定UIを[#43](https://github.com/asam-masa/manga-update-manager/issues/43)、表示切り替えを[#44](https://github.com/asam-masa/manga-update-manager/issues/44)へ分割した。親Issue #28に関連付けて追跡する。

依存ライブラリ・保存形式・方向補正、保存と後処理の成功境界、配信の実機確認、画面寸法は未決定として明記し、推奨候補を採用済みと扱わない。各Issueの受け入れ条件とDoRを確認してから実装する。

## 一次資料

- [Go image](https://pkg.go.dev/image#DecodeConfig)
- [WebPデコーダー](https://pkg.go.dev/golang.org/x/image/webp)
- [縮小処理の候補](https://pkg.go.dev/golang.org/x/image/draw)
- [PNG仕様](https://www.w3.org/TR/png-3/)
- [WebP仕様](https://developers.google.com/speed/webp/docs/riff_container)
- [Wails AssetServer](https://wails.io/docs/reference/options/#assetserver)
- [Wails v2.14.0 AssetServerソース](https://github.com/wailsapp/wails/blob/v2.14.0/pkg/assetserver/assethandler_external.go)
- [Wails v2.14.0ファイル選択](https://github.com/wailsapp/wails/blob/v2.14.0/pkg/runtime/dialog.go)
