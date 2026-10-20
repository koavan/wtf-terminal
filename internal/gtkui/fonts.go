//go:build linux

package gtkui

// #cgo pkg-config: fontconfig glib-2.0
// #include <fontconfig/fontconfig.h>
// #include <glib.h>
// #include <stdlib.h>
//
// static int wtf_add_fonts(const char *dir) {
// 	return FcConfigAppFontAddDir(NULL, (const FcChar8 *)dir);
// }
//
// static void wtf_set_prgname(const char *name) { g_set_prgname(name); }
import "C"

import (
	"log"
	"os"
	"path/filepath"
	"unsafe"
)

// addBundledFonts makes the fonts wtf ships (JetBrains Mono, in
// share/wtf/fonts next to the binary's bin folder: /usr/share/wtf/fonts for
// the packages, inside the AppImage for that) available to this process
// only, like the macOS app's ATSApplicationFontsPath. Before GTK starts, so
// Pango's first font list has them. Not finding the folder is fine: the
// terminal falls back to the system's monospace font.
func addBundledFonts() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	dir := filepath.Join(filepath.Dir(filepath.Dir(exe)), "share", "wtf", "fonts")
	if _, err := os.Stat(dir); err != nil {
		return
	}
	cdir := C.CString(dir)
	defer C.free(unsafe.Pointer(cdir))
	if C.wtf_add_fonts(cdir) == 0 {
		log.Printf("couldn't add the fonts in %s", dir)
	} else {
		log.Printf("fonts from %s", dir)
	}
}

// setProgramName names the program after the app ID, so X11's WM_CLASS
// matches the desktop file (com.dayanraj.wtf.desktop) the way Wayland's
// app_id does, and docks show wtf's icon.
func setProgramName(name string) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	C.wtf_set_prgname(cname)
}
