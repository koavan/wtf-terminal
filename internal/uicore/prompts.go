package uicore

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

// Tracks the prompts and commands a shell reports through OSC 133 (see
// internal/terminal/shellintegration) and turns each into a command block:
// its mark in the margin, its duration, and the command's text and output
// for copying, selecting and running again. A port of
// macos/Sources/WtfCore/PromptTracker.swift.

// Block is what's known about one command, for its menu.
type Block struct {
	ID   int
	Text string
	// ExitCode is nil while it runs.
	ExitCode   *int
	Duration   time.Duration
	StartedAt  time.Time
	FinishedAt time.Time
	// Its output is still in the scrollback.
	HasOutput bool
}

func (b Block) Running() bool { return b.ExitCode == nil }

// DurationLabel is the shortest command that gets a duration label.
const DurationLabel = time.Second

const maxRecords = 1000

type record struct {
	id     int
	prompt *Marker
	// Where the command's output starts and ends.
	output, end *Marker
	startedAt   time.Time
	text        string
	exitCode    *int
	duration    time.Duration
	finishedAt  time.Time
}

// PromptTracker turns OSC 133 and 633 marks into command blocks.
type PromptTracker struct {
	OnPrompt       func()
	OnCommandStart func(text string, id int)
	OnCommandEnd   func(id, exitCode int, d time.Duration)
	// OnEnv gets the variables that pick the shell's cloud and cluster
	// context, at each prompt.
	OnEnv func(string)
	// OnChange: marks changed, draw them again.
	OnChange func()

	g       Grid
	lines   *Lines
	now     func() time.Time
	records []*record
	current *record
	pending string
	nextID  int
}

func NewPromptTracker(g Grid, lines *Lines) *PromptTracker {
	return &PromptTracker{g: g, lines: lines, now: time.Now, nextID: 1}
}

func (p *PromptTracker) changed() {
	if p.OnChange != nil {
		p.OnChange()
	}
}

// Unescape undoes the shell scripts' escaping (\\ and \xNN).
func Unescape(s string) string {
	var out []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			if s[i+1] == '\\' {
				out = append(out, '\\')
				i++
				continue
			}
			if s[i+1] == 'x' && i+3 < len(s) {
				if v, err := strconv.ParseUint(s[i+2:i+4], 16, 8); err == nil {
					out = append(out, byte(v))
					i += 3
					continue
				}
			}
		}
		out = append(out, s[i])
	}
	return string(out)
}

// OSC633 handles "E;<command>" and "P;WtfEnv=<vars>".
func (p *PromptTracker) OSC633(data string) {
	switch {
	case strings.HasPrefix(data, "E;"):
		p.pending = Unescape(data[2:])
	case strings.HasPrefix(data, "P;WtfEnv="):
		if p.OnEnv != nil {
			p.OnEnv(Unescape(data[len("P;WtfEnv="):]))
		}
	}
}

// OSC133 handles a mark (data is what follows "133;"), with the cursor
// where the mark was.
func (p *PromptTracker) OSC133(data string, col, row int) {
	kind, arg, _ := strings.Cut(data, ";")
	switch kind {
	case "A":
		rec := &record{id: p.nextID, prompt: p.lines.Add(row, col)}
		p.nextID++
		// Prompts only move down: one at or above an earlier prompt means
		// the screen was cleared (clear, Ctrl+L), which erases lines in
		// place, so the old marks would linger on blank rows.
		k := 0
		for _, r := range p.records {
			if r.prompt.Row >= row {
				p.lines.Drop(r.prompt, r.output, r.end)
			} else {
				p.records[k] = r
				k++
			}
		}
		p.records = append(p.records[:k], rec)
		if n := len(p.records) - maxRecords; n > 0 {
			for _, r := range p.records[:n] {
				p.lines.Drop(r.prompt, r.output, r.end)
			}
			p.records = append(p.records[:0], p.records[n:]...)
		}
		p.current = rec
		if p.OnPrompt != nil {
			p.OnPrompt()
		}
		p.changed()
	case "C":
		if rec := p.current; rec != nil && rec.output == nil {
			rec.output = p.lines.Add(row, col)
			rec.startedAt = p.now()
			rec.text = p.pending
		}
		id := 0
		if p.current != nil {
			id = p.current.id
		}
		if p.OnCommandStart != nil {
			p.OnCommandStart(p.pending, id)
		}
		p.pending = ""
	case "D":
		rec := p.current
		if rec == nil || rec.output == nil || rec.end != nil {
			return
		}
		rec.end = p.lines.Add(row, col)
		code, _ := strconv.Atoi(arg)
		rec.exitCode = &code
		rec.finishedAt = p.now()
		rec.duration = rec.finishedAt.Sub(rec.startedAt)
		if p.OnCommandEnd != nil {
			p.OnCommandEnd(rec.id, code, rec.duration)
		}
		p.changed()
	}
}

// Queries

func (p *PromptTracker) record(id int) *record {
	for _, r := range p.records {
		if r.id == id {
			return r
		}
	}
	return nil
}

func (p *PromptTracker) block(r *record) Block {
	return Block{ID: r.id, Text: r.text, ExitCode: r.exitCode, Duration: r.duration, StartedAt: r.startedAt,
		FinishedAt: r.finishedAt, HasOutput: r.output.Valid()}
}

// RowBlock is a command whose prompt is on a given row.
type RowBlock struct {
	Row   int
	Block Block
}

// Blocks returns the commands whose prompts are on rows [from, to], for
// drawing marks.
func (p *PromptTracker) Blocks(from, to int) []RowBlock {
	var out []RowBlock
	for _, r := range p.records {
		if r.prompt.Valid() && r.prompt.Row >= from && r.prompt.Row <= to {
			out = append(out, RowBlock{r.prompt.Row, p.block(r)})
		}
	}
	return out
}

