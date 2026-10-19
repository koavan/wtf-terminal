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
void wtf_term_set_colors(VteTerminal *t, const char *fg, const char *bg)
{
	GdkRGBA f, b;
	gdk_rgba_parse(&f, fg);
	gdk_rgba_parse(&b, bg);
	vte_terminal_set_colors(t, &f, &b, NULL, 0);
}
