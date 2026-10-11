# 依存更新

[Renovate設定](../../../.github/renovate.json)は更新PRの提案だけを行い、採用はレビュー後に手動で判断します。判断の背景は[ADR-0010](../../adr/0010-propose-dependency-updates-with-renovate.md)を参照してください。

## 対象と提案ルール

- 通常対象はGo直接依存、npm依存、GitHub Actionsです。Go間接依存は直接依存の更新に伴って変わる場合がありますが、通常の個別更新は初期対象にしません。
- Goの`go`・`toolchain`設定の専用更新PRは無効です。依存更新でGo設定が変わる場合は、採用前に別の設計判断が必要か確認します。
- Wails本体とCI内の`go install`に固定したCLIを同じグループにします。CLIは限定したregexで検出します。
- 推奨presetの関連パッケージのグループを使用し、無関係な依存の全体グループは作りません。メジャー更新は非メジャーと分離します。
- Goのメジャー更新でimport path変更が必要なものは、自動コード書き換えを有効にしません。別Issueで移行を検討します。
- 通常PRは同時最大3件。新規更新の許可時間は月曜0〜6時、`Asia/Tokyo`です。毎週の作成や特定時刻の実行を保証しません。時間外にも既存ブランチは更新され得ます。
- 自動マージは通常・脆弱性修正とも無効です。GitHub ActionsはSHA固定を維持します。
- Actionsの初期対象はAction本体と再利用workflowです。Node版・runner・コンテナーなどのCI実行環境は対象外にし、手動判断に残します。
- lockfileの更新は依存更新に伴って行います。全体のlockfile定期メンテナンスは初期設定では有効にしません。

## 脆弱性修正の境界

Renovateの`vulnerabilityAlerts`はGitHubの脆弱性アラートに基づきます。依存関係グラフとアラートの利用可否、Appのアクセス権限を導入時に確認します。アラートがないことを安全の証明としません。

対応可能な修正PRは通常スケジュールや3件制限を外れ、間接依存を含む場合もあります。修正版の存在・検出対象・互換性によっては自動提案できません。必要なら手動調査・更新を行います。機能追加を優先してセキュリティ上の判断を先送りしません。

## 導入手順

1. この設定PRをレビューし、マージする。これはAppの権限付与ではありません。
2. 個別の承認を得て[Renovate App](https://github.com/apps/renovate)のインストール画面を確認する。
3. 要求される読み取り・書き込み権限を確認し、`Only select repositories`でこのリポジトリだけを選ぶ。承認前にInstallを実行しない。
4. 設定がmainにあるため、onboarding PRなしで更新提案が始まる可能性がある。導入後の警告、Dependency Dashboard、検出対象、実際のPRを確認する。
5. 自動マージが無効、Wailsのグループと版の一致、ActionsのSHA固定、Windows CIの成功を確認する。Appの運用確認が終わるまではADRをPendingとする。

Appを停止・対象から外す操作にも個別の承認を得ます。既存PRやブランチの削除、履歴破棄は自動で行いません。トークンをリポジトリや文書へ保存しません。

## 更新PRのレビュー

CI成功だけで採用しません。変更履歴、互換性・必要Go版、API変更、生成物、lockfile、利用者データへの影響を確認します。Wails・SQLite・UIなどは必要な実機確認も行います。

Botの提案は通常の開発Issueとは別にGitHub上のPR・Dependency Dashboard Issueで追います。設計変更や移行が必要な場合は個別Issueを作ります。Codexによる修正・コミット・PR更新には既存のレビュー手順を適用します。Botが作る英語の件名は提案として扱い、人が作るIssue・PR説明・Squashコミットは日本語の運用に従います。

## 設定の検証

`node --test tools/verification/renovate-config.test.mjs`で基本方針・限定したWails検出・本体とCLIの現在版の一致を確認します。Windows CIにも同じテストを含めます。JSのregexテストだけではRenovate全体の解決・更新を保証しません。

設定変更時は公式validatorも実行します。今回確認する版はRenovate 44.149.3です。検証ツールはアプリの依存に追加しません。

```powershell
npx --yes --package renovate@44.149.3 -- renovate-config-validator --strict --no-global .github/renovate.json
```

`--no-global`でリポジトリ設定として検証します。設定の構文検証と、Appの実際の検出・更新処理の確認は分けて記録します。
