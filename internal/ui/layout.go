package ui

import (
	"fmt"
	"strings"
)

// 出力の組み立て方の決まり。どのコマンドも次の順で出す。
//
//	Title    コマンドと対象（先頭に1行）
//	Section  内容のまとまりごとの節（▌見出し  補足）
//	         本文は、数値を比べるときだけ Table、それ以外は Tree（ファイルなどでまとめた一覧）か
//	         Columns（罫線なしで桁をそろえた一覧）、状態の件数は Counts
//	Footer   区切り線と、1行のまとめ（最後に1つ）
//
// 字下げは2桁ずつ。節の本文は2桁下げて始める。

const indent = "  "

// Title は出力の先頭の行を返す。command は太字、context は薄く出す。
func Title(command string, context ...string) string {
	t := Paint(Strong, command)
	if len(context) > 0 {
		t += Paint(Muted, " · "+strings.Join(context, " · "))
	}
	return t
}

// Section は節の見出しと本文を返す。meta は見出しの後ろに添える補足（件数など。呼び出し側で色を付ける）。
func Section(title, meta, body string) string {
	head := Paint(Strong, "▌"+title)
	if meta != "" {
		head += "  " + meta
	}
	if body == "" {
		return head
	}
	return head + "\n" + body
}

// Count は件数を、節の補足に使う薄い字で返す。
func Count(n int) string {
	return Paint(Muted, fmt.Sprint(n))
}

// None は、節の中身が無いことを示す行を返す。
func None() string {
	return indent + Paint(Muted, "なし")
}

// Node は入れ子の一覧の1行と、その下の行。Text は呼び出し側で色や印を付ける。
type Node struct {
	Text     string
	Children []Node
}

// Tree は入れ子の一覧を、深さごとに2桁ずつ字下げして返す。枝の線は付けない。
func Tree(nodes ...Node) string {
	var lines []string
	var walk func(ns []Node, depth int)
	walk = func(ns []Node, depth int) {
		for _, n := range ns {
			lines = append(lines, strings.Repeat(indent, depth)+n.Text)
			walk(n.Children, depth+1)
		}
	}
	walk(nodes, 1)
	return strings.Join(lines, "\n")
}

// Group は一覧をまとめる見出し（ファイル名など）を返す。n が2以上なら件数を添える。
func Group(label string, n int) string {
	g := Paint(Path, label)
	if n > 1 {
		g += Paint(Muted, fmt.Sprintf("  (%d)", n))
	}
	return g
}

// Item は状態の印の付いた一覧の行を返す。note は後ろに薄く添える。
func Item(l Level, text, note string) string {
	s := Mark(l) + " " + text
	if note != "" {
		s += "  " + Paint(Muted, note)
	}
	return s
}

// Columns は、罫線なしで桁をそろえた一覧。
type Columns struct {
	Rows [][]string
	// Widths を渡すと、各列をその表示幅まで広げる。複数の一覧で桁をそろえたいときに使う
	Widths []int
	// Right は右に寄せる列（番号など）
	Right []bool
	// Tone は各セルの見た目を決める。nil なら Plain
	Tone func(row, col int) Tone
}

func (c Columns) String() string {
	var widths []int
	if len(c.Rows) > 0 {
		widths = ColumnWidths(make([]string, len(c.Rows[0])), c.Rows)
	}
	for i, w := range c.Widths {
		if i < len(widths) {
			widths[i] = max(widths[i], w)
		}
	}
	lines := make([]string, len(c.Rows))
	for i, r := range c.Rows {
		cells := make([]string, len(r))
		for j, v := range r {
			switch {
			case j >= len(widths):
			case j == len(r)-1 && !(j < len(c.Right) && c.Right[j]):
				// 最後の列は埋めない（行末に空白を残さない）
			case j < len(c.Right) && c.Right[j]:
				v = PadLeft(v, widths[j])
			default:
				v = Pad(v, widths[j])
			}
			if c.Tone != nil {
				v = Paint(c.Tone(i, j), v)
			}
			cells[j] = v
		}
		lines[i] = indent + strings.Join(cells, "  ")
	}
	return strings.Join(lines, "\n")
}

// CountItem は状態ごとの件数の1つ。
type CountItem struct {
	Level Level
	Label string
	N     int
}

// Counts は状態ごとの件数を1行に並べる。件数が0のものは出さない。
func Counts(items []CountItem) string {
	var parts []string
	for _, it := range items {
		if it.N > 0 {
			parts = append(parts, MarkText(it.Level, fmt.Sprintf("%s %d", it.Label, it.N)))
		}
	}
	if len(parts) == 0 {
		return Paint(Muted, "（エントリなし）")
	}
	return strings.Join(parts, "   ")
}

// Footer は区切り線と、1行のまとめを返す。parts は呼び出し側で色を付ける。
func Footer(parts ...string) string {
	return Rule() + "\n" + strings.Join(parts, "   ")
}
