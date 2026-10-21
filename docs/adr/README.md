# Architecture Decision Records

重要な設計判断を時系列で記録します。作成方法は[ADR運用ガイド](../develop/architecture/architecture-decision.md)を参照してください。

## 一覧

| 番号 | タイトル | Status | Validation | Date | Related |
| --- | --- | --- | --- | --- | --- |
| [0001](./0001-select-desktop-application-foundation.md) | デスクトップアプリケーション基盤を選定する | Accepted | Verified | 2026-10-04 | [#3](https://github.com/asam-masa/manga-update-manager/issues/3) |
| [0002](./0002-select-application-architecture.md) | 内部アーキテクチャを選定する | Accepted | Pending | 2026-10-04 | [#7](https://github.com/asam-masa/manga-update-manager/issues/7) |
| [0003](./0003-select-frontend-foundation.md) | フロントエンド構成を選定する | Accepted | Pending | 2026-10-04 | [#13](https://github.com/asam-masa/manga-update-manager/issues/13) |
| [0004](./0004-select-sqlite-access.md) | SQLiteアクセス方式を選定する | Accepted | Pending | 2026-10-04 | [#13](https://github.com/asam-masa/manga-update-manager/issues/13) |
| [0005](./0005-define-library-behavior.md) | 作品一覧の操作と表示状態の扱いを決める | Accepted | Pending | 2026-10-09 | [#25](https://github.com/asam-masa/manga-update-manager/issues/25)〜[#31](https://github.com/asam-masa/manga-update-manager/issues/31) |
| [0006](./0006-persist-display-settings.md) | 表示設定をSQLiteへ保存する | Accepted | Verified | 2026-10-10 | [#27](https://github.com/asam-masa/manga-update-manager/issues/27) |
| [0007](./0007-require-change-reviews.md) | 差分レビューと重要変更の独立レビューを必須にする | Accepted | Pending | 2026-10-10 | [#38](https://github.com/asam-masa/manga-update-manager/issues/38) |
| [0008](./0008-manage-imported-cover-images.md) | 利用者の画像を一覧用に取り込んで管理する | Accepted | Pending | 2026-10-10 | [#28](https://github.com/asam-masa/manga-update-manager/issues/28)、[#40](https://github.com/asam-masa/manga-update-manager/issues/40) |
| [0009](./0009-verify-windows-build-in-ci.md) | Windows CIでテストと配布用ビルドを検証する | Accepted | Pending | 2026-10-11 | [#46](https://github.com/asam-masa/manga-update-manager/issues/46) |
| [0010](./0010-propose-dependency-updates-with-renovate.md) | Renovateで依存更新を提案し手動レビューする | Accepted | Pending | 2026-10-11 | [#48](https://github.com/asam-masa/manga-update-manager/issues/48) |
| [0011](./0011-generate-png-cover-thumbnails.md) | 静止画像を検証してPNGサムネイルを生成する | Accepted | Verified | 2026-10-11 | [#41](https://github.com/asam-masa/manga-update-manager/issues/41) |
| [0012](./0012-check-documentation-after-merge.md) | 文書確認を差分レビューとマージ後の照合に分ける | Accepted | Pending | 2026-10-11 | [#52](https://github.com/asam-masa/manga-update-manager/issues/52) |
| [0013](./0013-define-cover-operation-boundaries.md) | 画像操作と不要ファイル削除の成功境界を分ける | Accepted | Pending | 2026-10-11 | [#42](https://github.com/asam-masa/manga-update-manager/issues/42)、[#56](https://github.com/asam-masa/manga-update-manager/issues/56) |
| [0014](./0014-lock-storage-before-startup.md) | DB接続より先に保存先の排他を確保する | Accepted | Verified | 2026-10-11 | [#58](https://github.com/asam-masa/manga-update-manager/issues/58) |
| [0015](./0015-separate-go-minimum-and-validation-version.md) | Goの最低版と検証版を分ける | Accepted | Verified | 2026-10-12 | [#67](https://github.com/asam-masa/manga-update-manager/issues/67) |
| [0016](./0016-preserve-safe-api-errors-through-wails.md) | Wailsのエラー包装を越えて安全なAPI情報を保持する | Accepted | Verified | 2026-10-20 | [#181](https://github.com/asam-masa/manga-update-manager/issues/181)、[#180](https://github.com/asam-masa/manga-update-manager/issues/180) |
| [0017](./0017-poll-cover-progress-snapshots.md) | 画像処理の観測状態を定期取得する | Accepted | Verified | 2026-10-20 | [#186](https://github.com/asam-masa/manga-update-manager/issues/186)、[#183](https://github.com/asam-masa/manga-update-manager/issues/183) |

