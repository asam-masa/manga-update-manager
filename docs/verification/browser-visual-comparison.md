# 代表6画面のブラウザー画像比較

## 対象と現在の結果

Issue [#360](https://github.com/asam-masa/manga-update-manager/issues/360)で、小型・行一覧・カードの通常幅／狭い幅に画像比較とレイアウト検証を追加した。製品のGo・React・CSSは変更しない。実装は[visual.spec.ts](../../frontend/e2e/visual.spec.ts)、判断の正本は[ADR-0021](../adr/0021-verify-react-ui-with-playwright.md)と[検証設計](../browser-ui-testing.md)である。

2026-11-02のローカルWindowsでは、既存スモーク2件＋画像比較6件を3回ずつ、合計24件成功した。Codexが6基準画像を開き、文字・画像・操作ボタン・折り返しを確認した。Windows CIでの校正結果はPRへ記録する。代表操作と必須チェック化は#361へ残す。

## 基準条件

| 項目 | 条件 |
| --- | --- |
| ブラウザー | Playwright Test 1.64.1のheadless Chromium 156.0.8078.4（revision 1248） |
| 通常幅／狭い幅 | 1200×900 / 600×900 CSS px、deviceScaleFactor 1 |
| 時刻・地域 | 2026-11-02 09:00（日本時間）、Asia/Tokyo、ja-JP |
| 撮影 | ページ全体、CSSピクセル、アニメーション停止、キャレット非表示、マスクなし |
| 比較 | `maxDiffPixels: 0`、色差の`threshold: 0.2`。異なると判定したピクセルは許容しない |
| データ | 合成6作品、長いタイトルとURL、日付あり／未設定、合成表紙・画像未設定・意図した404 |
| フォント | 製品のCSSを維持。`document.fonts.ready`を待ち、タイトルで実際に使われたフォントをCDPで記録 |

基準画像は[専用フォルダー](../../frontend/e2e/visual.spec.ts-snapshots/)の6 PNGだけを管理する。OS・Node.js・ブラウザー・タイトルの実フォント等は各試験の`environment.json`とHTMLレポートへ残す。基準画像の版は、当該PNGを追加・変更したコミットで識別する。

初回ローカル環境はWindows `10.0.26300`、Node.js `v24.22.1`。タイトルではYu Gothic UI BoldとSegoe UI Boldが使われた。CIは`windows-2025`で校正する。Windowsという名称だけで同一環境とみなさず、異なる結果では画像と環境記録を照合する。OSやフォント差を隠すために許容差を広げない。

## 画像と別に確認する条件

- 全6作品があり、ページ全体が横へはみ出さない
- 小型は通常5列／狭幅2列、カードは通常2列／狭幅1列で、同形式内の作品幅が揃う
- 行一覧は通常幅で横スクロール不要、狭幅では専用領域内でスクロールし、交互背景を保つ
- タイトルは小型3行・カード2行・行一覧1行以内に収まり、操作ボタンと重ならない
- 長いURLは小型以外で省略され、`title`属性に元のURLを残す
- 小型・カードは合成表紙の読込と意図した画像失敗表示が完了し、行一覧は画像を持たない

画像とフォントの準備を明示的に待った後、Playwrightが連続する撮影の安定も確認する。読込待ちや失敗表示をマスクして比較対象から外さない。

## 実行と意図した更新

```powershell
npm --prefix frontend run test:browser
npm --prefix frontend run test:browser -- --repeat-each=3
```

通常は`updateSnapshots: 'none'`で、基準の欠落・差分を自動更新しない。意図した表示変更に限り、理由と対象を確認して次を実行する。

```powershell
npm --prefix frontend run test:browser -- --grep "cards 1200" --update-snapshots
```

変更対象を絞って生成し、旧版とのGit差分・新しい撮影・環境条件を確認する。6 PNGは初回すべてCodexが確認する。新デザインは利用者との初回合意も必要になる。画像を更新しただけで回帰を解消したと判断しない。

## Windows校正と失敗の調査

既存Windows CIの`Browser visual calibration`ジョブで同じ比較を実行する。初期校正のため非ブロッキングとし、既存の必須`Windows checks`とは分ける。ジョブ全体やPR全体の緑だけで合格とせず、`Compare visual snapshots`ステップが成功したことを確認する。#361で代表操作と必須化を行う。

撮影・差分・トレース・環境情報・基準画像を、公式`actions/upload-artifact` v7.0.2のコミット固定で7日間保持する。標準のartifactとして校正結果を取得するために採用した。対象は合成データの出力だけであり、DB・利用者画像・通常プロファイルは含めない。取得と入力仕様は[公式Action](https://github.com/actions/upload-artifact/tree/v7.0.2)を参照する。

失敗時はartifact `browser-visual-calibration`、またはローカルの`frontend/playwright-report/index.html`を確認する。`*-actual.png`と`*-diff.png`、`environment.json`を照合し、製品差分・描画準備・環境差を切り分ける。CIは基準画像を自動更新しない。

## 回帰検出の検証と限界

ローカルでカード通常幅の背景を一時的に変更すると、834,804ピクセルの差分で失敗し、実撮影・差分画像・トレースが生成された。基準PNGのSHA-256は変わらなかった。一時コードを除去すると8件が再び成功した。

6枚は代表画面であり、全ページやモーダル、検索・タグ管理の操作、全Windows環境、実WebView2・IME・実保存を保証しない。製品のデザインは変えず、既存の必要な手動確認を一律には省略しない。

## 参考

- [Playwrightの画像比較と環境差](https://playwright.dev/docs/test-snapshots)
- [撮影アサーション](https://playwright.dev/docs/api/class-pageassertions#page-assertions-to-have-screenshot-1)
