# ADR-0003: フロントエンド構成を選定する

- Status: Accepted
- Validation Status: Pending
- Date: 2026-10-04
- Related: [#13](https://github.com/asam-masa/manga-update-manager/issues/13)

## Context

本プロジェクトは、漫画作品を表紙カードと表形式で一覧表示し、登録、編集、絞り込み、更新状態の確認を一画面で行う。デスクトップ基盤にはGo＋Wails v2を採用済みである。

Wails v2の公式テンプレートには、React、Vue、Svelte、Preact、Lit、VanillaのJavaScript版とTypeScript版がある。別リポジトリの`browser-launcher`ではReact＋TypeScriptを使用している。

本プロジェクトの主な学習対象はGoである。フロントエンドへ新しい技術を過度に追加せず、一覧とフォームを安全に実装できる構成を決定する。具体的なUIデザイン、ルーティング、状態管理ライブラリ、UIコンポーネントライブラリは対象外とする。

## Decision Drivers

- 表紙カード、表形式、登録・編集フォームを実装しやすいこと
- TypeScriptでWails境界の型を扱えること
- `browser-launcher`の知識を再利用できること
- Goの学習へ集中できること
- 初期依存と構成を小さく保てること
- Unit TestとUI Testを追加できること

## Considered Options

### React＋TypeScript

- 利点:
  - `browser-launcher`の構成と知識を再利用できる
  - 状態を持つ一覧、フォーム、絞り込みをコンポーネントへ分けやすい
  - Wailsに公式の`react-ts`テンプレートがある
  - TypeScriptでGoとの入出力境界を確認できる
- 欠点:
  - React固有の状態管理と再描画を理解する必要がある
  - 小さな画面でもビルド環境と依存パッケージが必要になる
- リスク:
  - コンポーネント分割や状態管理を増やしすぎる可能性がある
  - `browser-launcher`の構成を必要性の検討なしに移植する可能性がある

### Vanilla＋TypeScript

- 利点:
  - フレームワーク固有の依存と概念が少ない
  - DOMとブラウザーAPIの動きを直接理解できる
  - Wailsに公式の`vanilla-ts`テンプレートがある
- 欠点:
  - 画面状態、入力フォーム、一覧更新を自前で同期する必要がある
  - 機能追加に伴ってDOM操作が分散する可能性がある
- リスク:
  - 初期の単純さと引き換えに、状態管理とテストが複雑になる可能性がある

### Preact＋TypeScript

- 利点:
  - Reactに近いコンポーネントモデルを小さい実装で利用できる
  - Wailsに公式の`preact-ts`テンプレートがある
- 欠点:
  - 本アプリでは軽量化による効果が限定的である
  - Reactとの差異を追加で確認する必要がある
- リスク:
  - 既存知識を再利用する目的に対して、中間的な選択になる可能性がある

### VueまたはSvelte＋TypeScript

- 利点:
  - 宣言的なUIとコンポーネント分割を利用できる
  - Wailsに公式のTypeScriptテンプレートがある
- 欠点:
  - Goと並行して新しいフロントエンド技術を学ぶ必要がある
  - `browser-launcher`との知識共有が減る
- リスク:
  - アプリ完成よりフロントエンド基盤の学習が中心になる可能性がある

## Decision

React＋TypeScriptを採用し、Wails v2の公式`react-ts`テンプレートを基準にする。

本プロジェクトではGoの理解を深めることを優先する。既に使用経験のあるReact＋TypeScriptを再利用し、作品一覧、フォーム、絞り込みに必要な画面構造を実装する。

初期版では、外部の状態管理ライブラリ、UIコンポーネントライブラリ、CSSフレームワーク、ルーターを追加しない。React標準の状態管理と通常のCSSで不足が確認された場合に、解決する問題を明確にして別途検討する。

## Consequences

- フロントエンドはReactコンポーネントとTypeScriptで実装する
- Wailsが生成するGoとのバインディングをTypeScriptから利用する
- `browser-launcher`の知識は再利用するが、構成を機械的に複製しない
- 画面全体へ状態を広げず、利用する場所に近いコンポーネントで管理する
- 状態共有が複雑になったという実測可能な問題が生じるまで、外部状態管理を導入しない
- UI部品の一貫性やアクセシビリティを自前で確認する必要がある

## Validation

Wails雛形生成と最初の画面実装で次を確認する。

- Windows 11で`react-ts`テンプレートの開発モードを起動できる
- TypeScriptからGoのMethodを呼び出せる
- 表紙カードと表形式の最小表示をReactコンポーネントで構成できる
- フロントエンドのビルドとテストを実行できる
- 外部状態管理とUIコンポーネントライブラリなしで初期画面を実装できる

2026-10-04にWails v2.14.0の公式`react-ts`テンプレートを基準とした雛形で次を確認した。

- Windows 11で開発モードを起動できた
- TypeScriptからGoの`App.Status`を呼び出すバインディングを利用できた
- `npm --prefix frontend run build`が成功した
- 外部状態管理ライブラリ、UIコンポーネントライブラリ、CSSフレームワーク、ルーターなしで初期画面を実装できた

表紙カードと表形式の最小表示、およびフロントエンドテストは後続Issueで検証する。すべての検証項目を完了していないため、Validation Statusは`Pending`を維持する。

Issue #23で登録フォームとプレースホルダー付きの作品カードをReact標準の状態とCSSで実装した。APIを差し替えたDOMテストで登録、一覧、失敗、キーボード操作を確認した。表形式と実際の表紙表示は未実装であり、Validation Statusは`Pending`を維持する。

## References

- [Wails v2: Creating a Project](https://v2.wails.io/docs/gettingstarted/firstproject/)
- [ADR-0001](./0001-select-desktop-application-foundation.md)
- [要件](../要件.md)
