package sys

import (
	"bufio"
	"strings"
	"testing"
)

// A tool that redraws a progress bar with '\r' and never emits a newline must
// still produce tokens; with bufio's default ScanLines it produces none, which
// is what made the install's longest step look frozen.
func TestScanLinesOrCRSplitsProgressBars(t *testing.T) {
	in := "starting\n[==   ] 10%\r[====  ] 40%\r[======] 100%\rdone\n"
	sc := bufio.NewScanner(strings.NewReader(in))
	sc.Split(scanLinesOrCR)

	var got []string
	for sc.Scan() {
		got = append(got, sc.Text())
	}
	want := []string{"starting", "[==   ] 10%", "[====  ] 40%", "[======] 100%", "done"}
	if len(got) != len(want) {
		t.Fatalf("got %d tokens %q, want %d %q", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// CRLF is one terminator, not two - otherwise every Windows-style line would
// be followed by a spurious blank entry in the log.
func TestScanLinesOrCRTreatsCRLFAsOneTerminator(t *testing.T) {
	sc := bufio.NewScanner(strings.NewReader("one\r\ntwo\r\n"))
	sc.Split(scanLinesOrCR)
	var got []string
	for sc.Scan() {
		got = append(got, sc.Text())
	}
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("got %q, want [one two]", got)
	}
}
