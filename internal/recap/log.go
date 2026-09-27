package recap

import (
	"fmt"
	"path/filepath"
	"slices"
)

// openLog は、月ごとの要約の記録を選び、bat（読む）か yazi（フォルダを開く）で開く。記録が1つなら選ぶ段階は飛ばす。
func openLog() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	files, _ := filepath.Glob(filepath.Join(cfg.LogDir, "*.md"))
	if len(files) == 0 {
		return fmt.Errorf("%s に記録がない（recap today --summary などで作られる）", cfg.LogDir)
	}
	slices.Sort(files)
	slices.Reverse(files) // 新しい月から
	for {
		file := files[0]
		if len(files) > 1 {
			selected, ok, err := fzf(files,
				"--list-label= 要約の記録 ",
				"--preview-label= 中身 ",
				"--header=Enter 次へ · Esc 終わる",
				"--preview=bat --color=always --style=plain {}",
			)
			if err != nil || !ok {
				return err
			}
			file = selected[0]
		}
		lines := []string{
			style("1", "bat  ") + " " + style("2", "記録を読む") + "\tbat",
			style("1", "yazi ") + " " + style("2", "記録のフォルダを開く") + "\tyazi",
		}
		selected, ok, err := fzf(lines,
			"--delimiter=\t",
			"--with-nth=1",
			"--list-label= 開き方 ",
			"--header=Enter 開く · Esc 戻る",
			"--no-preview",
		)
		if err != nil {
			return err
		}
		if !ok {
			if len(files) == 1 {
				return nil
			}
			continue // 記録の選択に戻る
		}
		if selected[0][len(selected[0])-3:] == "bat" {
			return interactive(cfg.LogDir, "bat", "--paging=always", file)
		}
		return interactive(cfg.LogDir, "yazi", file)
	}
}
