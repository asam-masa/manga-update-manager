# ブランチ戦略

## 方針

GitHub Flowを採用し、`main`と短命な作業ブランチで運用します。

- `main`は常にビルド・テスト可能な状態を保つ
- 作業ブランチは最新の`main`から作る
- 一つのブランチでは一つの関心事を扱う
- `main`への変更は原則としてPRを経由する
- レビューと必要な検証が完了してからマージする
- 原則としてSquash and mergeを使用する

## ブランチ名

```text
<type>/<issue-number>-<short-description>
```

| type | 用途 | 例 |
| --- | --- | --- |
| `feature` | 機能追加 | `feature/12-manga-registration` |
| `fix` | 不具合修正 | `fix/18-schedule-calculation` |
| `refactor` | 外部仕様を変えない改善 | `refactor/21-storage-boundary` |
| `docs` | 文書のみ | `docs/5-adr-policy` |
| `chore` | 開発環境や保守 | `chore/7-ci-setup` |

Issueがない軽微な作業では番号を省略できます。説明部分には小文字の英数字とハイフンを使用します。

