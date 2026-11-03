# テスト方針

## 基本原則

- 実装内部ではなく、外部から観測できる振る舞いを検証する
- 高速なUnit Testを中心にし、境界のリスクをIntegration Testで補う
- 正常系だけでなく重要な異常系と境界値を検証する
- 不具合修正では可能な限り再現テストを先に追加する
- 時刻、OS、ファイル、データベース、外部通信を制御する
- 開発者の実データやブラウザー設定を変更しない

## 種類

| 種類 | 対象 |
| --- | --- |
| Unit Test | 状態判定、更新予定計算、入力検証など |
| Integration Test | SQLite、ファイル、ブラウザー起動境界など |
| UI Test | 登録、編集、絞り込み、エラー表示など |
| Manual Test | Windows上の表示、既定ブラウザー連携など |

## 特に保護する振る舞い

- 1週間・2週間・1か月の要確認判定
- 日付、タイムゾーン、月末、スリープ復帰時の計算
- アプリからURLを開いた場合だけ最終アクセス日時を更新すること
- 起動時、手動、指定時刻の確認が重複して不整合を起こさないこと
- バックアップと復元で利用者データが欠落しないこと
- 自動取得失敗時に手動管理へ安全に戻れること

Goのテストは`go test ./...`、静的解析は`go vet ./...`で実行します。フロントエンドは`npm --prefix frontend test`でVitest・jsdom・React Testing Library・user-eventを使用し、Wails APIを差し替えて画面操作を検証します。

配布用ビルドは`npm --prefix frontend run build`と`wails build`で確認します。WebView2、実DB、日本語IMEの結合動作はDOMテストだけでは検証できません。必要な実機確認と未実施項目をPRへ記録します。[Windows CI](../workflow/continuous-integration.md)ではクリーンな環境で自動検証します。Goは画面の成果物を埋め込むため、CIではフロントエンドのビルドをGoテストより先に実行します。

## ブラウザーでの画面検証と手動確認

[ADR-0021](../../adr/0021-verify-react-ui-with-playwright.md)に従い、Playwrightで実React画面を合成API・画像から検証する方針です。操作・レイアウト検証に加え、小型・行一覧・カードの通常幅／狭い幅をWindowsの統一環境で画像比較します。製品ビルドへ差し替えを含めず、既存Vitest・Go試験を維持します。具体的な導入順序と境界は[ブラウザー画面検証の設計](../../browser-ui-testing.md)を参照してください。

スモーク2件と[6画面の画像比較・レイアウト検証](../../verification/browser-visual-comparison.md)、[代表操作5件と手動確認の対応表](../../verification/browser-operations.md)を`npm --prefix frontend run test:browser`で実行します。Windows CIの`Browser checks`は13件を3回ずつ検証し、失敗をジョブへ伝えます。GitHub側の[必須チェック](../workflow/browser-required-checks.md)も2026-11-03に個別承認後の適用と読み戻しを確認済みです。自動化が実装・検証された範囲から、通常の画面変更はPlaywright成功とCodexによる撮影画像の確認を標準にします。基準画像は初回に確認し、意図した変更のときだけ更新します。未導入・未検証の試験を理由に必要な手動確認を省略しません。

新デザインの初回合意、実WebView2・IME・ネイティブ選択・実保存など未自動化の境界に影響する場合は、対象と自動化できない理由を添えて手動確認します。ブラウザーでの合成API成功を実Wails・DB結合の成功とは扱いません。実Wails接続の再調査は別Spikeとします。

