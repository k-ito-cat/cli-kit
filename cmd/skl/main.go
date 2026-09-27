// skl は、dotfiles（chezmoi）で管理する Skill を一覧で表示する。使い方は README.md。
package main

import (
	"bufio"
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"cli-kit/internal/cli"
	"cli-kit/internal/ui"
)

// 表示の順番。ここに無いカテゴリは警告を出し、一覧の後ろに回す
var categories = []string{"仕様書運用", "レビュー・監査", "開発", "環境・設定", "執筆", "その他"}

const uncategorized = "未分類"

func main() {
	root := cli.Root("skl", "dotfiles（chezmoi）で管理する Skill を扱う", "")
	root.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "カテゴリごとに、スキル名と用途を一覧で表示する",
		Args:  cli.NoArgs,
		RunE:  func(c *cobra.Command, args []string) error { return list() },
	})
	cli.Execute(root)
}

type skill struct {
	name, category, summary string
}

func list() error {
	dir, err := skillsDir()
	if err != nil {
		return err
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*", "SKILL.md"))
	if err != nil {
		return err
	}
	slices.Sort(paths)

	groups := map[string][]skill{}
	var warnings []string
	for _, p := range paths {
		fields, err := readFrontmatter(p)
		if err != nil {
			return err
		}
		s := skill{
			name:     cmp.Or(fields["name"], filepath.Base(filepath.Dir(p))),
			category: cmp.Or(fields["category"], uncategorized),
			summary:  fields["summary"],
		}
		switch {
		case s.category == uncategorized:
			warnings = append(warnings, s.name+": metadata.category がない")
		case !slices.Contains(categories, s.category):
			warnings = append(warnings, fmt.Sprintf("%s: 一覧にないカテゴリ「%s」", s.name, s.category))
		}
		groups[s.category] = append(groups[s.category], s)
	}

	var extra []string
	for c := range groups {
		if c != uncategorized && !slices.Contains(categories, c) {
			extra = append(extra, c)
		}
	}
	slices.Sort(extra)
	order := append(append(slices.Clone(categories), extra...), uncategorized)

	var all [][]string
	for _, items := range groups {
		for _, s := range items {
			all = append(all, []string{s.name, s.summary})
		}
	}
	// カテゴリをまたいで桁をそろえる
	widths := ui.ColumnWidths([]string{"", ""}, all)

	blocks := []string{ui.Title("skl list")}
	for _, c := range order {
		items, ok := groups[c]
		if !ok {
			continue
		}
		rows := make([][]string, len(items))
		for i, s := range items {
			rows[i] = []string{s.name, s.summary}
		}
		cols := ui.Columns{Rows: rows, Widths: widths, Tone: func(_, col int) ui.Tone {
			if col == 1 {
				return ui.Muted
			}
			return ui.Plain
		}}
		blocks = append(blocks, ui.Section(c, ui.Count(len(items)), cols.String()))
	}
	footer := []string{fmt.Sprintf("Skill %d 件", len(all))}
	if len(warnings) > 0 {
		footer = append(footer, ui.MarkText(ui.LevelWarn, fmt.Sprintf("%d 警告", len(warnings))))
	}
	blocks = append(blocks, ui.Footer(footer...))
	ui.Println(ui.Blocks(blocks...))
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "skl: %s\n", w)
	}
	return nil
}

func skillsDir() (string, error) {
	out, err := exec.Command("chezmoi", "source-path").Output()
	if err != nil {
		return "", fmt.Errorf("chezmoi source-path に失敗: %w", err)
	}
	return filepath.Join(strings.TrimSpace(string(out)), "dot_agents", "skills"), nil
}

// readFrontmatter は name と metadata 直下の値だけを読む。
// それ以外の項目は使わないので解釈しない。
func readFrontmatter(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fields := map[string]string{}
	sc := bufio.NewScanner(f)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return fields, sc.Err()
	}
	inMetadata := false
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "---" {
			break
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inMetadata = strings.TrimRight(line, " \t") == "metadata:"
			if v, ok := strings.CutPrefix(line, "name:"); ok {
				fields["name"] = strings.TrimSpace(v)
			}
			continue
		}
		if key, value, ok := strings.Cut(strings.TrimSpace(line), ":"); inMetadata && ok {
			fields[key] = strings.TrimSpace(value)
		}
	}
	return fields, sc.Err()
}
