package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"cli-kit/internal/ui"
)

var (
	checkItemRE  = regexp.MustCompile(`^- 確認手段[:：]`)
	statusItemRE = regexp.MustCompile(`^` + ws + `*- ステータス[:：]`)
)

// statusSetAt は、判断のステータス以外の表を使う行について、行番号→表の名前を返す。
// スタブ（XXX-000）の行は空文字。
func statusSetAt(rel string, lines []string) map[int]string {
	out := map[int]string{}
	sec, hasSec, stub, inCheck := "", false, false, false
	for i, l := range lines {
		n := i + 1
		if h, ok := matchHeading(l); ok {
			tx := normHeading(h.text)
			switch h.level {
			case 2:
				sec, hasSec, stub = tx, true, false
			case 3:
				stub = stubRE.MatchString(tx)
			}
			inCheck = false
			continue
		}
		if checkItemRE.MatchString(l) {
			inCheck = true
		} else if strings.HasPrefix(l, "- ") {
			inCheck = false
		}
		if statusItemRE.MatchString(l) && hasSec {
			key := specialKey{rel, sec, strings.HasPrefix(l, " ") && inCheck}
			if name, ok := specialSetName(key); ok {
				if stub {
					name = ""
				}
				out[n] = name
			}
		}
	}
	return out
}

type invalidStatus struct {
	where, value, set string // where は行番号か「表」
}

// openItem は未決事項の1まとまり。label は項目名（無ければ空）、values はその下の未決事項。
type openItem struct {
	label  string
	values []string
}

type fileStat struct {
	path     string
	items    int
	filled   int
	statuses map[string]int
	invalid  []invalidStatus
	open     []openItem
	special  map[string]map[string]int // 表の名前 → {ID: 件数}
}

// openCount は未決事項の件数。項目名だけの行は数えない。
func (s fileStat) openCount() int {
	n := 0
	for _, o := range s.open {
		n += len(o.values)
	}
	return n
}

var (
	openLabelRE  = regexp.MustCompile(`^- ([^:：\n` + "`" + `]+?)(?:：|:(?:` + ws + `|$))[ \t]*(.*?)` + ws + `*$`)
	openNestedRE = regexp.MustCompile(`^` + ws + `+- (.*?)` + ws + `*$`)
	openTopRE    = regexp.MustCompile(`^- (.*?)` + ws + `*$`)
)

// parseOpenSection は「未決事項」の節を、項目名と、その下の未決事項に分ける。
//
//   - 項目名: 値        → 項目名の下に値が1つ
//   - 項目名:           → 項目名の下に、続く字下げした行が並ぶ
//   - 値
//   - 値（項目名なし）   → 項目名なしの値
func parseOpenSection(lines []string) []openItem {
	var out []openItem
	for _, l := range lines {
		if m := openLabelRE.FindStringSubmatch(l); m != nil {
			item := openItem{label: strings.TrimSpace(m[1])}
			if m[2] != "" {
				item.values = []string{m[2]}
			}
			out = append(out, item)
		} else if m := openNestedRE.FindStringSubmatch(l); m != nil {
			if len(out) == 0 {
				out = append(out, openItem{})
			}
			out[len(out)-1].values = append(out[len(out)-1].values, m[1])
		} else if m := openTopRE.FindStringSubmatch(l); m != nil {
			out = append(out, openItem{values: []string{m[1]}})
		}
	}
	for _, t := range tableRows(lines) {
		for _, r := range t.body {
			out = append(out, openItem{values: []string{cell(r, 0)}})
		}
	}
	return out
}

