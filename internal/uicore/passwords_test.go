package uicore

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPasswords(t *testing.T) {
	o := NewPasswordOptions()
	if got := PasswordEntropyBits(o); got != 155 {
		t.Errorf("entropy %d", got)
	}
	o.Lower, o.Upper, o.Symbols, o.Length = false, false, false, 10
	if got := PasswordEntropyBits(o); got != 33 {
		t.Errorf("digits entropy %d", got)
	}
	all := NewPasswordOptions()
	all.Length = 4
	for range 50 {
		pw := GeneratePassword(all, nil)
		if utf8.RuneCountInString(pw) != 4 {
			t.Fatalf("length of %q", pw)
		}
		for _, set := range []string{PasswordLower, PasswordUpper, PasswordDigits, PasswordSymbols} {
			if !strings.ContainsAny(pw, set) {
				t.Fatalf("%q lacks one of %q", pw, set)
			}
		}
	}
	all.AvoidAmbiguous = true
	all.Length = 128
	if pw := GeneratePassword(all, nil); strings.ContainsAny(pw, PasswordLookAlikes) || len(pw) != 128 {
		t.Errorf("look-alikes in %q", pw)
	}
	none := PasswordOptions{Length: 24}
	if GeneratePassword(none, nil) != "" || PasswordEntropyBits(none) != 0 {
		t.Error("no classes")
	}
	// Values past the last whole multiple of n are drawn again.
	draws := []uint32{4_294_967_295, 13}
	next := func() uint32 { v := draws[0]; draws = draws[1:]; return v }
	if got := PasswordRandomInt(10, next); got != 3 {
		t.Errorf("random int %d", got)
	}
}
