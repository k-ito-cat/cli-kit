package recap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"cli-kit/internal/cli"
	"cli-kit/internal/ui"
)

// repoState は、リポジトリの未コミットの変更と、未 push のコミットの数。
type repoState struct {
	repo    string
	changes map[string]int // 変更の種類（M・A・D・R・??）ごとのファイルの数
	ahead   int            // 上流のブランチより先行しているコミットの数
	branch  string
}

func (s repoState) dirty() bool { return len(s.changes) > 0 }

// statusOrder は、変更の種類を表示する順と、その色。
var statusOrder = []struct {
	code string
	tone ui.Tone
}{{"M", ui.Warn}, {"A", ui.OK}, {"D", ui.Error}, {"R", ui.Path}, {"??", ui.Muted}}

func (s repoState) changesText() string {
	var parts []string
	for _, o := range statusOrder {
		if n := s.changes[o.code]; n > 0 {
			parts = append(parts, ui.Paint(o.tone, o.code)+" "+strconv.Itoa(n))
		}
	}
	return strings.Join(parts, ui.Paint(ui.Muted, " · "))
}

// states は、対象のリポジトリを並列に調べる。
func states(cfg config) ([]repoState, error) {
	repos, err := repositories(cfg)
	if err != nil {
		return nil, err
	}
	r := make([]repoState, len(repos))
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for i, repo := range repos {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			r[i] = stateOf(repo)
		})
	}
	wg.Wait()
	return r, nil
}

func stateOf(repo string) repoState {
	s := repoState{repo: repo, changes: map[string]int{}}
	out, err := exec.Command("git", "-C", repo, "status", "--porcelain").Output()
	if err != nil {
		return s // 調べられないリポジトリ（取得途中など）は、変更なしとして扱う
	}
	for l := range strings.Lines(string(out)) {
		if len(l) < 2 {
			continue
		}
		xy := l[:2]
		switch {
		case xy == "??":
			s.changes["??"]++
		case strings.ContainsAny(xy, "R"):
			s.changes["R"]++
		case strings.ContainsAny(xy, "A"):
			s.changes["A"]++
		case strings.ContainsAny(xy, "D"):
			s.changes["D"]++
		default:
			s.changes["M"]++
		}
	}
	b, _ := exec.Command("git", "-C", repo, "branch", "--show-current").Output()
	s.branch = strings.TrimSpace(string(b))
	// 上流のブランチが無ければ、未 push は数えない
	if n, err := exec.Command("git", "-C", repo, "rev-list", "--count", "@{upstream}..HEAD").Output(); err == nil {
		s.ahead, _ = strconv.Atoi(strings.TrimSpace(string(n)))
	}
	return s
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "未コミットの変更と、未 push のコミットがあるリポジトリを表示する",
		Args:  cli.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			all, err := states(cfg)
			if err != nil {
				return err
			}
			ui.Println(renderStatus(all))
			return nil
		},
	}
}

func renderStatus(all []repoState) string {
	var dirty, ahead [][]string
	for _, s := range all {
		name := filepath.Base(s.repo)
		if s.dirty() {
			dirty = append(dirty, []string{name, s.changesText()})
		}
		if s.ahead > 0 {
			ahead = append(ahead, []string{name, fmt.Sprintf("%d コミット先行", s.ahead), ui.Paint(ui.Muted, s.branch)})
		}
	}
	section := func(title string, rows [][]string) string {
		body := ui.None()
		if len(rows) > 0 {
			body = ui.Columns{Rows: rows}.String()
		}
		return ui.Section(title, ui.Paint(ui.Muted, fmt.Sprintf("%d リポジトリ", len(rows))), body)
	}
	return ui.Blocks(
		ui.Title("recap status", formatTime(time.Now())),
		section("未コミット", dirty),
		section("未 push", ahead),
		ui.Footer(fmt.Sprintf("対象 %d リポジトリ · 未コミット %d · 未 push %d", len(all), len(dirty), len(ahead))),
	)
}

// dirtyWarning は、未コミットの変更が残っているリポジトリを知らせる1行を返す。無ければ空文字。
// 要約はコミットしか見ないので、残っている作業に気づけるようにする。
func dirtyWarning(cfg config) string {
	all, err := states(cfg)
	if err != nil {
		return ""
	}
	var names []string
	for _, s := range all {
		if s.dirty() {
			names = append(names, filepath.Base(s.repo))
		}
	}
	if len(names) == 0 {
		return ""
	}
	return ui.MarkText(ui.LevelWarn, "未コミットの変更あり: "+strings.Join(names, "、")+"（要約に入っていない。recap status で確認）")
}

func configCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "設定ファイルを bat で表示する",
		Args:  cli.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			path := configPath()
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("設定ファイル %s がない。次の形で置く:\n\n%s", path, configExample)
			}
			return interactive(filepath.Dir(path), "bat", path)
		},
	}
}
