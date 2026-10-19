// A VteTerminal subclass that tells Go when its grid changes size: VTE has
// no signal for that, and when Go owns the PTY it has to resize it itself.
#pragma once
#include <vte/vte.h>

G_DECLARE_FINAL_TYPE(WtfTerm, wtf_term, WTF, TERM, VteTerminal)

GtkWidget *wtf_term_new(void);

// Text from (start_row, start_col) to (end_row, end_col), end exclusive,
// absolute rows. Free with g_free.
char *wtf_term_text(VteTerminal *t, glong start_row, glong start_col, glong end_row, glong end_col);
void wtf_term_set_colors(VteTerminal *t, const char *fg, const char *bg, const char **palette,
	const char *cursor, const char *cursor_text, const char *sel_bg, const char *sel_fg);
int wtf_term_add_url_match(VteTerminal *t, const char *pattern);
char *wtf_term_link_at(VteTerminal *t, double x, double y);
// The vertical adjustment: its bounds, value (top row shown) and page size.
void wtf_term_view(VteTerminal *t, double *lower, double *upper, double *value, double *page);
void wtf_term_scroll_to(VteTerminal *t, double row);
GtkAdjustment *wtf_term_vadjustment(VteTerminal *t);
