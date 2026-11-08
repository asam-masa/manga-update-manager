# タグ管理読取テストの期限（Issue #506）

## 観測と確認範囲

PR #505の[CI初回](https://github.com/asam-masa/manga-update-manager/actions/runs/42378480325/attempts/1)で、TestTagManagementReadsAndReopenが16.39秒かかり、準備後のListWorksでcontext deadline exceededとなった。headは`0a35c72ea99c884716c7eaadfbb64e1487fd3913`。このPRにstorageの変更はなく、変更対象のApplicationは成功していた。

旧テストは先頭で10秒のbounded contextを作り、空DBの準備・確認、複数のDB操作を行うfixture作成、読取と再接続の確認まで共有していた。各fixture操作には別の期限があるため、fixtureが正常終了しても外側の期限が先に切れる構成だった。

変更前のローカル20回は成功し、同じheadの[Windowsジョブ再実行](https://github.com/asam-masa/manga-update-manager/actions/runs/42378480325/attempts/2)も成功した。CIの遅延原因と再現条件は未確定であり、画像検証の変更が原因とは断定しない。

## 修正と検証の限界

2026-11-08、正常系の各読取・再接続呼出しの直前にbounded(t)を作る方式へ変更する。1操作あたりの10秒制限とt.Cleanupによる取消は保ち、準備や前の確認の時間を次の操作の期限へ含めない。正常系の内容・非変更・再接続後の結果の確認は維持する。

変更はこのテストの期限の範囲だけであり、製品の期限、共通bounded、専用の取消・タイムアウト試験、CI設定を変更しない。1操作自体が10秒を超える場合の失敗は残る。CIのあらゆる期限切れや環境遅延を解消したとは扱わない。

対象テストの繰返し、storage全体の再実行、Go全体のテスト・静的解析・ビルド、Windows CIで確認する。具体的なコマンドと対象版・結果はIssue #506とPRへ記録する。検証は一時SQLiteの合成データに限り、利用者データ・実画面の操作は対象外とする。
