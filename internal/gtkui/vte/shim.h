// A VteTerminal subclass that tells Go when its grid changes size: VTE has
// no signal for that, and when Go owns the PTY it has to resize it itself.
#pragma once
#include <vte/vte.h>

G_DECLARE_FINAL_TYPE(WtfTerm, wtf_term, WTF, TERM, VteTerminal)

GtkWidget *wtf_term_new(void);

// Text from (start_row, start_col) to (end_row, end_col), end exclusive,
// absolute rows. Free with g_free.
char *wtf_term_text(VteTerminal *t, glong start_row, glong start_col, glong end_row, glong end_col);
void wtf_term_set_colors(VteTerminal *t, const char *fg, const char *bg);
