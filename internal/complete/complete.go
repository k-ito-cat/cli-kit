// Package complete は、cli-kit の CLI の zsh 補完スクリプトを作る。
// 各 CLI はサブコマンドとフラグを Spec に書き、`<CLI> completion zsh` で出力する。
package complete

import (
	"fmt"
	"strings"
)

// Spec は1つの CLI の補完の定義。
type Spec struct {
	Program  string
	Commands []Command
	// Helpers は Args から呼ぶ zsh の関数の定義
	Helpers string
}

// Command はサブコマンド。
type Command struct {
	Name, Description string
	Flags             []Flag
	// Args は位置引数の _arguments の指定（例: "1::プロジェクト:_pdocs_projects"）
	Args []string
}

// Flag は --name 形式のフラグ。Values があれば値を取る。
type Flag struct {
	Name, Description string
	Values            []string
}

// FlagNames は、コマンド name のフラグ名（-- を除く）を返す。引数の解析で、補完と同じ定義を使うため。
func (s Spec) FlagNames(name string) []string {
	for _, c := range s.Commands {
		if c.Name == name {
			var out []string
			for _, f := range c.Flags {
				out = append(out, f.Name)
			}
			return out
		}
	}
	return nil
}

// Zsh は zsh の補完スクリプトを返す。compinit のあとに source して使う。
func (s Spec) Zsh() string {
	fn := "_" + strings.ReplaceAll(s.Program, "-", "_")
	var b strings.Builder
	if s.Helpers != "" {
		b.WriteString(strings.TrimSpace(s.Helpers) + "\n\n")
	}
	fmt.Fprintf(&b, "%s() {\n", fn)
	b.WriteString("  local -a commands\n  commands=(\n")
	for _, c := range s.Commands {
		fmt.Fprintf(&b, "    %s\n", quote(c.Name+":"+c.Description))
	}
	b.WriteString("  )\n")
	b.WriteString("  if (( CURRENT == 2 )); then\n    _describe 'コマンド' commands\n    return\n  fi\n")
	b.WriteString("  local cmd=$words[2]\n  shift words\n  (( CURRENT-- ))\n  case $cmd in\n")
	for _, c := range s.Commands {
		var specs []string
		for _, f := range c.Flags {
			spec := "--" + f.Name
			if len(f.Values) > 0 {
				spec += "=[" + escape(f.Description) + "]:値:(" + strings.Join(f.Values, " ") + ")"
			} else {
				spec += "[" + escape(f.Description) + "]"
			}
			specs = append(specs, quote(spec))
		}
		for _, a := range c.Args {
			specs = append(specs, quote(a))
		}
		fmt.Fprintf(&b, "    %s)\n", c.Name)
		if len(specs) > 0 {
			fmt.Fprintf(&b, "      _arguments \\\n        %s\n", strings.Join(specs, " \\\n        "))
		}
		b.WriteString("      ;;\n")
	}
	b.WriteString("  esac\n}\n")
	fmt.Fprintf(&b, "compdef %s %s\n", fn, s.Program)
	return b.String()
}

// GitUserCommand は、git-<name> を `git <Tab>` の候補に説明付きで加える zsh の設定を返す。
// 他の git-<name> の設定を消さないよう、既存の値に足す。
func GitUserCommand(name, description string) string {
	return fmt.Sprintf(`() {
  local -a cmds
  zstyle -a ':completion:*:*:git:*' user-commands cmds
  zstyle ':completion:*:*:git:*' user-commands $cmds %s
}
`, quote(name+":"+description))
}

// escape は _arguments の説明（[ ] の中）で特別な意味を持つ文字を逃がす。
func escape(s string) string {
	return strings.NewReplacer(`[`, `\[`, `]`, `\]`, `:`, `\:`).Replace(s)
}

// quote は zsh のシングルクォートで囲む。
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
