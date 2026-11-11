package uicore

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const manualDir = "../../manual"

var manualCtx = ManualContext{
	Mac: true,
	Label: func(id string) (string, bool) {
		l, ok := map[string]string{"sidebar.ssh": "Show SSH Hosts", "tab.close": "Close Tab"}[id]
		return l, ok
	},
	Shortcut: func(id string) string { return map[string]string{"sidebar.ssh": "⌘⇧S"}[id] },
}

// manualHTML renders a page as HTML, so these tests can match the macOS
// app's (macos/Tests/WtfCoreTests/ManualTests.swift).
func manualHTML(p ManualPage) string {
	runs := func(rs []ManualRun) string {
		var b strings.Builder
		for _, r := range rs {
			t := manualEscape(r.Text)
			if r.Code {
				t = "<code>" + t + "</code>"
			}
			if r.Key {
				t = "<kbd>" + t + "</kbd>"
			}
			if r.Italic {
				t = "<i>" + t + "</i>"
			}
			if r.Bold {
				t = "<b>" + t + "</b>"
			}
			if r.Topic != "" {
				t = `<a href="wtf-manual:` + r.Topic + `">` + t + "</a>"
			}
			if r.URL != "" {
				t = `<a href="` + manualEscape(r.URL) + `">` + t + "</a>"
			}
			b.WriteString(t)
		}
		return b.String()
	}
	var out []string
	for _, b := range p.Blocks {
		switch b.Kind {
		case ManualHeading:
			h := "h" + string(rune('0'+b.Level))
			out = append(out, "<"+h+">"+runs(b.Text)+"</"+h+">")
		case ManualParagraph:
			out = append(out, "<p>"+runs(b.Text)+"</p>")
		case ManualTip:
			out = append(out, "<aside>"+runs(b.Text)+"</aside>")
		case ManualCode:
			out = append(out, "<pre><code>"+manualEscape(b.Code)+"</code></pre>")
		case ManualList:
			tag := "ul"
			if b.Ordered {
				tag = "ol"
			}
			s := "<" + tag + ">"
			for _, it := range b.Items {
				s += "<li>" + runs(it) + "</li>"
			}
			out = append(out, s+"</"+tag+">")
		case ManualTable:
			s := "<table><thead><tr>"
			for _, c := range b.Rows[0] {
				s += "<th>" + runs(c) + "</th>"
			}
			s += "</tr></thead><tbody>"
			for _, row := range b.Rows[1:] {
				s += "<tr>"
				for _, c := range row {
					s += "<td>" + runs(c) + "</td>"
				}
				s += "</tr>"
			}
			out = append(out, s+"</tbody></table>")
		}
	}
	return strings.Join(out, "\n")
}

func expectContains(t *testing.T, html string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(html, w) {
			t.Errorf("missing %q in\n%s", w, html)
		}
	}
}

func TestManualEscapes(t *testing.T) {
	p := ManualParse("# A <b>title</b>\n\nRun `rm -rf <dir>` & <script>alert(1)</script>", manualCtx)
	html := manualHTML(p)
	expectContains(t, html, "<h1>A &lt;b&gt;title&lt;/b&gt;</h1>", "<code>rm -rf &lt;dir&gt;</code> &amp; &lt;script&gt;")
	if strings.Contains(html, "<script>") {
		t.Error("script tag")
	}
	// The runs hold the text as written.
	if got := p.Blocks[0].Text; len(got) != 1 || got[0].Text != "A <b>title</b>" || got[0].Bold {
		t.Errorf("heading runs %+v", got)
	}
	if got := p.Blocks[1].Text[1]; got != (ManualRun{Text: "rm -rf <dir>", Code: true}) {
		t.Errorf("code run %+v", got)
	}
}

func TestManualSubset(t *testing.T) {
	src := strings.Join([]string{"## Lists", "- one **bold**", "- two *italic*", "  continued", "", "1. first", "2. second", "",
		"| A | B |", "|---|---|", "| `x` | y |", "", "> A tip", "", "```", "echo <hi>", "```"}, "\n")
	html := manualHTML(ManualParse(src, manualCtx))
	expectContains(t, html,
		"<h2>Lists</h2>",
		"<ul><li>one <b>bold</b></li><li>two <i>italic</i> continued</li></ul>",
		"<ol><li>first</li><li>second</li></ol>",
		"<table><thead><tr><th>A</th><th>B</th></tr></thead><tbody><tr><td><code>x</code></td><td>y</td></tr></tbody></table>",
		"<aside>A tip</aside>",
		"<pre><code>echo &lt;hi&gt;</code></pre>")
}

func TestManualCommands(t *testing.T) {
	src := "Open {{cmd:sidebar.ssh}}, press {{key:sidebar.ssh}}, or {{cmd:tab.close}}. {{mac:⌘-click|other:Ctrl-click}} {{cmd:nope}}"
	p := ManualParse(src, manualCtx)
	expectContains(t, manualHTML(p), "<b>Show SSH Hosts</b> <kbd>⌘⇧S</kbd>, press <kbd>⌘⇧S</kbd>, or <b>Close Tab</b> (command palette). ⌘-click")
	expectList(t, "missing", p.Missing, []string{"nope"})
	other := manualCtx
	other.Mac = false
	expectContains(t, manualHTML(ManualParse(src, other)), "(command palette). Ctrl-click")
	// A shortcut-only mention of a command without one names it.
	expectContains(t, manualHTML(ManualParse("{{key:tab.close}}", manualCtx)), "<b>Close Tab</b> in the command palette")
}

