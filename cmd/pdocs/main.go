// pdocs は、project-documents の文書の記入漏れとルールからの逸脱を早く見つける。使い方は README.md。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"cli-kit/internal/cli"
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

func main() {
	root := cli.Root("pdocs",
		"文書の記入漏れとルールからの逸脱を早く見つけ、形骸化を防ぐ",
		"project-documents の文書の記入漏れとルールからの逸脱を早く見つけ、文書が形骸化してプロダクトの品質が下がるのを防ぐ。\n"+
			"project-documents の場所は PDOCS_DIR で指定する（必須）。プロジェクトを省略すると fzf で選ぶ。")
	cli.ColorFlag(root)
	root.AddCommand(statCmd(), checkCmd(), idCmd())
	cli.Execute(root)
}

// prepare は、文書の場所を決めて、ステータスの定義を読む。各コマンドの実行の最初に呼ぶ。
func prepare() statusSet {
	setRoot()
	if !isDir(root) {
		fail(root + " が見つかりません（PDOCS_DIR を確認してください）")
	}
	return loadStatuses()
}

// exitCode は、コマンドの終了コードを cobra に渡す形にする。
func exitCode(code int) error {
	if code == 0 {
		return nil
	}
	return cli.ExitError{Code: code}
}

// completeProjects は、プロジェクト名の補完。PDOCS_DIR の下から、実行のたびに探す。
func completeProjects(c *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 || os.Getenv("PDOCS_DIR") == "" {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	setRoot()
	return projects(), cobra.ShellCompDirectiveNoFileComp
}

func statCmd() *cobra.Command {
	var a statAspects
	c := &cobra.Command{
		Use:               "stat [プロジェクト]",
		Short:             "記入漏れと未判断を洗い出す",
		Long:              "記入漏れ・未判断・未決事項を文書ごとに洗い出し、書き残しによるプロダクトの品質の低下を防ぐ。観点の指定がなければ全て出す。",
		Args:              cli.RangeArgs(0, 1),
		ValidArgsFunction: completeProjects,
		RunE: func(c *cobra.Command, args []string) error {
			ids := prepare()
			project := ""
			if len(args) == 1 {
				project = args[0]
			}
			chosen := a
			showAll := chosen == statAspects{}
			if showAll {
				chosen = statAspects{true, true, true}
			}
			return exitCode(cmdStat(ids, project, chosen, showAll))
		},
	}
	c.Flags().BoolVar(&a.status, "status", false, "ステータスの件数")
	c.Flags().BoolVar(&a.fill, "fill", false, "項目の記入状況")
	c.Flags().BoolVar(&a.open, "open", false, "未決事項")
	return c
}

func checkCmd() *cobra.Command {
	flags := []struct{ name, usage string }{
		{"readme", "プロダクトREADMEの文書地図の漏れとリンク切れ"},
		{"status", "定義にないステータス値"},
		{"template", "プロダクト文書の見出し構成と雛形の一致"},
		{"id", "エントリの ID の有無・重複・参照先"},
		{"sources", "文章以外で定義するものの有無（未完了は警告）"},
		{"quality", "品質特性の水準の ID・ステータス・基準と水準・確認手段（未決定・未確立は警告）"},
		{"items", "雛形にない項目名（警告のみ。既定の検査には含めない）"},
	}
	chosen := map[string]*bool{}
	c := &cobra.Command{
		Use:   "check",
		Short: "ルールからの逸脱を検査する",
		Long:  "文書が方針・雛形のルールから外れていないかを検査し、形骸化を早く見つける。問題があれば終了コード 1。観点の指定がなければ items 以外の全てを検査する。",
		Args:  cli.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			ids := prepare()
			aspects := checkSet{}
			for name, on := range chosen {
				if *on {
					aspects[name] = true
				}
			}
			if len(aspects) == 0 {
				aspects = defaultChecks
			}
			return exitCode(cmdCheck(ids, aspects))
		},
	}
	for _, f := range flags {
		chosen[f.name] = c.Flags().Bool(f.name, false, f.usage)
	}
	return c
}

func idCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "id [プロジェクト] <対象>",
		Short:             "次に使う ID、または ID の定義と参照を表示する",
		Long:              "対象が接頭辞（例: FRQ）なら次に使う番号を、ID（例: FRQ-001、button.no-fill）なら定義と参照している場所を表示する。",
		Args:              cli.RangeArgs(1, 2),
		ValidArgsFunction: completeProjects,
		RunE: func(c *cobra.Command, args []string) error {
			prepare()
			project, target := "", args[0]
			if len(args) == 2 {
				project, target = args[0], args[1]
			}
			return exitCode(cmdID(project, target))
		},
	}
}
