package recap

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"cli-kit/internal/ui"
)

// summaryPrompt は、要約を頼む内容。目的ごとに抽象化した見出しと、機能の単位での具体的な変更を書かせる。
// 対象の範囲を明示し、渡したコミットだけを材料にさせる。出力の形を決めておき、CLI の側で色と配置を付ける。
const summaryPrompt = `以下は、%[1]s のうち、まだ要約していない私の git のコミット（%[2]d 件）です。
リポジトリごとに並べています。括弧内は変わったディレクトリ、字下げした行はコミットの本文です。%[3]s
何をしていたのかを、日本語でまとめてください。

- 以下のコミットだけを材料にする。ほかの期間の作業や、ツールやコマンドで調べた情報は書かない
- リポジトリごとに、目的のまとまりを1〜3項目にする。複数のリポジトリにまたがる作業は、それぞれのリポジトリでの役割として書く
- 見出しは「何のための作業だったか」を20字程度で書く（例: 「ブログのレイアウト改善」）
- 補足は、見出しを具体的にする例を読点で2〜4個並べる
- 具体は、機能の単位で「何をどう変えたか」を1行ずつ2〜4行書く。ファイル名や関数名の単位までは下げない
- 次の形だけで出力する。前置き、結び、強調の記号は書かない

全体: <この範囲の作業を一言で表す1文>
## <リポジトリ名>
<見出し> ｜ <補足>
- <具体>
- <具体>
## <リポジトリ名>
<見出し> ｜ <補足>
- <具体>

%[4]s`

// deepNote は、--deep のときに頼む内容に加える説明。
const deepNote = `
各コミットの下の diff は、そのコミットの差分の一部です（大きなものは省略しています）。具体を書く根拠にしてください。`

func summarize(cfg config, p period, sp span, changes []change, deep, force bool) error {
	command := "recap " + p.name + " --summary"
	if deep {
		command += " --deep"
	}
	if force {
		command += " --force"
	}
	title := ui.Title(command, sp.String())
	fresh := changes
	if !force {
		covered, err := loadCovered(cfg.LogDir)
		if err != nil {
			return err
		}
		fresh = uncovered(changes, covered)
	}
	m := material(fresh, deep, cfg.Deep)
	if m.commits == 0 {
		// 差分が無ければ AI は実行しない
		ui.Println(ui.Blocks(title, ui.Footer(
			"新しいコミットはありません（この範囲は要約済み）",
			ui.Paint(ui.Muted, "記録は recap log で開く。--force でもう一度要約する"))))
		return nil
	}
	if cfg.Summarize.Command == "" {
		return fmt.Errorf("%s に [summarize] の command がない（例: command = \"claude -p --output-format json --tools ''\"）", configPath())
	}

	note := ""
	if deep {
		note = deepNote
	}
	prompt := fmt.Sprintf(summaryPrompt, sp.String(), m.commits, note, m.text)
	target := "まだ要約していないコミット"
	if force {
		target = "コミット（要約済みの範囲も含む）"
	}
	fmt.Fprintf(os.Stderr, "要約しています（%s %d 件、頼む内容 約 %s 文字）…\n", target, m.commits, thousands(len([]rune(prompt))))
	cmd := exec.Command("sh", "-c", cfg.Summarize.Command)
	// 今いるプロジェクトの設定や hook を読ませないよう、一時ディレクトリで実行する
	cmd.Dir = os.TempDir()
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stderr = os.Stderr
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("[summarize] の「%s」に失敗: %w", cfg.Summarize.Command, err)
	}
	text, usage := parseSummary(out.Bytes())
	s := parseSections(text, m)
	if len(s.repos) == 0 {
		return errors.New("要約が空だった")
	}

	footer := []string{fmt.Sprintf("%s %d 件をもとに AI がまとめたもの", target, m.commits)}
	blocks := []string{title}
	blocks = append(blocks, s.render()...)
	if usage != nil {
		blocks = append(blocks, ui.Section("使ったモデルとトークン", "", usage.render()))
		footer = append(footer, ui.Paint(ui.Muted, fmt.Sprintf("推定 $%.4f（手元の見積もりで、請求とずれることがある）", usage.TotalCostUSD)))
	}
	logFile, err := appendLog(cfg.LogDir, command, sp, m.commits, s, usage)
	if err != nil {
		return err
	}
	footer = append(footer, ui.Paint(ui.Muted, "記録: "+logFile))
	blocks = append(blocks, ui.Footer(footer...))
	ui.Println(ui.Blocks(blocks...))
	return nil
}

