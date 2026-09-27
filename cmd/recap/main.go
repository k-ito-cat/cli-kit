// recap は、自分のコミットをリポジトリ横断で振り返る。変わったファイルを選んで開き、AI に要点をまとめさせて記録する。
// 本体は internal/recap。
package main

import "cli-kit/internal/recap"

func main() {
	recap.Main()
}
