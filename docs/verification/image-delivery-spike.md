# 画像配信の事前検証

## 結果と範囲

2026-10-11、Issue #56で、Wails v2.14.0のAssetServer Middlewareから仮画像を渡せることを確認した。Vite 7.3.6を使う開発モードと、Viteなしのproduction exeの両方で、実WebView2からGo API・画像経路へアクセスできた。

試作は`tools/spikes/image-delivery`に独立させ、通常アプリのAPI、SQLite、画面、依存設定は変更していない。新しい依存ライブラリは追加していない。実行ごとに専用のAPPDATA・TEMPを使い、16×24pxの赤・青のPNGと破損データだけを生成した。ユーザー画像と普段のDBは使わない。

これは本番の配信方式の採用ではない。Go設定は1.25.0、初回の実測はローカルGo 1.26.5で行った。Go 1.25.0・CGO無効で配信の3テストとvet、検証用exeのビルドも成功し、開発モードとexeの実WebView2を再確認した。全Goテスト・vet、既存画面44テストとTypeScript/Viteビルドも成功した。

## 確認したこと

| 項目 | 開発モード | production exe |
| --- | --- | --- |
| Go APIによる仮の画像参照変更 | 成功 | 成功 |
| 初回画像のデコード・Canvas上の赤画素 | 成功 | 成功 |
| 同じURLから再取得した青画素 | 成功 | 成功 |
| 未設定・欠損の404と仮表示への分岐 | 成功 | 成功 |
| 破損PNGのデコード失敗と仮表示への分岐 | 成功 | 成功 |
| POST拒否、二重エンコード・未知IDの404 | 成功 | 成功 |
| 応答のCache-Control: no-store | 確認 | 確認 |

画面スクリプトが実WebView2内で観測し、型付きの結果をGo APIへ返した。目視・キーボード・アクセシビリティ確認ではない。仮表示のDOMレイアウトまで確認済みとは扱わない。

Goの自動テストでは、Wailsの静的アセットと開発プロキシ両方の公開Handlerを通じて同じMiddlewareを確認した。固定経路以外、エンコードされたID、二重エンコード、パス移動、Windows絶対パス形式、クエリー、GET以外を拒否した。一時領域外のファイルへのsymlinkを固定画像名に作り、404と本文非公開を確認した。初回Windows実測でこのテストはskipせず成功した。

試作のファイル読み取りは固定した名前だけを`os.Root`から開き、reparse属性・通常ファイル・64KiB上限を確認する。これは本番の管理ファイル識別、Windowsの全reparse種類、hard linkや同一利用者による改変競合の完全な防御を証明しない。

## 再現

ルートのフロントエンド依存を`npm --prefix frontend ci`で用意する。試作専用package.jsonは既存のViteを参照し、新しいnpm依存はインストールしない。

```powershell
# このビルドだけ、tools/spikes/image-deliveryを作業ディレクトリにする。
wails build -s -m -nosyncgomod

# 以下はリポジトリのルートから実行する。
go test ./tools/spikes/image-delivery -v
go vet ./tools/spikes/image-delivery
./tools/spikes/image-delivery/run-probe.ps1 -Mode production
./tools/spikes/image-delivery/run-probe.ps1 -Mode development
```

exeはGit対象外の`build/bin/image-delivery-build/bin`、生成バインディングも`build/bin`へ出す。試作画面のHTML・JSを直接埋め込み、通常のReactビルド成果物は使わない。開発時は同じHTML・JSをViteが配信する。

起動スクリプトは一回ごとに`build/bin/image-delivery-run-<GUID>`を作り、子プロセスだけのAPPDATA・TEMP・TMPを向ける。型付き結果の9項目がすべてtrueなら0、期限までに結果がないか失敗なら2を返す。この呼び出しで起動したプロセスだけを通常終了させ、必要なら子プロセスも終了する。検証データは自動削除しない。

初回の開発モードではWails終了後にViteが残ったため、ランチャーで生存中の親子関係と作成時刻を確認し、子プロセスのハンドルを保持する方式へ修正した。残った検証用Viteは起動日時・コマンドを照合して終了した。再実行後に検証用ポート34115・34116の待ち受けがないことを確認した。空きポートとCIMへのアクセスを起動前に確認し、使用中の場合は他プロセスを停止せず失敗する。開発モードも`-m`でgo mod tidyを省略する。

## 未確認と後続

合成画像・限定した経路の試作であり、通常アプリのReactとDB、ファイル保存・原子的確定・削除・復旧、容量不足、複数起動、並行操作、利用者画像、長時間のキャッシュ挙動は未検証である。開発時の固定ポート34115・34116が他のアプリで使用中なら、他プロセスを停止せず調査する。

推奨候補はMiddlewareで専用経路を処理し、作品IDと現在の管理画像をGo側で解決する方式である。no-storeは今回の確認に使った候補であり、本番のキャッシュ方式は未採用。結果を踏まえてIssue #42で採否とAPI・警告契約を決める。本番実装へ試作コードを機械的にコピーしない。

## 根拠

- [Wails Options](https://v2.wails.io/docs/reference/options/)
- [Wails v2.14.0開発用Handler](https://github.com/wailsapp/wails/blob/v2.14.0/pkg/assetserver/assethandler_external.go)
- [Go 1.25.0 os.Root](https://pkg.go.dev/os@go1.25.0#Root)
- [ADR-0013](../adr/0013-define-cover-operation-boundaries.md)