// ---- 要約済みの範囲 ------------------------------------------------------------

// 要約済みの範囲は、記録（*.md）の各項目に埋め込んだ印から読む。記録を消せば、その範囲はまた要約できる。
// 印は Markdown の表示では見えないコメントにする。
var coveredMarkRE = regexp.MustCompile(`<!-- recap: (\d+) (\d+) -->`)

func coveredMark(sp span) string {
	return fmt.Sprintf("<!-- recap: %d %d -->", sp.from.Unix(), sp.to.Unix())
}

// interval は要約済みの範囲 [From, To]（Unix 秒）。コミットの時刻（committer date）で判定する。
type interval struct{ From, To int64 }

func loadCovered(logDir string) ([]interval, error) {
	files, err := filepath.Glob(filepath.Join(logDir, "*.md"))
	if err != nil {
		return nil, err
	}
	var r []interval
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		for _, m := range coveredMarkRE.FindAllStringSubmatch(string(b), -1) {
			from, _ := strconv.ParseInt(m[1], 10, 64)
			to, _ := strconv.ParseInt(m[2], 10, 64)
			r = append(r, interval{from, to})
		}
	}
	return r, nil
}

// uncovered は、要約済みの範囲に入っていないコミット（と、その変わったファイル）だけを返す。
func uncovered(changes []change, covered []interval) []change {
	var out []change
	keep := false
	for _, c := range changes {
		if c.file == wholeCommit {
			t := c.at.Unix()
			keep = !slices.ContainsFunc(covered, func(iv interval) bool { return iv.From <= t && t <= iv.To })
		}
		if keep {
			out = append(out, c)
		}
	}
	return out
}

// ---- 材料 ----------------------------------------------------------------------

// summaryMaterial は、要約の材料と、CLI が数えたリポジトリごとのコミットの数。
type summaryMaterial struct {
	text    string         // AI に渡す材料
	commits int            // コミットの総数
	counts  map[string]int // リポジトリ名 → コミットの数
	order   []string       // リポジトリ名。コミットの多い順
}

