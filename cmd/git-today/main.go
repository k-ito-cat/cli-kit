// git-today は、今日のコミットをリポジトリ横断で選び、hunk で表示する。
// PATH 上の git-<name> は git <name> として呼べるので、git today として使う。
//
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
	"strings"
	"sync"
	"time"

	"cli-kit/internal/complete"
)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "completion" && os.Args[2] == "zsh" {
		fmt.Print(complete.GitUserCommand("today", "今日のコミットを選んで hunk で表示する"))
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

func run() error {
	repos, err := repositories()
	if err != nil {
		return err
	}
	since := time.Now().Format("2006-01-02") + " 00:00:00"
	entries := todayCommits(repos, since)
	if len(entries) == 0 {
		return nil
	}

	selected, ok, err := pick(entries)
	if err != nil || !ok {
		return err
	}
	// 行は「リポジトリ名、件名、パス、ハッシュ」のタブ区切り
	fields := strings.Split(selected, "\t")
	if len(fields) != 4 {
		return fmt.Errorf("選んだ行を解釈できない: %q", selected)
	}
	hunk := exec.Command("hunk", "show", fields[3])
	hunk.Dir = fields[2]
	hunk.Stdin, hunk.Stdout, hunk.Stderr = os.Stdin, os.Stdout, os.Stderr
	return hunk.Run()
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

// todayCommits はリポジトリを並列に調べ、リポジトリの並び順のまま返す。
// git log に失敗したリポジトリ（取得途中など）は飛ばす。
func todayCommits(repos []string, since string) []string {
	results := make([][]string, len(repos))
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for i, repo := range repos {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			out, err := exec.Command("git", "-C", repo, "log", "--all", "--since="+since, "--format=%h\t%s").Output()
			if err != nil {
				return
			}
			for line := range strings.Lines(string(out)) {
				hash, subject, _ := strings.Cut(strings.TrimRight(line, "\n"), "\t")
				if hash != "" {
					results[i] = append(results[i], strings.Join([]string{filepath.Base(repo), subject, repo, hash}, "\t"))
				}
			}
		})
	}
	wg.Wait()

	var entries []string
	for _, r := range results {
		entries = append(entries, r...)
	}
	return entries
}

// pick は fzf で1行を選ばせる。取り消し（Esc など）は ok=false で返す。
func pick(entries []string) (selected string, ok bool, err error) {
	fzf := exec.Command("fzf",
		"--delimiter=\t",
		"--with-nth=1,2",
		"--ansi",
		"--gap",
		"--preview=git -C {3} show --color=always --stat --format=fuller {4}",
		"--preview-window=right,60%,wrap",
	)
	fzf.Stdin = strings.NewReader(strings.Join(entries, "\n") + "\n")
	fzf.Stderr = os.Stderr
	var out bytes.Buffer
	fzf.Stdout = &out
	if err := fzf.Run(); err != nil {
		// fzf は一致なしで 1、取り消しで 130 を返す
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && (exitErr.ExitCode() == 1 || exitErr.ExitCode() == 130) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("fzf に失敗: %w", err)
	}
	return strings.TrimRight(out.String(), "\n"), true, nil
}
