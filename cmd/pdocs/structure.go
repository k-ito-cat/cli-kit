package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// templateFor は、プロダクトの文書に対応する雛形の相対パスを返す。対応しなければ空文字。
func templateFor(relPath string) string {
	first, _, _ := strings.Cut(relPath, string(filepath.Separator))
	switch {
	case first == "decisions" && filepath.Base(relPath) != "README.md":
		return "" // ADR 本体は adrs が管理する
	case first == "operations":
		return "operations/runbook.md"
	}
	return relPath
}

// pjFiles は、雛形と比べるプロダクトの文書を返す。
func pjFiles(pj string) []string {
	files := []string{filepath.Join(pj, "README.md")}
	files = append(files, mdFilesUnder(filepath.Join(pj, "spec"))...)
	files = append(files, mdFilesUnder(filepath.Join(pj, "design"))...)
	for _, f := range []string{"decisions/README.md", "prototype/README.md"} {
		if p := filepath.Join(pj, f); exists(p) {
			files = append(files, p)
		}
	}
	return append(files, mdFilesIn(filepath.Join(pj, "operations"))...)
}

// chainKey は見出しの連なり（親から自分まで）を比べられる形にする。
func chainKey(chain []heading) string {
	var b strings.Builder
	for _, h := range chain {
		fmt.Fprintf(&b, "%d\x00%s\x01", h.level, h.text)
	}
	return b.String()
}

func pushChain(chain []heading, h heading) []heading {
	var out []heading
	for _, c := range chain {
		if c.level < h.level {
			out = append(out, c)
		}
	}
	return append(out, h)
}

var firstKeyRE = regexp.MustCompile(`^- ([^:：]+)[:：]`)

type headingLine struct {
	line int
	text string
}

// compareStructure は、雛形にない見出し（NG）と、採用していない雛形の見出し（未採用）を返す。
func compareStructure(tmplRel, tmpl, target string, rules map[string][]entryRule) ([]headingLine, []string) {
	stubs := rules[tmplRel]
	isStub := func(h heading) bool {
		for _, s := range stubs {
			if s.heading == h {
				return true
			}
		}
		return false
	}

	// 雛形の固定見出し（スタブとその子孫を除く）と、スタブごとの子見出し
	type fixedPath struct {
		key   string
		chain []heading
	}
	var fixed []fixedPath
	fixedKeys := map[string]bool{}
	stubChildren := map[string]map[string]bool{}
	var chain []heading
	var inStub *heading
	for _, h := range headings(tmpl) {
		if h.level == 1 {
			chain = nil
			continue
		}
		chain = pushChain(chain, h.heading)
		if inStub != nil && h.level > inStub.level {
			if stubChildren[inStub.text] == nil {
				stubChildren[inStub.text] = map[string]bool{}
			}
			stubChildren[inStub.text][h.text] = true
			continue
		}
		inStub = nil
		if isStub(h.heading) {
			inStub = &heading{h.level, h.text}
			continue
		}
		if k := chainKey(chain); !fixedKeys[k] {
			fixedKeys[k] = true
			fixed = append(fixed, fixedPath{k, slices.Clone(chain)})
		}
	}

	// スタブ直後の最初の項目名。エントリは同じ項目から書き始める
	firstKey := map[heading]string{}
	tl := read(tmpl)
	for i, l := range tl {
		m, ok := matchHeading(strings.TrimSpace(l))
		if !ok {
			continue
		}
		h := heading{m.level, normHeading(m.text)}
		if !isStub(h) {
			continue
		}
		for _, x := range tl[i+1:] {
			if strings.TrimSpace(x) == "" {
				continue
			}
			if k := firstKeyRE.FindStringSubmatch(x); k != nil {
				firstKey[h] = strings.TrimSpace(k[1])
			}
			break
		}
	}

	targetLines := read(target)
	opensWith := func(n int, key string) bool {
		head := ""
		for _, x := range targetLines[n:] {
			if strings.TrimSpace(x) != "" {
				head = x
				break
			}
		}
		return regexp.MustCompile(`^- ` + regexp.QuoteMeta(key) + `[:：]`).MatchString(head)
	}

	var ng []headingLine
	seen := map[string]bool{}
	chain = nil
	var entry *heading
	for _, h := range headings(target) {
		if h.level == 1 {
			chain = nil
			continue
		}
		chain = pushChain(chain, h.heading)
		label := strings.Repeat("#", h.level) + " " + h.text
		if entry != nil && h.level > entry.level {
			if !stubChildren[entry.text][h.text] {
				ng = append(ng, headingLine{h.line, label})
			}
			continue
		}
		entry = nil
		if k := chainKey(chain); fixedKeys[k] {
			seen[k] = true
			continue
		}
		var parent *heading
		if len(chain) > 1 {
			parent = &chain[len(chain)-2]
		}
		for _, s := range stubs {
			if h.level != s.level {
				continue
			}
			if (s.parent.kind == placeRoot && parent == nil) ||
				(s.parent.kind == placeAnyH2 && parent != nil && parent.level == 2) ||
				(s.parent.kind == placeHeading && parent != nil && *parent == s.parent.heading) {
				key, ok := firstKey[s.heading]
				if !ok || opensWith(h.line, key) {
					entry = &heading{h.level, s.text}
				}
				break
			}
		}
		if entry == nil {
			ng = append(ng, headingLine{h.line, label})
		}
	}

	// 未採用の節は、浅いものから雛形の順に並べる
	var unseen []fixedPath
	for _, p := range fixed {
		if !seen[p.key] {
			unseen = append(unseen, p)
		}
	}
	slices.SortStableFunc(unseen, func(a, b fixedPath) int { return len(a.chain) - len(b.chain) })
	missing := make([]string, len(unseen))
	for i, p := range unseen {
		texts := make([]string, len(p.chain))
		for j, h := range p.chain {
			texts[j] = h.text
		}
		missing[i] = strings.Join(texts, " > ")
	}
	return ng, missing
}

