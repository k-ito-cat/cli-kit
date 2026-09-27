package recap

import (
	"testing"
	"time"
)

// テストは、入力と期待する結果を表にして並べる（テーブル駆動）。ケースを足すときは表に1行足す。
func TestParseDate(t *testing.T) {
	now := time.Date(2026, 9, 27, 18, 0, 0, 0, time.Local)
	tests := []struct {
		name    string
		in      string
		want    time.Time
		wantErr bool
	}{
		{"年月日", "2026-09-25", time.Date(2026, 9, 25, 0, 0, 0, 0, time.Local), false},
		{"月日は今年", "09-25", time.Date(2026, 9, 25, 0, 0, 0, 0, time.Local), false},
		{"昨日", "yesterday", time.Date(2026, 9, 26, 0, 0, 0, 0, time.Local), false},
		{"読めない", "2026/09/25", time.Time{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDate(now, tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseDate(%q) のエラー = %v、期待はエラーあり=%v", tt.in, err, tt.wantErr)
			}
			if !got.Equal(tt.want) {
				t.Errorf("parseDate(%q) = %v、期待は %v", tt.in, got, tt.want)
			}
		})
	}
}