func statFile(path, relPath string, ids statusSet) fileStat {
	st := fileStat{path: relPath, statuses: map[string]int{}, special: map[string]map[string]int{}}
	lines := withoutCode(read(path))
	sets := loadStatusSets()
	special := statusSetAt(relPath, lines)

	countStatus := func(value, where string) {
		switch {
		case value == "":
			// 方針で、空欄は表の最初の値（DS01 未定義）と同じ扱いとしている
			if len(ids.ids) > 0 {
				st.statuses[ids.ids[0]]++
			}
		case ids.has(value):
			st.statuses[value]++
		default:
			st.invalid = append(st.invalid, invalidStatus{where, value, dsSet})
		}
	}

	heading := ""
	for i, line := range lines {
		n := i + 1
		if h, ok := matchHeading(line); ok {
			heading = normHeading(h.text)
		}
		m := itemRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, value := strings.TrimSpace(m[1]), m[2]
		name, isSpecial := special[n]
		switch {
		case key == "ステータス" && isSpecial:
			if name == "" {
				continue
			}
			if st.special[name] == nil {
				st.special[name] = map[string]int{}
			}
			if value != "" && !sets[name].has(value) {
				st.invalid = append(st.invalid, invalidStatus{strconv.Itoa(n), value, name})
			} else {
				st.special[name][value]++
			}
		case key == "ステータス":
			countStatus(value, strconv.Itoa(n))
		case key == "未決事項":
			// エントリの中の未決事項は、そのエントリの見出しを項目名にする
			if value != "" {
				st.open = append(st.open, openItem{heading, []string{value}})
			}
		default:
			st.items++
			if value != "" {
				st.filled++
			}
		}
	}

	for _, t := range tableRows(lines) {
		if k := t.col("ステータス"); k >= 0 {
			for _, row := range t.body {
				countStatus(cell(row, k), "表")
			}
		}
	}

	st.open = append(st.open, parseOpenSection(section(lines, "未決事項"))...)
	return st
}

// statAspects は stat で表示する観点。
type statAspects struct{ status, fill, open bool }

func rightFrom(n int) []bool {
	right := make([]bool, n)
	for i := 1; i < n; i++ {
		right[i] = true
	}
	return right
}

// renderItemTable は、項目の記入と未決事項の件数の表を返す。単位は項目。
func renderItemTable(stats []fileStat, a statAspects) string {
	head := []string{"文書"}
	if a.fill {
		head = append(head, "記入", "空欄")
	}
	if a.open {
		head = append(head, "未決")
	}
	row := func(path string, filled, items, open int) []string {
		r := []string{path}
		if a.fill {
			r = append(r, fmt.Sprintf("%d/%d", filled, items), strconv.Itoa(items-filled))
		}
		if a.open {
			r = append(r, strconv.Itoa(open))
		}
		return r
	}
	var rows [][]string
	filled, items, open := 0, 0, 0
	for _, s := range stats {
		rows = append(rows, row(s.path, s.filled, s.items, s.openCount()))
		filled += s.filled
		items += s.items
		open += s.openCount()
	}
	return ui.Table{
		Headers: head, Rows: rows, Footer: row("合計", filled, items, open), Right: rightFrom(len(head)),
		Tone: func(col int, value string) ui.Tone { return cellTone(head[col], value) },
	}.String()
}

// renderStatusTable は、判断のステータスの件数の表を返す。単位はエントリ。
// 見出しは2行で、ステータスの列は ID を、ステータスでない列（エントリの総数、不正）は - を添える。
func renderStatusTable(stats []fileStat, ids statusSet) string {
	head := []string{"文書", "エントリ\n-"}
	for _, id := range ids.ids {
		head = append(head, ids.labels[id]+"\n（"+id+"）")
	}
	head = append(head, "不正\n-")
	row := func(path string, statuses map[string]int, invalid int) []string {
		entries := invalid
		for _, n := range statuses {
			entries += n
		}
		r := []string{path, strconv.Itoa(entries)}
		for _, id := range ids.ids {
			r = append(r, strconv.Itoa(statuses[id]))
		}
		return append(r, strconv.Itoa(invalid))
	}
	var rows [][]string
	total, invalid := map[string]int{}, 0
	for _, s := range stats {
		rows = append(rows, row(s.path, s.statuses, len(s.invalid)))
		for k, v := range s.statuses {
			total[k] += v
		}
		invalid += len(s.invalid)
	}
	last := len(head) - 1
	return ui.Table{
		Headers: head, Rows: rows, Footer: row("合計", total, invalid), Right: rightFrom(len(head)),
		Tone: func(col int, value string) ui.Tone {
			switch {
			case col <= 1:
				return ui.Plain
			case value == "0":
				return ui.Muted
			case col == last:
				return ui.Error
			}
			return ui.Plain
		},
	}.String()
}

