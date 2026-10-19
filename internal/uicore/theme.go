package uicore

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// RGB is a color as 0–255 components and an alpha of 0–1.
type RGB struct{ R, G, B, A float64 }

// ParseRGB reads "#rrggbb" or "#rrggbbaa", as the themes write them.
func ParseRGB(hex string) (RGB, bool) {
	h := strings.TrimSpace(hex)
	if !strings.HasPrefix(h, "#") || len(h) != 7 && len(h) != 9 {
		return RGB{}, false
	}
	n, err := strconv.ParseUint(h[1:], 16, 32)
	if err != nil {
		return RGB{}, false
	}
	if len(h) == 7 {
		return RGB{float64(n >> 16 & 255), float64(n >> 8 & 255), float64(n & 255), 1}, true
	}
	return RGB{float64(n >> 24 & 255), float64(n >> 16 & 255), float64(n >> 8 & 255), float64(n&255) / 255}, true
}

func rgbOr(hex string, fallback RGB) RGB {
	if c, ok := ParseRGB(hex); ok {
		return c
	}
	return fallback
}

// Mix goes part of the way from c to o: 0 is c, 1 is o.
func (c RGB) Mix(o RGB, t float64) RGB {
	return RGB{c.R + (o.R-c.R)*t, c.G + (o.G-c.G)*t, c.B + (o.B-c.B)*t, c.A + (o.A-c.A)*t}
}

func (c RGB) Hex() string {
	return fmt.Sprintf("#%02x%02x%02x", int(math.Round(c.R)), int(math.Round(c.G)), int(math.Round(c.B)))
}

// Luminance is the WCAG relative luminance, 0–1.
func (c RGB) Luminance() float64 {
	lin := func(v float64) float64 {
		s := v / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

func (c RGB) chroma() float64 { return (max(c.R, c.G, c.B) - min(c.R, c.G, c.B)) / 255 }

func (c RGB) contrast(o RGB) float64 {
	hi, lo := max(c.Luminance(), o.Luminance()), min(c.Luminance(), o.Luminance())
	return (hi + 0.05) / (lo + 0.05)
}

// CoverColor is what secrets are covered with while hidden: a solid bar
// between the theme's text and background (coverColor in lib/secretmask.ts).
func CoverColor(foreground, background string) RGB {
	c := rgbOr(foreground, RGB{128, 128, 128, 1}).Mix(rgbOr(background, RGB{0, 0, 0, 1}), 0.6)
	return RGB{math.Round(c.R), math.Round(c.G), math.Round(c.B), 1}
}

// Accent is the UI accent: the cursor color when colorful and readable,
// else the theme's blue, then magenta (accentFor in lib/themes.ts).
func Accent(cursor, background string, palette []string, dark bool) RGB {
	bg := rgbOr(background, RGB{0, 0, 0, 1})
	candidates := []string{cursor}
	for _, i := range []int{12, 4, 13, 5} {
		if i < len(palette) {
			candidates = append(candidates, palette[i])
		}
	}
	for _, s := range candidates {
		if c, ok := ParseRGB(s); ok && c.chroma() > 0.25 && c.contrast(bg) >= 2.5 {
			return RGB{c.R, c.G, c.B, 1}
		}
	}
	if dark {
		return RGB{0x8d, 0x9c, 0xff, 1}
	}
	return RGB{0x4c, 0x5b, 0xd4, 1}
}

// Duration is "0.4s", "12s", "3m 5s" or "2h 10m" (formatDuration in
// frontend/src/lib/toast.svelte.ts).
func Duration(d time.Duration) string {
	ms := float64(d) / float64(time.Millisecond)
	if ms < 1000 {
		return fmt.Sprintf("%.1fs", ms/1000)
	}
	s := int(math.Round(ms / 1000))
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	m := s / 60
	if m < 60 {
		return fmt.Sprintf("%dm %ds", m, s%60)
	}
	return fmt.Sprintf("%dh %dm", m/60, m%60)
}
