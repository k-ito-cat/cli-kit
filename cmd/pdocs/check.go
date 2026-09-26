package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type finding struct {
	project string
	aspect  string // readme / status / template / id / sources / quality / items
	level   string // error（問題）/ warn（警告）/ undefined（未定義）/ info（参考）
	file    string
	line    int // 0 は行を持たない
	text    string
	note    string
}

type addFunc func(finding)

// checkReadme は、各プロダクト README が必須文書と共通運用を直接指し、
// リンク切れと、地図に載っていない文書がないかを検査する。
func checkReadme(add addFunc) {
	var required []string
	for _, t := range links(section(read(policy), "必須")) {
		if strings.HasPrefix(t, "_template/spec/") {
			required = append(required, strings.TrimPrefix(t, "_template/"))
		}
	}
	var common []string
	for _, t := range links(read(filepath.Join(template, "README.md"))) {
		if strings.HasPrefix(t, "../documentation-policy.md") {
			common = append(common, t)
		}
	}
	for _, name := range projects() {
		pj := filepath.Join(root, name)
		pjLinks := links(read(filepath.Join(pj, "README.md")))
		errorf := func(text, note string) {
			add(finding{name, "readme", "error", "README.md", 0, text, note})
		}
		for _, doc := range required {
			if !exists(filepath.Join(pj, doc)) {
				errorf(doc, "必須文書がない")
			} else if !linksTo(pjLinks, doc) {
				errorf(doc, "文書地図に載っていない")
			}
		}
		for _, t := range common {
			if !contains(pjLinks, t) {
				errorf(t, "文書地図に載っていない")
			}
		}
		for _, t := range pjLinks {
			if note := checkLink(pj, t); note != "" {
				errorf(t, note)
			}
		}
		// 雛形の地図には必ずある文書だけが載るため、任意の文書を採用したときの載せ忘れをここで拾う
		mapped := map[string]bool{}
		for _, t := range pjLinks {
			mapped[resolve(filepath.Join(pj, linkPath(t)))] = true
		}
		if designReadme := filepath.Join(pj, "design", "README.md"); isFile(designReadme) {
			for _, t := range links(read(designReadme)) {
				mapped[resolve(filepath.Join(pj, "design", linkPath(t)))] = true
			}
		}
		docs := mdFilesUnder(filepath.Join(pj, "spec"))
		docs = append(docs, mdFilesIn(filepath.Join(pj, "design"))...)
		docs = append(docs, mdFilesUnder(filepath.Join(pj, "operations"))...)
		docs = append(docs, filepath.Join(pj, "decisions", "README.md"), filepath.Join(pj, "prototype", "README.md"))
		for _, f := range docs {
			if isFile(f) && !mapped[resolve(f)] {
				errorf(rel(pj, f), "文書地図に載っていない")
			}
		}
	}
}

// linkPath はリンク先から #アンカーを除いた部分を返す。
func linkPath(target string) string {
	p, _, _ := strings.Cut(target, "#")
	return p
}

func linksTo(targets []string, doc string) bool {
	for _, t := range targets {
		if linkPath(t) == doc {
			return true
		}
	}
	return false
}

func contains(items []string, v string) bool {
	for _, it := range items {
		if it == v {
			return true
		}
	}
	return false
}

// checkLink はリンク先の問題を返す。問題がなければ空文字。
func checkLink(base, target string) string {
	path, anchor, _ := strings.Cut(target, "#")
	if path == "" {
		return "リンク切れ"
	}
	dest := resolve(filepath.Join(base, path))
	if !exists(dest) {
		return "リンク切れ"
	}
	if anchor != "" && isFile(dest) {
		want := slug(anchor)
		for _, l := range read(dest) {
			if h, ok := matchHeading(l); ok && slug(h.text) == want {
				return ""
			}
		}
		return "リンク先の見出しがない"
	}
	return ""
}

func checkStatus(ids statusSet, add addFunc) {
	for _, name := range append(projects(), "_template") {
		base := filepath.Join(root, name)
		for _, d := range targetDirs {
			for _, f := range mdFilesUnder(filepath.Join(base, d)) {
				r := rel(base, f)
				for _, inv := range statFile(f, r, ids).invalid {
					line, _ := strconv.Atoi(inv.where)
					note := fmt.Sprintf("「%s」にないステータス", inv.set)
					if inv.where == "表" {
						note += "（表）"
					}
					add(finding{name, "status", "error", r, line, inv.value, note})
				}
			}
		}
	}
}

// ---- 成果物の有無 ------------------------------------------------------------

const sources = "編集元と参照先"

type sourceEntry struct {
	target string
	line   int
	values map[string]string
}

var (
	sourceTargetRE = regexp.MustCompile("^- ([^:：`]+?)[:：]" + ws + "*$")
	sourceValueRE  = regexp.MustCompile(`^  - ([^:：]+?)[:：][ \t]*(.*?)` + ws + `*$`)
	backquotedRE   = regexp.MustCompile("`([^`]+)`")
)

