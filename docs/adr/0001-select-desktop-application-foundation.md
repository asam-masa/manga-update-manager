# ADR-0001: デスクトップアプリケーション基盤を選定する

- Status: Proposed
- Validation Status: Pending
- Date: 2026-10-04
- Related: [#3](https://github.com/asam-masa/manga-update-manager/issues/3)

## Context

本プロジェクトでは、漫画のURL、タイトル、画像、更新予定、最終アクセス日時を端末内で管理するWindows向けデスクトップアプリを作成する。

初期版で必要な主な処理は、SQLiteへの保存、CRUD、日時計算、アプリ起動中の定時処理、既定ブラウザーでのURL起動、バックアップである。将来は、利用規約と公開仕様を確認できたサイトに限り、RSSまたは公開情報を使った更新確認を追加する。

デスクトップ基盤の候補は、Go＋Wails v2とRust＋Tauri v2である。どちらもWeb技術で画面を作り、WindowsではWebView2を使用する。

確認済みのローカル環境は次のとおりである。

- Go 1.26.5を導入済み
- Node.js 24.18.0とnpm 11.16.0を導入済み
- RustとCargoは未導入
- Microsoft C++ Build Toolsは確認できていない
- WebView2は権限制約によりレジストリから確認できていない

別リポジトリの`browser-launcher`では、Go＋Wails v2＋React＋TypeScriptを使用している。本プロジェクトでも同じ構成を採用すると知識と運用を共有できる。一方、Rust＋Tauriを採用すると、新しい言語とデスクトップ基盤を実用アプリを通じて学習できる。

このADRではデスクトップ基盤だけを決定する。SQLiteライブラリ、フロントエンドフレームワーク、状態管理、自動取得方式は対象外とする。

## Decision Drivers

- 実用アプリの完成と学習を両立できること
- 初期環境構築の負担
- MVP完成までの時間
- SQLite、日時、スケジュール、HTTP処理を安全に実装できること
- Windows向けビルドと配布が可能であること
- Unit TestとCIを構築しやすいこと
- 小規模な個人用アプリとして保守しやすいこと
- 既存のGo＋Wailsプロジェクトと知識を共有するか、学習範囲を広げるか

## Considered Options

### Go＋Wails v2

Wails v2は、Goでアプリケーションロジックを実装し、Web技術で画面を作成する。Goの公開メソッドをJavaScriptから呼び出すバインディングと、Go構造体に対応するTypeScriptモデルの生成機能を持つ。

- 利点:
  - GoとNode.jsが導入済みで、初期環境構築が少ない
  - `browser-launcher`のWails、React、テスト、CIの知識を再利用できる
  - CRUD、SQLite、goroutine、`context.Context`、HTTPをGoで学習できる
  - Goの文法とビルドが比較的単純で、MVPを早く完成させやすい
  - Windows向けビルドとWebView2の配布方法が公式に用意されている
- 欠点:
  - デスクトップ基盤の学習内容が`browser-launcher`と重複する
  - Rustを学ぶ機会にはならない
  - Wails v2固有のバインディングとランタイムへ依存する
- リスク:
  - 二つのGo＋Wailsプロジェクトで似た設計を機械的に流用し、各プロジェクトに不要な複雑さを持ち込む可能性がある
  - 新しい技術を学ぶ動機が弱くなり、学習目的の一部を満たさない可能性がある

### Rust＋Tauri v2

Tauri v2は、Rustでアプリケーションロジックを実装し、Web技術で画面を作成する。JavaScriptからRustのCommandを`invoke`で呼び出し、Capabilityで利用可能なCore機能やPlugin Commandを制御できる。

- 利点:
  - 所有権、借用、`Result`、列挙型、非同期処理などRustの主要概念を実用コードで学習できる
  - `browser-launcher`と異なる言語と基盤を経験し、学習範囲を広げられる
  - Tauri v2には公式SQL Pluginがあり、SQLiteへ接続できる
  - 権限とCommandの境界を明示でき、自動取得やファイル操作を必要最小限に制限しやすい
  - Windows向けにMSIとNSISのインストーラーを生成できる
- 欠点:
  - Rust、Cargo、Microsoft C++ Build Toolsの準備が必要である
  - Rust未経験のため、所有権、ライフタイム、非同期処理、エラー設計の学習時間が必要になる
  - Rust、Tauri、TypeScriptを同時に学ぶため、Go＋WailsよりMVP完成が遅くなる可能性が高い
  - コンパイル時間と依存関係の理解に追加の負担がある
- リスク:
  - 学習範囲を広げすぎて、アプリの完成より環境構築とコンパイルエラーへの対応が中心になる可能性がある
  - Tauri Pluginの採用範囲を広げると、Capability設定と更新追従の負担が増える

## Comparison

| 判断基準 | Go＋Wails v2 | Rust＋Tauri v2 |
| --- | --- | --- |
| 現在の環境 | Go導入済み | RustとC++ Build Toolsの準備が必要 |
| MVP完成速度 | 有利 | 学習時間により不利 |
| Goの学習 | 深められる | 対象外 |
| Rustの学習 | 対象外 | 実用コードで学べる |
| 既存知識の共有 | `browser-launcher`と共有できる | 共有範囲は主にフロントエンドと運用 |
| SQLite・HTTP・日時処理 | 十分対応可能 | 十分対応可能 |
| Windows UI | WebView2 | WebView2 |
| 配布 | Wails CLIでビルド | MSIまたはNSISを生成可能 |
| 初期リスク | 技術的には低い | 学習と環境構築のリスクが高い |

## Decision

未決定。

現在の推奨案はRust＋Tauri v2である。`browser-launcher`ですでにGo＋Wailsを学習しているため、本プロジェクトではRustを採用することで学習範囲を広げられる。アプリの要件は段階的に実装でき、最初は登録、一覧、リンク起動に限定すれば、Rust未経験によるリスクを抑えられる。

ただし、MVPの早期完成と`browser-launcher`で得た知識の定着を優先する場合は、Go＋Wails v2を採用する方が適している。ユーザーが学習範囲と完成速度のどちらを優先するか選択した後にDecisionを確定する。

## Consequences

### Rust＋Tauri v2を採用する場合

- Rust、Cargo、Microsoft C++ Build Tools、WebView2の開発環境を確認・準備する
- 最初の実装範囲を、作品モデル、入力検証、一覧、URL起動に限定する
- SQLite、スケジュール、自動取得は別Issueで段階的に追加する
- Rustの学習事項と判断理由をPRのLearningへ記録する
- Tauri Capabilityは必要になった権限だけを追加する

### Go＋Wails v2を採用する場合

- `browser-launcher`の構成を参考にするが、不要なレイヤーや抽象化を機械的に移植しない
- SQLite、スケジュール、HTTPなど、`browser-launcher`と異なる学習対象を明確にする
- Wails v2のバージョンを固定し、生成物とCIの扱いを文書化する

## Validation

採用後、次を確認する。

- Windows 11で開発モードを起動できる
- フロントエンドからバックエンドの最小CommandまたはMethodを呼び出せる
- Windows向け実行ファイルをローカルでビルドできる
- Unit TestとフロントエンドビルドをGitHub Actionsで実行できる
- 既定ブラウザーでURLを開ける

Decisionが未確定のため、Validation Statusは`Pending`とする。

## References

- [Wails v2 Installation](https://v2.wails.io/docs/gettingstarted/installation/)
- [Wails v2 Introduction](https://v2.wails.io/docs/v2.13.0/introduction/)
- [Wails v2 Windows Guide](https://v2.wails.io/docs/guides/windows/)
- [Tauri v2 Prerequisites](https://v2.tauri.app/start/prerequisites/)
- [Tauri v2 Overview](https://v2.tauri.app/start/)
- [Tauri v2 SQL Plugin](https://v2.tauri.app/plugin/sql/)
- [Tauri v2 Windows Installer](https://v2.tauri.app/distribute/windows-installer/)

