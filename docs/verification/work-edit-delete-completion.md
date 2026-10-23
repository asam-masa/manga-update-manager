# 編集・削除の検証完了の照合

2026-10-23、親Issue [#205](https://github.com/asam-masa/manga-update-manager/issues/205)（編集、完了済み）と[#206](https://github.com/asam-masa/manga-update-manager/issues/206)（削除）の受け入れ条件を、基準main `9b4913c92dbd831cd64c84ae485872f94c85b66c`の実装・テストとマージ済みの検証記録に照合した。正本は[ADR-0018](../adr/0018-edit-and-delete-works.md)。新機能や安全条件の緩和ではなく、段階的な検証結果を集約した完了判断である。

## 編集の条件と根拠

| 条件 | 実装・検証の根拠 |
| --- | --- |
| 4項目を保存/キャンセルで変更 | #209/#213/#215。[保存基盤](./work-edit-storage.md)、[API](./work-edit-api.md)、[編集ダイアログ](./work-edit-dialog.md)。App.edit.test.tsxと実WebView2で保存・未保存破棄を確認 |
| ID・画像・登録日時・最終アクセスの保持、不正入力・重複拒否 | Store.EditWorkは4項目と更新日時だけを変更。一時SQLiteとAPIテストで非空の保持項目・再接続・変更なし・不正入力・重複を確認 |
| 同作品の競合拒否、別作品継続、終了との順序 | WorkOperations、Appの編集/画像/URL入口、[共通受付](./work-operation-admission.md)と編集APIテスト。別作品も既存のDB・画像・参照排他の制約を受ける |
| 失敗時の入力、検索、古い応答、フォーカス | App.edit.test.tsxで遅延応答・失敗・検索条件外通知を確認。実WebView2の保存/取消と利用者のTab/Shift+Tab・Esc・復帰確認を併用 |
| 一時DB・合成画像、実WebView2、レビュー・文書 | #205は完了済み。上記と#219の[保存中の終了案内](./work-edit-close.md)、各PRの通常/独立レビュー・CI、[学習資料](../learning/work-edit-delete.md)を確認 |

## 削除の条件と根拠

| #206の条件 | 実装・検証の根拠 |
| --- | --- |
| 作品名・URLの確認後に完全削除、取消では変更しない | DeleteWorkDialogとApp.delete.test.tsx。#224の[確認UI](./work-delete-dialog.md)で初期キャンセル・取消・削除・キー操作・復帰を実WebView2確認。Undo・ゴミ箱は追加しない |
| 元画像・共有画像・所有/安全性未確認のファイルを保持 | WorkDeleterから呼ぶCoverImporter.cleanupDeletedWorkは既存の未参照・管理記録・ファイル同一性と安全性確認を通す。TestWorkDeleterImagesAndStartupCleanupでnormal/shared/unknown/changed-file等を一時SQLite・合成PNGで比較。元画像に見立てた管理外ファイルのバイト保持も確認。[Application検証](./work-delete-application.md) |
| DB確定・結果不明・後処理失敗を区別し、戻し・自動反復をしない | [Store](./work-delete-storage.md)でCommit競合と合成Commit済みエラー、[Application](./work-delete-application.md)で呼出し1回・確定前の画像準備なし・確定後警告、[API](./work-delete-api.md)で安全なコードと警告、UIテストで不明結果の停止と再送なしを確認 |
| 同作品の5経路の競合拒否、別作品と既存停止・整理・終了の保護 | TestAppDeleteWorkAdmissionThroughCleanupAndCloseは実Store削除後の画像未参照確認を待機させ、同作品5経路拒否・別作品編集・終了拒否/再要求を確認。TestWorkDeleterRetainsReferencesThroughReleaseは資源解放までの参照排他を確認。画像だけ停止時の警告付き削除、未開始の起動時整理を終了拒否で取り消さないことも確認 |
| 検索・古い一覧/操作応答・確認対象の陳腐化・フォーカス | App.delete.test.tsxで検索・遅延一覧・ID再利用・対象変更時の再取得/再確認・取得失敗・不存在・不明結果を確認。Appは画像/編集/アクセス応答を登録日時も照合して反映。Storeは現在の5項目の完全一致と削除を同じTxで結ぶ。キー操作は#224の実機確認を併用 |
| 一時DB・合成PNG、UI自動テストと実WebView2、解析/ビルド、レビュー・文書 | 上記と#230の[削除中の終了案内](./work-delete-close.md)で受付内待機・専用案内・解除後実削除・自動終了なし・再要求終了・待機点なし通常版の再起動保持を確認。通常/独立レビューは各PR、統合版の再検証は本PRへ記録 |

## マージ済みの版とCI

2026-10-23にGitHub上で以下のPRがMERGED、各Windows checksがSUCCESSであることを確認した。CIは各PRのheadに対する結果であり、squash後のmainに同じ実行を行ったとは扱わない。CI成功は実機確認の代わりではない。

| PR | 範囲 | squash merge | 成功したWindows CI run |
| --- | --- | --- | --- |
| [#210](https://github.com/asam-masa/manga-update-manager/pull/210) | 編集SQLite | `02eaf44a9455f57bb9ea9c5e972b70558a60be8e` | [39614768355](https://github.com/asam-masa/manga-update-manager/actions/runs/39614768355) |
| [#212](https://github.com/asam-masa/manga-update-manager/pull/212) | 共通受付 | `ea687038d36f6210e4400643d685dbcfeaf0d853` | [39619974044](https://github.com/asam-masa/manga-update-manager/actions/runs/39619974044) |
| [#214](https://github.com/asam-masa/manga-update-manager/pull/214) | 編集API | `3ec1847a80c1d3361cd8712bf7e6b5338ca5e216` | [39777325945](https://github.com/asam-masa/manga-update-manager/actions/runs/39777325945) |
| [#216](https://github.com/asam-masa/manga-update-manager/pull/216) | 編集画面 | `025a61564b15f746ebb9db9b6dbc95a485cffe31` | [39783287798](https://github.com/asam-masa/manga-update-manager/actions/runs/39783287798) |
| [#220](https://github.com/asam-masa/manga-update-manager/pull/220) | 編集終了案内 | `d1811a27a5460a3e3b399aef39fe8324cb9e4edf` | [39789655073](https://github.com/asam-masa/manga-update-manager/actions/runs/39789655073) |
| [#226](https://github.com/asam-masa/manga-update-manager/pull/226) | 削除SQLite | `acbf5201ecfcdfc719277712babdd326db29c5e1` | [39880948489](https://github.com/asam-masa/manga-update-manager/actions/runs/39880948489) |
| [#228](https://github.com/asam-masa/manga-update-manager/pull/228) | 削除Application | `94e3fc1ef80f4fe14de31d6736c3ed6f1f223302` | [39892769427](https://github.com/asam-masa/manga-update-manager/actions/runs/39892769427) |
| [#229](https://github.com/asam-masa/manga-update-manager/pull/229) | 削除API | `76720d9b764343227991a00309890d06257b80c4` | [39898845146](https://github.com/asam-masa/manga-update-manager/actions/runs/39898845146) |
| [#231](https://github.com/asam-masa/manga-update-manager/pull/231) | 削除画面 | `4a2b2efed0c3f7a052bec2e3ef993fb1bbf5be78` | [39909505772](https://github.com/asam-masa/manga-update-manager/actions/runs/39909505772) |
| [#232](https://github.com/asam-masa/manga-update-manager/pull/232) | 削除終了案内 | `9b4913c92dbd831cd64c84ae485872f94c85b66c` | [39912450394](https://github.com/asam-masa/manga-update-manager/actions/runs/39912450394) |

## 判断と適用範囲

ADR-0018で採用した初回の編集・確認後完全削除と、その追加合意の検証条件を上記の範囲で満たしたため、Validation StatusをVerifiedとする。過去の検証記録にある「未接続」「Pending」は各子Issue当時の範囲として残し、現在の状態は本照合とADRの最新記録を参照する。設計の再承認や新機能の導入ではない。

Verifiedは、全障害・全競合順序・全環境で安全なことの証明ではない。物理Commit/Close故障、電源断、無改変productionでの長時間I/O遅延、実利用者データでの自動削除は未検証であり、既存の合成driver・境界注入・計測待機をそれらへ読み替えない。UIの異常応答は合成DTO、キー操作と通常の保存/削除・再起動は実WebView2の根拠を併用する。

同じ登録の識別に用いるIDと登録日時は永続的な世代IDではなく、任意の時計巻戻しによる完全一致まで保証しない。管理画像の即時回収、復元・Undo・バックアップ、自動最新話取得は保証・実装範囲に含めない。ADR-0013の既存検証を新しい削除機能の検証として流用せず、今回のStore/Application/API/UIの根拠で判断した。

#206のDefinition of Doneのうち、今回の文書差分のレビュー・CI・mainへのマージは本PRで完了させる。IssueはこのPRのマージ時に閉じ、未マージの文書をmainの確定状態として報告しない。
