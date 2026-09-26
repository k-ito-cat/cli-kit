package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// ws は Python の \s と同じく、全角の空白なども含む空白。Go の \s は ASCII の空白だけを指す
const ws = `[\t\n\v\f\r\x1c-\x1f\x85\p{Z}]`

var (
	// Go の正規表現は先読みを持たないため、`:` の後の空白は消費して扱う（項目の値には含まれない）
	itemRE    = regexp.MustCompile(`^` + ws + `*- ([^:：\n` + "`" + `]+?)(?:：|:(?:` + ws + `|$))[ \t]*(.*?)` + ws + `*$`)
	linkRE    = regexp.MustCompile(`\[[^\]]*\]\(([^)\t\n\v\f\r\x1c-\x1f\x85\p{Z}]+)\)`)
	headingRE = regexp.MustCompile(`^(#{1,6})` + ws + `+(.*?)` + ws + `*$`)
	schemeRE  = regexp.MustCompile(`^[a-z]+:`)
	ruleRowRE = regexp.MustCompile(`^[|\t\n\v\f\r\x1c-\x1f\x85\p{Z}:-]+$`)
	spacesRE  = regexp.MustCompile(ws + `+`)
	stubRE    = regexp.MustCompile(`^[A-Z]{3}-000(?:` + ws + `|$)`)
)

// read はファイルを行に分けて返す。読めなければ終了する。
func read(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		fail(err.Error())
	}
	return strings.Split(string(b), "\n")
}

type heading struct {
	level int
	text  string
}

func matchHeading(line string) (heading, bool) {
	m := headingRE.FindStringSubmatch(line)
	if m == nil {
		return heading{}, false
	}
	return heading{len(m[1]), m[2]}, true
}

func normHeading(text string) string {
	return strings.TrimSpace(spacesRE.ReplaceAllString(strings.ReplaceAll(text, "`", ""), " "))
}

// section は見出し title の節の本文を、次の同階層以上の見出しまで返す。
func section(lines []string, title string) []string {
	var out []string
	level := 0
	for _, line := range lines {
		h, ok := matchHeading(line)
		if ok && level > 0 && h.level <= level {
			break
		}
		if level > 0 {
			out = append(out, line)
		} else if ok && h.text == title {
			level = h.level
		}
	}
	return out
}

type mdTable struct {
	head []string
	body [][]string
}

func (t mdTable) col(name string) int {
	return slices.Index(t.head, name)
}

func (t mdTable) has(names ...string) bool {
	for _, n := range names {
		if !slices.Contains(t.head, n) {
			return false
		}
	}
	return true
}

func cells(line string) []string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}

// cell は行の i 列目を返す。列が足りなければ空文字。
func cell(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return row[i]
}

// tableRows は Markdown の表を（見出し, 本文行）の組で返す。
func tableRows(lines []string) []mdTable {
	var tables []mdTable
	for i := 0; i < len(lines); {
		next := "x"
		if i+1 < len(lines) && lines[i+1] != "" {
			next = lines[i+1]
		}
		if strings.HasPrefix(lines[i], "|") && i+1 < len(lines) && ruleRowRE.MatchString(next) {
			t := mdTable{head: cells(lines[i])}
			for i += 2; i < len(lines) && strings.HasPrefix(lines[i], "|"); i++ {
				t.body = append(t.body, cells(lines[i]))
			}
			tables = append(tables, t)
		} else {
			i++
		}
	}
	return tables
}

func withoutCode(lines []string) []string {
	out := make([]string, len(lines))
	fenced := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimLeftFunc(line, isSpaceRune), "```") {
			fenced = !fenced
			continue // 空行にして、行番号を元のファイルと揃える
		}
		if !fenced {
			out[i] = line
		}
	}
	return out
}

func links(lines []string) []string {
	var out []string
	for _, l := range lines {
		for _, m := range linkRE.FindAllStringSubmatch(l, -1) {
			if !schemeRE.MatchString(m[1]) {
				out = append(out, m[1])
			}
		}
	}
	return out
}

// slug は見出しから GitHub のアンカーを作る。
func slug(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		switch {
		case isWordRune(r) || r == '-':
			b.WriteRune(r)
		case isSpaceRune(r):
			b.WriteRune('-')
		}
	}
	return b.String()
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' }

// isSpaceRune は Python の str.isspace と同じ範囲の空白か。
func isSpaceRune(r rune) bool { return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f) }

type numberedHeading struct {
	line int
	heading
}

// headings は（行番号, 深さ, 見出し）を返す。コードブロック内は除く。
func headings(path string) []numberedHeading {
	var out []numberedHeading
	for i, l := range withoutCode(read(path)) {
		if h, ok := matchHeading(l); ok {
			out = append(out, numberedHeading{i + 1, heading{h.level, normHeading(h.text)}})
		}
	}
	return out
}

// sectionOf は行番号（1 始まり）ごとに、その行が属する見出しを返す。
func sectionOf(path string) []string {
	out := []string{""}
	cur := ""
	for _, l := range withoutCode(read(path)) {
		if h, ok := matchHeading(l); ok {
			cur = normHeading(h.text)
		}
		out = append(out, cur)
	}
	return out
}

// ---- ファイルの列挙 --------------------------------------------------------

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// sortPaths は Python の pathlib と同じく、パスを区切りごとの部品で比べて並べる。
func sortPaths(paths []string) []string {
	slices.SortFunc(paths, func(a, b string) int {
		return slices.Compare(strings.Split(a, "/"), strings.Split(b, "/"))
	})
	return paths
}

// mdFilesUnder は dir 以下（入れ子を含む）の .md を並べて返す。
func mdFilesUnder(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
			out = append(out, p)
		}
		return nil
	})
	return sortPaths(out)
}

// mdFilesIn は dir 直下の .md を並べて返す。
func mdFilesIn(dir string) []string {
	out, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	return sortPaths(out)
}

func rel(base, path string) string {
	r, err := filepath.Rel(base, path)
	if err != nil {
		return path
	}
	return r
}

// resolve は Python の Path.resolve と同じく、辿れる所までシンボリックリンクを辿った絶対パスを返す。
func resolve(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

// uniq は順番を保ったまま重複を除く。
func uniq(items []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range items {
		if !seen[it] {
			seen[it] = true
			out = append(out, it)
		}
	}
	return out
}
