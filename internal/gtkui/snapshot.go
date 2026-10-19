//go:build linux

package gtkui

// #cgo pkg-config: gtk4
// #include <gtk/gtk.h>
// #include <stdint.h>
// #include <stdlib.h>
//
// // Renders a widget the way GTK paints it and saves it as a PNG. In C:
// // gotk4 v0.3.1 handles GskRenderNode as a GObject, which it isn't.
// static gboolean wtf_snapshot_png(uintptr_t widget, const char *path) {
// 	GtkWidget *w = (GtkWidget *)widget;
// 	int width = gtk_widget_get_width(w), height = gtk_widget_get_height(w);
// 	GdkPaintable *p = gtk_widget_paintable_new(w);
// 	GtkSnapshot *s = gtk_snapshot_new();
// 	gdk_paintable_snapshot(p, GDK_SNAPSHOT(s), width, height);
// 	g_object_unref(p);
// 	GskRenderNode *node = gtk_snapshot_free_to_node(s);
// 	if (node == NULL) return FALSE;
// 	graphene_rect_t viewport = GRAPHENE_RECT_INIT(0, 0, width, height);
// 	GdkTexture *t = gsk_renderer_render_texture(gtk_native_get_renderer(gtk_widget_get_native(w)), node, &viewport);
// 	gsk_render_node_unref(node);
// 	gboolean ok = gdk_texture_save_to_png(t, path);
// 	g_object_unref(t);
// 	return ok;
// }
import "C"

import (
	"fmt"
	"runtime"
	"unsafe"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
)

// snapshot saves the window as a PNG.
func (w *Window) snapshot(path string) (string, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	ok := C.wtf_snapshot_png(C.uintptr_t(coreglib.InternObject(w.win).Native()), cpath) != 0
	runtime.KeepAlive(w.win)
	if !ok {
		return "", fmt.Errorf("couldn't render or write %s", path)
	}
	return fmt.Sprintf("%s (%d×%d)", path, w.win.Width(), w.win.Height()), nil
}
