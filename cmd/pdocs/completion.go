package main

import (
	"cli-kit/internal/complete"
	"cli-kit/internal/ui"
)

var colorFlag = complete.Flag{Name: "color", Description: "色付け", Values: ui.ColorModes}

// spec はサブコマンドとフラグの定義。補完と引数の解析の両方で使う。
var spec = complete.Spec{
	Program: "pdocs",
	Commands: []complete.Command{
		{
			Name: "stat", Description: "記入漏れと未判断を洗い出す",
			Flags: []complete.Flag{
				{Name: "status", Description: "ステータスの件数"},
				{Name: "fill", Description: "項目の記入状況"},
				{Name: "open", Description: "未決事項"},
				colorFlag,
			},
			Args: []string{"1::プロジェクト:_pdocs_projects"},
		},
		{
			Name: "check", Description: "ルールからの逸脱を検査する",
			Flags: []complete.Flag{
				{Name: "readme", Description: "文書地図の漏れとリンク切れ"},
				{Name: "status", Description: "定義にないステータス値"},
				{Name: "template", Description: "見出し構成と雛形の一致"},
				{Name: "id", Description: "エントリの ID の有無・重複・参照先"},
				{Name: "sources", Description: "文章以外で定義するものの有無"},
				{Name: "quality", Description: "品質特性の水準と確認手段"},
				{Name: "items", Description: "雛形にない項目名"},
				colorFlag,
			},
		},
		{
			Name: "id", Description: "次に使う ID、または ID の定義と参照を表示する",
			Flags: []complete.Flag{colorFlag},
			Args:  []string{"1:プロジェクト:_pdocs_projects", "2::対象（接頭辞か ID）:"},
		},
		{
			Name: "completion", Description: "zsh の補完スクリプトを出力する",
			Args: []string{"1:シェル:(zsh)"},
		},
	},
	// プロジェクト名は、実行のたびに PDOCS_DIR の下から探す（pdocs の projects と同じ条件）
	Helpers: `
_pdocs_projects() {
  local -a names
  names=(${PDOCS_DIR}/*/README.md(N:h:t))
  compadd -- ${names:#_*}
}`,
}
