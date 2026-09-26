// pdocs は、project-documents の文書の記入漏れとルールからの逸脱を早く見つけ、
// 文書が形骸化してプロダクトの品質が下がるのを防ぐ。
//
// ステータスの定義・必須文書は documentation-policy.md、共通運用へのリンクは _template/README.md から
// 実行のたびに読み取る。この CLI には値を持たない。
// project-documents の CI からも実行する（project-documents の _decisions/0038）。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"cli-kit/internal/ui"
)

var (
	root       string
	template   string
	policy     string
	targetDirs = []string{"spec", "design"}
)

// setRoot は文書リポジトリの場所を PDOCS_DIR から決める。
// リポジトリの名前や置き場所が変わりうるため、既定値は持たない。
func setRoot() {
	dir := os.Getenv("PDOCS_DIR")
	if dir == "" {
		fail("PDOCS_DIR に project-documents の場所を設定してください")
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	root = dir
	template = filepath.Join(root, "_template")
	policy = filepath.Join(root, "documentation-policy.md")
}

func fail(msg string) {
	fmt.Fprintf(os.Stderr, "pdocs: %s\n", msg)
	os.Exit(2)
}

func projects() []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		fail(err.Error())
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}
		if p := filepath.Join(root, name); isDir(p) && isFile(filepath.Join(p, "README.md")) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// choose は fzf でプロジェクトを選ばせる。取り消したら空文字。
func choose(names []string) string {
	if _, err := exec.LookPath("fzf"); err != nil {
		fail("fzf が見つかりません。プロジェクト名を引数で指定してください")
	}
	cmd := exec.Command("fzf", "--prompt=project> ")
	cmd.Stdin = strings.NewReader(strings.Join(names, "\n"))
	out, _ := cmd.Output() // 取り消し（Esc など）は失敗として返るが、選ばなかっただけなので扱わない
	return strings.TrimSpace(string(out))
}

const usage = `使い方: pdocs <コマンド> [オプション]

文書の記入漏れとルールからの逸脱を早く見つけ、文書が形骸化してプロダクトの品質が下がるのを防ぐ。

コマンド:
  stat [プロジェクト]        記入漏れ・未判断・未決事項を文書ごとに洗い出し、書き残しによるプロダクトの品質の低下を防ぐ
                           （観点の指定がなければ全て）
      --status             ステータスの件数
      --fill               項目の記入状況
      --open               未決事項
  check                    文書が方針・雛形のルールから外れていないかを検査し、形骸化を早く見つける
                           問題があれば終了コード 1（観点の指定がなければ items 以外の全て）
      --readme             プロダクトREADMEの文書地図の漏れとリンク切れ
      --status             定義にないステータス値
      --template           プロダクト文書の見出し構成と雛形の一致
      --id                 エントリの ID の有無・重複・参照先
      --sources            文章以外で定義するものの有無（未完了は警告）
      --quality            品質特性の水準の ID・ステータス・基準と水準・確認手段（未決定・未確立は警告）
      --items              雛形にない項目名（警告のみ。既定の検査には含めない）
  id [プロジェクト] <対象>    接頭辞（例: FRQ）なら次の番号、ID（例: FRQ-001、button.no-fill）なら定義と参照を表示する
  completion zsh           zsh の補完スクリプトを出力する

共通のオプション:
  --color auto|always|never  色付け（auto は端末のときだけ。NO_COLOR が設定されていれば付けない）

プロジェクトを省略すると fzf で選ぶ。project-documents の場所は PDOCS_DIR で指定する（必須）。`

func usageError(msg string) {
	fmt.Fprintf(os.Stderr, "pdocs: %s\n\n%s\n", msg, usage)
	os.Exit(2)
}

// args は、コマンドの後ろの引数を、フラグと位置引数に分けたもの。
type args struct {
	flags      map[string]bool
	color      string
	positional []string
}

func parseArgs(raw []string, allowed []string) args {
	a := args{flags: map[string]bool{}, color: "auto"}
	for i := 0; i < len(raw); i++ {
		arg := raw[i]
		switch {
		case arg == "-h" || arg == "--help":
			fmt.Println(usage)
			os.Exit(0)
		case arg == "--color":
			if i+1 >= len(raw) {
				usageError("--color に値がありません")
			}
			i++
			a.color = raw[i]
		case strings.HasPrefix(arg, "--color="):
			a.color = strings.TrimPrefix(arg, "--color=")
		case strings.HasPrefix(arg, "-"):
			name := strings.TrimPrefix(arg, "--")
			if !slices.Contains(allowed, name) {
				usageError("不明なオプション: " + arg)
			}
			a.flags[name] = true
		default:
			a.positional = append(a.positional, arg)
		}
	}
	return a
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	if len(argv) == 0 {
		usageError("コマンドを指定してください")
	}
	command, rest := argv[0], argv[1:]
	var a args
	switch command {
	case "stat":
		a = parseArgs(rest, spec.FlagNames(command))
		if len(a.positional) > 1 {
			usageError("引数が多すぎます")
		}
	case "check":
		a = parseArgs(rest, spec.FlagNames(command))
		if len(a.positional) > 0 {
			usageError("引数が多すぎます")
		}
	case "id":
		a = parseArgs(rest, spec.FlagNames(command))
		if len(a.positional) == 0 || len(a.positional) > 2 {
			usageError("対象（接頭辞か ID）を指定してください")
		}
	case "completion":
		if len(rest) != 1 || rest[0] != "zsh" {
			usageError("対応しているシェルは zsh だけです")
		}
		fmt.Print(spec.Zsh())
		return 0
	case "-h", "--help":
		fmt.Println(usage)
		return 0
	default:
		usageError("不明なコマンド: " + command)
	}
	if err := ui.SetColor(a.color); err != nil {
		usageError(err.Error())
	}

	setRoot()
	if !isDir(root) {
		fail(root + " が見つかりません（PDOCS_DIR を確認してください）")
	}
	ids := loadStatuses()

	switch command {
	case "check":
		aspects := checkSet{}
		for f := range a.flags {
			aspects[f] = true
		}
		if len(aspects) == 0 {
			aspects = defaultChecks
		}
		return cmdCheck(ids, aspects)
	case "id":
		project, target := "", a.positional[0]
		if len(a.positional) == 2 {
			project, target = a.positional[0], a.positional[1]
		}
		return cmdID(project, target)
	}
	project := ""
	if len(a.positional) == 1 {
		project = a.positional[0]
	}
	chosen := statAspects{a.flags["status"], a.flags["fill"], a.flags["open"]}
	showAll := chosen == statAspects{}
	if showAll {
		chosen = statAspects{true, true, true}
	}
	return cmdStat(ids, project, chosen, showAll)
}
