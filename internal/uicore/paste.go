package uicore

import (
	"fmt"
	"strings"
)

// Multi-line paste confirmation: text with more than one line, or with
// control characters, is shown before it reaches the shell. Decided by the
// text alone — never by guessing what a command does.
// A port of macos/Sources/WtfCore/Paste.swift.

// PasteFacts describes a paste.
type PasteFacts struct {
	// Lines it has; a final newline doesn't start another.
	Lines int
	// Whether it ends with a newline, which runs the last line without
	// bracketed paste.
	FinalNewline bool
	// Control characters in it.
	Controls int
}

// NewPasteFacts describes text.
func NewPasteFacts(text string) PasteFacts {
	f := PasteFacts{FinalNewline: hasFinalNewline(text)}
	body := text
	if f.FinalNewline {
		body = WithoutFinalNewline(text)
	}
	// \r\n, \n and \r each end a line.
	f.Lines = len(strings.Split(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n"), "\n"))
	for _, r := range text {
		if isControl(r) {
			f.Controls++
		}
	}
	return f
}

// NeedsConfirm says whether a paste is shown first: more than one line, a
// final newline, or a control character.
func (f PasteFacts) NeedsConfirm() bool { return f.Lines > 1 || f.FinalNewline || f.Controls > 0 }

// isControl: C0 controls other than tab and newlines, DEL, and C1 controls
// (ESC [ as one character).
func isControl(r rune) bool {
	switch {
	case r <= 0x08, r == 0x0B, r == 0x0C, r >= 0x0E && r <= 0x1F, r == 0x7F, r >= 0x80 && r <= 0x9F:
		return true
	}
	return false
}

func hasFinalNewline(text string) bool {
	return strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\r")
}

// WithoutFinalNewline returns the text without its final newline, so the
// shell waits for Enter.
func WithoutFinalNewline(text string) string {
	if t, ok := strings.CutSuffix(text, "\r\n"); ok {
		return t
	}
	if hasFinalNewline(text) {
		return text[:len(text)-1]
	}
	return text
}

// ShowControls makes control characters visible: ␛ for ESC, ␀–␟ for the
// rest, ␡ for DEL, ⟨9B⟩ for C1.
func ShowControls(text string) string {
	var out strings.Builder
	for _, r := range text {
		switch {
		case !isControl(r):
			out.WriteRune(r)
		case r < 0x20:
			out.WriteRune(0x2400 + r)
		case r == 0x7F:
			out.WriteRune('␡')
		default:
			fmt.Fprintf(&out, "⟨%X⟩", r)
		}
	}
	return out.String()
}
