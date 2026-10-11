# ADR-0010: Renovateで依存更新を提案し手動レビューする

- Status: Accepted
- Validation Status: Pending
- Date: 2026-10-11
- Related: [#48](https://github.com/asam-masa/manga-update-manager/issues/48)

## Context

Go・npm・GitHub Actionsの更新候補を手作業だけで追うと、更新の見落としや確認負担が増える。Windows CIは導入済みで、mainでの実行も成功した。更新を提案する仕組みと、採用を判断する工程を分ける。

## Decision Drivers

- 更新候補を継続的に把握できること
- 小さなPRで原因を切り分けられること
- 学習とレビューのため、採用判断を人が行うこと
- 秘密情報の管理と実行環境の保守を増やしすぎないこと
- 外部サービスへの権限付与を設定準備とは別に承認すること

## Considered Options

### Mend提供のRenovate GitHub App

実行環境やトークンを自前で保守せず、複数の依存形式を扱える。一方、第三者サービスへリポジトリの読み取りと更新ブランチ・PR作成の権限を与える必要がある。

### 自前でRenovateを実行する

実行環境を管理できるが、認証・定期実行・Renovate自体の更新を保守する必要がある。

### 手動確認を続ける

外部Appへの権限付与は不要だが、更新調査とlockfile更新の負担が残る。

## Decision

承認されたGitHub App方式を採用し、導入対象をこのリポジトリに限定する。ただし、Appの実際の導入と要求権限は個別承認の後に確定する。今回のPRは設定と文書の準備だけを行う。

Go直接依存、npm依存、GitHub Actionsを初期対象とする。Goの間接依存の通常個別更新とGo設定版の更新提案は対象外とする。Wails本体とCIのCLI版は同一PRへまとめる。関連する依存は推奨presetでまとめるが、無関係な依存を一括更新しない。

ActionsはAction本体と再利用workflowを対象とし、Node版・runner・コンテナーなどのCI実行環境は手動判断に残す。

通常PRは同時最大3件。メジャー更新は非メジャー更新と分ける。通常の新規更新を日本時間の月曜0〜6時に許可し、すべての自動マージを無効にする。GitHubの脆弱性アラートに基づく対応可能な修正は通常の時間・件数制限とは別に扱う。

## Consequences

更新候補の見落としを減らせる一方、PRのレビュー負担は残る。スケジュールは実行時刻を保証しない。既存ブランチの更新は時間外にも行われる。

Go依存の更新処理で必要なGo版や間接依存が変わる場合がある。Go設定版の専用提案を止めても、依存更新がGo設定を変えない保証にはならない。差分のGo設定・必要版を手動で確認し、方針変更が必要なら別途決定する。Goのimport pathが変わるメジャー移行は自動のコード書き換えを有効にせず、別Issueで扱う。

脆弱性修正はアラートの利用可否と修正可能な版に依存する。間接依存も修正対象になる場合があるが、すべての脆弱性を検知・修正する保証はない。Appを停止・対象から外すことで自動提案を止められる。履歴や利用者データは削除しない。

## Validation

公式validator、Wails検出ルールと本体・CLI版の一致テスト、導入PRのWindows CIで設定準備を確認する。結果はPRへ記録する。

2026-10-11、ユーザーがこのリポジトリだけを対象としてAppの導入とオンボーディングを完了した。ユーザー提示画面ではRenovateがEnabled・onboarded、Interactive、Community (Free)であることを確認した。

同日の反映確認で、[Dependency Dashboard #50](https://github.com/asam-masa/manga-update-manager/issues/50)にGo直接依存、npm依存、GitHub Actions、Wails CLIの検出と、Wails本体・CLIの同一更新候補が掲載されていることを確認した。警告欄はなく、更新候補はAwaiting Scheduleにあった。この時点では公開中の更新PRはなく、更新PRの作成・CI、通常の継続運用と脆弱性修正の実動は未検証だった。

2026-10-12、次の更新PRの作成、レビュー、Windows CI成功、ユーザーの明示的な許可によるSquash mergeを確認した。

- [PR #60](https://github.com/asam-masa/manga-update-manager/pull/60): Vite 7.3.6から7.3.7へ更新した。通常・独立レビューは指摘なし。画面44テスト、フロントエンドビルド、Renovate設定3テストと、実WebView2開発モードでの画像配信9項目が成功した。[PR CI](https://github.com/asam-masa/manga-update-manager/actions/runs/38150204208)の対象headは`9995a3527530ba31a6a196d1612ad75e928083f4`、マージコミットは`ccf416a89ef59fa35bd52d64c06a8a4bacb618c8`。
- [PR #61](https://github.com/asam-masa/manga-update-manager/pull/61): Wails本体とCIのCLIを同一PRで2.14.0から2.16.0へ更新した。通常レビューは指摘なし。配布モジュールの比較でWindows実装・AssetServer・生成処理に変更がないことを確認した。Go 1.25.0・CGO_ENABLED=0でGoテスト・vet、画面44テスト、フロントエンドビルド、Renovate設定3テスト、Wailsビルドと、専用保存先の実WebView2での起動・終了6項目が成功した。[文書修正後のPR CI](https://github.com/asam-masa/manga-update-manager/actions/runs/38151716249)の対象headは`abf05fc6ed1a73926061428b61a6e211d019bf3f`、マージコミットは`af70eb94c2d9141fdb314c3667ad28e19056615d`。

同日のマージ後確認対象は`af70eb94c2d9141fdb314c3667ad28e19056615d`。検証に利用者DB・画像を使わず、画面全体の手動操作は未確認としてPRへ記録した。[PR #62](https://github.com/asam-masa/manga-update-manager/pull/62)はGo設定の変更を含むため、この採用実績には含めない。更新提案から採用までの初回実績は確認できたが、通常の継続運用と脆弱性修正の実動は未検証なのでValidation Statusは`Pending`を維持する。設定上の制御を一律に実動で検証済みとは扱わない。

## References

- [依存更新運用](../develop/workflow/dependency-updates.md)
- [Renovate導入](https://docs.renovatebot.com/getting-started/installing-onboarding/)
- [設定の検証](https://docs.renovatebot.com/config-validation/)
- [Go Modules](https://docs.renovatebot.com/modules/manager/gomod/)
- [Custom regex manager](https://docs.renovatebot.com/modules/manager/regex/)
- [設定項目](https://docs.renovatebot.com/configuration-options/)
