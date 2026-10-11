# ADR-0011: 静止画像を検証してPNGサムネイルを生成する

- Status: Accepted
- Validation Status: Verified
- Date: 2026-10-11
- Related: [#41](https://github.com/asam-masa/manga-update-manager/issues/41)、[ADR-0008](./0008-manage-imported-cover-images.md)

## Context

画像管理のうち、入力検証と縮小画像の生成を先に実装する。ファイル保存、DB更新、Wails配信、画面操作は対象外とする。ADR-0008で未決定だった出力形式、依存ライブラリ、方向補正について、ユーザーとのgrillingで決定した。

アプリのGo設定は1.25.0。`golang.org/x/image v0.45.0`はGo 1.25.0に対応する。確認したv0.47.0はGo 1.26.0が必要で、v0.45.0からの差分はgo.modとgo.sumだけだった。画像処理コードの差がないことは、将来の版や依存ライブラリの安全性を保証しない。

## Decision Drivers

- JPEG・PNG・WebPをCGOなしで処理できること
- 透過と縦横比を保持し、元データを変更しないこと
- 巨大寸法を画素展開の前に拒否すること
- アプリのGo設定変更を今回の作業へ混ぜないこと
- 利用者画像なしで境界と方向を再現できること

## Considered Options

| 問い | 候補 | 利点 | 欠点・リスク |
| --- | --- | --- | --- |
| 出力 | JPEG | 写真や表紙の容量を抑えやすい | 透過を保持できず、再圧縮で劣化する |
| 出力 | PNG | 透過を保持し、出力の扱いを統一できる | JPEGより容量が増える場合がある |
| 処理 | ブラウザーへ委ねる | 画面側のAPIを使える | Go側の制限・テストと分離する |
| 処理 | Go標準＋x/image | WebPと縮小をGo側で検証できる | 追加依存と版の管理が必要 |
| 版 | x/image v0.45.0 | Go 1.25設定を維持できる | 最新版ではないため、修正差分の確認が必要 |
| 版 | x/image v0.47.0 | 新しい版を使える | Go設定と関連依存の更新が必要 |
| 方向 | メタデータを無視する | 実装が小さい | 写真などが回転・反転したまま表示される |
| 方向 | 縮小前に補正する | 表示方向と縦横比を合わせられる | EXIFの境界検証と8方向のテストが必要 |

## Decision

- 出力はPNGに統一し、透過を保持する。
- JPEG・PNGは標準ライブラリ、WebPは`x/image/webp`、縮小は`x/image/draw`のCatmullRomを使用する。`x/image v0.45.0`を直接依存として採用する。BSD 3-Clauseの通知をリポジトリへ置き、配布時に同梱する。
- JPEGのAPP1、PNGのeXIf、WebPのEXIFからTIFF IFD0のOrientationを読み、1〜8の回転・反転を縮小前に適用する。方向情報がない場合は1とする。不正な方向の型・件数・値、IFD0の範囲外、重複EXIFはエラーにする。
- 20 MiB以下、2,000万画素以下、長辺600px以下を維持する。縮小後の短辺は整数へ切り捨て、最低1pxとする。拡大・切り抜きはしない。
- `internal/thumbnail.Generate(io.Reader)`は内容から形式を判定し、検証後にPNGのバイト列を返す。失敗時は部分出力を返さず、パス、ファイル、DBには触れない。
- 読み取りから生成までをプロセス内で1件ずつ実行する。入力は20 MiB + 1 byteで制限する。同じ制限済みデータを寸法検査と完全デコードに使う。

## Consequences

`x/text`は依存条件によってv0.39.0からv0.41.0へ更新する。Go設定や他の直接依存は変えない。Renovateによる提案があっても、Go互換性と差分を確認してから採用する。

PNGはCRCとチャンク境界、WebPはRIFF長とチャンク境界を確認し、アニメーションの制御チャンク・フラグを拒否する。WebPはVP8Xの寸法だけを信用せず、VP8/VP8L自体の寸法も展開前に検査する。JPEGはマーカー境界と終端を確認する。どの形式もデコーダーで画素を読み取れることを必要とするが、形式仕様の完全な適合性検査や未知のデコーダー不具合の防止を保証しない。

色プロファイルの変換、EXIFのサブIFD・埋め込みプレビュー、任意のメタデータの完全性検査は行わない。メタデータを出力へ引き継がない。エラーの詳細を将来のUIへ直接表示せず、識別可能なエラーから案内へ変換する。

画素数を制限しても、入力バッファ、デコーダー内部、縮小処理のメモリーは必要になる。同時処理の制限は絶対的なメモリー上限ではない。Readerのキャンセル・期限、UIの待機表示は利用側を実装する際に検討する。

既存画像を維持するファイル・DBの操作契約はIssue #42で実装する。今回の生成処理だけで画像管理全体の成功を主張しない。

## Validation

2026-10-11、合成画像によるテストで、3形式、透過、縮小、8方向、20 MiB・2,000万画素の上限ちょうどと超過、読み取り失敗、破損、アニメーション拒否を確認した。Go 1.25.0・CGO無効で全Goテスト・vet・Wailsビルドが成功した。画面44テストとビルド、Renovate設定3テストも成功した。

Go 1.26.5・CGO無効で20秒の変異入力テストが1,588,541回成功した。単体テストのstatement coverageは90.1%だった。通常レビューと読み取り専用1名の独立レビューに未解決指摘はない。[PR #51](https://github.com/asam-masa/manga-update-manager/pull/51)の実装コミット`687e3f3cd78987b3851dd628f85acd8ef78fb626`に対する[Windows CI](https://github.com/asam-masa/manga-update-manager/actions/runs/38121889028)も全ステップ成功した。

Verifiedはこの生成処理と合成データによる検証範囲を意味する。実際の利用者画像、ファイル保存、DB、WebView2での表示は対象外であり、画像管理全体のADR-0008はPendingを維持する。

## References

- [x/image v0.45.0のGo設定](https://github.com/golang/image/blob/v0.45.0/go.mod)
- [v0.45.0からv0.47.0への差分](https://github.com/golang/image/compare/v0.45.0...v0.47.0)
- [x/imageのライセンス](https://github.com/golang/image/blob/v0.45.0/LICENSE)
- [Go image](https://pkg.go.dev/image)
- [CatmullRom](https://pkg.go.dev/golang.org/x/image/draw#CatmullRom)
- [PNG仕様](https://www.w3.org/TR/png-3/)
- [WebPコンテナ仕様](https://developers.google.com/speed/webp/docs/riff_container)
- [WebP lossless仕様](https://developers.google.com/speed/webp/docs/webp_lossless_bitstream_specification)