// material は、要約の材料（リポジトリごとの件名、本文の最初の数行、変わったディレクトリ）を作る。
// deep なら、各コミットの差分も上限の範囲で加える。
func material(changes []change, deep bool, dc deepConfig) summaryMaterial {
	m := summaryMaterial{counts: map[string]int{}}
	var b strings.Builder
	repo := ""
	var dirs []string
	var pending *change // 変わったディレクトリを書き終えてから、本文と差分を書くコミット
	budget := cmp.Or(dc.MaxLinesTotal, defaultMaxLinesTotal)
	flush := func() {
		if pending == nil {
			return
		}
		if len(dirs) > 0 {
			fmt.Fprintf(&b, "（%s）", strings.Join(dirs, "、"))
		}
		b.WriteString("\n")
		for i, l := range strings.Split(pending.body, "\n") {
			if i >= bodyLines || strings.TrimSpace(l) == "" {
				continue
			}
			fmt.Fprintf(&b, "    %s\n", strings.TrimSpace(l))
		}
		if deep && budget > 0 {
			d, used := commitDiff(*pending, dc, budget)
			budget -= used
			if d != "" {
				fmt.Fprintf(&b, "```diff\n%s\n```\n", d)
			}
		}
		dirs, pending = nil, nil
	}
	for _, c := range changes {
		if c.file == wholeCommit {
			flush()
			name := filepath.Base(c.repo)
			if c.repo != repo {
				repo = c.repo
				m.order = append(m.order, name)
				fmt.Fprintf(&b, "\n## %s\n", name)
			}
			fmt.Fprintf(&b, "- %s", c.subject)
			m.commits++
			m.counts[name]++
			cc := c
			pending = &cc
			continue
		}
		// 材料はディレクトリまでにとどめる。ファイル名まで渡すと、要約がファイル単位に寄るため
		dir := filepath.Dir(c.file)
		if dir == "." {
			dir = "直下"
		}
		if !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	flush()
	slices.SortStableFunc(m.order, func(a, b string) int { return m.counts[b] - m.counts[a] })
	m.text = strings.TrimSpace(b.String())
	return m
}

const (
	bodyLines                = 3    // 材料に入れる本文の行数
	defaultMaxLinesPerCommit = 120  // --deep で1コミットあたりに入れる差分の上限
	defaultMaxLinesTotal     = 4000 // --deep で全体に入れる差分の上限
)

// defaultDeepExclude は、--deep で既定で除外するパス。どのプロジェクトでも要約の材料にならないもの。
var defaultDeepExclude = []string{
	"**/package-lock.json", "**/pnpm-lock.yaml", "**/yarn.lock", "**/bun.lock", "**/bun.lockb", "**/go.sum",
	"**/*.min.js", "**/*.min.css", "**/*.map",
}

// commitDiff は、コミットの差分を上限の行数まで返す。使った行数も返す。
func commitDiff(c change, dc deepConfig, budget int) (string, int) {
	excludes := slices.Clone(dc.Exclude)
	if dc.UseDefaultExclude == nil || *dc.UseDefaultExclude {
		excludes = append(excludes, defaultDeepExclude...)
	}
	args := []string{"show", "--format=", "--no-color", "--no-ext-diff", "--unified=2", c.hash, "--", "."}
	for _, e := range excludes {
		args = append(args, ":(exclude,glob)"+e)
	}
	out, err := c.git(args...).Output()
	if err != nil {
		return "", 0
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	limit := min(cmp.Or(dc.MaxLinesPerCommit, defaultMaxLinesPerCommit), budget)
	if len(lines) <= limit {
		return strings.Join(lines, "\n"), len(lines)
	}
	return strings.Join(lines[:limit], "\n") + fmt.Sprintf("\n…（差分を %d 行省略）", len(lines)-limit), limit
}

// ---- AI の出力の解釈と表示 ------------------------------------------------------

type summaryItem struct {
	title, detail string
	concrete      []string // 機能の単位での具体的な変更
}

type repoSummary struct {
	name    string
	commits int // CLI が数えた数。材料に無い名前なら 0
	items   []summaryItem
}

type summarySections struct {
	lead  string
	repos []repoSummary
}

// parseSections は、AI の出力（「全体: …」「## リポジトリ」「見出し ｜ 補足」）を分ける。
// 節はコミットの多い順に並べる。決めた形でない行は、その位置の節に見出しとして加える。
func parseSections(text string, m summaryMaterial) summarySections {
	var s summarySections
	idx := map[string]int{}
	cur := ""
	for l := range strings.Lines(text) {
		l = strings.TrimSpace(l)
		switch {
		case l == "":
		case strings.HasPrefix(l, "全体:") || strings.HasPrefix(l, "全体："):
			s.lead = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(l, "全体:"), "全体："))
		case strings.HasPrefix(l, "#"):
			cur = strings.TrimSpace(strings.TrimLeft(l, "#"))
		case (strings.HasPrefix(l, "-") || strings.HasPrefix(l, "・")) && len(s.repos) > 0 && idxOK(idx, cur):
			// 直前の見出しの具体
			r := &s.repos[idx[cur]]
			if n := len(r.items); n > 0 {
				r.items[n-1].concrete = append(r.items[n-1].concrete, strings.TrimSpace(strings.TrimLeft(l, "-・")))
			}
		default:
			l = strings.TrimSpace(strings.TrimLeft(l, "-・*●"))
			title, detail, ok := strings.Cut(l, "｜")
			if !ok {
				title, detail, _ = strings.Cut(l, "|")
			}
			i, ok := idx[cur]
			if !ok {
				i = len(s.repos)
				idx[cur] = i
				s.repos = append(s.repos, repoSummary{name: cur, commits: m.counts[cur]})
			}
			s.repos[i].items = append(s.repos[i].items, summaryItem{title: strings.TrimSpace(title), detail: strings.TrimSpace(detail)})
		}
	}
	// コミットの多い順。材料に無い名前（AI が付けた見出しなど）は後ろに回す
	rank := func(name string) int {
		if i := slices.Index(m.order, name); i >= 0 {
			return i
		}
		return len(m.order)
	}
	slices.SortStableFunc(s.repos, func(a, b repoSummary) int { return rank(a.name) - rank(b.name) })
	return s
}

func idxOK(idx map[string]int, cur string) bool {
	_, ok := idx[cur]
	return ok
}

func (r repoSummary) heading() string {
	if r.name == "" {
		return "要点"
	}
	return r.name
}

