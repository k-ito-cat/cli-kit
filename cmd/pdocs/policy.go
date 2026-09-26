package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// statusSet はステータスの表1つ分。IDs は方針に書かれた順。
type statusSet struct {
	ids    []string
	labels map[string]string
}

func (s statusSet) has(id string) bool {
	_, ok := s.labels[id]
	return ok
}

const (
	dsSet     = "判断のステータス"
	sourceSet = "成果物の状況"
)

// specialKey は、判断のステータス以外の表を使う場所（文書, 節, 確認手段の入れ子か）。
type specialKey struct {
	rel, section string
	nested       bool
}

// specialSets は判断のステータス以外の表を使う場所と、その表の名前。並びは stat の表示順。
var specialSets = []struct {
	key  specialKey
	name string
}{
	{specialKey{"spec/requirements/non-functional.md", "品質特性の水準", false}, "品質特性の採否"},
	{specialKey{"spec/requirements/non-functional.md", "品質特性の水準", true}, "確認手段の状況"},
	{specialKey{"spec/product.md", "利用者の仮説", false}, "仮説の扱い"},
}

func specialSetName(k specialKey) (string, bool) {
	for _, s := range specialSets {
		if s.key == k {
			return s.name, true
		}
	}
	return "", false
}

// loadStatusSets は documentation-policy.md の「ステータス」の小見出しごとの表を読む。
var loadStatusSets = sync.OnceValue(func() map[string]statusSet {
	sets := map[string]statusSet{}
	cur := ""
	var buf []string
	flush := func() {
		if cur == "" {
			return
		}
		for _, t := range tableRows(buf) {
			if !t.has("ID", "ラベル") {
				continue
			}
			i, j := t.col("ID"), t.col("ラベル")
			s := statusSet{labels: map[string]string{}}
			for _, row := range t.body {
				if len(row) > max(i, j) {
					if !s.has(row[i]) {
						s.ids = append(s.ids, row[i])
					}
					s.labels[row[i]] = row[j]
				}
			}
			sets[cur] = s
			return
		}
	}
	for _, line := range section(read(policy), "ステータス") {
		if h, ok := matchHeading(line); ok {
			flush()
			cur, buf = normHeading(h.text), nil
		} else {
			buf = append(buf, line)
		}
	}
	flush()

	var missing []string
	for _, name := range []string{dsSet, sourceSet} {
		if _, ok := sets[name]; !ok {
			missing = append(missing, name)
		}
	}
	for _, s := range specialSets {
		if _, ok := sets[s.name]; !ok && !slices.Contains(missing, s.name) {
			missing = append(missing, s.name)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		fail(fmt.Sprintf("%s のステータスの節に表が見つかりません: %s", policy, strings.Join(missing, "、")))
	}
	return sets
})

func loadStatuses() statusSet {
	return loadStatusSets()[dsSet]
}

// ---- 見出しの構成と繰り返すエントリ ------------------------------------------

const (
	rootParent = "文書直下"
	anyH2      = "各##節"
)

// placeKind は、繰り返すエントリを置く節の種類。
type placeKind int

const (
	placeRoot    placeKind = iota // 文書直下
	placeAnyH2                    // 各##節
	placeHeading                  // 特定の見出しの下
	placeOther                    // 解釈できない指定。どの見出しにも当てはまらない
)

type place struct {
	kind placeKind
	heading
}

type entryRule struct {
	heading // 繰り返す見出しの深さと文言
	parent  place
}

var (
	stubHeadingRE = regexp.MustCompile(`^(#+)` + ws + `+(.*)`)
	placeRE       = regexp.MustCompile(`^(#+)(.*)`)
)

// loadEntryRules は、方針の「見出しの構成と繰り返すエントリ」表から、雛形ごとの繰り返す見出しと置く節を読む。
func loadEntryRules() map[string][]entryRule {
	rules := map[string][]entryRule{}
	for _, t := range tableRows(section(read(policy), "見出しの構成と繰り返すエントリ")) {
		if !t.has("雛形", "繰り返す見出し", "置く節") {
			continue
		}
		i, j, k := t.col("雛形"), t.col("繰り返す見出し"), t.col("置く節")
		for _, row := range t.body {
			tmpl := strings.Trim(cell(row, i), "` ")
			stub := stubHeadingRE.FindStringSubmatch(strings.Trim(cell(row, j), "` "))
			if stub == nil {
				continue
			}
			p := strings.ReplaceAll(strings.ReplaceAll(cell(row, k), "`", ""), " ", "")
			var parent place
			switch {
			case p == rootParent:
				parent = place{kind: placeRoot}
			case p == anyH2:
				parent = place{kind: placeAnyH2}
			default:
				if pm := placeRE.FindStringSubmatch(p); pm != nil {
					parent = place{placeHeading, heading{len(pm[1]), normHeading(pm[2])}}
				} else {
					parent = place{kind: placeOther}
				}
			}
			rules[tmpl] = append(rules[tmpl], entryRule{heading{len(stub[1]), normHeading(stub[2])}, parent})
		}
	}
	if len(rules) == 0 {
		fail(fmt.Sprintf("%s に見出しの構成の表が見つかりません", policy))
	}
	return rules
}

type idRule struct {
	tmpl   string
	level  int
	place  string
	prefix string
	shared bool // 他のエントリと同じ ID を使う（収録状況など）
}

var (
	stubLevelRE = regexp.MustCompile(`^(#+)` + ws)
	prefixRE    = regexp.MustCompile("^`([A-Z]{3})`")
)

// loadIDRules は、方針の「見出しの構成と繰り返すエントリ」表の ID 列から、接頭辞を持つエントリを読む。
func loadIDRules() []idRule {
	var rules []idRule
	for _, t := range tableRows(section(read(policy), "見出しの構成と繰り返すエントリ")) {
		if !t.has("雛形", "繰り返す見出し", "置く節", "ID") {
			continue
		}
		i, j, k, x := t.col("雛形"), t.col("繰り返す見出し"), t.col("置く節"), t.col("ID")
		for _, row := range t.body {
			stub := stubLevelRE.FindStringSubmatch(strings.Trim(cell(row, j), "` "))
			prefix := prefixRE.FindStringSubmatch(strings.TrimSpace(cell(row, x)))
			if stub != nil && prefix != nil {
				rules = append(rules, idRule{
					tmpl:   strings.Trim(cell(row, i), "` "),
					level:  len(stub[1]),
					place:  strings.TrimSpace(strings.ReplaceAll(cell(row, k), "`", "")),
					prefix: prefix[1],
					shared: strings.Contains(cell(row, x), "同じ ID"),
				})
			}
		}
	}
	return rules
}

// loadSourceTargets は、方針の「文章以外で定義するもの」の表から、成果物の有無を記す対象を読む。
func loadSourceTargets() []string {
	for _, t := range tableRows(section(read(policy), "文章以外で定義するもの")) {
		if t.has("対象", "正本の候補") {
			var out []string
			for _, r := range t.body {
				out = append(out, cell(r, t.col("対象")))
			}
			return out
		}
	}
	fail(fmt.Sprintf("%s の「文章以外で定義するもの」に対象の表が見つかりません", policy))
	return nil
}

// loadQualityIDs は、方針の「品質特性の選び方」の表から、品質特性の ID を読む。
func loadQualityIDs() map[string]bool {
	for _, t := range tableRows(section(read(policy), "品質特性の選び方")) {
		if t.has("ID", "品質特性") {
			out := map[string]bool{}
			for _, r := range t.body {
				out[cell(r, t.col("ID"))] = true
			}
			return out
		}
	}
	fail(fmt.Sprintf("%s の「品質特性の選び方」に ID のある表が見つかりません", policy))
	return nil
}

// role はステータスの、検査での扱い。
type role string

type roleWord struct {
	label string
	role  role
}

// loadRoles は、ステータスの表 name のラベルから、検査での扱いを {ID: 役割} で読む。
func loadRoles(name string, words []roleWord) map[string]role {
	s := loadStatusSets()[name]
	roles := map[string]role{}
	for _, id := range s.ids {
		for _, w := range words {
			if s.labels[id] == w.label {
				roles[id] = w.role
			}
		}
	}
	for _, w := range words {
		found := false
		for _, r := range roles {
			found = found || r == w.role
		}
		if !found {
			labels := make([]string, len(words))
			for i, w := range words {
				labels[i] = w.label
			}
			fail(fmt.Sprintf("%s の「%s」に、%s のラベルが見つかりません", policy, name, strings.Join(labels, "・")))
		}
	}
	return roles
}
