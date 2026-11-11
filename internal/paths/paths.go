// Package paths says where wtf keeps its caches. A build can be given
// caches of its own (SetCacheRoot), so two builds side by side don't clear
// or overwrite each other's files.
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
