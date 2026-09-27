# cli-kit

自作の CLI をまとめたリポジトリです。自作の CLI のソースは、すべてここに置きます。

各 CLI の使い方は、それぞれの README を見てください。

| CLI | できること | 使い方 |
|---|---|---|
| `pdocs` | project-documents の文書の記入漏れと、ルールからの逸脱を早く見つける | [cmd/pdocs](cmd/pdocs/README.md) |
| `recap` | 自分のコミットをリポジトリ横断で振り返る。変わったファイルを開き、AI に要点をまとめさせて記録する | [cmd/recap](cmd/recap/README.md) |
| `skl` | dotfiles で管理している Skill を、カテゴリごとに一覧で表示する | [cmd/skl](cmd/skl/README.md) |

## 入れ方

Go は mise で入れます。このリポジトリの直下で、次を実行します。

```sh
mise trust        # 初回だけ。mise.toml のタスクを使えるようにする
mise run install
```

`mise run install` は、次の2つを行います。

- `cmd/` の下のすべての CLI をビルドし、`~/.local/bin` に置く
- 各 CLI の zsh の補完をまとめて、`${XDG_DATA_HOME:-~/.local/share}/cli-kit/completions.zsh` に書き出す

シェルの設定では、書き出したファイルを読み込みます。

```zsh
source "${XDG_DATA_HOME:-$HOME/.local/share}/cli-kit/completions.zsh"
```

ソースを直したあとも、同じ `mise run install` で入れ直します。

## 開発

### 構成

```
cmd/<CLI名>/      各 CLI の入口（main.go）と、その CLI の README
internal/cli/     cobra の共通の設定（日本語の使い方の表示、エラーと終了コードの扱い、--color）
internal/ui/      出力の共通の部品（タイトル、節、表、一覧、状態の印など）
internal/recap/   recap の本体
```

Go の公式の勧め（[Organizing a Go module](https://go.dev/doc/modules/layout)）に沿って、コマンドは `cmd/` に、外に公開しない部品は `internal/` に置いています。

### テスト

```sh
mise run test     # go test ./...
```

テストは Go の標準の `testing` で書きます。入力と期待する結果を表にして並べる形（テーブル駆動）にそろえます。見本は `internal/recap/recap_test.go` です。

### CI

push と pull request のたびに、GitHub Actions で `go vet`、`go build`、`go test` を実行します（`.github/workflows/ci.yml`）。

## CLI を足すときの決まり

- `cmd/<CLI名>/main.go` に置き、使い方を `cmd/<CLI名>/README.md` に書く。上の表にも1行足す。
- 名前は「対象を表す短い名前 ＋ 動作のサブコマンド」にする（例: `skl list`、`pdocs check`）。ハイフンでつないだ名前にしない。
- コマンドとオプションは cobra で定義し、最上位のコマンドは `internal/cli` の `cli.Root` で作り、`cli.Execute` で実行する。使い方の表示、エラーの出し方、終了コード、`completion` のサブコマンドがそろう。
- リポジトリの名前やパスなどの固有名詞は、CLI に書かない。環境変数、設定ファイル、コマンド（`chezmoi source-path` など）から取る。
- 出力は `internal/ui` の部品で組み、罫線や色を自前で組まない。組み立て方は次の順にそろえる（詳しくは `internal/ui/layout.go` の先頭）。
  1. `Title`：コマンドと対象を1行
  2. `Section`：まとまりごとの節（`▌見出し  件数`）。本文は、数値を比べるときだけ `Table`、それ以外はまとめた一覧の `Tree` か、桁をそろえた `Columns`。状態ごとの件数は `Counts`
  3. `Footer`：区切り線と、1行のまとめ
- 他のコマンドから値として使う出力（`pdocs id` の次の番号など）は、装飾せずに値だけを出す。