func checkTemplate(add addFunc) {
	rules := loadEntryRules()
	for _, name := range projects() {
		pj := filepath.Join(root, name)
		for _, f := range pjFiles(pj) {
			r := rel(pj, f)
			tmplRel := templateFor(r)
			if tmplRel == "" {
				continue
			}
			tmpl := filepath.Join(template, tmplRel)
			if !exists(tmpl) {
				add(finding{name, "template", "error", r, 0, tmplRel, "雛形がない"})
				continue
			}
			ng, missing := compareStructure(tmplRel, tmpl, f, rules)
			for _, h := range ng {
				add(finding{name, "template", "error", r, h.line, h.text, "雛形にない見出し"})
			}
			if len(missing) > 0 {
				add(finding{name, "template", "info", r, 0, strings.Join(missing, " / "), fmt.Sprintf("未採用 %d 節", len(missing))})
			}
		}
	}
}

// ---- 項目名 --------------------------------------------------------------------

var itemKeyRE = regexp.MustCompile(`^- ([^:：` + "`" + `]+?)(?:：|:(?:` + ws + `|$))`)

type itemKey struct {
	line int
	key  string
}

// itemKeys は行頭の `- 項目名:` を（行番号, 項目名）で返す。
// 雛形の型はコードブロック内にもあるため、includeCode で含める。
func itemKeys(path string, includeCode bool) []itemKey {
	lines := read(path)
	if !includeCode {
		lines = withoutCode(lines)
	}
	var out []itemKey
	for i, l := range lines {
		if m := itemKeyRE.FindStringSubmatch(l); m != nil {
			out = append(out, itemKey{i + 1, strings.TrimSpace(m[1])})
		}
	}
	return out
}

// freeSections は、雛形で項目を持たない見出し（文書地図、Index、未決事項など自由に書く節）を返す。
func freeSections(tmpl string) map[string]bool {
	owner := sectionOf(tmpl)
	withItems := map[string]bool{}
	for _, k := range itemKeys(tmpl, false) {
		withItems[owner[k.line]] = true
	}
	free := map[string]bool{}
	for _, h := range headings(tmpl) {
		if !withItems[h.text] {
			free[h.text] = true
		}
	}
	return free
}

// checkItems は、雛形にない項目名を警告として並べる（問題の件数には数えない）。
func checkItems(add addFunc) {
	for _, name := range projects() {
		pj := filepath.Join(root, name)
		for _, f := range pjFiles(pj) {
			r := rel(pj, f)
			tmplRel := templateFor(r)
			if tmplRel == "" || !exists(filepath.Join(template, tmplRel)) {
				continue
			}
			tmpl := filepath.Join(template, tmplRel)
			known := map[string]bool{}
			for _, k := range itemKeys(tmpl, true) {
				known[k.key] = true
			}
			free := freeSections(tmpl)
			owner := sectionOf(f)
			for _, k := range itemKeys(f, false) {
				if !known[k.key] && !free[owner[k.line]] {
					add(finding{name, "items", "warn", r, k.line, k.key, "雛形にない項目"})
				}
			}
		}
	}
}