// cellTone は、記入は全部なら OK・一部なら警告・なしなら薄く、空欄は警告、0 は薄く表示する。
func cellTone(col, value string) ui.Tone {
	switch {
	case col == "文書":
		return ui.Plain
	case col == "記入":
		done, total, _ := strings.Cut(value, "/")
		d, _ := strconv.Atoi(done)
		t, _ := strconv.Atoi(total)
		switch {
		case t > 0 && d == t:
			return ui.OK
		case d == 0:
			return ui.Muted
		default:
			return ui.Warn
		}
	case value == "0":
		return ui.Muted
	case col == "空欄":
		return ui.Warn
	}
	return ui.Plain
}

// setLevels は、ステータスの表ごとに、方針での扱いを状態の印に対応させる。
type setLevels struct {
	words  []roleWord
	levels map[role]ui.Level
	other  ui.Level // 扱いが決まっていない値
}

var levelsBySet = map[string]setLevels{
	sourceSet: {sourceRoles, map[role]ui.Level{"done": ui.LevelOK, "later": ui.LevelInfo, "unneeded": ui.LevelInfo}, ui.LevelUndefined},
	"品質特性の採否": {qualityRoles, map[role]ui.Level{
		"adopted": ui.LevelOK, "planned": ui.LevelInfo, "later": ui.LevelInfo, "excluded": ui.LevelInfo}, ui.LevelUndefined},
	"確認手段の状況": {checkRoles, map[role]ui.Level{"done": ui.LevelOK, "later": ui.LevelInfo}, ui.LevelWarn},
	// 仮説の扱いは、方針に検査での扱いが無いため区別しない
	"仮説の扱い": {nil, nil, ui.LevelInfo},
}

// renderBreakdown は、判断のステータス以外の表ごとに、状態の件数を並べる。
func renderBreakdown(pj string, stats []fileStat) []ui.Node {
	sets := loadStatusSets()
	type row struct {
		name, where string
		values      []string
	}
	var readmeValues []string
	for _, e := range sourceEntries(filepath.Join(pj, "README.md")) {
		readmeValues = append(readmeValues, e.values["ステータス"])
	}
	rows := []row{{sourceSet, "README の編集元と参照先", readmeValues}}
	for _, s := range specialSets {
		var values []string
		for _, st := range stats {
			for k, n := range st.special[s.name] {
				for range n {
					values = append(values, k)
				}
			}
		}
		where := s.key.section
		if s.key.nested {
			where += "の確認手段"
		}
		rows = append(rows, row{s.name, where, values})
	}

	var nodes []ui.Node
	for _, r := range rows {
		cl := levelsBySet[r.name]
		var roles map[string]role
		if cl.words != nil {
			roles = loadRoles(r.name, cl.words)
		}
		var items []ui.CountItem
		for _, id := range sets[r.name].ids {
			level, ok := cl.levels[roles[id]]
			if !ok {
				level = cl.other
			}
			items = append(items, ui.CountItem{Level: level, Label: sets[r.name].labels[id], N: countOf(r.values, id)})
		}
		items = append(items, ui.CountItem{Level: ui.LevelWarn, Label: "空欄", N: countOf(r.values, "")})
		label := r.name + ui.Paint(ui.Muted, "（"+r.where+"）")
		nodes = append(nodes, ui.Node{Text: label, Children: []ui.Node{{Text: ui.Counts(items)}}})
	}
	return nodes
}

func countOf(values []string, v string) int {
	n := 0
	for _, x := range values {
		if x == v {
			n++
		}
	}
	return n
}

// total は、全文書を通した判断のステータス id の件数。
func total(stats []fileStat, id string) int {
	n := 0
	for _, s := range stats {
		n += s.statuses[id]
	}
	return n
}

