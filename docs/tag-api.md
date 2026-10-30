# タグの公開API

Issue #303、親#295/#30。[内部保存](./tag-storage.md)をApplicationのTagReaderとWailsへ接続する。判断は[ADR-0020](./adr/0020-confirm-tags-with-work-edits.md)。#306で作品編集、#310で削除確認画面、#320で管理用の取得、#314で管理の変更APIへ接続する。管理画面・絞り込みは後続である。

## 読取と識別子

LoadTags()は未使用を含む全候補、LoadWorkTags(workID)は指定作品のタグをID昇順で返す。空はJSON []、要素はTagDTO{id: string, name: string}。作成・修復は行わず、AppのDB寿命ロックを処理完了まで保持し、ApplicationからStoreへ委譲する。

タグIDは正のint64を10進文字列で渡し、JSの数値丸めを避ける。入力の空・0・負・符号付き・先頭ゼロ・空白・指数・範囲外を拒否する。既存作品IDとWorkDTOの形は変更しない。既存タグ名は正確な保存名として照合し、入力時にtrimして修復しない。

## 編集と削除

EditWorkInput.tagsは任意のTagEditInput{existing: TagDTO[], newNames: string[]}。省略・nullは保持、明示した空選択は全解除する。空オブジェクトも空選択になるため、未読込の画面から解除として送らない。新規名はドメインの正規化へ渡し、複数追加・完全同名の既存再利用・重複付与一つを維持する。

Appは入力変換時に検証し、既存のWorkEditor・共通受付・DB寿命・結果分類を使う。EditWorkResultは従来の作品と警告で、タグIDを返すためのSQLを確定後に追加しない。確定IDは必要に応じて読取APIから取得する。後続画面は保存成功で編集を閉じ、次の編集開始で最新候補を読む。

DeleteWorkInput.tagsは任意のTagDTO[]。省略・nullは従来互換、空配列はタグなしの正確な確認。指定したID/名前集合が変わればwork_changedで拒否し、タグも含めた再確認を案内する。未使用候補は作品削除後も保持する。#310の[削除確認画面](./work-screen.md#削除確認のタグ接続issue-310親295)は取得成功したタグ集合を必ず指定し、失敗時は削除せず、変更検出後は作品情報とタグを読み直して再確認する。

## 安全なエラー

| 条件 | APIコード |
| --- | --- |
| 初期化前・終了後 | database_unavailable |
| 作品不存在 | work_not_found |
| 読取失敗・不正保存値・通常保存失敗 | storage_error |
| 入力のID/名前/選択が不正 | invalid_input |
| 保存時に選択済みタグの改名・削除を検出 | tags_changed |
| DB停止（他の原因と併存しても優先） | database_stopped |

結果不明・確定後警告は既存のwork_edit_outcome_unknown / database_stopped_after_commit等を維持する。Repository側のタグ名エラーを入力ミスと扱わず、内部診断・パスを表示しない。tags_changedは既存の安全な包装とフロントエンドの許可コードへ追加し、それだけではDB停止としない。

## 検証と後続

一時DBでJSONの省略/null/空選択、複数新規と再利用、再起動保持、改名・削除と削除確認、入力拒否・未確定タグ非作成を検証する。合成Repositoryではint64上限の往復、取消/停止優先・不正保存値・部分戻り値破棄・内部診断非公開、初期化前/終了後、DB寿命と同作品受付を確認する。

包装テストでは最初tags_changedが未知扱いで失敗した。許可コードへ追加し、案内とコードを復元でき、DB停止とはしないことを確認した。Goの直接API試験と生成型のビルドは実WebView2のタグ操作確認ではない。

生成型へ入れ子のタグが加わりconvertValuesメソッドが必須になったため、初回TypeScriptビルドは既存の編集/削除入力で失敗した。両呼出しで生成モデルのcreateFromを使用し、タグ未指定の既存送信とint64文字列のJSON往復をテストする。画面配置やタグ操作は追加していない。

#295の[画面合意](./work-screen.md#タグ編集の失敗時の合意issue-295未接続)を#306で[編集画面へ接続](./work-screen.md#タグ編集の画面接続issue-306)する。公開API段階の試験と実画面の検証範囲は区別する。タグ管理・絞り込みは#296/#297に残し、ADR-0020はPendingを維持する。

## 管理用の取得API（Issue #320、親#314）