func TestManualLinks(t *testing.T) {
	p := ManualParse("See [tabs](#tabs), [docs](https://example.com/x?a=1&b=2) and [bad](javascript:alert(1)).", manualCtx)
	expectList(t, "links", p.Links, []string{"tabs"})
	html := manualHTML(p)
	expectContains(t, html, `<a href="wtf-manual:tabs">tabs</a>`, `href="https://example.com/x?a=1&amp;b=2"`)
	if strings.Contains(html, "javascript") {
		t.Error("javascript link")
	}
	for _, r := range p.Blocks[0].Text {
		if r.Text == "docs" && r.URL != "https://example.com/x?a=1&b=2" {
			t.Errorf("URL %q", r.URL)
		}
	}
}

func TestManualPlainTextAndSearch(t *testing.T) {
	if got := ManualPlainText("# SSH **Keys**\n{{cmd:x}} use `ssh-add` [agent](#a)"); got != " ssh keys use ssh-add agent" {
		t.Errorf("plain %q", got)
	}
	pages := []ManualSearchPage{{ManualTopics[0], "open a tab"}, {ManualTopics[1], "groups of tabs"}}
	if got := ManualSearch("TAB groups", pages); len(got) != 1 || got[0].ID != "tabs" {
		t.Errorf("search %v", got)
	}
	if got := ManualSearch("", pages); len(got) != 2 {
		t.Errorf("empty search %v", got)
	}
}

func TestManualTopicsMatchPages(t *testing.T) {
	names, err := filepath.Glob(filepath.Join(manualDir, "*.md"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no pages in %s: %v", manualDir, err)
	}
	var files, ids []string
	for _, n := range names {
		files = append(files, strings.TrimSuffix(filepath.Base(n), ".md"))
	}
	for _, tp := range ManualTopics {
		ids = append(ids, tp.ID)
	}
	slices.Sort(files)
	sorted := slices.Sorted(slices.Values(ids))
	expectList(t, "pages", sorted, files)
	if len(slices.Compact(sorted)) != len(ManualTopics) {
		t.Error("a topic is listed twice")
	}
	// The same pages, order, groups and titles as the macOS app's list.
	swift, err := os.ReadFile("../../macos/Sources/WtfCore/Manual.swift")
	if err != nil {
		t.Fatal(err)
	}
	var listed []ManualTopic
	for _, m := range regexp.MustCompile(`\("([^"]*)", "([^"]*)", "([^"]*)"\)`).FindAllStringSubmatch(string(swift), -1) {
		listed = append(listed, ManualTopic{m[1], m[2], m[3]})
	}
	if !slices.Equal(listed, ManualTopics) {
		t.Errorf("Manual.swift lists\n%v\nManualTopics\n%v", listed, ManualTopics)
	}
	leftover := regexp.MustCompile(`\{\{(cmd|key|mac):`)
	ctx := ManualContext{Label: func(id string) (string, bool) { return id, true }, Shortcut: func(string) string { return "" }}
	// Every page links only to real pages, and starts with its own title.
	for _, tp := range ManualTopics {
		src, err := os.ReadFile(filepath.Join(manualDir, tp.ID+".md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(strings.TrimSpace(string(src)), "# ") {
			t.Errorf("%s doesn't start with a title", tp.ID)
		}
		p := ManualParse(string(src), ctx)
		for _, l := range p.Links {
			if _, ok := ManualTopicByID(l); !ok {
				t.Errorf("%s links to #%s", tp.ID, l)
			}
		}
		if html := manualHTML(p); leftover.MatchString(html) {
			t.Errorf("%s has markup left over", tp.ID)
		}
		if len(p.Blocks) == 0 || p.Blocks[0].Kind != ManualHeading || p.Blocks[0].Level != 1 {
			t.Errorf("%s: first block %+v", tp.ID, p.Blocks[0])
		}
	}
}

func TestManualHelpTopics(t *testing.T) {
	s := NewSidebarState()
	if got := ManualHelpTopic(s); got != "getting-started" {
		t.Errorf("closed: %s", got)
	}
	s.Open = true
	s.Tab = SidebarSSH
	s.SSHSection = "keys"
	if got := ManualHelpTopic(s); got != "ssh-keys" {
		t.Errorf("ssh keys: %s", got)
	}
	s.Tab = SidebarKube
	s.KubeView = "contexts"
	if got := ManualHelpTopic(s); got != "kube-contexts" {
		t.Errorf("contexts: %s", got)
	}
	s.Tab = SidebarFindings
	if got := ManualHelpTopic(s); got != "findings" {
		t.Errorf("findings: %s", got)
	}
	for _, tab := range SidebarTabs {
		if _, ok := ManualTopicByID(ManualSidebarTopic[tab]); !ok {
			t.Errorf("no page for %s", tab)
		}
	}
}

func TestMissingTools(t *testing.T) {
	for in, want := range map[string]string{
		"kubectl isn't installed (or isn't on the shell's PATH)": "kubectl",
		"the aws CLI isn't installed":                            "aws",
		"lsof isn't installed":                                   "lsof",
		"permission denied":                                      "",
		"":                                                       "",
	} {
		if got := MissingTool(in); got != want {
			t.Errorf("MissingTool(%q) = %q, want %q", in, got, want)
		}
	}
}