// openNodes は未決事項を、項目名の下に値を並べる形にする。項目名の無い値はそのまま並べる。
func openNodes(items []openItem) []ui.Node {
	var nodes []ui.Node
	for _, o := range items {
		var values []ui.Node
		for _, v := range o.values {
			values = append(values, ui.Node{Text: ui.Mark(ui.LevelInfo) + " " + v})
		}
		switch {
		case len(values) == 0:
		case o.label == "":
			nodes = append(nodes, values...)
		case len(nodes) > 0 && nodes[len(nodes)-1].Text == o.label:
			// 同じ項目名が続けば、1つの項目名の下にまとめる
			nodes[len(nodes)-1].Children = append(nodes[len(nodes)-1].Children, values...)
		default:
			nodes = append(nodes, ui.Node{Text: o.label, Children: values})
		}
	}
	return nodes
}

// byFile は、ファイルごとにまとめた一覧の節を作る。
func byFile(files []string, items map[string][]string) []ui.Node {
	var nodes []ui.Node
	for _, f := range files {
		if len(items[f]) == 0 {
			continue
		}
		n := ui.Node{Text: ui.Group(f, len(items[f]))}
		for _, it := range items[f] {
			n.Children = append(n.Children, ui.Node{Text: it})
		}
		nodes = append(nodes, n)
	}
	return nodes
}

func cmdStat(ids statusSet, project string, a statAspects, showAll bool) int {
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
	var stats []fileStat
	var files []string
	for _, d := range targetDirs {
		for _, f := range mdFilesUnder(filepath.Join(pj, d)) {
			s := statFile(f, rel(pj, f), ids)
			stats = append(stats, s)
			files = append(files, s.path)
		}
	}
	blocks := []string{ui.Title("pdocs stat", name)}
	// 未決事項だけを見るときは、件数は未決事項の節に出るので表を出さない
	if a.fill {
		blocks = append(blocks, ui.Section("項目の記入", ui.Paint(ui.Muted, "項目ごと"), renderItemTable(stats, a)))
	}
	if a.status {
		blocks = append(blocks, ui.Section("判断のステータス", ui.Paint(ui.Muted, "エントリごと"), renderStatusTable(stats, ids)))
	}
	var footer []string

	if a.fill {
		filled, items := 0, 0
		for _, s := range stats {
			filled += s.filled
			items += s.items
		}
		footer = append(footer, fmt.Sprintf("記入 %d/%d", filled, items))
	}
	if a.open {
		var nodes []ui.Node
		n := 0
		for _, s := range stats {
			if s.openCount() == 0 {
				continue
			}
			n += s.openCount()
			nodes = append(nodes, ui.Node{Text: ui.Group(s.path, s.openCount()), Children: openNodes(s.open)})
		}
		body := ui.None()
		if n > 0 {
			body = ui.Tree(nodes...)
		}
		blocks = append(blocks, ui.Section("未決事項", ui.Count(n), body))
		footer = append(footer, fmt.Sprintf("未決 %d", n))
	}
	if a.status {
		entries := 0
		for _, st := range stats {
			for _, v := range st.statuses {
				entries += v
			}
		}
		footer = append(footer, fmt.Sprintf("判断 %d 件（%s %d）", entries, ids.labels[ids.ids[0]], total(stats, ids.ids[0])))
		blocks = append(blocks, ui.Section("判断以外のステータス", "", ui.Tree(renderBreakdown(pj, stats)...)))
		invalid, n := map[string][]string{}, 0
		for _, s := range stats {
			for _, inv := range s.invalid {
				where := inv.where
				if where != "表" {
					where = "L" + where
				}
				invalid[s.path] = append(invalid[s.path], ui.Item(ui.LevelError, ui.Paint(ui.Muted, where)+"  "+inv.value, ""))
				n++
			}
		}
		if n > 0 {
			blocks = append(blocks, ui.Section("不正なステータス値", ui.Paint(ui.Error, fmt.Sprint(n)), ui.Tree(byFile(files, invalid)...)))
			footer = append(footer, ui.MarkText(ui.LevelError, fmt.Sprintf("不正 %d", n)))
		}
	}
	if showAll {
		adrs, summary := renderADRs(pj)
		blocks = append(blocks, adrs)
		found := runChecks(ids, defaultChecks)
		e, w, u := tally(filterFindings(found, func(f finding) bool { return f.project == name }))
		check := "検査 " + ui.Tally(e, w, u)
		if e+w+u > 0 {
			check += ui.Paint(ui.Muted, "（pdocs check で詳細）")
		}
		footer = append(footer, check)
		if summary != "" {
			footer = append(footer, summary)
		}
	}
	blocks = append(blocks, ui.Footer(footer...))
	ui.Println(ui.Blocks(blocks...))
	return 0
}

