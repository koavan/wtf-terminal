//go:build !darwin && !linux

package usage

const (
	memoryKind       = ""
	supportsDetached = false
)

func readStat(int) (stat, bool)                 { return stat{}, false }
func children(int) []int                        { return nil }
func scanDetached(int) ([]detachedProc, string) { return nil, "" }