func (p *PromptTracker) Info(id int) (Block, bool) {
	if r := p.record(id); r != nil {
		return p.block(r), true
	}
	return Block{}, false
}

// Jump returns the row to scroll to for the previous (-1) or next (1)
// prompt from the top of the view; ok is false when there's none (for next:
// scroll to the bottom).
func (p *PromptTracker) Jump(direction, top int) (row int, ok bool) {
	if direction < 0 {
		for i := len(p.records) - 1; i >= 0; i-- {
			if r := p.records[i]; r.prompt.Valid() && r.prompt.Row < top {
				return r.prompt.Row, true
			}
		}
		return 0, false
	}
	for _, r := range p.records {
		if r.prompt.Valid() && r.prompt.Row > top {
			return r.prompt.Row, true
		}
	}
	return 0, false
}

// PromptRow is the row of a command's prompt; ok is false once it left the
// scrollback.
func (p *PromptTracker) PromptRow(id int) (int, bool) {
	if r := p.record(id); r != nil && r.prompt.Valid() {
		return r.prompt.Row, true
	}
	return 0, false
}

// BlockAt returns the command whose block holds a row: from its prompt to
// the next prompt. Only commands that ran count.
func (p *PromptTracker) BlockAt(row int) (int, bool) {
	var found *record
	for _, r := range p.records {
		if !r.prompt.Valid() {
			continue
		}
		if r.prompt.Row > row {
			break
		}
		found = r
	}
	if found == nil || found.output == nil {
		return 0, false
	}
	return found.id, true
}

// Running is the command running now, if any.
func (p *PromptTracker) Running() (int, bool) {
	if r := p.current; r != nil && r.output != nil && r.end == nil {
		return r.id, true
	}
	return 0, false
}

// LastFinished is the most recent finished command whose output is still
// in the scrollback.
func (p *PromptTracker) LastFinished() (int, bool) {
	for i := len(p.records) - 1; i >= 0; i-- {
		if r := p.records[i]; r.end != nil && r.output.Valid() {
			return r.id, true
		}
	}
	return 0, false
}

// OutputRows are the rows of a command's output: from its output mark to
// before its end mark, or the cursor while it runs. ok is false once it
// left the scrollback, or when it printed nothing.
func (p *PromptTracker) OutputRows(id int) (first, last int, ok bool) {
	r := p.record(id)
	if r == nil || !r.output.Valid() {
		return 0, 0, false
	}
	if r.end.Valid() {
		last = r.end.Row - 1
	} else {
		_, cursor := p.g.Cursor()
		last = cursor
	}
	return r.output.Row, last, last >= r.output.Row
}

// Output is a command's output so far; ok is false once it left the
// scrollback.
func (p *PromptTracker) Output(id int) (string, bool) {
	r := p.record(id)
	if r == nil || !r.output.Valid() {
		return "", false
	}
	first, last, ok := p.OutputRows(id)
	if !ok {
		return "", true
	}
	return strings.TrimRight(TextOf(p.g, first, last), "\n"), true
}

// BlockRows are the rows of a command's whole block, prompt to end, for
// highlighting.
func (p *PromptTracker) BlockRows(id int) (first, last int, ok bool) {
	r := p.record(id)
	if r == nil || !r.prompt.Valid() {
		return 0, 0, false
	}
	last = r.prompt.Row
	if _, l, ok := p.OutputRows(id); ok {
		last = max(last, l)
	}
	return r.prompt.Row, last, true
}

// Cleared forgets marks whose lines are gone, after the scrollback was
// cleared.
func (p *PromptTracker) Cleared() {
	p.lines.Prune()
	p.changed()
}

// TextOf returns rows [first, last] as text: soft-wrapped rows joined,
// trailing blanks dropped, one line end per line.
func TextOf(g Grid, first, last int) string {
	var b strings.Builder
	for row := first; row <= last; row++ {
		wraps := row < last && !g.EndsLine(row)
		text := g.Row(row)
		if !wraps {
			text = strings.TrimRight(text, " ")
		}
		b.WriteString(text)
		if !wraps {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// Transcript is every command that got as far as running, with its output
// (nil once it has left the scrollback), for a Markdown transcript.
func (p *PromptTracker) Transcript() []ExportBlock {
	var out []ExportBlock
	for _, r := range p.records {
		if r.output == nil {
			continue
		}
		b := ExportBlock{Text: r.text, StartedAt: r.startedAt, ExitCode: r.exitCode}
		if r.exitCode != nil {
			d := r.duration
			b.Duration = &d
		}
		if o, ok := p.Output(r.id); ok {
			b.Output = &o
		}
		out = append(out, b)
	}
	return out
}

// RecentIDs are the ids of the last n finished commands, oldest first.
func (p *PromptTracker) RecentIDs(n int) []int {
	var out []int
	for i := len(p.records) - 1; i >= 0 && len(out) < n; i-- {
		if r := p.records[i]; r.exitCode != nil && r.text != "" {
			out = append(out, r.id)
		}
	}
	slices.Reverse(out)
	return out
}

// Recent is the last n finished commands, with what's left of their
// output in the scrollback, oldest first.
func (p *PromptTracker) Recent(n int) []ExportBlock {
	var out []ExportBlock
	for i := len(p.records) - 1; i >= 0 && len(out) < n; i-- {
		r := p.records[i]
		if r.exitCode == nil || r.text == "" {
			continue
		}
		// A command that printed nothing still counts.
		o, _ := p.Output(r.id)
		out = append(out, ExportBlock{Text: r.text, StartedAt: r.startedAt, ExitCode: r.exitCode, Output: &o})
	}
	slices.Reverse(out)
	return out
}
