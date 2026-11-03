# Windows CI

[Windows CI](../../../.github/workflows/ci.yml)は、main向けPRとmainへのpushで自動実行します。mainへのマージ後は手動実行もできます。判断の背景は[ADR-0009](../../adr/0009-verify-windows-build-in-ci.md)を参照してください。

## Windows checks

Windows 2025のGitHub-hosted runnerで、次の順に実行します。

1. Go 1.26系の最新修正版とNode.js 24を準備する
   - `node --test tools/verification/renovate-config.test.mjs`で依存更新設定とWails本体・CLI版の一致を確認する
2. `npm --prefix frontend ci`でlockfileに従って依存を導入する
3. `npm --prefix frontend test`と`npm --prefix frontend run build`を実行する
4. `go test ./...`と`go vet ./...`を実行する
5. Wails CLI v2.16.0を導入し、`wails build -s -m -nosyncgomod`を実行する

`go.mod`の最低Go版は1.26.0です。CIでは`go-version: '1.26.x'`と`check-latest: true`で1.26系の最新修正版を選びます。実際の版はSet up Goのログで確認します。`GOTOOLCHAIN=local`で依存による暗黙のGo更新を防ぎ、`CGO_ENABLED=0`で検証します。最低版と検証版の分離は[ADR-0015](../../adr/0015-separate-go-minimum-and-validation-version.md)を参照してください。`frontend/dist`はGoの埋め込み対象なので、クリーンな環境ではフロントエンドを先にビルドします。

Wailsの`-s`はフロントエンドの再ビルドを省略します。`-m`と`-nosyncgomod`はビルド時のモジュール調整を省略します。これにより、CI中に`npm install`や依存版の変更を行いません。Wailsの依存版を更新するときは、ワークフローのCLI版も合わせます。

## Browser checks

同じ`windows-2025`上の独立ジョブで、Node.js 24・lockfileの依存・PlaywrightのChromiumを準備し、`npm --prefix frontend run test:browser -- --repeat-each=3`を実行します。スモーク2件・画像比較6件・代表操作5件の39件を検証し、非0終了はジョブを失敗させます。旧校正ジョブの`continue-on-error`は使いません。対象と手動確認の対応は[代表操作](../../verification/browser-operations.md)、基準環境は[画像比較](../../verification/browser-visual-comparison.md)に記録します。

`Browser checks`と`Windows checks`は別のチェック名です。GitHub側の[外部設定](./browser-required-checks.md)も2026-11-03に個別承認を受けて適用し、両チェックがmain向けPRで必須となることを読み戻して確認しました。ジョブ追加だけを必須設定の完了としません。

## 安全性と結果の確認

- Actionsは公式リリースに対応する完全なコミットSHAへ固定します。更新時は版コメントも更新します。
- トークン権限は`contents: read`だけにし、checkoutは認証情報を保持しません。秘密情報や利用者のDB・画像を渡しません。
- `pull_request_target`は使いません。PRのコードは使い捨てのrunnerで実行します。
- キャッシュ対象はGoとnpmの依存です。実行ファイルの配布は行いません。ブラウザーの合成データのレポート・結果・基準画像だけをartifact `browser-checks`として7日間保存します。失敗時もアップロードします。
- 失敗時は最初に失敗したステップのログを調べます。PRには実行URL、結果、未確認事項を記録します。
- 新しい実行で古い同一refの実行をキャンセルします。Windows checksは30分、Browser checksは15分で打ち切ります。

CIの成功は実WebView2、IME、既定ブラウザー、利用者データの操作を保証しません。必要な実機確認は別に行います。自動マージとブランチ保護の変更は対象外です。依存更新の提案と採用は[依存更新運用](./dependency-updates.md)に従います。
