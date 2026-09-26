package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"cli-kit/internal/ui"
)

// Go の正規表現は先読みを持たないため、ID の後ろの空白は消費する。ID は m[1]-m[2] で組み立てる
var idRE = regexp.MustCompile(`^([A-Z]{3})-([0-9]{3})(?:` + ws + `|$)`)

type idDef struct {
	file  string
	line  int
	title string
}

type entryHeading struct {
	line  int
	title string
	rule  idRule
}

// entryHeadings は、ID を持つべきエントリの見出しを返す。雛形の固定見出しは除く。
func entryHeadings(path, relPath string, rules []idRule) []entryHeading {
	var mine []idRule
	for _, r := range rules {
		if r.tmpl == relPath {
			mine = append(mine, r)
		}
	}
	if len(mine) == 0 {
		return nil
	}
	// 置く節が決まっている規則を、「各##節」の規則より先に当てる
	slices.SortStableFunc(mine, func(a, b idRule) int {
		return boolInt(a.place == anyH2) - boolInt(b.place == anyH2)
	})
	fixed := map[heading]bool{}
	for _, h := range headings(filepath.Join(template, relPath)) {
		fixed[h.heading] = true
	}
	var out []entryHeading
	parent, hasParent := "", false
	for _, h := range headings(path) {
		if h.level == 2 {
			parent, hasParent = h.text, true
		}
		if fixed[h.heading] {
			continue
		}
		for _, r := range mine {
			if r.level == h.level && ((r.place == rootParent && h.level == 2) ||
				(r.place == anyH2 && h.level == 3 && hasParent) ||
				(strings.HasPrefix(r.place, "## ") && h.level == 3 && hasParent && parent == normHeading(r.place[3:]))) {
				out = append(out, entryHeading{h.line, h.text, r})
				break
			}
		}
	}
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// collectIDs は、プロダクトで定義された ID を返す。add が nil でなければ形式の問題を報告する。
func collectIDs(pj string, rules []idRule, add addFunc) map[string]idDef {
	report := func(f finding) {
		if add != nil {
			add(f)
		}
	}
	name := filepath.Base(pj)
	defs := map[string]idDef{}
	type sharedRef struct {
		idDef
		key string
	}
	var shared []sharedRef
	for _, f := range pjFiles(pj) {
		r := rel(pj, f)
		for _, e := range entryHeadings(f, r, rules) {
			m := idRE.FindStringSubmatch(e.title)
			if m == nil {
				report(finding{name, "id", "error", r, e.line, e.title, fmt.Sprintf("ID がない（%s-000 の形）", e.rule.prefix)})
				continue
			}
			if m[1] != e.rule.prefix {
				report(finding{name, "id", "error", r, e.line, e.title, fmt.Sprintf("接頭辞が違う（%s を使う）", e.rule.prefix)})
				continue
			}
			key := m[1] + "-" + m[2]
			if e.rule.shared {
				shared = append(shared, sharedRef{idDef{r, e.line, e.title}, key})
			} else if d, ok := defs[key]; ok {
				report(finding{name, "id", "error", r, e.line, e.title, fmt.Sprintf("ID が重複（%s:%d）", d.file, d.line)})
			} else {
				defs[key] = idDef{r, e.line, e.title}
			}
		}
	}
	for _, s := range shared {
		if _, ok := defs[s.key]; !ok {
			report(finding{name, "id", "error", s.file, s.line, s.title, "対応するエントリがない"})
		}
	}
	return defs
}

type reference struct {
	file string
	line int
	key  string
}

// bodyFiles は、参照を探すプロダクトの文書と ADR を返す。
func bodyFiles(pj string) []string {
	return uniq(append(pjFiles(pj), mdFilesIn(filepath.Join(pj, "decisions"))...))
}

// idReferences は、プロダクトの文書と ADR の本文に書かれた ID の参照を返す。見出しと雛形の -000 は除く。
func idReferences(pj string, prefixes []string) []reference {
	sorted := slices.Sorted(slices.Values(prefixes))
	pat := regexp.MustCompile(`(?:` + strings.Join(sorted, "|") + `)-[0-9]{3}`)
	var out []reference
	for _, f := range bodyFiles(pj) {
		for i, l := range withoutCode(read(f)) {
			if _, ok := matchHeading(l); ok {
				continue
			}
			for _, loc := range pat.FindAllStringIndex(l, -1) {
				// 前は英数字と - 以外、後ろは数字以外であること（\b は日本語の文字も語の一部とみなすため使わない）
				if loc[0] > 0 && isIDRune(l[loc[0]-1]) {
					continue
				}
				if loc[1] < len(l) && l[loc[1]] >= '0' && l[loc[1]] <= '9' {
					continue
				}
				if key := l[loc[0]:loc[1]]; !strings.HasSuffix(key, "-000") {
					out = append(out, reference{rel(pj, f), i + 1, key})
				}
			}
		}
	}
	return out
}

func isIDRune(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-'
}

const hig = "design/hig.md"

var (
	// 見出しはバッククォートを除いて比べる
	higRuleRE = regexp.MustCompile(`^([a-z0-9-]+)\.([a-z0-9-]+)$`)
	higCatRE  = regexp.MustCompile("^- `([a-z0-9-]+)\\.\\*`")
)

// higRules は、hig の「ルール ID」に挙げたカテゴリと、ルールの定義を返す。
func higRules(pj string, add addFunc) (map[string]bool, map[string]idDef) {
	f := filepath.Join(pj, hig)
	cats, defs := map[string]bool{}, map[string]idDef{}
	if !exists(f) {
		return cats, defs
	}
	for _, l := range section(read(f), "ルール ID") {
		if m := higCatRE.FindStringSubmatch(l); m != nil {
			cats[m[1]] = true
		}
	}
	name := filepath.Base(pj)
	for _, h := range headings(f) {
		if h.level != 3 {
			continue
		}
		m := higRuleRE.FindStringSubmatch(h.text)
		if m == nil {
			continue
		}
		key := m[1] + "." + m[2]
		if !cats[m[1]] && add != nil {
			add(finding{name, "id", "error", hig, h.line, h.text, "カテゴリが「ルール ID」にない"})
		}
		if d, ok := defs[key]; ok {
			if add != nil {
				add(finding{name, "id", "error", hig, h.line, h.text, fmt.Sprintf("ID が重複（%s:%d）", hig, d.line)})
			}
			continue
		}
		defs[key] = idDef{hig, h.line, h.text}
	}
	return cats, defs
}

// higReferences は、本文に `category.rule-name` の形で書かれた hig のルールへの参照を返す。
func higReferences(pj string, cats map[string]bool) []reference {
	if len(cats) == 0 {
		return nil
	}
	names := slices.Sorted(func(yield func(string) bool) {
		for c := range cats {
			if !yield(c) {
				return
			}
		}
	})
	pat := regexp.MustCompile("`((?:" + strings.Join(names, "|") + `)\.[a-z0-9-]+)` + "`")
	var out []reference
	for _, f := range bodyFiles(pj) {
		for i, l := range withoutCode(read(f)) {
			if _, ok := matchHeading(l); ok {
				continue
			}
			for _, m := range pat.FindAllStringSubmatch(l, -1) {
				out = append(out, reference{rel(pj, f), i + 1, m[1]})
			}
		}
	}
	return out
}

func idPrefixes(rules []idRule) []string {
	var out []string
	for _, r := range rules {
		if !slices.Contains(out, r.prefix) {
			out = append(out, r.prefix)
		}
	}
	return out
}

func checkIDs(add addFunc) {
	rules := loadIDRules()
	prefixes := idPrefixes(rules)
	for _, name := range projects() {
		pj := filepath.Join(root, name)
		defs := collectIDs(pj, rules, add)
		for _, r := range idReferences(pj, prefixes) {
			if _, ok := defs[r.key]; !ok {
				add(finding{name, "id", "error", r.file, r.line, r.key, "参照先の ID がない"})
			}
		}
		cats, higDefs := higRules(pj, add)
		for _, r := range higReferences(pj, cats) {
			if _, ok := higDefs[r.key]; !ok {
				add(finding{name, "id", "error", r.file, r.line, "`" + r.key + "`", "参照先の hig ルールがない"})
			}
		}
	}
}

// cmdID は、接頭辞なら次の番号を、ID なら定義と参照の場所を表示する。
func cmdID(project, target string) int {
	names := projects()
	name := project
	if name == "" {
		name = choose(names)
		if name == "" {
			return 0
		}
	}
	if !slices.Contains(names, name) {
		fail("プロジェクトが見つかりません: " + name)
	}
	pj := filepath.Join(root, name)
	rules := loadIDRules()
	prefixes := idPrefixes(rules)
	defs := collectIDs(pj, rules, nil)
	cats, higDefs := higRules(pj, nil)

	if higRuleRE.MatchString(target) {
		d, ok := higDefs[target]
		if !ok {
			ui.Println(renderUndefined(name, target))
			return 1
		}
		var refs []reference
		for _, r := range higReferences(pj, cats) {
			if r.key == target {
				refs = append(refs, r)
			}
		}
		ui.Println(renderDefinition(name, d, refs))
		return 0
	}

	target = strings.ToUpper(target)
	if slices.Contains(prefixes, target) {
		used := 0
		for k := range defs {
			if n, err := strconv.Atoi(strings.TrimPrefix(k, target+"-")); err == nil && strings.HasPrefix(k, target+"-") {
				used = max(used, n)
			}
		}
		fmt.Printf("%s-%03d\n", target, used+1)
		return 0
	}
	if m := idRE.FindStringSubmatch(target); m == nil || !slices.Contains(prefixes, m[1]) {
		fail(fmt.Sprintf("接頭辞（%s）か ID（例: FRQ-001）を指定してください: %s",
			strings.Join(slices.Sorted(slices.Values(prefixes)), " "), target))
	}
	d, ok := defs[target]
	if !ok {
		ui.Println(renderUndefined(name, target))
		return 1
	}
	var refs []reference
	for _, r := range idReferences(pj, prefixes) {
		if r.key == target {
			refs = append(refs, r)
		}
	}
	ui.Println(renderDefinition(name, d, refs))
	return 0
}

func renderDefinition(project string, d idDef, refs []reference) string {
	def := ui.Tree(ui.Node{Text: ui.Paint(ui.Path, fmt.Sprintf("%s:%d", d.file, d.line)) + "  " + d.title})
	body := ui.None()
	if len(refs) > 0 {
		var nodes []ui.Node
		for _, r := range refs {
			nodes = append(nodes, ui.Node{Text: ui.Paint(ui.Path, fmt.Sprintf("%s:%d", r.file, r.line))})
		}
		body = ui.Tree(nodes...)
	}
	return ui.Blocks(
		ui.Title("pdocs id", project),
		ui.Section("定義", "", def),
		ui.Section("参照", ui.Count(len(refs)), body),
		ui.Footer(fmt.Sprintf("参照 %d 件", len(refs))),
	)
}

func renderUndefined(project, target string) string {
	return ui.Blocks(
		ui.Title("pdocs id", project),
		ui.Footer(ui.MarkText(ui.LevelError, target+" は定義されていません")),
	)
}