// render は、画面に出す節を返す。
func (s summarySections) render() []string {
	var blocks []string
	if s.lead != "" {
		blocks = append(blocks, ui.Section("一言で", "", ui.Tree(ui.Node{Text: s.lead})))
	}
	for _, r := range s.repos {
		var nodes []ui.Node
		for _, it := range r.items {
			n := ui.Node{Text: ui.Paint(ui.Path, "●") + " " + ui.Paint(ui.Strong, it.title)}
			if it.detail != "" {
				n.Children = append(n.Children, ui.Node{Text: ui.Paint(ui.Muted, it.detail)})
			}
			for _, c := range it.concrete {
				n.Children = append(n.Children, ui.Node{Text: ui.Paint(ui.Muted, "-") + " " + c})
			}
			nodes = append(nodes, n)
		}
		meta := ""
		if r.commits > 0 {
			meta = ui.Paint(ui.Muted, fmt.Sprintf("%d コミット", r.commits))
		}
		blocks = append(blocks, ui.Section(r.heading(), meta, ui.Tree(nodes...)))
	}
	return blocks
}

// markdown は、記録に追記する内容を返す。画面と同じ内容を Markdown で書く。
func (s summarySections) markdown() string {
	var b strings.Builder
	if s.lead != "" {
		fmt.Fprintf(&b, "%s\n\n", s.lead)
	}
	for _, r := range s.repos {
		if r.commits > 0 {
			fmt.Fprintf(&b, "### %s（%d コミット）\n\n", r.heading(), r.commits)
		} else {
			fmt.Fprintf(&b, "### %s\n\n", r.heading())
		}
		for _, it := range r.items {
			if it.detail != "" {
				fmt.Fprintf(&b, "- **%s** — %s\n", it.title, it.detail)
			} else {
				fmt.Fprintf(&b, "- **%s**\n", it.title)
			}
			for _, c := range it.concrete {
				fmt.Fprintf(&b, "  - %s\n", c)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ---- 記録 ----------------------------------------------------------------------

// appendLog は、要約を月ごとの記録（範囲の終わりの月の yyyy-mm.md）に追記し、そのパスを返す。
func appendLog(logDir, command string, sp span, commits int, s summarySections, usage *summaryResult) (string, error) {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(logDir, sp.to.Format("2006-01")+".md")
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n%s\n\n", sp.String(), coveredMark(sp))
	fmt.Fprintf(&b, "- 取得: %s（%s）\n", formatTime(time.Now()), command)
	fmt.Fprintf(&b, "- まだ要約していなかったコミット: %d 件\n", commits)
	if usage != nil {
		fmt.Fprintf(&b, "- モデル: %s · 推定 $%.4f\n", strings.Join(usage.models(), ", "), usage.TotalCostUSD)
	}
	b.WriteString("\n")
	b.WriteString(s.markdown())
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(b.String()); err != nil {
		return "", err
	}
	return path, nil
}

// ---- 使ったモデルとトークン ------------------------------------------------------

// summaryResult は、claude -p --output-format json の結果のうち、使う項目。
type summaryResult struct {
	Result       string                `json:"result"`
	IsError      bool                  `json:"is_error"`
	TotalCostUSD float64               `json:"total_cost_usd"`
	ModelUsage   map[string]modelUsage `json:"modelUsage"`
}

type modelUsage struct {
	InputTokens              int     `json:"inputTokens"`
	OutputTokens             int     `json:"outputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
	CostUSD                  float64 `json:"costUSD"`
}

// parseSummary は、要約のコマンドの出力が claude -p --output-format json の形なら、
// 文章と使用量に分ける。そうでなければ出力をそのまま文章として返す。
func parseSummary(out []byte) (string, *summaryResult) {
	var r summaryResult
	if err := json.Unmarshal(out, &r); err != nil || r.Result == "" || r.IsError {
		return string(out), nil
	}
	return r.Result, &r
}

func (r *summaryResult) models() []string {
	return slices.Sorted(func(yield func(string) bool) {
		for m := range r.ModelUsage {
			if !yield(m) {
				return
			}
		}
	})
}

func (r *summaryResult) render() string {
	var nodes []ui.Node
	for _, m := range r.models() {
		u := r.ModelUsage[m]
		nodes = append(nodes, ui.Node{
			Text: ui.Paint(ui.Strong, m),
			Children: []ui.Node{{Text: fmt.Sprintf("入力 %s · 出力 %s · キャッシュ読み込み %s · キャッシュ作成 %s · 推定 $%.4f",
				thousands(u.InputTokens), thousands(u.OutputTokens),
				thousands(u.CacheReadInputTokens), thousands(u.CacheCreationInputTokens), u.CostUSD)}},
		})
	}
	return ui.Tree(nodes...)
}

// thousands は、数を3桁ごとにカンマで区切る。
func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
