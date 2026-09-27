// Package recap は、recap コマンドの本体。使い方は cmd/recap/README.md。
package recap

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"cli-kit/internal/cli"
)

// ---- 期間 ----------------------------------------------------------------------

// period は対象の期間の種類。
type period struct {
	name       string // サブコマンド名
	dated      bool   // --date で日を指定できるか
	timeFormat string // 一覧に出すコミットの時刻の書式（git log --date=format:）
}

var (
	today = period{name: "today", dated: true, timeFormat: "%H:%M"}
	week  = period{name: "week", timeFormat: "%m/%d(%a) %H:%M"}
)

// span は、実際に対象にする時刻の範囲 [from, to)。
type span struct{ from, to time.Time }

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func (p period) span(now time.Time, date string) (span, error) {
	if p.name == week.name {
		return span{startOfDay(now).AddDate(0, 0, -6), now}, nil
	}
	if date == "" {
		return span{startOfDay(now), now}, nil
	}
	day, err := parseDate(now, date)
	if err != nil {
		return span{}, err
	}
	if !day.Before(now) {
		return span{}, fmt.Errorf("%s はまだ来ていない", date)
	}
	end := day.AddDate(0, 0, 1)
	if end.After(now) {
		end = now
	}
	return span{day, end}, nil
}

// parseDate は、2026-09-25、09-25（今年）、yesterday を受け付ける。
func parseDate(now time.Time, s string) (time.Time, error) {
	if s == "yesterday" {
		return startOfDay(now).AddDate(0, 0, -1), nil
	}
	for _, layout := range []string{"2006-01-02", "01-02"} {
		t, err := time.ParseInLocation(layout, s, now.Location())
		if err != nil {
			continue
		}
		if layout == "01-02" {
			t = time.Date(now.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("日付「%s」を読めない（2026-09-25、09-25、yesterday のどれか）", s)
}

var weekdays = []string{"日", "月", "火", "水", "木", "金", "土"}

// formatTime は、年と曜日を含む表示用の日時。
func formatTime(t time.Time) string {
	return t.Format("2006-01-02") + "(" + weekdays[t.Weekday()] + ") " + t.Format("15:04")
}

func (s span) String() string {
	return formatTime(s.from) + " 〜 " + formatTime(s.to)
}

// ---- 入口 ----------------------------------------------------------------------

// Main は recap の入口。
func Main() {
	root := cli.Root("recap",
		"自分のコミットをリポジトリ横断で振り返る",
		"今日・直近7日の自分のコミットで変わったファイルを選んで開く。--summary で、まだ要約していないコミットの要点を AI にまとめさせ、記録に追記する。\n"+
			"設定: "+configPath())
	root.AddCommand(periodCmd(today), periodCmd(week), logCmd(), previewCmd())
	cli.Execute(root)
}

type options struct {
	date                 string
	summary, deep, force bool
}

func periodCmd(p period) *cobra.Command {
	var o options
	c := &cobra.Command{
		Use:   p.name,
		Short: "直近7日のコミットを振り返る",
		Args:  cli.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			if (o.deep || o.force) && !o.summary {
				return cli.UsageError{Msg: "--deep と --force は --summary と一緒に使う"}
			}
			return runPeriod(p, o)
		},
	}
	if p.dated {
		c.Short = "今日（--date で指定した日）のコミットを振り返る"
		c.Flags().StringVar(&o.date, "date", "", "`日付` 対象の日（2026-09-25、09-25、yesterday）。既定は今日")
		_ = c.RegisterFlagCompletionFunc("date", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			now := time.Now()
			days := []string{"yesterday"}
			for i := range 7 {
				days = append(days, now.AddDate(0, 0, -i).Format("2006-01-02"))
			}
			return days, cobra.ShellCompDirectiveNoFileComp
		})
	}
	c.Long = c.Short + "。変わったファイルを選び、差分（hunk）・yazi・今の中身（bat）で開く。"
	c.Flags().BoolVar(&o.summary, "summary", false, "まだ要約していないコミットの要点を AI にまとめさせ、記録に追記する（新しいコミットが無ければ AI は呼ばない）")
	c.Flags().BoolVar(&o.deep, "deep", false, "--summary の材料に差分も加え、より具体的にまとめさせる（トークンを多く使う）")
	c.Flags().BoolVar(&o.force, "force", false, "--summary で、要約済みの範囲も含めてもう一度要約させる")
	return c
}

func logCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "log",
		Short: "要約の記録（月ごとの yyyy-mm.md）を開く",
		Args:  cli.NoArgs,
		RunE:  func(c *cobra.Command, args []string) error { return openLog() },
	}
}

