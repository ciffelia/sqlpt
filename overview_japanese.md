## このプロジェクトの概要メモ

- sqlptは、Goでデータベース (RDBMS) に依存するテストを並列で実行するためのライブラリ
- 名前は sql parallel testing から来ている
- テストごとに`CREATE DATABASE`でデータベースを作成し、終了後に削除することで、分離を実現している
- Rustの`sqlx`クレートの`sqlx::test`マクロから着想を得ており、大部分の実装も`sqlx`からの移植による
- 現時点では`pgx/v5`に対応

## README構成案

- ライブラリが実現することを簡潔に説明
- 使い方の短い例
- どうやって実現しているか
- `DATA-DOG/go-txdb` との違い
- `sqlx::test`をacknowledgeする
