# ブラウザー画面検証の設計

## 現在の状態

2026-11-02に合意した導入方針。Issue #359でPlaywright基盤と2件のスモーク試験、#360で6画面の画像比較・レイアウト検証を追加した。#361では[代表操作5件](./verification/browser-operations.md)と、失敗をジョブへ伝える`Browser checks`へ進めた。[6画面の条件・更新手順](./verification/browser-visual-comparison.md)も参照する。2026-11-03に利用者の個別承認を受け、GitHub側の[必須チェック設定](./develop/workflow/browser-required-checks.md)を適用・読み戻し確認した。[ADR-0021](./adr/0021-verify-react-ui-with-playwright.md)のValidationはVerifiedとし、親Issue [#357](https://github.com/asam-masa/manga-update-manager/issues/357)のブラウザー検証導入を完了した。実Wails接続は別Spikeであり、未検証の範囲の手動確認を省略しない。

## 検証する境界

製品と同じReactの入口・コンポーネント・CSSをブラウザーで開き、Wails APIとイベントの境界だけをテスト側の合成実装へ差し替える。別の見本画面を作って製品画面の検証としない。生成済みWailsバインディングの直接編集や、製品側のテストモードは導入しない。

テストごとのブラウザー状態と合成データを分離し、固定した日付・作品・タグ・画像で開始する。テスト用コードと画像は製品の入口や公開資産から参照せず、製品ビルドへ混入しないことも確認する。ローカルの検証用サーバーと合成境界だけで完結させ、通常DB・通常プロファイル・外部サイトは使用しない。撮影画像・トレース・CI出力にも利用者データを含めない。

| 試験 | 確認できる範囲 | これだけでは確認しない範囲 |
| --- | --- | --- |
| 既存Vitest・jsdom | 状態遷移・入力・DOM操作の多数の条件 | 実ブラウザーの描画とOS結合 |
| Playwright＋合成境界 | 製品React画面のブラウザー描画、操作、フォーカス | 実Go・SQLite・ネイティブ操作の成功 |
| Go・SQLite試験 | ドメイン、API、保存・競合の条件 | 画面と実Wailsを通る全経路 |
| 必要な実機確認 | 実WebView2、IME、ファイル選択、実保存などの結合 | 未確認の全操作・全環境 |

## 画像比較と安定化

初期対象は小型・行一覧・カードそれぞれの通常幅・狭い幅の計6画面とする。画像比較とは別に、はみ出しや操作要素の配置など期待するレイアウトを検証する。具体的な幅・撮影範囲・期待値は画像比較Issueの着手前に定める。

基準画像は比較に使うWindows・Playwright／ブラウザー版・フォント・viewport等を揃えた環境で生成する。日付や画像、アニメーション等を制御し、必要なフォント・画像・画面状態の準備を待つ。差分を消すために対象の不具合や画像未読込をマスクしない。別OSでの結果はWindowsの合格証拠としない。

Codexは初回の6基準画像を実際に開いて確認する。以後の通常画面変更も試験成功と撮影画像の確認を行う。新デザインの初回合意は利用者に求める。画像の自動更新だけを合格根拠にせず、更新理由・対象・比較環境と旧版からの差をPRへ残す。環境更新による差も環境変更として理由を確認する。

失敗時は実際の撮影・差分・必要なトレースを確認できるようにする。基準画像以外の一時結果やトレースはGitへ追加しない。CI artifact `browser-checks`は合成データのレポート・結果・基準画像だけを7日間保持する。

## 代表操作と手動確認

検索、ANDタグ絞り込み、表示切替、ページ移動、タグ管理、再取得失敗を代表操作とする。[シナリオと期待結果](./verification/browser-operations.md)に従い、キーボードとフォーカスも検証する。画像が同じでもボタンが反応しない回帰を見落とさず、既存の全Vitestケースをそのまま複製しない。

必要な自動化が実装され、対象版で成功し、撮影画像をCodexが確認した項目から、通常の手動表示確認を置き換える。PRには自動化済みの項目と未確認の境界を対応付ける。

