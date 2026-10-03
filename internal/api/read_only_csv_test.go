package api

import "testing"

func TestReadOnlyCSVTextEscapesFormulasAfterWhitespace(t *testing.T) {
	for _, value := range []string{"=SUM(A1)", "+cmd", "-cmd", "@cmd", "\t =cmd", "\r\n+cmd"} {
		if safeCSVValue(value) != "'"+value {
			t.Fatalf("formula was not escaped: %q", value)
		}
	}
	for _, value := range []string{"", " \t\r\n", "Team", "模型"} {
		if safeCSVValue(value) != value {
			t.Fatalf("safe label changed: %q", value)
		}
	}
}
