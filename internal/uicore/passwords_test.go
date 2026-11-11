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

// More cases of the Svelte build's passwords tests.

func TestPasswordsUseOnlyTheChosenClasses(t *testing.T) {
	digits := PasswordOptions{Length: 30, Digits: true}
	if pw := GeneratePassword(digits, nil); len(pw) != 30 || strings.Trim(pw, PasswordDigits) != "" {
		t.Errorf("digits only: %q", pw)
	}
	var all strings.Builder
	for _, set := range PasswordClasses(NewPasswordOptions()) {
		all.WriteString(string(set))
	}
	if strings.ContainsAny(all.String(), "'\"\\` ") {
		t.Errorf("classes hold a quote, backslash, backtick or space: %q", all.String())
	}
}

func TestPasswordsClampTheLength(t *testing.T) {
	o := NewPasswordOptions()
	for in, want := range map[int]int{1: 4, 1000: 128} {
		o.Length = in
		if pw := GeneratePassword(o, nil); utf8.RuneCountInString(pw) != want {
			t.Errorf("length %d gave %d characters", in, utf8.RuneCountInString(pw))
		}
	}
}

func TestPasswordsDrawEvenly(t *testing.T) {
	var counts [10]int
	for range 20000 {
		counts[PasswordRandomInt(10, nil)]++
	}
	for i, c := range counts {
		if c <= 1700 {
			t.Errorf("%d drawn %d times of 20000", i, c)
		}
	}
}
