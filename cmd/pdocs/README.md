# pdocs

project-documents の文書の記入漏れと、ルールからの逸脱を早く見つけ、文書が形骸化してプロダクトの品質が下がるのを防ぎます。

## 準備

project-documents の場所を、環境変数 `PDOCS_DIR` で指定します（必須）。

```sh
export PDOCS_DIR="$HOME/src/.../project-documents"
```

プロジェクトを選ぶ場面では fzf を使います。

## 使い方

```sh
pdocs stat [プロジェクト]          # 記入漏れと未判断を洗い出す
pdocs check                        # ルールからの逸脱を検査する
pdocs id [プロジェクト] <対象>      # 次に使う ID、または ID の定義と参照を表示する
```

プロジェクトを省略すると、fzf で選びます。どのコマンドでも `--color auto|always|never` で色付けを変えられます（`auto` は端末のときだけ色を付け、`NO_COLOR` があれば付けません）。

### stat

プロジェクトの文書ごとに、次を表示します。

- **項目の記入**（項目ごと）：`- 項目名: 値` のうち、値が書いてある数と空欄の数、未決事項の数
- **判断のステータス**（エントリごと）：エントリの数と、ステータスの値ごとの数。空欄は未定義（DS01）として数える
- **未決事項**：項目名の下に、未決の内容を並べる
- **判断以外のステータス**：成果物の状況、品質特性の採否などの内訳
- **ADR**：`adrs list -l` の結果
- 最後に、検査の件数のまとめ

観点を絞るオプション：`--fill`（項目の記入）、`--status`（ステータス）、`--open`（未決事項）。どれも付けなければ全部出します。

### check

文書が方針と雛形のルールから外れていないかを検査します。**問題があれば終了コード 1** です。警告と未定義は表示するだけです。

| オプション | 検査すること |
|---|---|
| `--readme` | プロダクト README の文書地図の漏れとリンク切れ |
| `--status` | 定義にないステータス値 |
| `--template` | 文書の見出しの構成が雛形と一致しているか |
| `--id` | エントリの ID の有無・重複・参照先 |
| `--sources` | 文章以外で定義するもの（成果物）の有無 |
| `--quality` | 品質特性の水準の ID・ステータス・基準と水準・確認手段 |
| `--items` | 雛形にない項目名（警告だけ。既定の検査には含めない） |

オプションを付けなければ、`--items` 以外をすべて検査します。

### id

- 対象が接頭辞（例: `FRQ`）なら、次に使う番号を表示します。装飾なしで値だけを出すので、他のコマンドから使えます。
- 対象が ID（例: `FRQ-001`、`button.no-fill`）なら、定義している場所と、参照している場所を表示します。

## 読み取るもの

pdocs は値を持たず、実行のたびに project-documents から読み取ります。

- `documentation-policy.md` のステータスの表、「必須」、見出しの構成と繰り返すエントリの表
- `_template/README.md` の共通運用へのリンク
- 各文書の書式（`- 項目:`、`- ステータス:`、ステータス列を持つ表、`## 未決事項`、`- 未決事項:`）

これらの書式や置き場所を変えたときは、pdocs も確認してください。表示は記入の目安で、内容の正しさは保証しません。

## CI で使う

別のリポジトリの GitHub Actions から使うときは、このリポジトリを取得してビルドします。モジュール名にリポジトリの場所を含めていないため、`go install <URL>@main` では入れられません。取得するリポジトリの名前は、使う側のワークフローの `env` に書きます（git で管理でき、名前を変えたときに検索で見つけられます）。

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
  - run: '"$RUNNER_TEMP/pdocs" check --color never'
    env:
      PDOCS_DIR: ${{ github.workspace }}
```

pdocs を変えて CI が落ちたら、pdocs か文書のどちらかを直す合図として扱います。