新デザインの初回合意や、実WebView2・IME・ネイティブファイル選択・実保存など未自動化の境界に影響する場合は、確認対象とブラウザー検証で代替できない理由を添えて手動確認する。合成APIの保存成功を実DBへの保存成功と報告しない。実Wails接続は[#362](https://github.com/asam-masa/manga-update-manager/issues/362)で調べ、現時点で成功扱いにしない。

## 導入順序

| Issue | 単独で確認する成果 | 着手前に具体化すること |
| --- | --- | --- |
| [#358](https://github.com/asam-masa/manga-update-manager/issues/358) | ADR・本設計・テスト方針 | 承認済みの範囲と分割 |
| [#359](https://github.com/asam-masa/manga-update-manager/issues/359) | 実React画面を合成境界で起動する基盤とスモーク試験 | 採用版・依存条件・起動方法・成果物の分離確認 |
| [#360](https://github.com/asam-masa/manga-update-manager/issues/360) | 6画面のレイアウト検証・画像比較・基準画像確認 | 幅・撮影範囲・安定化・Windows基準作成と反復検証 |
| [#361](https://github.com/asam-masa/manga-update-manager/issues/361) | 代表操作とWindows CIの必須チェック | シナリオ・artifact・失敗の伝達・必要な外部設定 |

画像比較の段階でもWindowsで基準作成と校正を行う。正式なCI必須化を待って基準環境を決めることはしない。各PRは既存試験と製品ビルドを維持し、導入済み範囲だけを結果として記録する。実Wails Spikeはこの順序を止めない。

## 基盤の実行手順（#359）

Playwright Test 1.65.0（Apache-2.0）を開発用依存として固定する。ブラウザー操作・アサーション・ブラウザー管理を揃えて使えるため採用した。プロジェクトの実行環境はNode.js 24とし、固定版のCLIで対応Chromiumを取得する。取得元は[公式npmパッケージ](https://www.npmjs.com/package/@playwright/test/v/1.65.0)。#359の初回採用は1.64.1で、下記の日付付き検証結果は当時の記録として残す。

2026-11-13、PR #534の更新版1.65.0と同梱Chromium 158.0.8127.18を確認した。固定head `853bb0a4bd7c95822c2c6d701c76f500fe32819b`の[Windows CI 43100816297](https://github.com/asam-masa/manga-update-manager/actions/runs/43100816297)は初回成功し、ブラウザー13試験を各3回、計39試験（6画面の画像比較を含む）が成功した。基準画像・比較条件は変更していない。CI同梱の基準画像の代表例と、差分なしの比較結果を確認した。これは実WebView2の検証ではない。

ローカルWindows/Node.js 24.22.1でも`npm ci`、Vitest 439件、製品ビルド、`npm --prefix frontend run test:browser -- --grep-invert '配置と画像' --repeat-each=3`の代表操作21件が成功した。ローカルの画像比較は実施せず、画像比較はWindows CIの結果として区別する。[公式リリースノート](https://github.com/microsoft/playwright/releases/tag/v1.65.0)のclass field・inert待機・アクセシブル名等の変更を既存利用と照合し、テストや閾値を緩和せず確認した範囲である。更新後も実Wails・IME・ネイティブダイアログ等の未検証境界を手動確認から外さない。

```powershell
npm --prefix frontend ci
npm --prefix frontend exec -- playwright install chromium
npm --prefix frontend run test:browser
```

ブラウザーの取得にはネットワークと専用キャッシュへの書き込みが必要になる。通常のブラウザー設定やプロファイルは変更しない。試験はheadless Chromiumを使い、各テストはPlaywrightの独立したブラウザーコンテキストで実行する。

`test:browser`は試験コードの型確認、製品と同じ入口・設定を使うproductionビルド、Playwrightの順に実行する。`e2e/vite.config.ts`はモジュール解析時の混入検査だけを製品設定へ加える。`e2e/`やPlaywright本体へのimportが製品の依存グラフへ入ったら、tree shaking前にビルドを失敗させる。通常の`npm --prefix frontend run build`は変更しない。

Playwrightが`127.0.0.1:4179`でVite previewを起動し、終了時に起動したサーバーを終了する。既存サーバーを再利用せず、ポートが使用中なら失敗する。手動で別アプリを停止する必要はない。ブラウザー要求はこのoriginに限定し、`/__covers/works/1`はテスト側で合成SVGを返す。他のoriginへの要求と未定義の画像要求は遮断して試験を失敗させる。

`e2e/fixture.ts`がページのスクリプト実行前に`window.go.main.App`と起動時のイベント購読境界を注入する。未対応APIは成功を返さず試験を失敗させる。イベントは現在のスモークに必要な購読・解除だけの代替で、画像選択完了イベントの実配信を検証したことにはしない。日時は2026-11-02 09:00（日本時間）に固定し、画面のタイマーは進める。登録フォームの表示変更はそのページ内の合成状態だけへ保存する。

成功時もスモーク画面をHTMLレポートへ添付する。失敗時は撮影画像とトレースも残る。`frontend/playwright-report/index.html`を確認し、必要なら`npm --prefix frontend exec -- playwright show-report frontend/playwright-report`でレポートを開く（GUI起動は環境の承認に従う）。レポートと`frontend/test-results/`はGitへ追加しない。これは画像比較の基準画像ではない。

## 基盤の検証結果（2026-11-02）

Windows / Node.js 24.22.1 / Playwright 1.64.1の同梱Chromiumで、2件のスモーク試験が成功した。合成表紙と未設定時の仮画像、登録フォームの表示を撮影画像でも確認した。通常ビルドと混入検査付きビルドの出力3ファイルはSHA-256がすべて一致した。

一時的な故障注入では、製品入口からテスト用fixtureをimportすると出力前にビルドが失敗した。未対応APIの呼び出しと外部HTTP要求も、テスト側で例外を握りつぶしても試験を失敗させた。後者の2件では失敗画像とトレースが生成された。一時コードは除去し、通常の2件が再び成功したことを確認した。

既存Vitestは36ファイル・437件、`go test ./...`・`go vet ./...`・通常フロントエンドビルド・Wails 2.17.1の配布用ビルドが成功した。ローカルPATHのCLIは2.14.0だったため、配布ビルドは`go run github.com/wailsapp/wails/v2/cmd/wails@v2.17.1 build -m -nosyncgomod`で版指定した。システムのCLI設定は変更していない。

今回の保証はブラウザー上の合成境界に限る。実Wails接続、IME、実保存、6画面の画像比較、PlaywrightのCI実行は未検証・未実装のままである。
