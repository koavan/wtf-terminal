package uicore

// konamiCode is ↑ ↑ ↓ ↓ ← → ← → B A.
var konamiCode = [...]string{"up", "up", "down", "down", "left", "right", "left", "right", "b", "a"}

// Konami follows the keys typed toward the Konami code.
type Konami struct{ n int }

// Key takes the next key ("up", "down", "left", "right", "a", "b", or
// anything else) and reports whether it finished the code.
func (k *Konami) Key(name string) bool {
	switch {
	case name == konamiCode[k.n]:
		k.n++
	case name != "up":
		k.n = 0
	case k.n != 2:
		// A third ↑ still leaves the last two to go on from.
		k.n = 1
	}
	if k.n == len(konamiCode) {
		k.n = 0
		return true
	}
	return false
}
