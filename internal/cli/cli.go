// Package cli は、cli-kit の CLI が共通で使う cobra の設定を持つ。
// 使い方の表示を日本語にそろえ、エラーの出し方と終了コードの扱いを統一する。
package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"cli-kit/internal/ui"
)

// usageTemplate は、cobra の使い方の表示を日本語にしたもの。
const usageTemplate = `使い方:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} <コマンド>{{end}}{{if gt (len .Aliases) 0}}

別名:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

例:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

コマンド:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

オプション:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

共通のオプション:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

コマンドの使い方は "{{.CommandPath}} <コマンド> --help" で表示する。{{end}}
`

// ExitError は、エラーの表示なしで終了コードだけを返すためのもの（検査で問題が見つかった場合など）。
type ExitError struct{ Code int }

func (e ExitError) Error() string { return fmt.Sprintf("終了コード %d", e.Code) }

// UsageError は、引数の誤り。使い方を添えて、終了コード 2 で終わる。
type UsageError struct{ Msg string }

func (e UsageError) Error() string { return e.Msg }

// Root は、共通の設定をした最上位のコマンドを返す。
func Root(name, short, long string) *cobra.Command {
	root := &cobra.Command{
		Use:           name,
		Short:         short,
		Long:          long,
		SilenceUsage:  true, // 実行中のエラーで使い方まで出さない
		SilenceErrors: true, // エラーは Execute でまとめて出す
	}
	root.SetUsageTemplate(usageTemplate)
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return UsageError{translate(err.Error())}
	})
	return root
}

// translate は、cobra と pflag が返す英語のエラーを日本語にする。知らない形はそのまま返す。
func translate(msg string) string {
	for _, t := range []struct{ prefix, ja string }{
		{"unknown flag: ", "知らないオプション: "},
		{"unknown shorthand flag: ", "知らない短いオプション: "},
		{"flag needs an argument: ", "オプションに値がない: "},
		{"unknown command ", "知らないコマンド: "},
	} {
		if rest, ok := strings.CutPrefix(msg, t.prefix); ok {
			if t.prefix == "unknown command " {
				rest, _, _ = strings.Cut(rest, " for ")
			}
			return t.ja + rest
		}
	}
	return msg
}

// NoArgs は、位置引数を取らないコマンドの検査。
var NoArgs = RangeArgs(0, 0)

// RangeArgs は、位置引数の数が min 以上 max 以下かを検査する。
func RangeArgs(min, max int) cobra.PositionalArgs {
	return func(c *cobra.Command, args []string) error {
		if len(args) >= min && len(args) <= max {
			return nil
		}
		want := fmt.Sprintf("%d〜%d 個", min, max)
		if min == max {
			want = fmt.Sprintf("%d 個", min)
		}
		return UsageError{fmt.Sprintf("引数の数が合わない（%s。渡されたのは %d 個）", want, len(args))}
	}
}

// ColorFlag は、共通の --color を加え、実行の前に ui.SetColor へ渡す。
// 既定値は空にして、使い方の表示に英語の (default …) が出ないようにする。空は auto として扱う。
func ColorFlag(root *cobra.Command) {
	var mode string
	root.PersistentFlags().StringVar(&mode, "color", "", "`auto|always|never` 色付け。既定は auto（端末のときだけ。NO_COLOR が設定されていれば付けない）")
	_ = root.RegisterFlagCompletionFunc("color", cobra.FixedCompletions(ui.ColorModes, cobra.ShellCompDirectiveNoFileComp))
	prev := root.PersistentPreRunE
	root.PersistentPreRunE = func(c *cobra.Command, args []string) error {
		if mode == "" {
			mode = "auto"
		}
		if err := ui.SetColor(mode); err != nil {
			return UsageError{err.Error()}
		}
		if prev != nil {
			return prev(c, args)
		}
		return nil
	}
}

// Execute は root を実行し、終了する。
// エラーは「<名前>: <理由>」で出し、引数の誤りは使い方も添えて終了コード 2、それ以外は 1 にする。
func Execute(root *cobra.Command) {
	localize(root)
	cmd, err := root.ExecuteC()
	if err == nil {
		return
	}
	var exit ExitError
	if errors.As(err, &exit) {
		os.Exit(exit.Code)
	}
	var usage UsageError
	if !errors.As(err, &usage) && strings.HasPrefix(err.Error(), "unknown command ") {
		usage = UsageError{translate(err.Error())}
	}
	if usage.Msg != "" {
		fmt.Fprintf(os.Stderr, "%s: %s\n\n%s", root.Name(), usage.Msg, cmd.UsageString())
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "%s: %v\n", root.Name(), err)
	os.Exit(1)
}

// localize は、cobra が自動で足す help・completion と、-h の説明を日本語にする。
func localize(root *cobra.Command) {
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.InitDefaultHelpFlag()
		// 使い方の行に英語の [flags] を付けず、日本語で書く。-h を足したあとに判定する
		if !c.DisableFlagsInUseLine && c.HasAvailableFlags() {
			c.DisableFlagsInUseLine = true
			c.Use += " [オプション]"
		}
		if f := c.Flags().Lookup("help"); f != nil {
			f.Usage = "使い方を表示する"
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	for _, c := range root.Commands() {
		switch c.Name() {
		case "help":
			c.Short = "コマンドの使い方を表示する"
		case "completion":
			c.Short = "シェルの補完スクリプトを出力する"
			for _, sh := range c.Commands() {
				sh.Short = sh.Name() + " の補完スクリプトを出力する"
				if f := sh.Flags().Lookup("no-descriptions"); f != nil {
					f.Usage = "補完の候補に説明を付けない"
				}
			}
		}
	}
	walk(root)
}
