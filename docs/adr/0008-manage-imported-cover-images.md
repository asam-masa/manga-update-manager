# ADR-0008: 利用者の画像を一覧用に取り込んで管理する

- Status: Accepted
- Validation Status: Pending
- Date: 2026-10-10
- Related: [#28](https://github.com/asam-masa/manga-update-manager/issues/28)、[#40](https://github.com/asam-masa/manga-update-manager/issues/40)

## Context

利用者が用意した画像を作品一覧へ表示する。画像管理と表示切り替えを一度に実装せず、ファイル・DB・画面の境界を分けて進める。今回の対象は承認済みの振る舞い、技術確認、後続Issueの整備であり、アプリへの実装ではない。

既存作品にはアプリ管理領域内の相対パスを保存する`thumbnail_path`があるが、画像の取り込み・配信・変更・解除は未実装である。Wails v2.14.0にはファイル選択とAssetServerのHandler・Middlewareがある。現在のViteは7系で、公式文書にはHandlerとVite 5以降の組み合わせの制約が記載されている。

## Decision Drivers

- 元ファイルの移動・削除に左右されないこと
- 元画像を変更・削除しないこと
- 一覧用の容量・表示負荷を抑え、表紙を欠けさせないこと
- 失敗時に現在の画像を維持すること
- 利用者データをリポジトリや外部へ送らないこと

## Considered Options

| 問い | 候補 | 利点 | 欠点・リスク |
| --- | --- | --- | --- |
| 保存 | 元ファイルを参照 | コピー容量が不要 | 移動・削除で表示できなくなる |
| 保存 | アプリ領域へ取り込む | 元ファイルから独立する | 管理領域の容量と後処理が必要 |
| 内容 | 元画像も保持 | 原寸表示や再加工に使える | 初回の用途に対して容量が増える |
| 内容 | 一覧用の縮小画像だけ保持 | 容量と負荷を抑えられる | 将来の原寸表示は取り込み直しが必要 |
| 操作 | 登録フォームで画像を選ぶ | 登録時にまとめて設定できる | 登録失敗時のファイル後処理が増える |
| 操作 | 登録後に画像を設定 | 作品登録と画像操作を分けられる | 設定に別操作が必要 |

## Decision

ユーザーとのgrillingで承認した次の振る舞いを採用する。

- 元画像を変更・削除せず、アプリ専用領域へ一覧用の縮小画像を取り込む。大きな元画像はアプリ内に保持しない。
- JPEG・PNG・WebPの静止画を入力対象とする。アニメーションは初回非対応として案内する。
- 入力は1ファイル20 MiB（20 × 1024 × 1024 bytes）以下、2,000万画素以下とする。
- 縦横比を維持して長辺最大600pxへ縮小する。小さい画像は拡大せず、切り抜かない。
- 登録後の一覧から「画像を設定」「画像を変更」「画像を外す」を操作する。登録フォームには画像選択を追加しない。
- 変更・解除の失敗時は現在の画像を維持する。元ファイルには触れない。

保存形式、ライブラリと版、配信経路、EXIF方向の扱い、障害時の後処理、APIの詳細はこのDecisionでは採用しない。[技術確認](../verification/image-management-investigation.md)の候補・未決定事項を各実装Issueの着手前に確認する。

## Consequences

入力の容量と画素数を両方検証する。ヘッダー取得だけで成功とせず、全体のデコードと静止画判定が必要になる。2,000万画素でも展開時のメモリーは大きく、同時処理数を制限する必要がある。

ファイルとSQLiteは一つのトランザクションで変更できない。旧画像を先に消さず、DB更新が確定するまで旧参照を維持する設計を検討する。物理的な旧ファイル削除や後処理失敗を含む契約は、未決定事項として残す。

一覧の3形式は別Issueで扱う。画像がない場合は仮表示を維持し、検索・アクセス記録の既存契約を変えない。原寸表示、画像の自動取得、切り抜き編集、バックアップ機能は対象外とする。

## Validation

一次資料と採用済みWails v2.14.0のソースを確認した。[小さな検証](../../tools/spikes/image-boundaries/image_boundaries_test.go)の4テストは成功した。利用者画像・実DBを使っていない。

確認できた範囲はJPEG・PNGの合成画像の読み取り、ヘッダー検査と完全デコードの差、アニメーション制御チャンクを含む合成PNGの受理、WailsのGo側Handler・Middlewareである。完全なAPNG適合性、WebP、縮小、ファイル・DBの障害、実WebView2・Viteでの配信は未検証のためPendingを維持する。

## References

- [画像管理の技術確認](../verification/image-management-investigation.md)
- [Go image](https://pkg.go.dev/image#DecodeConfig)
- [Go x/image/webp](https://pkg.go.dev/golang.org/x/image/webp)
- [Wails AssetServer](https://wails.io/docs/reference/options/#assetserver)
- [PNG仕様](https://www.w3.org/TR/png-3/)
- [WebPコンテナ仕様](https://developers.google.com/speed/webp/docs/riff_container)
