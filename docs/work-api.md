# 作品APIと起動・終了処理

Issue #21で、保存基盤をWails APIへ接続した。登録フォームと一覧画面は[Issue #23の画面設計](./work-screen.md)で扱う。

## 保存先と接続の寿命

`platform.DataDirectory`は`os.UserConfigDir`でOSのアプリデータ領域を解決し、`MangaUpdateManager`フォルダーを作成する。Windowsでは`%AppData%\MangaUpdateManager\manga.sqlite`へ保存する。作業フォルダーへのフォールバックは行わない。

Issue #58で、Wails開始前に保存先を一度準備し、`.instance.lock`を共有なしで保持する。確保した同じ保存先を`OnStartup`のDB接続とマイグレーションに使う。排他取得前にDBを開かない。`OnShutdown`で接続を閉じ、Wails実行から戻った後にも終了処理を確認してから排他を解放する。API実行中は読み取りロック、起動・終了処理は書き込みロックを取り、操作中の接続を閉じない。

Wailsの起動コールバックは非同期なので、同じAppの終了済み状態をmutexで保護する。終了後に遅延した起動コールバックが実行されてもDBを開かない。再起動時は新しいAppを作る。

画像の起動時整理は初回描画後の`LoadCoverWarnings`から開始する。開始済みの整理・設定・変更・解除中の通常終了は案内して拒否し、取消や自動終了予約へ変えない。結果後に利用者が再度閉じる。表示用読み取りだけなら新規受付を止め、開始済み読み取りと解放を待つ。画像API・読み取りが使うDBもAppのロックで寿命を保護する。契約は[画像保存・配信](./cover-persistence.md#close失敗と通常終了)、確認範囲は[起動時整理](./verification/startup-cover-cleanup.md)・[終了拒否](./verification/cover-close.md)・[HTTPとDB寿命](./verification/cover-http-delivery.md)を参照する。

保存先の準備・排他取得に失敗した場合は画面を起動せず、起動中または安全な起動失敗をWindowsの案内で示して終了する。排他取得後のDB初期化失敗では従来どおり画面を起動し、作品APIが`database_unavailable`を返す。その場合も画面終了まで排他を維持する。利用者へ内部パスやSQLiteの詳細を公開せず、保存先の空き容量・アクセス権限の確認と再起動を案内する。初期化と終了の失敗は、個人パスを含まないログにも記録する。実行中の復旧・再接続は対象外である。寿命と制約は[ADR-0014](./adr/0014-lock-storage-before-startup.md)を参照する。

## 責務

- Wails境界: 入力DTOからドメイン入力への変換、Application呼び出し、出力DTOと利用者向けエラーへの変換
- Application: 時刻取得、ドメインによる正規化・検証、保存処理の呼び出し
- manga: 作品の規則と重複URLの共通エラー
- storage: SQL、ID昇順の一覧、SQLiteのUNIQUEエラーから重複URLエラーへの変換
- platform: OS保存先と日本時間の変換

Applicationの`WorkRepository`は登録と一覧だけを定義する。時刻は関数として受け取り、固定時刻でテストできる。既存ADR-0002とADR-0004に従う局所的な実装であり、新しい依存ライブラリとADRは追加しない。

## API契約

| API | 入力 | 正常結果 |
| --- | --- | --- |
| `CreateWork` | `url`, `title`, `siteName`, `thumbnailPath`, `notes` | 採番済みの作品DTO |
| `ListWorks` | なし | ID昇順の作品DTO配列。未登録時は`[]` |
| `OpenWork` | 作品ID | 起動要求と日時保存に成功した作品DTO |
| `EditWork` | 作品ID、`url`, `title`, `siteName`, `notes` | `work`と`warnings`。4項目だけを変更した作品DTO |

作品DTOは入力項目に加え、`id`, `createdAt`, `updatedAt`を持つ。日時は`Asia/Tokyo`のRFC 3339文字列（`+09:00`、小数秒は必要な桁数）で返す。DB保存は既存のUTC・小数秒固定9桁を維持する。IDと登録日時は利用者入力として受け取らない。`CreateWork`のサムネイル相対パス入力は既存規則で検証し、登録API自身は画像を取り込まない。画像操作は下記の専用APIが担当する。

### 画像APIの現在の接続範囲（Issue #146）

2026-10-21、基準main `a84e0669c8ebff0057c26625e0205cb0c939f90a`で、設定・変更・解除、起動時整理と警告、限定HTTP配信、カードの画像操作UIまで接続済みである。以下は既存契約の参照用要約であり、新しいAPI・成功条件を定めるものではない。

| API | 入力 | 正常結果と役割 |
| --- | --- | --- |
| `SelectWorkCover` | `workID` | Go側の選択・取り込み。`cancelled`, `work`, `warnings`。取消では作品を変更しない |
| `SelectWorkCoverWithOperation` | `workID`, `operationID` | 同じ取り込みに選択完了通知と段階観測を対応付ける |
| `RemoveWorkCover` | `workID` | 参照解除と後処理。`work`, `warnings` |
| `RemoveWorkCoverWithOperation` | `workID`, `operationID` | 同じ解除に段階観測を対応付ける |
| `LoadCoverWarnings` | なし | 初回の起動時整理を実行し、警告の配列を返す。後続要求は同じ実行結果を共有する |
| `LoadCoverProgress` | なし | 実行中要求の最新段階。操作結果・警告の確定には使わない |

画面から元画像パスを受け取らず、作品IDから処理する。操作IDは要求の対応付け用であり、保存先や取消権限ではない。DB参照確定を成功とし、その後の旧画像削除失敗は成功結果の警告へ分ける。Commit結果不明は成功や旧状態維持を断定せず、新旧を保持して再起動後の確認を案内する。詳細は[画像保存契約](./cover-persistence.md)、[取り込み](./verification/cover-import-api.md)、[解除](./verification/cover-remove-api.md)、[選択完了通知](./cover-persistence.md#選択完了通知)を正本とする。

画像は`GET /__covers/works/<作品ID>`の専用経路で現在参照から取得する。任意パスは公開せず、限定形式の再取得用目印だけを許可する。未設定・欠損・破損は仮表示とし、DB参照を自動解除しない。[HTTP契約](./verification/cover-http-delivery.md)、[再取得](./verification/cover-image-refresh.md)、[画面設計](./work-screen.md#登録済み作品の画像操作issue-43)を参照する。

エラーはWailsの`ErrorFormatter`から、`manga-update-manager:api-error:v1:`と安全な`{code,message}`のJSONを連結した文字列として返す。WailsがこれをError.messageへ包み、画面の共通処理が識別子・上限・既知コード・文言を確認して復元する。生のError.messageや未知形式は表示せず、DB停止判定も同じ検証を使う。生成TypeScriptの戻り値型は成功時の型だけである。[ADR-0016](./adr/0016-preserve-safe-api-errors-through-wails.md)を正本とする。成功DTOとDBの形式は変えない。

| code | 意味 |
| --- | --- |
| `database_unavailable` | 起動前、初期化失敗、終了後でDBを利用できない |
| `invalid_input` | ドメイン入力の検証失敗 |
| `duplicate_url` | 正規化後のURLが完全一致で登録済み |
| `storage_error` | DB操作の失敗 |
| `internal_error` | その他のバックエンドエラー |

重複判定は事前検索ではなく、SQLiteのUNIQUE制約を根拠とする。現在の登録SQLでUNIQUE対象はURLだけである。制約を追加する場合はエラー変換も見直す。

## Issue #25: ページ起動と最終アクセス

`WorkOpener`は利用側の`AccessRepository`、起動関数、時刻関数を受け取る。作品をIDで取得し、保存済みURLを検証し、既定ブラウザーへ渡し、成功後の時刻を保存する。同じIDは処理中だけ排他し、異なる作品を一律に止めない。終了処理は実行中APIの完了を待つ。

DTOの`lastAccessedAt`は未アクセスならJSONのnull、それ以外は日本時間のRFC 3339文字列とする。Wailsの生成型は`lastAccessedAt?: string`となるため、画面側の`WorkView`でnullを含める。生成された型は手編集しない。

起動失敗は`browser_open_failed`、起動成功後の保存失敗は`access_save_failed`、存在しないIDは`work_not_found`、処理中の同じIDは`work_open_in_progress`を返す。保存失敗は部分成功であり、ページは閉じず、画面は保存済み日時を維持する。内部診断情報は利用者へ返さない。

Windowsでは`ShellExecuteW`のエラーを返す`windows.ShellExecute`を使用する。OSスレッドを固定してCOMをSTAで初期化し、終了時に解放する。既存依存の`golang.org/x/sys`と`github.com/go-ole/go-ole`を直接依存へ変更し、新しいモジュールは導入しない。エラーを返さないWailsの`BrowserOpenURL`は今回使用しない。対象OSはWindowsである。

画面のURLはリンク風のbuttonとし、シェルへURLを渡す処理はGo側だけが担当する。Enter・Spaceで操作でき、処理中は無効になる。URLの全文はtitle属性、表示は1行省略とする。タイトルは通常の文字列のまま残す。一覧取得中に保存が完了しても古い応答が日時を戻さないよう、読取開始後の保存結果を反映する。

一次情報: [ShellExecuteW](https://learn.microsoft.com/en-us/windows/win32/api/shellapi/nf-shellapi-shellexecutew)、[CoInitializeEx](https://learn.microsoft.com/en-us/windows/win32/api/combaseapi/nf-combaseapi-coinitializeex)。要求の成功はページ表示や読了の保証ではない。

## Issue #27: 表示設定API

Issue #27で表示設定のApplicationとAPIを追加する。作品APIとは独立した操作であり、設定エラーは作品用コードへ混在させない。

| API | 入力 | 正常結果 |
| --- | --- | --- |
| `LoadDisplaySettings` | なし | `registrationFormVisible: boolean`のDTO。未登録ならtrue |
| `SetRegistrationFormVisible` | boolean | 成功時はvoid |

設定の未登録は正常扱いとし、読み取りだけでは書き込まない。読み取り失敗・未知値・DB利用不能は`settings_read_failed`、保存失敗・DB利用不能は`settings_save_failed`の安全な文言でrejectする。画面は読み取り失敗を表示の初期値で補い、保存失敗でも操作を継続する。明示操作の保存だけを行い、作品データや診断情報を返さない。新しいライブラリは追加しない。

## 検証と制約

### 表示形式の専用API（Issue #235）

| API | 入力 | 正常結果 |
| --- | --- | --- |
| `LoadWorkListMode` | なし | `compact`/`rows`/`cards`の文字列。未登録は`cards` |
| `SetWorkListMode` | 同じ3値の文字列 | void。変更した表示形式だけを保存 |

フォーム専用の既存`LoadDisplaySettings`とDTOは維持し、未知の表示形式でフォームの読込まで失敗させない。読取の未知値・利用不能・失敗は`settings_read_failed`でrejectし、通常の案内はメモなしカードでの続行を示す。保存失敗・利用不能は`settings_save_failed`、不正な入力形式は`invalid_input`。両操作ともStoreの`ErrDBStopped`は`database_stopped`を優先し画像側にも停止を通知する。内部診断は返さない。失敗時のAPI戻り値は保存済み値として使わない。

既存SQLiteの設定列追加と項目単位UPSERTを使い、全設定の一括更新や自動修復は追加しない。画面は#252で切替を即反映し、フォーム開閉と共有した要求列から保存を操作順に一つずつ送る。APIは同時到着した要求をUI操作順に並べ替えるものではない。フォームの開閉と表示形式が互いを上書きしないことと、同じ形式の連続保存の順序は別の責務である。

一時SQLiteで未登録・3値保存・双方向の項目保持・旧4移行からの追加・再接続・未知値の非修復・CHECK・書込拒否・読取不能・取消・DB停止を確認する。ApplicationとAppで不正入力、再起動相当の再接続、安全なコード/メッセージを確認する。#252で画面から専用APIを呼び、初期復元・警告UI・連続操作の順序をDOMで確認する。実WebView2の一体検証は#237へ残す。ADR-0005のPendingを維持し、既存ADR-0006のVerifiedを新しい表示形式の検証へ流用しない。

Issue #211で同AppのURL起動・画像設定/変更/解除へApplicationの共通受付を接続した。作品IDごとに操作完了まで保持し、操作ID付きAPIも同じ入口を通る。競合は既存の`work_open_in_progress`/`cover_busy`で返す。画像だけの停止と共通受付の終了状態を混同しない。[受付と終了の検証](./verification/work-operation-admission.md)を参照する。#213で編集API、#215で編集UIも接続した。[画面検証の範囲](./verification/work-edit-dialog.md)はAPIの自動試験と区別する。

### 編集API（Issue #213）

`WorkEditor`は利用側の`WorkEditRepository`と時刻関数を受け取り、ドメインで入力を正規化・検証して保存へ渡す。登録用interfaceは拡大しない。AppはDB寿命ロック内で`BeginEdit`を取得し、Storeの後処理・エラー分類まで保持する。画像だけの停止は編集を止めない。同作品の競合は`work_busy`、通常終了は案内して拒否し、処理終了後に再要求できる。

正規化後に変更がなければ更新日時も保持する。画像・登録日時・最終アクセスを入力として受け取らず、古い表示で上書きしない。不存在は`work_not_found`、不正入力・重複は既存コードで返す。

Commit結果不明は`work_edit_outcome_unknown`で拒否し、DB停止を通知する。自動再試行・自動復旧しない。保存確定後の接続解放失敗は作品付きの成功結果と`database_stopped_after_commit`警告に分ける。通常成功の`warnings`は`[]`。警告の有無で保存成功を取り消さない。画面の共通エラー復元とDB停止判定に結果不明コードを追加し、#215で警告表示・入力保持・追加編集の停止を接続した。[API検証](./verification/work-edit-api.md)と[画面検証](./verification/work-edit-dialog.md)を参照する。

Issue #186で、画像操作の最新段階を返す`LoadCoverProgress`と、観測用操作IDを受け取る`RemoveWorkCoverWithOperation`を追加した。既存の操作ID付き設定・変更も観測状態へ接続する。状態はメモリだけに保持し、結果・警告は従来の操作APIで判断する。取得結果の欠落を完了と扱わない。[API契約](./verification/cover-progress-api.md)と[ADR-0017](./adr/0017-poll-cover-progress-snapshots.md)を参照する。後続#183で画面の定期取得と長時間案内を接続した。

一時保存先でOS保存先の解決、初期化失敗、空一覧、登録、重複、ID順、日時、再オープン後の保持、終了後のAPI拒否をテストする。実利用者のDBはテストに使わない。

Issue #21では登録フォームと一覧画面を対象外とし、Issue #23で追加した。ブラウザー起動と最終アクセスはIssue #25、フォーム開閉と検索はIssue #26、フォーム開閉設定の保存はIssue #27で追加した。Windows CIはIssue #46／[PR #47](https://github.com/asam-masa/manga-update-manager/pull/47)で導入し、PRとmainへのpushで成功を確認した。

画像の生成・保存・配信・設定UIは接続済みであり、ADR-0013は#199／PR #200で`Verified`となった。[最終判定](./verification/cover-validation-final.md)に、各検証の版・範囲・合成故障と実OS故障の違いを記録している。#176の特殊な自動前面化による終了は未修正であり、通常操作で再現した場合などは完了判定を再評価する。今回の文書更新自体が新たな実機確認や検証状態の変更を行うものではない。

作品編集は#213/#215、作品削除は#222/#227/#223の基盤・Application・APIと#224の確認UIを接続済みである。[編集・削除の完了照合](./verification/work-edit-delete-completion.md)に基づき、親#206／PR #233でADR-0018はVerifiedとなった。更新予定、バックアップは未実装である。画像や編集・削除の完了だけで他のADRの残条件を完了扱いにせず、ADR-0002とADR-0004のValidation Statusは`Pending`を維持する。

### 削除API（Issue #223）

`DeleteWork(id, expected)`は確認時の4テキストと画像参照先を正規化せず受け取り、削除確定時は`workID`と`warnings`を返す。対象変更は`work_changed`、不存在は`work_not_found`、結果不明は`work_delete_outcome_unknown`で拒否し、結果不明をDB停止として復元する。確定後の画像整理・解放失敗は削除済みの成功に警告を添える。

AppはDB寿命と`BeginDelete`を、画像後処理・資源解放・結果変換まで保持する。同作品の追加操作を拒否し、削除中の通常終了も案内して拒否する。結果後に再度閉じられる。画像だけ停止していてもDBが正常なら警告付き削除を許可する。具体的な入出力、停止優先、検証と未確認は[削除API検証](./verification/work-delete-api.md)を参照する。画面からの呼出し・再確認・一覧反映は#224の範囲であり、今回のAPI試験を実WebView2試験として扱わない。
