// Package paths says where wtf keeps its caches. The native macOS app runs
// beside the Wails one while it is being built, with caches of its own so
// the two don't clear or overwrite each other's files.
package paths

import (
	"os"
	"sync/atomic"
)

var cacheRoot atomic.Pointer[string]

// SetCacheRoot replaces the user's cache directory for this process. Call it
// before building any service.
func SetCacheRoot(dir string) { cacheRoot.Store(&dir) }

// UserCacheDir is os.UserCacheDir, unless SetCacheRoot changed it.
func UserCacheDir() (string, error) {
	if r := cacheRoot.Load(); r != nil {
		return *r, nil
	}
	return os.UserCacheDir()
}
