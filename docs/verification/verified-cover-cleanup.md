# 変更確定後の旧管理画像削除

Issue #148。正本は[画像保存契約](../cover-persistence.md#旧管理画像の削除と記録整理)と[ADR-0013](../adr/0013-define-cover-operation-boundaries.md)。旧参照そのものを削除許可とせず、DB記録と実ファイルを照合する。

## 接続範囲

`SelectWorkCover`の参照確定後だけ、共通排他・操作票・外側の祖先保持を継続して、候補取得→実ファイル削除と不存在確認→管理記録の条件付きDELETEを行う。各処理は別段階で停止を再確認し、停止対象エラーは段階終了前に通知する。成功した新参照と日時を後処理失敗で戻さない。

Storageの`UnusedManagedCover`は正常なready記録と全作品の参照を読む。共有参照は正常な保持であり、警告しない。`ForgetUnusedManagedCover`は呼出側の安全な不存在確認を前提に、同じready記録・識別情報・全作品から未参照の条件で1件だけ削除する。両処理は既存Storeの実行制御へ参加する。SQLだけではファイル安全性やアプリ内排他を証明しない。

Windowsの大小文字・区切り違いはSQLiteのBINARY一致では保護できないため、両Storage経路で全非空参照を検査する。直接の管理パスの大小文字・区切り違いを同じ参照として扱う。共有参照が確認できた場合は正常に保持し、それ以外で認識できない表記があれば警告して削除を保留する。条件付きDELETEにも大小文字・区切り違いの参照検査を含める。既存データを正規化・書き換えしない。

Windows境界`DeleteVerifiedCover`は固定IDのみ受け付け、祖先を保持して、共有なし・リンクを追跡しないハンドルで開く。既存の`captureCoverFile`で通常ファイル・単一リンク・128bit識別情報・サイズ・SHA-256を検証する。同じハンドルへ`FileDispositionInfo`を要求し、Closeと固定パスの不存在を確認する。元ファイル名・任意パスを受け取らず、別名やリンクを追って削除しない。

最初から不存在なら、固定ファイルの`ERROR_FILE_NOT_FOUND`だけを候補とし、保持中の同じパスをLstatで再確認する。`ERROR_PATH_NOT_FOUND`・アクセス拒否などは成功にしない。未知ファイルが存在すれば保持し、記録も処理しない。任意の外部プロセスに対する完全な攻防はADRの保護範囲外だが、リンク拒否・同一ハンドル照合・既存ファイル保護は維持する。

## 自動確認

- `TestDeleteVerifiedCoverAndAbsence`: 実生成PNGの削除、既に不存在の再処理、親パス不存在の拒否。
- `TestDeleteVerifiedCoverRefusals`: 同一サイズの改変・同一内容の別ファイル・hard link・symlink・ディレクトリ・共有違反・読取専用・識別情報/サイズ不一致・キャンセル・不正ID・祖先リンクの12条件で保持する。
- `TestUnusedCoverAndConditionalForget`: readyとreserved・不存在の区別、参照中の拒否、識別情報差分の拒否、条件一致時だけの記録整理。
- `TestCoverCleanupDBFailureAndStop`: DELETEの合成トリガー失敗で記録を残し、キャンセルと停止後の両新規SQL経路を拒否する。
- `TestCoverCleanupReferenceAliases`: 両Storage経路で既存の大小文字・区切り違いを共有参照として保持し、末尾ドット・ストリーム・短い名前・legacy参照は未参照と断定しない。
- `TestCoverCleanupPostCommitOutcomes`: 実画像とSQLiteで正常削除、共有参照、大小文字違いの共有参照、別名確認不能、改変、管理記録なし、reserved、記録DELETE失敗、合成解放失敗、削除前停止、削除後の記録整理前停止の11経路を確認する。確定済み新参照・日時を保持し、停止後の新規画像操作を拒否する。
- 同テストの記録DELETE失敗では実ファイルが削除され記録だけが残る。トリガーを除きDBを再接続後、実cleanup helperを必要な排他・祖先保持・段階受付とともに呼び、不存在から記録を整理する。自動起動整理の実装ではない。
- `TestSelectWorkCoverAPI`: APIでの再設定後に旧画像が削除され、元画像の内容は維持されることを確認する。

## 限界

一時SQLite・一時領域・合成PNGでの確認であり、利用者データは使わない。実OSのClose故障は再現せず、Applicationへ印を注入する。電源断・全I/O故障・全ドライブ種別・実WebView2は未確認。共有参照の通常保持は検証するが、配信中読取との競合は配信接続時に確認する。

解除API・起動候補の列挙と自動整理・警告UI・配信は未実装。予約や未知ファイルを走査で削除しないため、残置と容量増加はあり得る。結果不明・DB停止からの自動再接続・書戻し・復旧は行わない。ADR-0013はPendingを維持する。

直接の管理パスと認識できない参照が他作品に残る場合、無関係のように見えても旧画像削除を保留する。Windowsの全別名を対応済みとする代わりに、未参照を確認できない場合は削除しない初回の制約である。

## 一次資料

- [SetFileInformationByHandle](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-setfileinformationbyhandle): 削除用DELETE権限と同一ハンドルの削除要求。
- [FILE_DISPOSITION_INFO](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_disposition_info): 削除要求の構造体。API成功だけをDB記録整理の十分条件にせず、Closeと不存在も確認する。
- [Naming Files, Paths, and Namespaces](https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file): 大小文字と8.3別名などの命名規則。SQLiteの完全一致をWindowsの同一実体判定へ流用しない。
