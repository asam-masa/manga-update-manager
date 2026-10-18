# 管理記録確定と画像参照変更の共通排他

Issue #145。正本は[画像保存契約](../cover-persistence.md#参照と後処理の共通排他)と[ADR-0013](../adr/0013-define-cover-operation-boundaries.md)。削除の前提となるアプリ内排他を実経路へ接続する段階であり、実ファイル削除を追加しない。

## 対象と確認

同じAppの`CoverOperations`に容量1の受付枠を持つ。取得はcontextに応答し、返却は同じ返却関数の二重呼び出しでも1回だけ行う。画像の停止状態とは分離し、画像だけの停止で通常の作品登録を止めない。DB操作や画像段階の許可を代替しない。

- `TestCoverReferencesSerializationAndRelease`: 16 goroutineが各50回取得し、同時実行が1件であること、二重返却が別の保持を解除しないことを確認する。
- `TestCoverReferencesCancellationAndImageStop`: キャンセル済み取得を拒否し、画像停止後も登録用の排他を使える一方、画像の新規操作は拒否する。
- `TestCoverImportReferenceWait`: 一時PNG・一時SQLiteで、新規保存後の記録が待機中はreservedのままであること、別作品は選択をキャンセルできること、通常終了の拒否を確認する。排他返却後の正常完了、停止後の次段階拒否、キャンセル時の旧参照・予約保持を分けて確認する。
- `TestCoverImportReleaseOutcomesAndStageStop`: 最後のフォルダー解放中にも参照排他と操作票を保持する。解放失敗は通知してから排他を返す。
- `TestCreateWorkJoinsImageReferenceGate`: 保持中の枠へ作品登録を待機させ、contextの待機開始を同期点にしてRepositoryが呼ばれていないことを確認する。キャンセル後に完了し、画像だけの停止後も登録できる。

## 限界と後続

通常の競合を扱う一時領域での検証であり、実削除・共有参照削除・起動整理・配信・実WebView2を検証したものではない。後続の参照変更経路がこの枠を使わなければ保護できない。現在はWailsの作品登録と画像取り込みから接続しており、Storageの直接呼び出しにアプリ内排他を強制する機能ではない。

待機順の公平性やI/Oの完了時間は保証しない。画像停止は待機中の枠を強制回収せず、保持側が返却した後の段階開始で拒否する。取得中・待機中の変更票は終了まで保持する。実削除・配信・整理を接続して検証するまでADR-0013はPendingを維持する。
