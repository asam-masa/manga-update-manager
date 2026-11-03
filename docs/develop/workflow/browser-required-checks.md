# ブラウザー検証の必須チェック設定と適用記録

## 現在の状態

2026-11-03に利用者の個別承認を受け、repository ruleset `24450508`（Protect main）へ`Windows checks`と`Browser checks`の必須ルールを追加した。既定ブランチ対象・active・例外なしと、既存の削除禁止、強制push禁止、PR必須、線形履歴およびPR設定を保持した。適用前は`required_status_checks`が未設定だった。classic branch protectionの404だけでは保護なしと判断しない。

Issue [#361](https://github.com/asam-masa/manga-update-manager/issues/361)と[PR #367](https://github.com/asam-masa/manga-update-manager/pull/367)へ承認・適用・確認結果を記録した。CIジョブを追加しただけの状態と、GitHub側の必須設定の完了を区別する。

## 追加した内容

既存rulesetのルール・対象・例外・PR設定を保持し、次のルール1件を追加した。GitHub Actionsのapp ID `15368`は既存mainとPR #367の両check-runsで確認した。

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

2チェックの成功を要求し、待機・失敗中のマージを制限する設定である。`strict_required_status_checks_policy: false`は最新mainへの追従を別の必須条件にはしない。PR更新後のチェックは引き続き必要となる。旧`Browser visual calibration`は指定していない。新しいチェックが存在しない古いPRは、main取り込み等によるワークフロー実行が必要になる場合がある。PRの自動マージは有効にしていない。

## 読み戻しと検証結果

同日にrulesetとmainの有効ルールを再取得し、上記2チェック・発行元・strict設定・作成時の扱いを確認した。適用前後のJSON比較で、名前・対象・active・例外なしと既存4ルールの保持も確認した。`gh pr checks 367 --required`は両チェックを必須として返し、両方`SUCCESS`だった。PR #367のマージ後にも有効ルールと必須チェック結果を再確認した。

[CI run 41561064257](https://github.com/asam-masa/manga-update-manager/actions/runs/41561064257)はhead `f794a1459f38ff2228a83a67da41931acd15baf7`で、ブラウザー39件と既存Windows checksが成功した。マージコミット`1b434ed4c7e4f419e17b4536f0aac665b3a1462d`とのファイル差分もない。失敗を伝える経路は、ローカルの故障注入による非0終了とCIの失敗を許容しない設定で確認した。失敗状態での実マージや、この版のCIへ故障を入れる検証は行っていない。

## 今回の適用手順

以下の手順で実施した。今後の設定変更や復旧を包括的に承認するものではない。

1. PRの`Windows checks`と`Browser checks`、後者の操作・比較ステップ成功と証跡を確認する。
2. この追加内容について利用者の個別承認を得る。
3. rulesetを再取得し、取得時点から他の変更がないか確認する。最新の既存設定を保持した更新要求へ上記1件だけを加える。他のルール変更が必要なら追加で相談する。
4. repository rulesetの更新APIを使う。classic protectionを別途追加しない。
5. rulesetとmainの有効ルールを読み戻し、発行元・名前・active・例外なしと、既存ルールの保持を照合する。PRのチェック欄でも必須扱いを確認する。失敗状態での実マージは試さない。
6. 観測日時と結果をIssue・PRへ記録し、文書整合確認を実施する。ADR-0021の残る条件を確認するまでVerifiedにしない。

戻す必要が生じた場合も承認を得たうえで、追加した必須チェックルールだけを除去し、既存の保護ルールを残す。承認前には更新APIを呼ばない。

設定項目と更新APIは[GitHub公式REST仕様](https://docs.github.com/en/rest/repos/rules#update-a-repository-ruleset)に従う。認証情報は記録しない。
