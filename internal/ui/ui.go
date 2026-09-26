// Package ui は、cli-kit の CLI が共通で使う出力の部品を持つ。
// 見た目をそろえるため、各 CLI は罫線や色を自前で組まずにここを使う。
// 色や太字は意味（Error、Muted など）で指定し、具体的な色はここだけで決める。
package ui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/colorprofile"
)

// ColorModes は --color に指定できる値。
var ColorModes = []string{"auto", "always", "never"}

// SetColor は色付けの方針を決める。auto は端末のときだけ色を付け、NO_COLOR を尊重する。
func SetColor(mode string) error {
	switch mode {
	case "auto":
	case "always":
		lipgloss.Writer.Profile = colorprofile.ANSI
	case "never":
		lipgloss.Writer.Profile = colorprofile.NoTTY
	default:
		return fmt.Errorf("--color は %s のいずれか: %s", strings.Join(ColorModes, "・"), mode)
	}
	return nil
}

// Println は標準出力に書く。色は SetColor の方針に合わせて落とす。
func Println(blocks ...string) {
	lipgloss.Println(strings.Join(blocks, "\n"))
}

// Tone は文字の意味。意味ごとに見た目を決める。
type Tone int

const (
	Plain Tone = iota
	Strong
	Muted
	Path
	OK
	Warn
	Undefined
	Error
)

var tones = map[Tone]lipgloss.Style{
	Plain:     lipgloss.NewStyle(),
	Strong:    lipgloss.NewStyle().Bold(true),
	Muted:     lipgloss.NewStyle().Faint(true),
	Path:      lipgloss.NewStyle().Foreground(lipgloss.Cyan),
	OK:        lipgloss.NewStyle().Foreground(lipgloss.Green),
	Warn:      lipgloss.NewStyle().Foreground(lipgloss.Yellow),
	Undefined: lipgloss.NewStyle().Foreground(lipgloss.Cyan),
	Error:     lipgloss.NewStyle().Foreground(lipgloss.Red).Bold(true),
}

// Paint は text を tone の見た目にする。
func Paint(tone Tone, text string) string {
	return tones[tone].Render(text)
}

// Level は検査結果などの状態。
type Level int

const (
	LevelOK Level = iota
	LevelInfo
	LevelUndefined
	LevelWarn
	LevelError
)

var marks = map[Level]struct {
	symbol string
	tone   Tone
}{
	LevelOK:        {"✓", OK},
	LevelInfo:      {"·", Muted},
	LevelUndefined: {"?", Undefined},
	LevelWarn:      {"!", Warn},
	LevelError:     {"✗", Error},
}

// Mark は状態の印（✓ · ? ! ✗）を返す。
func Mark(l Level) string {
	m := marks[l]
	return Paint(m.tone, m.symbol)
}

// MarkText は状態の印と text を、状態の見た目でまとめて返す。
func MarkText(l Level, text string) string {
	m := marks[l]
	return Paint(m.tone, m.symbol+" "+text)
}

// LevelTone は状態の見た目を返す。
func LevelTone(l Level) Tone {
	return marks[l].tone
}

// Tally は問題・警告・未定義の件数を1行にまとめる。
// 未定義が残っていれば「指摘なし」とは出さない。
func Tally(errors, warns, undefined int) string {
	var parts []string
	if errors > 0 {
		parts = append(parts, MarkText(LevelError, fmt.Sprintf("%d 問題", errors)))
	}
	if warns > 0 {
		parts = append(parts, MarkText(LevelWarn, fmt.Sprintf("%d 警告", warns)))
	}
	if undefined > 0 {
		parts = append(parts, MarkText(LevelUndefined, fmt.Sprintf("%d 未定義", undefined)))
	}
	if len(parts) == 0 {
		return MarkText(LevelOK, "指摘なし")
	}
	return strings.Join(parts, "  ")
}

// Rule は区切りの横線を返す。
func Rule() string {
	return Paint(Muted, strings.Repeat("─", 40))
}

// Pad は text の右を空白で埋め、表示幅を w にする。全角の文字は2桁として数える。
func Pad(text string, w int) string {
	return text + strings.Repeat(" ", max(0, w-lipgloss.Width(text)))
}

