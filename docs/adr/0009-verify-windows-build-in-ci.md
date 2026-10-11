# ADR-0009: Windows CIでテストと配布用ビルドを検証する

- Status: Accepted
- Validation Status: Pending
- Date: 2026-10-11
- Related: [#46](https://github.com/asam-masa/manga-update-manager/issues/46)

## Context

これまでテストとビルドは開発端末で確認していた。依存更新と機能追加を進める前に、クリーンなWindows環境で自動検証する必要がある。Windows向けアプリであり、現段階では他OSへの配布は予定していない。

## Decision Drivers

- 現在のGo設定とlockfileで再現できること
- 小規模なプロジェクトで維持できること
- PRのコードへ不要な権限や秘密情報を渡さないこと
- CIの成功と実機での操作確認を区別すること

## Considered Options

### Windows一環境でテスト・静的解析・ビルドを実行する

対象OSのビルドを確認でき、手順も少ない。他OSの互換性とGUI操作は確認できない。

### 複数OSとE2Eを同時に導入する

確認範囲は広いが、現在の配布対象を超える。WebView2との接続やGUI実行環境の運用も必要になる。

### ローカル確認だけを続ける

導入負担はないが、端末に残った成果物や環境への依存を見逃す恐れがある。

## Decision

承認されたWindows一環境のGitHub Actionsを採用する。main向けPRとmainへのpushでGoテスト・vet、フロントエンドのテスト・ビルド、WailsのWindowsビルドを行う。Go 1.25.0、既存Wails版、Node.js 24を使用する。

Actionsは完全なSHAに固定し、トークンは読み取り権限だけにする。秘密情報は渡さず、PRコードを特権イベントで実行しない。E2E、自動配布、自動マージ、ブランチ保護変更、Renovateは含めない。

## Consequences

クリーンなrunnerで再現性を確認できる一方、依存ダウンロードやrunner更新による失敗を切り分ける必要がある。ActionsのSHAとWails CLI版を保守する。手動のWindows操作確認とPRレビューは引き続き必要である。

## Validation

2026-10-11、[導入PR #47のCI](https://github.com/asam-masa/manga-update-manager/actions/runs/38063106341)と[マージ後のmainへのpushのCI](https://github.com/asam-masa/manga-update-manager/actions/runs/38063371691)で全検証ステップの成功を確認した。Go 1.25.0・CGO無効でGoテスト・vet、フロントエンドのテスト・ビルド、WailsのWindowsビルドが成功した。

文書反映確認時点のmainコミット`e5dde6e3166becb0f073c2a6d9b263a3211804a1`に対する[CI](https://github.com/asam-masa/manga-update-manager/actions/runs/38125762131)も成功した。`workflow_dispatch`の実行履歴は確認できず、手動実行は未検証なのでValidation Statusは`Pending`を維持する。GUIの実機確認はCIの対象外である。

## References

- [CI運用](../develop/workflow/continuous-integration.md)
- [actions/checkout](https://github.com/actions/checkout)
- [actions/setup-go](https://github.com/actions/setup-go)
- [actions/setup-node](https://github.com/actions/setup-node)
- [Wails CLI](https://wails.io/docs/reference/cli/)
