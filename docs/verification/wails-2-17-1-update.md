# Wails 2.17.1への更新確認（PR #352）

## 対象と採用理由

2026-11-02、Wails本体とWindows CIのCLIを2.17.0から2.17.1へ揃える。[公式リリース](https://github.com/wailsapp/wails/releases/tag/v2.17.1)と[アドバイザリ](https://github.com/wailsapp/wails/security/advisories/GHSA-r5j2-ghh9-2jqw)を確認した。Linuxの子フレームからのIPC呼出しを制限する修正を含む。公式にはWindows/macOSはこの脆弱性の対象外であり、このWindows確認をLinux修正の実証にはしない。

直接依存に加え、Echo 4.15.3→4.16.1、go-colorable 0.1.14→0.1.15、x/crypto 0.53.0→0.57.0、x/net 0.56.0→0.60.0が変わる。上流go.modの要求Go版は1.26.0で、プロジェクトの設定を変更しない。go-webview2は1.0.22のまま。モジュールキャッシュの新旧Wailsソース差分では、Windowsのネイティブ実装に変更はなく、Linuxのガードとその試験、依存・開発用ファイルが変わっていることを確認した。

## 検証

最新main（da3f9392970987aed0bc01ab73640a60c0e3bee7）を取り込んだ版で、Go 1.26.5とWails CLI 2.17.1を使用した。

- `CGO_ENABLED=0`で`go test ./...`成功。内部パッケージに加え、既存の画像境界・配信・DB受付・Commit結果のSpikeも含む。
- `go vet ./...`成功。
- `npm --prefix frontend test -- --reporter=dot`は36ファイル・437試験成功。
- `npm --prefix frontend run build`で型検査とViteビルド成功。
- `go run github.com/wailsapp/wails/v2/cmd/wails@v2.17.1 build -s -m -nosyncgomod -o 352-wails-check.exe`成功。

実WebView2は専用APPDATA/TEMPと合成3作品で確認した。2026-11-02、ユーザーが横長の緑系画像の表示、欠損・未設定の仮表示、小型・行一覧・カードの切替、編集のキャンセルとTab/Esc操作、右上の×による終了を確認した。確認用実行ファイルのSHA-256は`54D9F721C726CDB3FDD20B93BFC3F38D1AF1CFAFF4FCA6AFA47EB260784F39B7`。通常の利用者データは使っていない。

更新後のWindows CIと通常・独立レビューの対象版・結果はPR本文へ記録する。Linux/macOS、外部サイト、実OS故障の確認ではなく、DB構造・保存形式・外部アクセスの変更も行わない。

## 残る境界

同じオリジンの子フレームは親と権限を共有するという上流の制約は残る。今回のアプリは信頼しないサイトを画面内へ埋め込む設計を追加しない。CI成功や今回のWindows操作確認を、Wails全体の安全性や全環境での動作保証とは扱わない。