// sourceEntries は README の「編集元と参照先」から、対象ごとの {項目: 値} を行番号とともに返す。
func sourceEntries(readme string) []*sourceEntry {
	var out []*sourceEntry
	byTarget := map[string]*sourceEntry{}
	inside := false
	var cur *sourceEntry
	for i, l := range withoutCode(read(readme)) {
		if h, ok := matchHeading(l); ok {
			inside, cur = normHeading(h.text) == sources, nil
			continue
		}
		if !inside {
			continue
		}
		if m := sourceTargetRE.FindStringSubmatch(l); m != nil {
			t := strings.TrimSpace(m[1])
			// 同じ対象が2度あれば、Python の dict と同じく位置は最初のまま中身を差し替える
			if e, ok := byTarget[t]; ok {
				e.line, e.values = i+1, map[string]string{}
				cur = e
			} else {
				cur = &sourceEntry{t, i + 1, map[string]string{}}
				byTarget[t] = cur
				out = append(out, cur)
			}
		} else if m := sourceValueRE.FindStringSubmatch(l); cur != nil && m != nil {
			cur.values[strings.TrimSpace(m[1])] = m[2]
		} else if strings.TrimSpace(l) != "" && !strings.HasPrefix(l, " ") {
			cur = nil
		}
	}
	return out
}

// appRepo は ghq の配下から、プロダクトと同じ名前のアプリのリポジトリを探す。
func appRepo(name string) string {
	if _, err := exec.LookPath("ghq"); err != nil {
		return ""
	}
	out, _ := exec.Command("ghq", "list", "-p").Output()
	for l := range strings.Lines(string(out)) {
		if p := strings.TrimRight(l, "\r\n"); filepath.Base(p) == name {
			return p
		}
	}
	return ""
}

var sourceRoles = []roleWord{{"作成済み", "done"}, {"後で作る", "later"}, {"不要", "unneeded"}}

func checkSources(add addFunc) {
	targets := loadSourceTargets()
	roles := loadRoles(sourceSet, sourceRoles)
	sourceIDs := loadStatusSets()[sourceSet]
	tmpl := map[string]bool{}
	for _, e := range sourceEntries(filepath.Join(template, "README.md")) {
		tmpl[e.target] = true
	}
	for _, t := range targets {
		if !tmpl[t] {
			add(finding{"_template", "sources", "error", "README.md", 0, t, "雛形に対象がない"})
		}
	}
	for _, name := range projects() {
		entries := map[string]*sourceEntry{}
		for _, e := range sourceEntries(filepath.Join(root, name, "README.md")) {
			entries[e.target] = e
		}
		repo := appRepo(name)
		for _, t := range targets {
			e, ok := entries[t]
			if !ok {
				add(finding{name, "sources", "error", "README.md", 0, t, "対象が載っていない"})
				continue
			}
			f := func(level, note, text string) {
				add(finding{name, "sources", level, "README.md", e.line, text, note})
			}
			st, src, why := e.values["ステータス"], e.values["正本"], e.values["理由"]
			r, known := roles[st]
			switch {
			case st != "" && !sourceIDs.has(st):
				f("error", fmt.Sprintf("「%s」にないステータス（%s）", sourceSet, st), t)
			case r == "done":
				if src == "" {
					f("error", "作成済みなのに正本の場所がない", t)
				}
				for _, path := range missingPaths(repo, src) {
					f("error", fmt.Sprintf("正本が見つからない（%s）", path), t)
				}
			case (r == "later" || r == "unneeded") && why == "":
				f("error", "理由がない", t)
			case r == "later":
				f("info", "後で作る", t+" — "+why)
			case !known:
				f("undefined", "作成済み・後で作る・不要のどれでもない", t)
			}
		}
	}
}

// missingPaths は、text にバッククォートで書かれたパスのうち、repo に無いものを返す。
// アプリのリポジトリが見つからなければ確かめない。
func missingPaths(repo, text string) []string {
	if repo == "" {
		return nil
	}
	var out []string
	for _, m := range backquotedRE.FindAllStringSubmatch(text, -1) {
		if !exists(filepath.Join(repo, m[1])) {
			out = append(out, m[1])
		}
	}
	return out
}

// ---- 品質特性の水準 ------------------------------------------------------------

const (
	qualityFile    = "spec/requirements/non-functional.md"
	qualitySection = "品質特性の水準"
)

var (
	qualityRoles = []roleWord{{"採用予定", "planned"}, {"採用", "adopted"}, {"後で決める", "later"}, {"対象外", "excluded"}}
	checkRoles   = []roleWord{{"確立済み", "done"}, {"後で確立", "later"}}

	qualityItemRE  = regexp.MustCompile(`^- ([^:：]+?)[:：][ \t]*(.*?)` + ws + `*$`)
	qualityCheckRE = regexp.MustCompile(`^  - ([^:：]+?)[:：][ \t]*(.*?)` + ws + `*$`)
)

