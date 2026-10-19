#include "shim.h"

struct _WtfTerm {
	VteTerminal parent;
	glong cols, rows;
};

G_DEFINE_FINAL_TYPE(WtfTerm, wtf_term, VTE_TYPE_TERMINAL)

enum { SIG_BEFORE_ALLOCATE, SIG_GRID_CHANGED, N_SIGNALS };
static guint signals[N_SIGNALS];

static void wtf_term_size_allocate(GtkWidget *w, int width, int height, int baseline)
{
	WtfTerm *self = WTF_TERM(w);
	// Before VTE rewraps, so Go can note where its marks are in logical lines.
	g_signal_emit(self, signals[SIG_BEFORE_ALLOCATE], 0, width);
	GTK_WIDGET_CLASS(wtf_term_parent_class)->size_allocate(w, width, height, baseline);
	glong cols = vte_terminal_get_column_count(VTE_TERMINAL(w));
	glong rows = vte_terminal_get_row_count(VTE_TERMINAL(w));
	if (cols != self->cols || rows != self->rows) {
		self->cols = cols;
		self->rows = rows;
		g_signal_emit(self, signals[SIG_GRID_CHANGED], 0, (gint)cols, (gint)rows);
	}
}

static void wtf_term_class_init(WtfTermClass *klass)
{
	GTK_WIDGET_CLASS(klass)->size_allocate = wtf_term_size_allocate;
	signals[SIG_BEFORE_ALLOCATE] = g_signal_new("before-allocate", G_TYPE_FROM_CLASS(klass),
		G_SIGNAL_RUN_LAST, 0, NULL, NULL, NULL, G_TYPE_NONE, 1, G_TYPE_INT);
	signals[SIG_GRID_CHANGED] = g_signal_new("grid-changed", G_TYPE_FROM_CLASS(klass),
		G_SIGNAL_RUN_LAST, 0, NULL, NULL, NULL, G_TYPE_NONE, 2, G_TYPE_INT, G_TYPE_INT);
}

static void wtf_term_init(WtfTerm *self) { (void)self; }

GtkWidget *wtf_term_new(void) { return g_object_new(wtf_term_get_type(), NULL); }

char *wtf_term_text(VteTerminal *t, glong start_row, glong start_col, glong end_row, glong end_col)
{
	gsize len = 0;
	return vte_terminal_get_text_range_format(t, VTE_FORMAT_TEXT, start_row, start_col,
		end_row, end_col, &len);
}
// Colors as "#rrggbb"; palette has 16 entries, or is NULL. Empty strings
// leave VTE's default.
void wtf_term_set_colors(VteTerminal *t, const char *fg, const char *bg, const char **palette,
	const char *cursor, const char *cursor_text, const char *sel_bg, const char *sel_fg)
{
	GdkRGBA f, b, pal[16], c;
	gdk_rgba_parse(&f, fg);
	gdk_rgba_parse(&b, bg);
	int n = 0;
	if (palette != NULL) {
		for (n = 0; n < 16 && gdk_rgba_parse(&pal[n], palette[n]); n++) {}
		if (n < 16) n = 0;
	}
	vte_terminal_set_colors(t, &f, &b, n ? pal : NULL, n);
	vte_terminal_set_color_cursor(t, gdk_rgba_parse(&c, cursor) ? &c : NULL);
	vte_terminal_set_color_cursor_foreground(t, gdk_rgba_parse(&c, cursor_text) ? &c : NULL);
	vte_terminal_set_color_highlight(t, gdk_rgba_parse(&c, sel_bg) ? &c : NULL);
	vte_terminal_set_color_highlight_foreground(t, gdk_rgba_parse(&c, sel_fg) ? &c : NULL);
}

// URLs, for Ctrl-click; returns the tag check_match_at reports.
int wtf_term_add_url_match(VteTerminal *t, const char *pattern)
{
	GError *err = NULL;
	// PCRE2_MULTILINE | PCRE2_UTF | PCRE2_NO_UTF_CHECK | PCRE2_CASELESS
	VteRegex *re = vte_regex_new_for_match(pattern, -1, 0x00000400u | 0x00080000u | 0x40000000u | 0x00000008u, &err);
	if (re == NULL) {
		g_warning("url regex: %s", err->message);
		g_error_free(err);
		return -1;
	}
	int tag = vte_terminal_match_add_regex(t, re, 0);
	vte_regex_unref(re);
	vte_terminal_match_set_cursor_name(t, tag, "pointer");
	return tag;
}

// The match or OSC 8 hyperlink at a point in the widget; free with g_free.
char *wtf_term_link_at(VteTerminal *t, double x, double y)
{
	char *link = vte_terminal_check_hyperlink_at(t, x, y);
	if (link != NULL) return link;
	int tag = -1;
	return vte_terminal_check_match_at(t, x, y, &tag);
}

void wtf_term_view(VteTerminal *t, double *lower, double *upper, double *value, double *page)
{
	GtkAdjustment *a = gtk_scrollable_get_vadjustment(GTK_SCROLLABLE(t));
	*lower = gtk_adjustment_get_lower(a);
	*upper = gtk_adjustment_get_upper(a);
	*value = gtk_adjustment_get_value(a);
	*page = gtk_adjustment_get_page_size(a);
}

void wtf_term_scroll_to(VteTerminal *t, double row)
{
	gtk_adjustment_set_value(gtk_scrollable_get_vadjustment(GTK_SCROLLABLE(t)), row);
}

GtkAdjustment *wtf_term_vadjustment(VteTerminal *t) { return gtk_scrollable_get_vadjustment(GTK_SCROLLABLE(t)); }