// previewCmd は、fzf のプレビューから呼ぶ隠しコマンド。シェルの if 文で組み立てず、ここで出し分ける。
func previewCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__preview <リポジトリ> <ハッシュ> <ファイル>",
		Hidden: true,
		Args:   cobra.ExactArgs(3),
		Run: func(c *cobra.Command, args []string) {
			if err := preview(args[0], args[1], args[2]); err != nil {
				fmt.Printf("プレビューに失敗: %v\n", err)
			}
		},
	}
}

func runPeriod(p period, o options) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	sp, err := p.span(time.Now(), o.date)
	if err != nil {
		return err
	}
	changes, err := collect(cfg, sp, p.timeFormat)
	if err != nil {
		return err
	}
	if o.summary {
		return summarize(cfg, p, sp, changes, o.deep, o.force)
	}
	if len(changes) == 0 {
		return nil
	}
	lines := make([]string, len(changes))
	for i, c := range changes {
		lines[i] = c.line()
	}
	for {
		c, ok, err := pickChange(lines, sp.String())
		if err != nil || !ok {
			return err
		}
		// コミット全体と、今の作業ツリーにないファイル（削除したものなど）は、差分でしか開けない
		onlyDiff := c.file == wholeCommit || !exists(filepath.Join(c.repo, c.file))
		v, ok, err := pickViewer(onlyDiff)
		if err != nil {
			return err
		}
		if !ok {
			continue // ファイルの選択に戻る
		}
		return v.open(c)
	}
}

// ---- 設定ファイル ------------------------------------------------------------

type config struct {
	Sources   []string `toml:"sources"` // 対象のリポジトリのパスを1行に1つずつ出すコマンド
	Exclude   []string `toml:"exclude"` // 除外するリポジトリ（名前かパス）
	Authors   []string `toml:"authors"` // 自分とみなす作者。空なら各リポジトリの user.email
	LogDir    string   `toml:"log_dir"` // 要約の記録の置き場所
	Summarize struct {
		Command string `toml:"command"` // 標準入力で頼む内容を受け取り、標準出力に要約を出すコマンド
	} `toml:"summarize"`
	Deep deepConfig `toml:"deep"`
}

// deepConfig は --deep で材料に加える差分の設定。
type deepConfig struct {
	Exclude           []string `toml:"exclude"`              // 既定に加えて除外するパス（git の glob）
	UseDefaultExclude *bool    `toml:"use_default_exclude"`  // false なら既定の除外を使わない
	MaxLinesPerCommit int      `toml:"max_lines_per_commit"` // 1コミットあたりの差分の上限
	MaxLinesTotal     int      `toml:"max_lines_total"`      // 全体の差分の上限
}

func configPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "cli-kit", "recap.toml")
}

// defaultLogDir は、要約の記録の既定の置き場所。
func defaultLogDir() string {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "recap")
}

const configExample = `sources = ["ghq list --full-path"]   # 対象のリポジトリのパスを1行に1つずつ出すコマンド
exclude = []                         # 除外するリポジトリ（名前かパス）
authors = []                         # 自分とみなす作者（空なら各リポジトリの user.email）
# log_dir = "~/.local/share/recap"   # 要約の記録の置き場所

[summarize]
command = "claude -p --output-format json --tools ''"`

func loadConfig() (config, error) {
	path := configPath()
	var cfg config
	md, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, os.ErrNotExist) {
		return config{}, fmt.Errorf("設定ファイル %s がない。次の形で置く:\n\n%s", path, configExample)
	}
	if err != nil {
		return config{}, fmt.Errorf("%s を読めない: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return config{}, fmt.Errorf("%s に知らないキーがある: %s", path, strings.Join(keys, "、"))
	}
	if len(cfg.Sources) == 0 {
		return config{}, fmt.Errorf("%s に sources が1つもない", path)
	}
	cfg.LogDir = expandHome(cfg.LogDir)
	if cfg.LogDir == "" {
		cfg.LogDir = defaultLogDir()
	}
	return cfg, nil
}

func expandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, rest)
	}
	return p
}

// repositories は、sources のコマンドが出したリポジトリから、除外するものを除いて返す。
func repositories(cfg config) ([]string, error) {
	var repos []string
	for _, src := range cfg.Sources {
		out, err := exec.Command("sh", "-c", src).Output()
		if err != nil {
			return nil, fmt.Errorf("sources の「%s」に失敗: %w", src, err)
		}
		for l := range strings.Lines(string(out)) {
			repo := strings.TrimSpace(l)
			if repo == "" || slices.Contains(repos, repo) {
				continue
			}
			if slices.Contains(cfg.Exclude, repo) || slices.Contains(cfg.Exclude, filepath.Base(repo)) {
				continue
			}
			repos = append(repos, repo)
		}
	}
	return repos, nil
}

// ---- コミットの収集 ----------------------------------------------------------

// git log の出力の区切り。ファイル名や本文には現れない制御文字を使う
const (
	commitMark = "\x1e" // コミットの始まり
	bodyMark   = "\x1f" // 件名と本文の区切り
	headerEnd  = "\x1d" // コミットの情報の終わり（このあとに変わったファイルが続く）
)

// collect は、範囲内の自分のコミットごとに「コミット全体」と変わったファイルを返す。
// 並びはリポジトリの順、その中はコミットの新しい順。git log に失敗したリポジトリ（取得途中など）は飛ばす。
func collect(cfg config, sp span, timeFormat string) ([]change, error) {
	repos, err := repositories(cfg)
	if err != nil {
		return nil, err
	}
	results := make([][]change, len(repos))
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for i, repo := range repos {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = repoChanges(repo, sp, timeFormat, cfg.Authors)
		})
	}
	wg.Wait()
	var changes []change
	for _, r := range results {
		changes = append(changes, r...)
	}
	return changes, nil
}

func repoChanges(repo string, sp span, timeFormat string, authors []string) []change {
	if len(authors) == 0 {
		out, _ := exec.Command("git", "-C", repo, "config", "user.email").Output()
		email := strings.TrimSpace(string(out))
		if email == "" {
			return nil // 自分を特定できないリポジトリは、他人のコミットを混ぜないよう飛ばす
		}
		authors = []string{email}
	}
	const layout = "2006-01-02 15:04:05"
	// --fixed-strings で、作者をメールアドレスそのままの文字列として照らし合わせる
	args := []string{"-C", repo, "log", "--all", "--since=" + sp.from.Format(layout), "--until=" + sp.to.Format(layout),
		"--fixed-strings", "--name-status", "--date=format:" + timeFormat,
		"--format=" + commitMark + "%h\t%ad\t%ct\t%s" + bodyMark + "%b" + headerEnd}
	for _, a := range authors {
		args = append(args, "--author="+a)
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil
	}
	var r []change
	for _, chunk := range strings.Split(string(out), commitMark)[1:] {
		header, files, _ := strings.Cut(chunk, headerEnd)
		head, body, _ := strings.Cut(header, bodyMark)
		f := strings.SplitN(head, "\t", 4)
		if len(f) != 4 {
			continue
		}
		ct, _ := strconv.ParseInt(f[2], 10, 64)
		c := change{repo: repo, hash: f[0], time: f[1], at: time.Unix(ct, 0), subject: f[3], body: strings.TrimSpace(body), file: wholeCommit}
		r = append(r, c)
		for l := range strings.Lines(files) {
			// 「種類<TAB>パス」。名前の変更とコピーは「R100<TAB>元<TAB>先」なので、最後のパスを使う
			fields := strings.Split(strings.TrimRight(l, "\n"), "\t")
			if len(fields) < 2 {
				continue
			}
			fc := c
			fc.body = ""
			fc.status = fields[0][:1]
			fc.file = fields[len(fields)-1]
			r = append(r, fc)
		}
	}
	// 各コミットの最後のファイルに印を付ける（次がコミットの行か、一覧の終わり）
	for j := range r {
		if r[j].file != wholeCommit && (j == len(r)-1 || r[j+1].file == wholeCommit) {
			r[j].last = true
		}
	}
	return r
}