type qualityEntry struct {
	line  int
	title string
	top   map[string]string
	check map[string]string
}

// qualityEntries は、非機能要求の「品質特性の水準」のエントリを、見出し・項目・確認手段の項目とともに返す。
func qualityEntries(path string) []*qualityEntry {
	var out []*qualityEntry
	inside, inCheck := false, false
	var cur *qualityEntry
	for i, l := range withoutCode(read(path)) {
		if h, ok := matchHeading(l); ok {
			tx := normHeading(h.text)
			if h.level == 2 {
				inside, cur = tx == qualitySection, nil
			} else if inside && h.level == 3 {
				cur = &qualityEntry{i + 1, tx, map[string]string{}, map[string]string{}}
				out = append(out, cur)
				inCheck = false
			}
			continue
		}
		if !inside || cur == nil {
			continue
		}
		if m := qualityItemRE.FindStringSubmatch(l); m != nil {
			key := strings.TrimSpace(m[1])
			cur.top[key] = m[2]
			inCheck = key == "確認手段"
		} else if m := qualityCheckRE.FindStringSubmatch(l); inCheck && m != nil {
			cur.check[strings.TrimSpace(m[1])] = m[2]
		}
	}
	return out
}

// checkQuality は、品質特性の水準のエントリが表の ID を指し、採用したものに水準を確かめる手段が確立しているかを検査する。
func checkQuality(add addFunc) {
	qroles := loadRoles("品質特性の採否", qualityRoles)
	croles := loadRoles("確認手段の状況", checkRoles)
	master := loadQualityIDs()
	for _, name := range projects() {
		f := filepath.Join(root, name, qualityFile)
		if !exists(f) {
			continue
		}
		repo := appRepo(name)
		seen := map[string]string{}
		var entries []*qualityEntry
		for _, e := range qualityEntries(f) {
			if !stubRE.MatchString(e.title) {
				entries = append(entries, e)
			}
		}
		if len(entries) == 0 {
			add(finding{name, "quality", "undefined", qualityFile, 0, qualitySection, "1つも定義されていない"})
		}
		for _, e := range entries {
			report := func(level, note, text string) {
				add(finding{name, "quality", level, qualityFile, e.line, text, note})
			}
			errorf := func(note string) { report("error", note, e.title) }

			qc := e.top["品質特性"]
			switch {
			case qc == "":
				errorf("品質特性の ID がない")
			case !master[qc]:
				errorf(fmt.Sprintf("表に無い品質特性の ID（%s）", qc))
			case seen[qc] != "":
				errorf(fmt.Sprintf("同じ品質特性のエントリがある（%s）", seen[qc]))
			default:
				seen[qc] = e.title
			}
			qst := e.top["ステータス"]
			if qst == "" {
				errorf("ステータスがない（採用する前提なら採用予定にする）")
				continue
			}
			// 表にない値は「ステータス値」の検査で出す
			qrole, known := qroles[qst]
			if !known || (qrole != "planned" && qrole != "adopted") {
				continue
			}
			if e.top["基準と水準"] == "" {
				if qrole == "adopted" {
					report("error", "採用なのに基準と水準がない", e.title)
				} else {
					report("warn", "採用する前提だが、基準と水準が決まっていない", e.title)
				}
				continue
			}
			c := e.check
			switch croles[c["ステータス"]] {
			case "done":
				if c["手段"] == "" || c["参照先"] == "" {
					report("error", "確立済みなのに手段か参照先がない", e.title)
				}
				for _, path := range missingPaths(repo, c["参照先"]) {
					report("error", fmt.Sprintf("参照先が見つからない（%s）", path), e.title)
				}
			case "later":
				if c["理由"] != "" {
					report("info", "後で確立", e.title+" — "+c["理由"])
				} else {
					report("error", "後で確立する理由がない", e.title)
				}
			default:
				report("warn", "採用した水準を確かめる手段が確立していない", e.title)
			}
		}
	}
}

// ---- 検査の実行 ----------------------------------------------------------------

type checkSet map[string]bool

// defaultChecks は観点の指定がないときの検査。items は含めない。
var defaultChecks = checkSet{"readme": true, "status": true, "template": true, "id": true, "sources": true, "quality": true}

func runChecks(ids statusSet, aspects checkSet) []finding {
	var found []finding
	add := func(f finding) { found = append(found, f) }
	if aspects["readme"] {
		checkReadme(add)
	}
	if aspects["status"] {
		checkStatus(ids, add)
	}
	if aspects["template"] {
		checkTemplate(add)
	}
	if aspects["id"] {
		checkIDs(add)
	}
	if aspects["sources"] {
		checkSources(add)
	}
	if aspects["quality"] {
		checkQuality(add)
	}
	if aspects["items"] {
		checkItems(add)
	}
	return found
}
