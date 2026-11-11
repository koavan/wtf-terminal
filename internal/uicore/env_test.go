package uicore

import (
	"maps"
	"testing"
)

func TestParseEnv(t *testing.T) {
	got := ParseEnv("AWS_PROFILE=dev\nnoequals\n=skip\nKUBECONFIG=/a=b\nAWS_PROFILE=prod\n")
	want := map[string]string{"AWS_PROFILE": "prod", "KUBECONFIG": "/a=b"}
	if !maps.Equal(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestTerminalReports(t *testing.T) {
	// With the cases of the Svelte build's broadcast tests.
	for _, s := range []string{"\x1b[12;40R", "\x1b[?62;c", "\x1b[I", "\x1b[<0;10;5M", "\x1b]11;rgb:0000/0000/0000\x07", "\x1bP1$r0m\x1b\\",
		"\x1b[?1;2c", "\x1b[>0;276;0c", "\x1b[O", "\x1b[M !!", "\x1b]11;rgb:0000/0000/0000\x1b\\", "\x1b[?2004;1$y"} {
		if !IsTerminalReport(s) {
			t.Errorf("%q isn't a report", s)
		}
	}
	for _, s := range []string{"ls\r", "\x1b[A", "a\x1b[12;40R", "ls -la\r", "a", "\x1b[1;5D", "\x03", "\x1b[200~pasted\x1b[201~", "\x1bOP"} {
		if IsTerminalReport(s) {
			t.Errorf("%q is a report", s)
		}
	}
}
