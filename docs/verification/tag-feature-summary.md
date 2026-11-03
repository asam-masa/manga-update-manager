# タグ機能全体の検証照合

Issue #372、2026-11-03。対象mainは`12842835c2a5d83d878eafedc0b28d219b92374d`。ADR-0020の全体検証条件を、実装と試験、先行実機記録に対応付けた。

| 条件 | 根拠 |
| --- | --- |
| 名前の境界と表記 | internal/manga/tag_test.goで空名・空白・50/51コードポイント・改行/制御文字・不正UTF-8、frontend/src/EditWorkDialog.tags.test.tsxでGo互換入力、tag-filter.test.tsで検索の表記差と保存名の区別 |
| 移行と再接続、既存データ保持 | internal/storage/tags_migration_test.goで旧DBの作品・画像・予定日・表示/整列/未来設定を保存し、2回の再接続で保持と移行一回のみを確認。移行途中失敗のロールバックも検証 |
| 一括確定・取消・失敗・競合 | internal/storage/tags_test.goの既存タグ再利用・明示解除・部分保存拒否・commit不明・rollback失敗、app_tags_test.goの公開境界、EditWorkDialog.tags.test.tsxの取消で非送信・入力保持・再確認 |
| 管理による作品と関連の保持 | internal/storage/tag_management_change_test.goで全作品の改名反映・タグ関連だけの削除・再接続後の作品/画像/他タグ保持、確認後の対象入替・競合writer・commit失敗を検証。app_tag_management_change_test.goで精度・受付/終了・結果分類を確認 |
| AND・検索・予定・ページ・遅延と通知 | tag-filter.test.ts、use-library-snapshot.test.tsx、use-tag-selection.test.tsx、App.tag-filter.test.tsx。#373 / PR #374で検索・複数タグ・未来OFFと20件/19件のページ分割を直接結合検証。古い応答・未確認表示・アクセス結果保持・遅い登録とIME/ページも検証 |
| 実WebView2の編集・管理・絞り込み | [編集](./work-tag-editor.md)、[削除確認](./work-delete-tags.md)、[管理一覧](./tag-management-list.md)、[改名](./tag-management-rename.md)、[削除](./tag-management-delete.md)、[絞り込み](./tag-filter-ui.md)に専用保存先・exe識別・利用者操作・再起動とDB照合を記録 |

上記の単体・SQLite結合・合成APIの画面試験と実機記録を合わせ、初回タグ機能の検証条件を満たしたと判断する。各段階で記録したPendingは当時の範囲を示す履歴である。

PR #374の[CI run 41567004562](https://github.com/asam-masa/manga-update-manager/actions/runs/41567004562)は両チェック成功。head `6ef30d4eb914d06400aeefca150e95cbd459d7d8`と対象mainはファイル差分なし。frontend438件の成功は同PRの実行記録に基づく。今回の照合ではタグ関連Go試験も再実行し、結果は本PRへ記録する。

残る制約は、実OSのディスク/接続故障、全IMEイベント列・全倍率/解像度の網羅、ブラウザーから実Wailsへの自動E2Eである。合成故障の成功を物理故障の再現とは扱わない。これらは初回仕様の未実装ではなく保証範囲の限界として残す。追加の利用者操作確認は行っていない。
