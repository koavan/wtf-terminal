package uicore

import (
	"crypto/rand"
	"encoding/binary"
	"math"
	"strings"
)

// The password generator: chosen character classes, at least one of each,
// from the system's secure random source. Port of lib/passwords.ts
// (macos/Sources/WtfCore/Passwords.swift).

const (
	PasswordMinLength = 4
	PasswordMaxLength = 128

	PasswordLower      = "abcdefghijklmnopqrstuvwxyz"
	PasswordUpper      = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	PasswordDigits     = "0123456789"
	PasswordSymbols    = "!#$%&()*+,-./:;<=>?@[]^_{|}~"
	PasswordLookAlikes = "Il1O0o|"
)

type PasswordOptions struct {
	Length                        int
	Lower, Upper, Digits, Symbols bool
	AvoidAmbiguous                bool
}

// NewPasswordOptions is the generator's starting point: 24 characters of
// every class.
func NewPasswordOptions() PasswordOptions {
	return PasswordOptions{Length: 24, Lower: true, Upper: true, Digits: true, Symbols: true}
}

// PasswordClasses are the chosen character classes, look-alikes left out
// when asked; empty classes are dropped.
func PasswordClasses(o PasswordOptions) [][]rune {
	var out [][]rune
	for _, c := range []struct {
		on  bool
		set string
	}{{o.Lower, PasswordLower}, {o.Upper, PasswordUpper}, {o.Digits, PasswordDigits}, {o.Symbols, PasswordSymbols}} {
		if !c.on {
			continue
		}
		var set []rune
		for _, r := range c.set {
			if !o.AvoidAmbiguous || !strings.ContainsRune(PasswordLookAlikes, r) {
				set = append(set, r)
			}
		}
		if len(set) > 0 {
			out = append(out, set)
		}
	}
	return out
}

func PasswordClamp(n int) int { return min(PasswordMaxLength, max(PasswordMinLength, n)) }

// SecureRandom is a uniform 32-bit value from crypto/rand.
func SecureRandom() uint32 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand doesn't fail on supported systems
	}
	return binary.LittleEndian.Uint32(b[:])
}

// PasswordRandomInt is 0..<n without modulo bias; random nil uses
// SecureRandom.
func PasswordRandomInt(n int, random func() uint32) int {
	if random == nil {
		random = SecureRandom
	}
	limit := (uint64(1) << 32) / uint64(n) * uint64(n)
	for {
		if v := uint64(random()); v < limit {
			return int(v % uint64(n))
		}
	}
}

// GeneratePassword makes one password; random nil uses SecureRandom.
func GeneratePassword(o PasswordOptions, random func() uint32) string {
	sets := PasswordClasses(o)
	if len(sets) == 0 {
		return ""
	}
	length := PasswordClamp(o.Length)
	var pool, out []rune
	for _, s := range sets {
		pool = append(pool, s...)
		out = append(out, s[PasswordRandomInt(len(s), random)])
	}
	for len(out) < length {
		out = append(out, pool[PasswordRandomInt(len(pool), random)])
	}
	for i := len(out) - 1; i > 0; i-- {
		j := PasswordRandomInt(i+1, random)
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

// PasswordEntropyBits is length × log2(pool size), rounded down.
func PasswordEntropyBits(o PasswordOptions) int {
	pool := 0
	for _, s := range PasswordClasses(o) {
		pool += len(s)
	}
	if pool == 0 {
		return 0
	}
	return int(math.Floor(float64(PasswordClamp(o.Length)) * math.Log2(float64(pool))))
}
