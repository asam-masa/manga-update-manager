# 生成物の管理

## 方針

自動生成物は、再生成可能性とレビューの必要性を確認してからGitで管理するか決定します。生成されたという理由だけで一律に追加または除外しません。

技術スタック確定後、次を一覧化します。

| 生成物 | Git管理 | 再生成コマンド | レビュー方法 |
| --- | --- | --- | --- |
| `frontend/wailsjs/` | 管理する | `wails generate module`または`wails build` | Goとのバインディング差分を確認する |
| `frontend/package.json.md5` | 管理する | `wails build` | package.json変更時のWails生成差分を確認する |
| `frontend/dist/` | 管理しない | `npm --prefix frontend run build` | ビルド成功を確認する |
| `frontend/.vite/` | 管理しない | `wails dev`またはViteの実行 | 再生成できることを確認する |
| `frontend/.vitest/`, `frontend/coverage/` | 管理しない | フロントエンドテスト | ソースとテスト結果をレビューする |
| `build/bin/` | 管理しない | `wails build` | Windows向けビルド成功を確認する |

## 判断基準

Wails v2.14.0が生成する`frontend/wailsjs/go/models.ts`には、空行のタブと末尾の余分な空行が含まれる場合がある。再生成後は末尾空白と末尾の余分な空行だけを除去し、`git diff --check`で確認する。型と変換処理は手編集しない。

- クリーンな環境で再生成できるか
- 生成ツールのバージョンを固定できるか
- 配布またはビルドに事前生成物が必要か
- 差分レビューに意味があるか
- 生成物へ個人パスや秘密情報が含まれないか

Gitで管理しない生成物は`.gitignore`へ追加し、再生成コマンドをREADMEまたはこの文書へ記載します。

