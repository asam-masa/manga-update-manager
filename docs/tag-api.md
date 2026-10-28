# タグの公開API

Issue #303、親#295/#30。[内部保存](./tag-storage.md)をApplicationのTagReaderとWailsへ接続する。判断は[ADR-0020](./adr/0020-confirm-tags-with-work-edits.md)。#306で作品編集、#310で削除確認画面へ接続する。管理・絞り込みは後続である。

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
