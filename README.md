# cli-kit

自作の CLI をまとめるリポジトリ。自作の CLI のソースは、すべてここに置く。

## コマンド

| コマンド | 用途 |
|---|---|
| `pdocs stat` / `check` / `id` | project-documents の文書の記入漏れとルールからの逸脱を早く見つけ、形骸化を防ぐ（`pdocs --help`） |
| `skl list` | dotfiles（chezmoi）で管理する Skill を、カテゴリごとに一覧で表示する |
| `git today` | 今日のコミットで変わったファイルをリポジトリ横断で選び、差分（hunk）・yazi・今のファイルの中身（bat）で開く |

## 入れ方と更新

Go は mise で入れる。このリポジトリの直下で次を実行する。

```sh
mise trust        # 初回だけ。mise.toml のタスクを使えるようにする
mise run install
```

`mise run install` は `cmd/` 以下のすべての CLI をビルドし、`~/.local/bin` に置く。あわせて、各 CLI の zsh の補完を `${XDG_DATA_HOME:-~/.local/share}/cli-kit/completions.zsh` に書き出す。ソースを直したあとも同じコマンドで入れ直す。

シェルの設定では、書き出したファイルを読み込む。

```zsh
source "${XDG_DATA_HOME:-$HOME/.local/share}/cli-kit/completions.zsh"
```

## 使うための設定

| 環境変数 | 使う CLI | 内容 |
|---|---|---|
| `PDOCS_DIR` | pdocs | project-documents の場所（必須） |

リポジトリの名前や置き場所は変わりうるため、CLI には書かない。dotfiles などで設定する。

## CI での使い方

モジュール名はリポジトリの場所を含まないため、`go install <URL>@main` では入れられない。CI ではこのリポジトリを取得し、その中でビルドする。取得するリポジトリの名前は、使う側のワークフローの `env` に書く（git で管理でき、名前を変えたときに検索で見つけられる）。

```yaml
env:
  CLI_KIT_REPO: <owner>/<このリポジトリ>

steps:
  - uses: actions/checkout@v7
    with:
      repository: ${{ env.CLI_KIT_REPO }}
      path: .cli-kit
  - uses: actions/setup-go@v7
    with:
      go-version-file: .cli-kit/go.mod
  - run: go build -o "$RUNNER_TEMP/pdocs" ./cmd/pdocs
    working-directory: .cli-kit
```

pdocs を変えて CI が落ちたら、pdocs か文書のどちらかを直す合図として扱う。

## CLI を足すときの決まり

- `cmd/<コマンド名>/main.go` に置く。
- 名前は「対象を表す短い名前 ＋ 動作のサブコマンド」にする（例: `skl list`）。ハイフンでつないだ名前にしない。
- git のサブコマンドにしたいものだけ、`git-<名前>` にする。PATH 上の `git-<名前>` は `git <名前>` として呼べる。
- リポジトリの名前やパスなどの固有名詞は書かない。環境変数やコマンド（`chezmoi source-path` など）から取る。
- 出力は `internal/ui` の部品で組み、罫線や色を自前で組まない。組み立て方は次の順にそろえる（詳しくは `internal/ui/layout.go` の先頭）。
  1. `Title`：コマンドと対象を1行
  2. `Section`：まとまりごとの節（`▌見出し  件数`）。本文は、数値を比べるときだけ `Table`、それ以外はファイルなどでまとめた `Tree` か、桁をそろえた `Columns`。状態ごとの件数は `Counts`
  3. `Footer`：区切り線と、1行のまとめ
- 他のコマンドから値として使う出力（`pdocs id` の次の番号など）は、装飾せずに値だけを出す。
- サブコマンドとフラグは `internal/complete` の `Spec` に書き、`<コマンド名> completion zsh` で zsh の補完スクリプトを出力できるようにする。`mise run install` がすべての CLI の出力をまとめて書き出すので、シェルの設定を足す必要はない。
- 追加したら、この README の「コマンド」の表に1行足す。
