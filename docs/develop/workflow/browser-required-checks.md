# ブラウザー検証の必須チェック設定案

## 現在の状態

2026-11-03にmainの有効ルールとrepository ruleset `24450508`（Protect main）を読み取った。既定ブランチ対象・active・例外なしで、削除禁止、強制push禁止、PR必須、線形履歴を設定済み。`required_status_checks`は未設定である。classic branch protectionの404だけでは保護なしと判断しない。

Issue [#361](https://github.com/asam-masa/manga-update-manager/issues/361)は外部保護設定の個別承認を要求している。この文書は未適用の提案であり、CIジョブの変更だけでマージが阻止されると報告しない。

## 追加する内容

既存rulesetのルール・対象・例外・PR設定は保持し、次のルール1件を追加する。GitHub Actionsのapp ID `15368`は既存mainのcheck-runsから確認した。新しい`Browser checks`もPRのcheck-runsで同じ発行元を確認してから適用する。

```json
{
  "type": "required_status_checks",
  "parameters": {
    "required_status_checks": [
      {"context": "Windows checks", "integration_id": 15368},
      {"context": "Browser checks", "integration_id": 15368}
    ],
    "strict_required_status_checks_policy": false,
    "do_not_enforce_on_create": false
  }
}
```

2チェックの成功を要求し、待機・失敗中はマージできなくする案である。`strict_required_status_checks_policy: false`は最新mainへの追従を別の必須条件にはしない。PR更新後のチェックは引き続き必要となる。旧`Browser visual calibration`は指定しない。新しいチェックが存在しない古いPRは、適用前にmain取り込み等によるワークフロー実行が必要になる場合がある。PRの自動マージは有効にしない。

## 承認後の適用と確認

1. PRの`Windows checks`と`Browser checks`、後者の操作・比較ステップ成功と証跡を確認する。
2. この追加内容について利用者の個別承認を得る。
3. rulesetを再取得し、取得時点から他の変更がないか確認する。最新の既存設定を保持した更新要求へ上記1件だけを加える。他のルール変更が必要なら追加で相談する。
4. repository rulesetの更新APIを使う。classic protectionを別途追加しない。
5. rulesetとmainの有効ルールを読み戻し、発行元・名前・active・例外なしと、既存ルールの保持を照合する。PRのチェック欄でも必須扱いを確認する。失敗状態での実マージは試さない。
6. 観測日時と結果をIssue・PRへ記録し、文書整合確認を実施する。ADR-0021の残る条件を確認するまでVerifiedにしない。

戻す必要が生じた場合も承認を得たうえで、追加した必須チェックルールだけを除去し、既存の保護ルールを残す。承認前には更新APIを呼ばない。

設定項目と更新APIは[GitHub公式REST仕様](https://docs.github.com/en/rest/repos/rules#update-a-repository-ruleset)に従う。認証情報は記録しない。
