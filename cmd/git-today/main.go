// git-today は、今日のコミットで変わったファイルをリポジトリ横断で選び、差分・yazi・今のファイルの中身で開く。
// PATH 上の git-<name> は git <name> として呼べるので、git today として使う。
//
// fzf で「ファイル → 開き方」の順に選ぶ。操作は Enter だけで、Esc で1つ前に戻る。
// 一覧の各コミットの先頭には、コミット全体を差分で開くための行を置く。
// 対象は ghq が管理するリポジトリと、dotfiles（chezmoi の管理元）。
// git-today completion zsh で、git today を git の補完の候補に加える設定を出力する。
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"cli-kit/internal/complete"
)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "completion" && os.Args[2] == "zsh" {
		fmt.Print(complete.GitUserCommand("today", "今日のコミットで変わったファイルを選び、差分・yazi・中身で開く"))
		return
	}
	// fzf のプレビューから呼ぶ。シェルの if 文で組み立てず、ここで出し分ける
	if len(os.Args) == 5 && os.Args[1] == "__preview" {
		if err := preview(os.Args[2], os.Args[3], os.Args[4]); err != nil {
			fmt.Printf("プレビューに失敗: %v\n", err)
		}
		return
	}
	// 知らない引数で fzf を起動しないよう、引数があれば止める
	if len(os.Args) > 1 {
		fmt.Fprintln(os.Stderr, "使い方: git today\n       git-today completion zsh")
		os.Exit(2)
	}
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "git-today: %v\n", err)
		os.Exit(1)
	}
}

// wholeCommit は、ファイルの代わりに置く「コミット全体」を表す行。
const wholeCommit = "（コミット全体）"

// change は一覧の1行。file が wholeCommit ならコミット全体を表す。
type change struct {
	repo, hash, subject, file string
	time                      string // コミットの時刻（HH:MM）
	status                    string // ファイルの変更の種類（A 追加・M 更新・D 削除・R 名前の変更など）
	last                      bool   // コミットの中で最後のファイルか（入れ子の枝の形を決める）
}

// statusColors は、変更の種類の印の色（ANSI）。ここに無い種類は薄く出す。
var statusColors = map[string]string{"A": "32", "M": "33", "D": "31", "R": "36"}

// line は fzf に渡す行。1列目が表示で、コミットは「リポジトリ名と件名」、ファイルはその下に字下げして並べる。
// 2列目以降は、プレビューと選んだあとの処理に使う。
func (c change) line() string {
	var display string
	if c.file == wholeCommit {
		// リポジトリ名（太字・シアン）、件名、時刻とハッシュ（薄く）
		display = style("1;36", filepath.Base(c.repo)) + "  " + c.subject + "  " + style("2", c.time+" "+c.hash)
	} else {
		// 枝（薄く）、ディレクトリ（薄く）、ファイル名
		branch := "├─ "
		if c.last {
			branch = "└─ "
		}
		color, ok := statusColors[c.status]
		if !ok {
			color = "2"
		}
		dir, base := filepath.Split(c.file)
		display = "  " + style("2", branch) + style("1;"+color, c.status) + " " + style("2", dir) + base
	}
	return strings.Join([]string{display, c.repo, c.hash, c.file, c.subject}, "\t")
}

