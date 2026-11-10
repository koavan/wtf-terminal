package notify

import "testing"

func TestHasCapability(t *testing.T) {
	for out, want := range map[string]bool{
		"(['actions', 'body', 'sound', 'persistence'],)\n": true,
		"(['actions', 'body', 'body-markup'],)\n":          false,
		"(['sounds'],)": false,
		"":              false,
	} {
		if got := hasCapability(out, "sound"); got != want {
			t.Errorf("%q: %v", out, got)
		}
	}
}