// PadLeft は text の左を空白で埋め、表示幅を w にする。
func PadLeft(text string, w int) string {
	return strings.Repeat(" ", max(0, w-lipgloss.Width(text))) + text
}

// PadCenter は text の左右を空白で埋めて中央に置き、表示幅を w にする。余りの1桁は右に回す。
func PadCenter(text string, w int) string {
	gap := max(0, w-lipgloss.Width(text))
	return strings.Repeat(" ", gap/2) + text + strings.Repeat(" ", gap-gap/2)
}

// Table は罫線付きの表。
type Table struct {
	Headers []string
	Rows    [][]string
	// Footer は合計などの最後の行。太字で出す
	Footer []string
	// Widths を渡すと、各列をその表示幅まで広げる。複数の表で列の幅をそろえたいときに使う
	Widths []int
	// Right は右に寄せる列（数値の列など）
	Right []bool
	// Tone は本文と Footer の各セルの見た目を決める。nil なら Plain
	Tone func(col int, value string) Tone
}

func (t Table) String() string {
	all := slices.Clone(t.Rows)
	if t.Footer != nil {
		all = append(all, t.Footer)
	}
	widths := ColumnWidths(t.Headers, all)
	for i, w := range t.Widths {
		if i < len(widths) {
			widths[i] = max(widths[i], w)
		}
	}
	rows := make([][]string, len(all))
	for i, r := range all {
		rows[i] = t.pad(r, widths)
	}
	headers := t.padHeaders(widths)
	headerLines := 1
	for _, h := range t.Headers {
		headerLines = max(headerLines, strings.Count(h, "\n")+1)
	}
	// lipgloss の表は見出しを必ず1行に切り詰める。2行以上の見出しは本文の1行目として描き、
	// その下に見出しの区切り線を差し込む
	multi := headerLines > 1
	offset := 0
	if multi {
		rows = append([][]string{headers}, rows...)
		offset = 1
	}
	footer := t.Footer != nil
	cell := lipgloss.NewStyle().Padding(0, 1)
	tb := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(tones[Muted]).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow || (multi && row == 0) || (footer && row == len(rows)-1) {
				return cell.Inherit(tones[Strong])
			}
			r := row - offset
			if t.Tone == nil || col >= len(all[r]) {
				return cell
			}
			return cell.Inherit(tones[t.Tone(col, all[r][col])])
		})
	if !multi {
		return tb.Headers(headers...).String()
	}
	lines := strings.Split(tb.String(), "\n")
	sep := strings.NewReplacer("┌", "├", "┬", "┼", "┐", "┤").Replace(lines[0])
	at := 1 + headerLines
	return strings.Join(slices.Insert(lines, at, sep), "\n")
}

// padHeaders は見出しを、列の幅の中央に置く。2行以上の見出しは行ごとに中央に置く。
func (t Table) padHeaders(widths []int) []string {
	out := make([]string, len(t.Headers))
	for i, h := range t.Headers {
		if i >= len(widths) {
			out[i] = h
			continue
		}
		lines := strings.Split(h, "\n")
		for j, l := range lines {
			lines[j] = PadCenter(l, widths[i])
		}
		out[i] = strings.Join(lines, "\n")
	}
	return out
}

func (t Table) pad(cells []string, widths []int) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		if i >= len(widths) {
			out[i] = c
			continue
		}
		// 2行以上のセル（見出しなど）は、行ごとに幅をそろえる
		lines := strings.Split(c, "\n")
		for j, l := range lines {
			if i < len(t.Right) && t.Right[i] {
				lines[j] = PadLeft(l, widths[i])
			} else {
				lines[j] = Pad(l, widths[i])
			}
		}
		out[i] = strings.Join(lines, "\n")
	}
	return out
}

// ColumnWidths は、見出しと全行を通した各列の最大の表示幅を返す。
// 全角の文字は2桁として数える。
func ColumnWidths(headers []string, rows [][]string) []int {
	widths := make([]int, len(headers))
	for _, r := range append([][]string{headers}, rows...) {
		for i, c := range r {
			if i < len(widths) {
				widths[i] = max(widths[i], lipgloss.Width(c))
			}
		}
	}
	return widths
}

// Blocks は、見出しや表などの塊を空行で区切ってつなげる。
func Blocks(blocks ...string) string {
	return strings.Join(blocks, "\n\n")
}