// style は、fzf の一覧に出す文字に ANSI の装飾を付ける（fzf は --ansi で解釈する）。
func style(code, text string) string {
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func parseLine(l string) (change, error) {
	f := strings.Split(l, "\t")
	if len(f) != 5 {
		return change{}, fmt.Errorf("選んだ行を解釈できない: %q", l)
	}
	return change{repo: f[1], hash: f[2], file: f[3], subject: f[4]}, nil
}

func (c change) git(args ...string) *exec.Cmd {
	return exec.Command("git", append([]string{"-C", c.repo}, args...)...)
}

func run() error {
	repos, err := repositories()
	if err != nil {
		return err
	}
	since := time.Now().Format("2006-01-02") + " 00:00:00"
	changes := todayChanges(repos, since)
	if len(changes) == 0 {
		return nil
	}
	lines := make([]string, len(changes))
	for i, c := range changes {
		lines[i] = c.line()
	}

	for {
		c, ok, err := pickChange(lines)
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

func repositories() ([]string, error) {
	out, err := exec.Command("ghq", "list", "--full-path").Output()
	if err != nil {
		return nil, fmt.Errorf("ghq list に失敗: %w", err)
	}
	source, err := exec.Command("chezmoi", "source-path").Output()
	if err != nil {
		return nil, fmt.Errorf("chezmoi source-path に失敗: %w", err)
	}
	var repos []string
	for line := range strings.Lines(string(out)) {
		if repo := strings.TrimRight(line, "\n"); repo != "" {
			repos = append(repos, repo)
		}
	}
	return append(repos, strings.TrimSpace(string(source))), nil
}

// commitMark は、git log の出力でコミットの始まりを示す印。ファイル名には現れない制御文字を使う。
const commitMark = "\x1e"

// todayChanges はリポジトリを並列に調べ、今日のコミットごとに「コミット全体」と変わったファイルを返す。
// 並びはリポジトリの順、その中はコミットの新しい順。git log に失敗したリポジトリ（取得途中など）は飛ばす。
func todayChanges(repos []string, since string) []change {
	results := make([][]change, len(repos))
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for i, repo := range repos {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			out, err := exec.Command("git", "-C", repo, "log", "--all", "--since="+since,
				"--name-status", "--date=format:%H:%M", "--format="+commitMark+"%h\t%ad\t%s").Output()
			if err != nil {
				return
			}
			var cur *change
			for l := range strings.Lines(string(out)) {
				l = strings.TrimRight(l, "\n")
				switch {
				case strings.HasPrefix(l, commitMark):
					f := strings.SplitN(strings.TrimPrefix(l, commitMark), "\t", 3)
					if len(f) != 3 {
						cur = nil
						continue
					}
					cur = &change{repo: repo, hash: f[0], time: f[1], subject: f[2], file: wholeCommit}
					results[i] = append(results[i], *cur)
				case l != "" && cur != nil:
					// 「種類<TAB>パス」。名前の変更とコピーは「R100<TAB>元<TAB>先」なので、最後のパスを使う
					fields := strings.Split(l, "\t")
					if len(fields) < 2 {
						continue
					}
					f := *cur
					f.status = fields[0][:1]
					f.file = fields[len(fields)-1]
					results[i] = append(results[i], f)
				}
			}
			// 各コミットの最後のファイルに印を付ける（次がコミットの行か、一覧の終わり）
			r := results[i]
			for j := range r {
				if r[j].file != wholeCommit && (j == len(r)-1 || r[j+1].file == wholeCommit) {
					r[j].last = true
				}
			}
		})
	}
	wg.Wait()

	var changes []change
	for _, r := range results {
		changes = append(changes, r...)
	}
	return changes
}

func pickChange(lines []string) (change, bool, error) {
	self, err := os.Executable()
	if err != nil {
		return change{}, false, err
	}
	selected, ok, err := fzf(lines,
		"--delimiter=\t",
		"--with-nth=1",
		"--list-label= 今日の変更 ",
		"--preview-label= 差分 ",
		"--header=コミットの行を選ぶとコミット全体 · Enter 次へ · Esc 終わる",
		"--preview="+shellQuote(self)+" __preview {2} {3} {4}",
	)
	if err != nil || !ok {
		return change{}, false, err
	}
	c, err := parseLine(selected[0])
	return c, err == nil, err
}

type viewer struct {
	name, description string
	// diff は、差分として開くか（コミット全体や、今の作業ツリーにないファイルでも開ける）
	diff bool
	open func(c change) error
}

var viewers = []viewer{
	{"hunk", "このコミットでの差分を見る", true, openHunk},
	{"yazi", "yazi でファイルを開く", false, openYazi},
	{"bat", "今のファイルの中身を見る", false, openBat},
}

// pickViewer は開き方を選ばせる。前回選んだものを先頭に置く。選べるものが1つなら聞かずに返す。
// onlyDiff なら、差分で開けるものだけを候補にする。
func pickViewer(onlyDiff bool) (viewer, bool, error) {
	last := readLastViewer()
	var candidates []viewer
	for _, v := range viewers {
		if !onlyDiff || v.diff {
			candidates = append(candidates, v)
		}
	}
	slices.SortStableFunc(candidates, func(a, b viewer) int {
		return boolInt(b.name == last) - boolInt(a.name == last)
	})
	if len(candidates) == 1 {
		return candidates[0], true, nil
	}
	lines := make([]string, len(candidates))
	for i, v := range candidates {
		lines[i] = style("1", fmt.Sprintf("%-5s", v.name)) + " " + style("2", v.description) + "\t" + v.name
	}
	selected, ok, err := fzf(lines,
		"--delimiter=\t",
		"--with-nth=1",
		"--list-label= 開き方 ",
		"--header=Enter 開く · Esc 戻る",
		"--no-preview",
	)
	if err != nil || !ok {
		return viewer{}, false, err
	}
	_, name, _ := strings.Cut(selected[0], "\t")
	for _, v := range candidates {
		if v.name == name {
			writeLastViewer(name)
			return v, true, nil
		}
	}
	return viewer{}, false, fmt.Errorf("開き方を解釈できない: %q", selected[0])
}

// preview は、一覧のプレビューを出す。コミット全体ならコミットの概要、ファイルならこのコミットでの差分。
func preview(repo, hash, file string) error {
	c := change{repo: repo, hash: hash}
	var cmd *exec.Cmd
	if file == wholeCommit {
		cmd = c.git("show", "--color=always", "--stat", "--format=fuller", hash)
	} else {
		cmd = c.git("show", "--color=always", "--format=", hash, "--", file)
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stdout
	return cmd.Run()
}

func openHunk(c change) error {
	args := []string{"show", c.hash}
	if c.file != wholeCommit {
		args = append(args, "--", c.file)
	}
	return interactive(c.repo, "hunk", args...)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// currentFile は、今の作業ツリーでのファイルのパスを返す。コミットのあとで消えていればエラー。
func currentFile(c change) (string, error) {
	p := filepath.Join(c.repo, c.file)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("%s は今の作業ツリーにない（その後に消えたファイルなど）", c.file)
	}
	return p, nil
}

func openYazi(c change) error {
	p, err := currentFile(c)
	if err != nil {
		return err
	}
	return interactive(c.repo, "yazi", p)
}

func openBat(c change) error {
	p, err := currentFile(c)
	if err != nil {
		return err
	}
	return interactive(c.repo, "bat", "--paging=always", p)
}

func interactive(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// shellQuote は、fzf のプレビューのコマンド（シェルで実行される）に埋め込む文字列を引用する。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func stateFile() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "git-today", "viewer")
}

func readLastViewer() string {
	b, _ := os.ReadFile(stateFile()) // 無ければ既定の並びのまま
	return strings.TrimSpace(string(b))
}

// writeLastViewer は前回の開き方を覚える。覚えられなくても開く操作は続ける。
func writeLastViewer(name string) {
	p := stateFile()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
		_ = os.WriteFile(p, []byte(name+"\n"), 0o644)
	}
}

// fzf は lines から選ばせ、選んだ行を返す。取り消し（Esc など）は ok=false で返す。
// 一覧は上から並べる（コミットの下にファイルが並ぶ入れ子を、逆さまにしないため）。
func fzf(lines []string, args ...string) (selected []string, ok bool, err error) {
	args = append([]string{
		"--ansi",
		"--layout=reverse",
		// 一覧・入力欄・プレビューをそれぞれ角の丸い枠で囲み、枠と説明は薄く、選んだ行と一致した文字はシアンで示す
		"--style=full:rounded",
		"--margin=0,1",
		"--info=inline-right",
		"--prompt=❯ ",
		"--pointer=▶",
		"--highlight-line",
		"--color=border:8,label:bold,header:8,info:8,separator:8,pointer:6,hl:6:bold,hl+:6:bold",
		"--preview-window=right,60%,wrap",
	}, args...)
	cmd := exec.Command("fzf", args...)
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n") + "\n")
	cmd.Stderr = os.Stderr
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		// fzf は一致なしで 1、取り消しで 130 を返す
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && (exitErr.ExitCode() == 1 || exitErr.ExitCode() == 130) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("fzf に失敗: %w", err)
	}
	for l := range strings.Lines(out.String()) {
		if l = strings.TrimRight(l, "\n"); l != "" {
			selected = append(selected, l)
		}
	}
	return selected, len(selected) > 0, nil
}