var adrLineRE = regexp.MustCompile(`^\s*(\d+)\s+(\S+)\s+(\d{4}-\d{2}-\d{2})\s+(.*?)\s*$`)

// adrLevel は ADR の状態の印。
func adrLevel(status string) ui.Level {
	switch strings.ToLower(status) {
	case "accepted":
		return ui.LevelOK
	case "proposed":
		return ui.LevelWarn
	case "deprecated", "superseded", "rejected":
		return ui.LevelInfo
	}
	return ui.LevelUndefined
}

// renderADRs は ADR の節と、まとめに添える1行を返す。
// adrs list -l の出力を番号・状態・日付・題名に読み分ける。読み分けられない行があれば、出力をそのまま見せる。
func renderADRs(pj string) (section, summary string) {
	if !exists(filepath.Join(pj, "adrs.toml")) {
		return ui.Section("ADR", "", ui.Tree(ui.Node{Text: ui.Paint(ui.Muted, "adrs.toml なし")})), ""
	}
	if _, err := exec.LookPath("adrs"); err != nil {
		return ui.Section("ADR", "", ui.Tree(ui.Node{Text: ui.Paint(ui.Muted, "adrs が見つかりません")})), ""
	}
	cmd := exec.Command("adrs", "-C", pj, "list", "-l")
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	out := strings.TrimRight(stdout.String(), " \t\r\n")

	var rows [][]string
	var levels []ui.Level
	parsed := err == nil
	for l := range strings.Lines(out) {
		m := adrLineRE.FindStringSubmatch(strings.TrimRight(l, "\r\n"))
		if m == nil {
			parsed = false
			break
		}
		lv := adrLevel(m[2])
		rows = append(rows, []string{m[1], ui.Mark(lv) + " " + m[2], m[3], m[4]})
		levels = append(levels, lv)
	}
	if !parsed {
		// 失敗しても、出力された内容（エラーの説明）をそのまま見せる
		raw := out
		if raw == "" {
			raw = strings.TrimRight(stderr.String(), " \t\r\n")
		}
		var nodes []ui.Node
		for l := range strings.Lines(raw) {
			nodes = append(nodes, ui.Node{Text: strings.TrimRight(l, "\r\n")})
		}
		return ui.Section("ADR", "", ui.Tree(nodes...)), ""
	}
	if len(rows) == 0 {
		return ui.Section("ADR", ui.Count(0), ui.None()), "ADR 0 件"
	}
	cols := ui.Columns{
		Rows:  rows,
		Right: []bool{true},
		Tone: func(row, col int) ui.Tone {
			if col == 2 {
				return ui.Muted
			}
			return ui.Plain
		},
	}
	summary = fmt.Sprintf("ADR %d 件", len(rows))
	if proposed := countOfLevel(levels, ui.LevelWarn); proposed > 0 {
		summary += ui.Paint(ui.Warn, fmt.Sprintf("（Proposed %d）", proposed))
	}
	return ui.Section("ADR", ui.Count(len(rows)), cols.String()), summary
}

func countOfLevel(levels []ui.Level, l ui.Level) int {
	n := 0
	for _, x := range levels {
		if x == l {
			n++
		}
	}
	return n
}
