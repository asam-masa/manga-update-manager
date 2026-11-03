# ADR-0021: ブラウザー上の実React画面をPlaywrightで検証する

- Status: Accepted
- Validation Status: Verified
- Date: 2026-11-02
- Related: [#357](https://github.com/asam-masa/manga-update-manager/issues/357)、[#358](https://github.com/asam-masa/manga-update-manager/issues/358)

## Context

画面変更と依存更新のたびに実WebView2での手動確認が増えている。既存のVitest・jsdomは画面状態や操作を検証するが、実ブラウザーのレイアウトや描画の回帰は保証しない。

2026-10-10の[#35](https://github.com/asam-masa/manga-update-manager/issues/35)では、Wails 2.14.0の環境変数方式でCDP接続口を確認できなかった。2026-11-02に現行Wails 2.17.1 / go-webview2 1.0.22のソースを確認しても環境変数消去処理は残っている。現行版への実接続は再試行しておらず、全方式が不可能とは判断しない。

## Decision Drivers

- 通常の画面変更に伴う手動確認を早く減らす
- 実際の製品画面を検証し、別サンプルとのずれを避ける
- 利用者データと製品ビルドからテストを分離する
- 自動化の保証範囲と実Wails・OS境界の未確認を区別する

## Considered Options

| 候補 | 利点 | 負担・リスク |
| --- | --- | --- |
| 既存試験と手動確認を継続する | 新規依存が不要 | 描画回帰の確認負担が残る |
| 実Wails接続の解決を先行する | 接続できればGoまで含む経路を検証できる | 接続の調査中は画面自動化を使い始められない |
| ブラウザーの実React画面を先行する | 表示・操作の回帰を先に保護できる | 合成APIの成功は実Go・DBの成功を証明しない |

## Decision

ブラウザー上の実React画面をPlaywrightで検証する。実Wailsへの接続は独立したSpikeへ分ける。Playwrightはブラウザー操作・レイアウト検証と基準画像比較を一つの基盤で扱えるため採用する。具体版とライセンス・依存条件は導入Issueで確認する。

小型・行一覧・カードの通常幅／狭い幅を代表画像比較の対象とする。撮影だけでなく期待する操作とレイアウトも検証する。基準画像と比較はWindowsの統一環境で行い、初回の基準画像を確認し、意図した表示変更のときだけ更新する。

合成API・データ・画像をテスト側で用意し、製品と同じReact画面を開く。差し替えはテスト境界に限定し、製品ビルドへテスト用コードや画像を混入させない。既存Vitest・Go試験と製品ビルドを維持する。

自動化済みの通常画面変更は、Playwright成功とCodexによる撮影画像の確認を標準にする。新デザインの初回合意、実WebView2・IME・ネイティブ選択・実保存など、ブラウザーで確認できない影響は理由と対象を添えて手動確認する。未実装・未検証の自動化を理由に既存の必要な手動確認を省略しない。

## Consequences

ブラウザー版の描画と操作の回帰を保護できる一方、合成境界の保守と基準画像のレビューが増える。環境の違いによる画像差分も扱う必要がある。基準画像を無条件に更新して失敗を消さない。

設計後は[#359](https://github.com/asam-masa/manga-update-manager/issues/359)の検証基盤、[#360](https://github.com/asam-masa/manga-update-manager/issues/360)の画像比較、[#361](https://github.com/asam-masa/manga-update-manager/issues/361)の代表操作とWindows CIへ分ける。実Wails調査[#362](https://github.com/asam-masa/manga-update-manager/issues/362)は先行実装のブロッカーにしない。

## Validation

方針と分割は利用者の承認済み。設計時点ではPlaywrightの導入・実行・画像比較・CI組み込みは未実施のためPendingとした。次を満たしたときにVerifiedを検討する。実Wails Spikeの成功はこのADRの条件に含めない。

- 合成境界から実React画面を検証でき、製品成果物へのテスト混入がない
- Windowsの同一条件で6代表画面を反復比較し、意図した変化を検出できる
- 代表操作と画像比較がCIで実行され、失敗が必須チェックへ伝わる
- 基準画像と失敗時出力を確認でき、自動化済み範囲と残る手動確認を説明できる

2026-11-02の#359ではPlaywright 1.64.1の基盤と2件のスモーク試験を追加した。画像比較・代表操作・CIの検証条件は残るためPendingを維持する。実行条件と保証範囲は[基盤の手順](../browser-ui-testing.md)を参照する。

同日の#360では6画面の比較と配置検証を追加し、ローカルWindowsで3回反復・故障注入を確認した。[校正と残る検証](../verification/browser-visual-comparison.md)を記録し、代表操作・CI必須化が残るためPendingを維持する。

2026-11-03の#361では[代表操作5件](../verification/browser-operations.md)を追加した。ローカルで3回反復の15件と、Esc故障の検出を確認した。CIではスモーク・画像比較を含む13件を3回反復し、非0終了を`Browser checks`へ伝える。CI結果はPRに記録する。mainの既存rulesetには必須ステータスチェックがなく、[追加案](../develop/workflow/browser-required-checks.md)の個別承認・適用と読み戻しが残るためPendingを維持する。合成APIの成功を実Wails・DB成功とは扱わない。

同日の外部設定承認後、[PR #367](https://github.com/asam-masa/manga-update-manager/pull/367)で必須チェックの適用・読み戻しを完了した。マージ後の[Issue #368](https://github.com/asam-masa/manga-update-manager/issues/368)で次の全条件を照合し、ValidationをVerifiedへ更新する。上記のPendingは各段階の履歴である。

| 検証条件 | 確認した根拠と結果 |
| --- | --- |
| 実React画面と合成境界、製品成果物の分離 | #359の基盤、製品入口を使うビルド、テストモジュール混入防止を維持。#361のCIで混入防止付きビルド・スモークも成功 |
| 同一Windows条件で6画面を反復比較し、変化を検出 | #360で背景変更と横スクロール不可能な故障を検出。#361の[run 41561064257](https://github.com/asam-masa/manga-update-manager/actions/runs/41561064257)でWindows 10.0.26100・Chromium 156.0.8078.4の比較を含む13件×3回が成功。基準PNG6枚は未変更 |
| 操作・比較のCI実行と必須チェックへの伝達 | ローカルのEsc・並び順変更の故障で非0終了を確認。CIはcontinue-on-errorなし。個別承認後にruleset 24450508へGitHub Actions発行の2チェックを追加し、mainの有効ルールとPR #367のrequired欄で確認。既存保護を保持 |
| 出力確認と残る手動確認の説明 | 初回6画像と失敗証跡、#361のCI撮影・環境記録・artifactを確認。[代表操作と手動境界の対応表](../verification/browser-operations.md#手動確認との対応)を記録 |

CIの対象headは`f794a1459f38ff2228a83a67da41931acd15baf7`。マージコミット`1b434ed4c7e4f419e17b4536f0aac665b3a1462d`とのファイル差分はない。CIへ故障を入れた実行や、失敗状態での実マージは行っていない。VerifiedはこのADRのブラウザー検証導入条件を満たした意味であり、全操作や全Windows環境の保証ではない。実Wails接続・IME・ネイティブ選択・実DB保存の結合と新デザインの初回合意は、引き続き別の確認対象である。

## References

- [ブラウザー画面検証の設計](../browser-ui-testing.md)
- [テスト方針](../develop/quality/testing.md)
- [過去の実Wails接続調査](../verification/webview2-playwright-spike.md)
- [Playwrightの画像比較](https://playwright.dev/docs/test-snapshots)
- [PlaywrightのWebView2接続](https://playwright.dev/docs/webview2)
- [現行loaderの環境変数処理](https://github.com/wailsapp/go-webview2/blob/v1.0.22/webviewloader/env_create.go)
- [Wails 2.17.1のWindows公開設定](https://github.com/wailsapp/wails/blob/v2.17.1/pkg/options/windows/windows.go)
