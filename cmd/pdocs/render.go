package main

import (
	"cmp"
	"fmt"
	"slices"

	"cli-kit/internal/ui"
)

var aspectLabels = []struct{ key, label string }{
	{"readme", "文書地図"}, {"status", "ステータス値"}, {"template", "見出し構成"}, {"id", "エントリの ID"},
	{"sources", "成果物の有無"}, {"quality", "品質特性の水準と確認手段"}, {"items", "項目名"},
}

var levels = map[string]ui.Level{
	"error": ui.LevelError, "warn": ui.LevelWarn, "undefined": ui.LevelUndefined, "info": ui.LevelInfo,
}

func tally(fs []finding) (errors, warns, undefined int) {
	for _, f := range fs {
		switch f.level {
		case "error":
			errors++
		case "warn":
			warns++
		case "undefined":
			undefined++
		}
	}
	return
}

func filterFindings(fs []finding, keep func(finding) bool) []finding {
	var out []finding
	for _, f := range fs {
		if keep(f) {
			out = append(out, f)
		}
	}
	return out
}

// renderFindings は、PJ → 検査の種類 → ファイルの順にまとめて表示する。
func renderFindings(found []finding, aspects checkSet) string {
	names := projects()
	if slices.ContainsFunc(found, func(f finding) bool { return f.project == "_template" }) {
		names = append(names, "_template")
	}
	blocks := []string{ui.Title("pdocs check", fmt.Sprintf("%d PJ", len(projects())))}
	clean := 0
	for _, name := range names {
		fs := filterFindings(found, func(f finding) bool { return f.project == name })
		e, w, u := tally(fs)
		if e+w+u == 0 {
			clean++
		}
		var nodes []ui.Node
		for _, a := range aspectLabels {
			af := filterFindings(fs, func(f finding) bool { return f.aspect == a.key })
			if !aspects[a.key] || (name == "_template" && len(af) == 0) {
				continue
			}
			nodes = append(nodes, renderAspect(a.label, af))
		}
		blocks = append(blocks, ui.Section(name, ui.Tally(e, w, u), ui.Tree(nodes...)))
	}

	e, w, u := tally(found)
	if e+w+u == 0 {
		blocks = append(blocks, ui.Footer(ui.MarkText(ui.LevelOK, fmt.Sprintf("%d PJ すべて指摘なし", len(names)))))
	} else {
		blocks = append(blocks, ui.Footer(ui.Tally(e, w, u), fmt.Sprintf("指摘なし %d/%d PJ", clean, len(names))))
	}
	return ui.Blocks(blocks...)
}

func renderAspect(label string, af []finding) ui.Node {
	e, w, u := tally(af)
	level, count := ui.LevelOK, 0
	switch {
	case e > 0:
		level, count = ui.LevelError, e
	case w > 0:
		level, count = ui.LevelWarn, w
	case u > 0:
		level, count = ui.LevelUndefined, u
	}
	head := ui.Mark(level) + " " + label
	// 説明がすべて同じなら見出しに一度だけ出す
	var notes []string
	for _, f := range af {
		if f.level != "info" && !slices.Contains(notes, f.note) {
			notes = append(notes, f.note)
		}
	}
	shared := len(notes) == 1
	if shared {
		head += ui.Paint(ui.Muted, "（"+notes[0]+"）")
	}
	if count > 0 {
		head += "  " + ui.Paint(ui.LevelTone(level), fmt.Sprint(count))
	}
	node := ui.Node{Text: head}

	var files []string
	for _, f := range af {
		files = append(files, f.file)
	}
	slices.Sort(files)
	for _, file := range uniq(files) {
		ff := filterFindings(af, func(f finding) bool { return f.file == file })
		slices.SortStableFunc(ff, func(a, b finding) int {
			return cmp.Or(boolInt(a.level != "error")-boolInt(b.level != "error"), a.line-b.line)
		})
		n, locW := 0, 0
		for _, f := range ff {
			if f.level != "info" {
				n++
			}
			if f.line > 0 {
				locW = max(locW, len(fmt.Sprintf("L%d", f.line)))
			}
		}
		group := ui.Node{Text: ui.Group(file, n)}
		for _, f := range ff {
			lv := levels[f.level]
			if f.level == "info" {
				group.Children = append(group.Children, ui.Node{Text: ui.MarkText(lv, f.note+": "+f.text)})
				continue
			}
			text := f.text
			if locW > 0 {
				l := ""
				if f.line > 0 {
					l = fmt.Sprintf("L%d", f.line)
				}
				text = ui.Paint(ui.Muted, ui.Pad(l, locW)) + "  " + text
			}
			note := f.note
			if shared {
				note = ""
			}
			group.Children = append(group.Children, ui.Node{Text: ui.Item(lv, text, note)})
		}
		node.Children = append(node.Children, group)
	}
	return node
}

func cmdCheck(ids statusSet, aspects checkSet) int {
	found := runChecks(ids, aspects)
	ui.Println(renderFindings(found, aspects))
	if e, _, _ := tally(found); e > 0 {
		return 1
	}
	return 0
}