`LoadTagUsage()`は全タグの`TagUsageDTO{tag: TagDTO, workCount: number}[]`、`LoadTagDeletePreview(tagID: string)`は`TagDeletePreviewDTO{tag: TagDTO, works: TagWorkDTO[]}`を返す。`TagWorkDTO`は`{id: string, createdAt: string, title: string}`。Applicationの`TagManagementReader`が利用側の読取interfaceを通じて[内部取得](./tag-management.md#管理用の内部取得issue-317親313)へ委譲し、タグ・作品を変更しない。全件のID昇順を維持し、未登録や未使用の対象はJSON `[]`。タグ不存在は`tag_not_found`であり、DB停止を意味しない。

新規DTOのタグID・作品IDは正のint64の10進文字列とする。既存WorkDTOの数値IDは変更しない。登録日時は日本時間のRFC 3339で小数秒の精度を落とさず返す。後続の確認送信ではIDと日時の文字列を保持し、JSの数値やDateを介して再生成しない。件数は0〜9007199254740991の正確なJSON数値だけを返す。

指定IDの不正な表記はStoreへ渡す前に`invalid_input`。Repositoryからのエラー・部分値は結果を破棄する。タグ/作品ID・名前・タイトル・登録日時・件数と重複、不一致の取得タグ、未取得の作品一覧も検証し、不正値を修復せず`storage_error`とする。件数一覧は作品の全フィールドの検証ではない。`database_stopped`は`tag_not_found`などに優先し、既存の停止観測へ渡す。内部診断・パス・取消の生エラーを公開しない。

読取開始からDTO変換完了までAppのDB寿命ロックを保持する。初期化前・終了後は`database_unavailable`。#320の取得APIでは変更受付・終了時のタグ処理案内を追加せず、後述の#314で接続する。

一時DBで空・未使用・不存在、使用数と対象・再起動保持・読取前後の作品不変を確認する。合成Repositoryでint64上限・日時の9桁小数秒とJSON往復、不正入力のSQL未到達、部分結果/不正値の破棄・停止優先・診断非公開、読取中のDB寿命と終了後拒否を検証する。生成TypeScriptモデルのJSON往復と`tag_not_found`の包装・非停止を試験する。Wails生成・ビルドは管理画面の実WebView2確認ではない。

## 管理用の変更API（Issue #314）

`RenameTag(expected: TagDTO, name: string)`と`DeleteTag(expected: TagDeletePreviewDTO)`を公開する。両方の返却型は`TagChangeResult{tag: TagDTO, warnings: WorkWarningDTO[]}`。削除結果のtagは削除したタグのIDと名前であり、残存候補ではない。Applicationの`TagManager`が利用側の変更interfaceを通じてStoreへ委譲する。改名は新しい名前だけを正規化し、旧名は正確に照合する。

削除確認のworksは必須で、省略/nullを拒否し、明示的な`[]`だけを未使用の確認として受け取る。タグ/作品IDは正のint64の正規10進文字列、登録日時は非ゼロの1〜9999年のRFC 3339 Nano形式とする。取得した文字列をそのまま返し、余分な小数桁の切捨てや末尾ゼロ等による別表現を受け入れない。作品IDの重複を拒否し、表示タイトルは削除の照合条件にしない。保存処理が同じTx内で旧名と作品ID/登録日時の集合を再確認する。

AppのDB寿命ロックをDTO返却まで保持する。既存WorkOperationsの同じmutexでタグ専用のID集合を管理し、同じタグへの改名と削除の重複を`tag_busy`で拒否する。作品IDの受付枠とは分け、作品編集と別タグの操作は受け付ける。これは同時保存の成功を保証せず、Storeの競合・停止・確認照合が最終結果を決める。管理操作中の通常終了は受付を閉じずに拒否し、ロックを外して専用の案内を表示する。確定済みの終了判断後は新しい管理操作を受け付けない。

| 条件 | API結果 |
| --- | --- |
| 公開入力のID/旧名/新名/削除確認が不正 | invalid_input。保存へ渡さない |
| 同じタグを変更中 | tag_busy |
| 確認した名前または削除対象が変化 | tag_changed。再取得して再確認 |
| 別タグの完全同名 | tag_duplicate_name。入力を保持して別名を指定 |
| 対象タグ不存在 | tag_not_found |
| DB停止と確認競合等が併存 | database_stoppedを優先 |
| Commit結果不明 | tag_change_outcome_unknown。成功値なし、停止、自動再送なし |
| Commit成功後の接続解放失敗 | 成功タグとdatabase_stopped_after_commit警告 |
| その他の保存失敗/Repositoryの不正値 | storage_error。生の診断やパスを公開しない |

確定後の追加SELECTは行わず、結果不明を通常失敗へ、確定後警告を保存失敗へ戻さない。新しいエラーコードは既存の安全なWails包装へ接続し、フロントエンドの停止判定は結果不明だけを追加する。警告の実画面表示は管理画面で接続する。

#314の一時SQLite試験で改名・重複名・古い旧名・未使用削除・対象入替・1ナノ秒差の確認拒否、関連変更後の改名、作品本体と日時の保持、再起動後の永続化を確認する。合成Repositoryでint64上限・コンテキスト伝達・不正入力の未到達、エラー優先順位・結果不明・確定後警告・診断非公開、同タグの二重送信・同じ数値IDの作品編集受付・処理中の通常終了拒否・DB寿命保持を確認する。共通受付の並行テストでタグ受付と終了の両方が成功しないこと、古い解放の再実行で新しい予約を解かないことを確認する。生成モデルと安全なエラー包装も試験する。実OSの資源解放故障や管理画面からの実WebView2操作・終了案内の視認は未検証で、#315へ残す。
