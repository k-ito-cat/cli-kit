# recap

自分のコミットを、リポジトリを横断して振り返ります。変わったファイルを選んで開いたり、AI に作業の要点をまとめさせて記録したりできます。

## 準備

設定ファイル `${XDG_CONFIG_HOME:-~/.config}/cli-kit/recap.toml` を置きます（[設定](#設定)）。無いと、書き方を表示して止まります。

| 種類 | ツール | 使う場面 |
|---|---|---|
| 必須 | git、fzf | コミットを集める、一覧から選ぶ |
| 実質必須 | hunk | 差分を開く（コミット全体や、削除したファイルは差分でしか開けない） |
| 任意 | yazi、bat | 開き方で選んだとき。bat は `recap log` と `recap config` でも使う |
| 設定しだい | 設定の `sources` と `summarize.command` に書いたコマンド | 例では ghq、chezmoi、claude CLI を使っているが、同じ役割のコマンドなら何でもよい |

## 使い方

```sh
recap today                      # 今日の自分のコミットを振り返る
recap today --date yesterday     # 指定した日（2026-09-25、09-25、yesterday）
recap week                       # 直近7日（今日を含む）
recap log                        # 要約の記録を開く
recap status                     # 未コミットの変更と、未 push のコミットがあるリポジトリ
recap config                     # 設定ファイルを bat で表示する
```

### 変わったファイルを開く

`recap today` と `recap week` は、コミットと、その下に変わったファイルを並べた一覧を出します。

```
dotfiles  feat(karabiner): Escで英数入力へ戻す  14:20 0425f89
  └─ M dot_config/private_karabiner/karabiner.json
```

- ファイルの左の印は、変更の種類です（`A` 追加、`M` 更新、`D` 削除、`R` 名前の変更）。
- **ファイルの行**を選ぶと、開き方を選べます。
  - `hunk`：このコミットでの差分を見る
  - `yazi`：yazi でファイルを開く
  - `bat`：今のファイルの中身を見る
- **コミットの行**や、今の作業ツリーにないファイル（削除したものなど）を選んだときは、差分（hunk）ですぐに開きます。
- 操作は Enter だけで、Esc で1つ前に戻ります。前回選んだ開き方が、次から一覧の先頭に来ます。

### 要点をまとめる（--summary）

```sh
recap week --summary             # まだ要約していないコミットの要点をまとめ、記録に追記する
recap week --summary --deep      # 差分も材料にして、より具体的にまとめる（トークンを多く使う）
recap week --summary --force     # 要約済みの範囲も含めて、もう一度まとめる
```

- AI に、リポジトリごとに「何のための作業だったか」の見出しと、具体的な変更をまとめさせます。材料は、コミットの件名、本文の最初の3行、変わったディレクトリです。`--deep` を付けると、差分も上限の範囲で加えます。
- すでに要約した範囲のコミットは、材料に入れません。**新しいコミットが無ければ、AI は呼びません。**
- 使ったモデル、トークン数、費用の見積もりも表示します（要約のコマンドが `claude -p --output-format json` の形の JSON を返す場合）。

- 最後に、未コミットの変更が残っているリポジトリを知らせます。要約はコミットしか見ないので、残っている作業に気づけるようにしています。

### 要約の記録

- 結果は `log_dir` の月ごとのファイル（`yyyy-mm.md`。範囲の終わりの月）に、Markdown で追記します。
- 各項目には、要約した範囲を、表示されないコメント（`<!-- recap: <開始> <終了> -->`）で埋め込みます。要約済みかどうかは、このコメントで判断します。**記録のファイルや項目を消せば、その範囲はまた要約できます。**
- `recap log` で、記録を選んで bat で読むか、yazi でフォルダを開けます。

### 残っている作業を確かめる（recap status）

対象のリポジトリのうち、未コミットの変更があるものと、未 push のコミットがあるもの（今のブランチが上流より先行しているもの）を表示します。変更の種類ごとの数は、`M` 変更、`A` 追加、`D` 削除、`R` 名前の変更、`??` 未追跡です。

## 設定

`${XDG_CONFIG_HOME:-~/.config}/cli-kit/recap.toml` に TOML で書きます。知らないキーがあるとエラーになります。

```toml
sources = ["ghq list --full-path", "chezmoi source-path"]
exclude = ["some-repo"]
authors = ["me@example.com"]
log_dir = "~/.local/share/recap"

[summarize]
command = "claude -p --output-format json --tools ''"

[deep]
exclude = ["dist/**"]
use_default_exclude = true
max_lines_per_commit = 120
max_lines_total = 4000
```

| キー | 必須 | 内容 |
|---|---|---|
| `sources` | 必須 | 対象のリポジトリのパスを1行に1つずつ出すコマンドの配列。出てきたリポジトリをすべて調べる |
| `exclude` | | 除外するリポジトリ（名前かパス） |
| `authors` | | 自分とみなす作者（メールアドレス）。空なら、各リポジトリの `user.email` で自分のコミットだけに絞る |
| `log_dir` | | 要約の記録の置き場所。既定は `${XDG_DATA_HOME:-~/.local/share}/recap` |
| `summarize.command` | `--summary` に必須 | 頼む内容を標準入力で受け取り、要約を標準出力に出すコマンド。一時ディレクトリで実行する。`--tools ''` を付けると、AI がコマンドなどで別の期間の情報を取りに行かない |
| `deep.exclude` | | `--deep` で差分から除外するパス（git の glob）。既定の除外に加わる |
| `deep.use_default_exclude` | | `false` にすると既定の除外を使わない。既定では lock ファイル（`package-lock.json`、`pnpm-lock.yaml`、`yarn.lock`、`bun.lock`、`go.sum` など）と、`*.min.js`、`*.min.css`、`*.map` を除外する |
| `deep.max_lines_per_commit` | | `--deep` で1コミットあたりに入れる差分の上限（既定 120 行） |
| `deep.max_lines_total` | | `--deep` で全体に入れる差分の上限（既定 4000 行） |
