package uicore

import (
	"regexp"
	"strconv"
	"strings"
)

// The in-app manual: the same Markdown pages as the Wails build
// (frontend/src/manual/*.md), parsed into blocks of styled runs for a
// native renderer. A port of lib/manual.ts and manual/index.ts
// (macos/Sources/WtfCore/Manual.swift, which renders HTML instead). The
// Markdown is a small subset — just what the pages use: headings (#, ##,
// ###), paragraphs, - and 1. lists, > tips, | tables |, ``` code blocks,
// `code`, **bold**, *italics*, and links: [text](#topic) to another page,
// [text](https://…) out of the app; any other link is just its text.
// Shortcuts come from the command registry, so the manual can't drift from
// the app:
//
//	{{cmd:sidebar.ssh}}  → the command's name and its shortcut
//	{{key:sidebar.ssh}}  → just the shortcut
//	{{mac:⌘-click|other:Ctrl-click}} → text for this platform

// ManualTopic is a page: its group in the contents, the id links use, and
// its title.
type ManualTopic struct{ Group, ID, Title string }

// ManualTopics lists the pages in reading order, as manual/index.ts does.
var ManualTopics = []ManualTopic{
	{"Basics", "getting-started", "Welcome to wtf"}, {"Basics", "tabs", "Tabs and tab groups"},
	{"Basics", "panes", "Split panes"}, {"Basics", "shell-integration", "Commands, prompts and output"},
	{"Basics", "logs", "Logs in the output"}, {"Basics", "inspect", "Tokens and certificates"},
	{"Basics", "secrets", "Hide Secrets (screen sharing)"}, {"Basics", "recording", "Recording and saving a terminal"},
	{"Basics", "find", "Find"}, {"Basics", "context-badges", "Address bar and context badges"},
	{"Basics", "broadcast", "Broadcast input"}, {"Basics", "snippets", "Snippets and runbooks"},
	{"Basics", "workspaces", "Workspaces"},
	{"Side pane", "side-pane", "The side pane"}, {"Side pane", "files", "Files"}, {"Side pane", "network", "Network"},
	{"Side pane", "processes", "Processes"}, {"Side pane", "history", "History"}, {"Side pane", "pins", "Pinned output"},
	{"Side pane", "share", "Share & Record"}, {"Side pane", "generate", "Generate: SSH keys and passwords"},
	{"Side pane", "findings", "Findings: security scans"},
	{"SSH", "ssh-hosts", "SSH hosts"}, {"SSH", "ssh-keys", "SSH keys and ssh-agent"},
	{"SSH", "git-identities", "Git identities (more than one GitHub user)"}, {"SSH", "ssh-host-keys", "Host keys (known_hosts)"},
	{"SSH", "ssh-tunnels", "SSH tunnels"}, {"SSH", "ssh-files", "Copying files over SSH"},
	{"Cloud", "aws", "AWS profiles"}, {"Cloud", "kubernetes", "Kubernetes: resources"}, {"Cloud", "kube-contexts", "Kubernetes: contexts"},
	{"Reference", "tools", "Command-line tools"}, {"Reference", "settings", "Settings and themes"},
	{"Reference", "usage", "Resource usage (wtf’s CPU and memory)"}, {"Reference", "sessions", "Sessions and windows"},
	{"Reference", "shortcuts", "Keyboard shortcuts"}, {"Reference", "privacy", "Privacy and safety"},
	{"Reference", "troubleshooting", "Troubleshooting"},
}

// ManualTopicByID finds a page.
func ManualTopicByID(id string) (ManualTopic, bool) {
	for _, t := range ManualTopics {
		if t.ID == id {
			return t, true
		}
	}
	return ManualTopic{}, false
}

// ManualSidebarTopic is the page for each side-pane tab, for its ? button.
var ManualSidebarTopic = map[SidebarTab]string{
	SidebarFiles: "files", SidebarSSH: "ssh-hosts", SidebarAWS: "aws", SidebarKube: "kubernetes", SidebarSnippets: "snippets",
	SidebarWorkspaces: "workspaces", SidebarNetwork: "network", SidebarProcesses: "processes", SidebarHistory: "history",
	SidebarPins: "pins", SidebarShare: "share", SidebarGenerate: "generate", SidebarFindings: "findings",
}

// ManualHelpTopic is help for what's in front: the side pane's tab (and
// section), or the welcome page.
func ManualHelpTopic(s SidebarState) string {
	if !s.Open {
		return "getting-started"
	}
	if s.Tab == SidebarSSH {
		if id, ok := map[string]string{"hosts": "ssh-hosts", "keys": "ssh-keys", "git": "git-identities", "tunnels": "ssh-tunnels"}[s.SSHSection]; ok {
			return id
		}
		return "ssh-hosts"
	}
	if s.Tab == SidebarKube && s.KubeView == "contexts" {
		return "kube-contexts"
	}
	if id, ok := ManualSidebarTopic[s.Tab]; ok {
		return id
	}
	return "getting-started"
}

// ManualContext names the commands the pages mention.
type ManualContext struct {
	// Mac picks the mac: side of {{mac:…|other:…}}.
	Mac bool
	// Label is a command's name; false if there's no such command.
	Label func(id string) (string, bool)
	// Shortcut is a command's shortcut, or "" if it has none.
	Shortcut func(id string) string
}

// ManualRun is a run of text in one style. Text is plain (not escaped).
type ManualRun struct {
	Text         string
	Bold, Italic bool
	// Code is `code`; Key is a keyboard shortcut.
	Code, Key bool
	// Topic is the page a link goes to; URL an https:// link out of the app.
	Topic, URL string
}

// ManualBlockKind is what a block is.
type ManualBlockKind int

const (
	ManualHeading ManualBlockKind = iota
	ManualParagraph
	ManualList
	ManualTable
	// ManualTip is a > tip.
	ManualTip
	ManualCode
)

// ManualBlock is one block of a page; only the fields for its Kind are set.
type ManualBlock struct {
	Kind ManualBlockKind
	// Heading: 1–3.
	Level int
	// List: 1. rather than -.
	Ordered bool
	// Heading, paragraph and tip.
	Text []ManualRun
	// List items.
	Items [][]ManualRun
	// Table rows of cells; the first row is the header.
	Rows [][][]ManualRun
	// Code block.
	Code string
}

// ManualPage is a parsed page.
type ManualPage struct {
	Blocks []ManualBlock
	// Command ids the page mentions that don't exist (tests keep this empty).
	Missing []string
	// Topic ids the page links to.
	Links []string
}

// manualEscape makes text safe for the markup inline works in; manualRuns
// undoes it.
func manualEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

var manualUnescape = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", `"`, "&amp;", "&")

// manualReplace replaces each match of a pattern with what with makes of
// its groups ("" for a group that didn't take part).
func manualReplace(s string, re *regexp.Regexp, with func(g []string) string) string {
	var out strings.Builder
	last := 0
	for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
		out.WriteString(s[last:m[0]])
		g := make([]string, len(m)/2)
		for i := range g {
			if m[2*i] >= 0 {
				g[i] = s[m[2*i]:m[2*i+1]]
			}
		}
		out.WriteString(with(g))
		last = m[1]
	}
	out.WriteString(s[last:])
	return out.String()
}

var (
	manualCodeSpan  = regexp.MustCompile("`([^`]+)`")
	manualCommand   = regexp.MustCompile(`\{\{(cmd|key):([\w.]+)\}\}`)
	manualPlatform  = regexp.MustCompile(`\{\{mac:([^|}]*)\|other:([^}]*)\}\}`)
	manualBold      = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	manualItalic    = regexp.MustCompile(`(^|[\s(])\*([^*\s][^*]*)\*`)
	manualLink      = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	manualCodeMark  = regexp.MustCompile("\x00(\\d+)\x00")
	manualHeadingRe = regexp.MustCompile(`^#{1,3}\s+`)
	manualTableRule = regexp.MustCompile(`^:?-+:?$`)
	manualListStart = regexp.MustCompile(`^(-|\d+\.)\s+`)
	manualListItem  = regexp.MustCompile(`^\s*(-|\d+\.)\s+(.*)$`)
)

// manualInline marks up one already-split piece of text, as the HTML the
// web build makes (all text escaped, the only tags its own), then reads
// that into runs.
func manualInline(text string, ctx ManualContext, out *ManualPage) []ManualRun {
	// Code spans first, so nothing inside them is touched.
	var codes []string
	s := manualReplace(text, manualCodeSpan, func(g []string) string {
		codes = append(codes, "<code>"+manualEscape(g[1])+"</code>")
		return "\x00" + strconv.Itoa(len(codes)-1) + "\x00"
	})
	s = manualEscape(s)
	s = manualReplace(s, manualCommand, func(g []string) string {
		kind, id := g[1], g[2]
		label, ok := "", false
		if ctx.Label != nil {
			label, ok = ctx.Label(id)
		}
		if !ok {
			out.Missing = append(out.Missing, id)
			return "<b>" + manualEscape(id) + "</b>"
		}
		key := ""
		if ctx.Shortcut != nil {
			key = ctx.Shortcut(id)
		}
		kbd := "<kbd>" + manualEscape(key) + "</kbd>"
		switch {
		case kind == "key" && key != "":
			return kbd
		case kind == "key":
			return "<b>" + manualEscape(label) + "</b> in the command palette"
		case key != "":
			return "<b>" + manualEscape(label) + "</b> " + kbd
		}
		return "<b>" + manualEscape(label) + "</b> (command palette)"
	})
	s = manualReplace(s, manualPlatform, func(g []string) string {
		if ctx.Mac {
			return g[1]
		}
		return g[2]
	})
	s = manualReplace(s, manualBold, func(g []string) string { return "<b>" + g[1] + "</b>" })
	s = manualReplace(s, manualItalic, func(g []string) string { return g[1] + "<i>" + g[2] + "</i>" })
	s = manualReplace(s, manualLink, func(g []string) string {
		label, href := g[1], g[2]
		if id, ok := strings.CutPrefix(href, "#"); ok {
			out.Links = append(out.Links, id)
			return `<a href="#` + id + `">` + label + "</a>"
		}
		if strings.HasPrefix(href, "https://") {
			return `<a href="` + href + `">` + label + "</a>"
		}
		return label
	})
	s = manualReplace(s, manualCodeMark, func(g []string) string {
		i, _ := strconv.Atoi(g[1])
		return codes[i]
	})
	return manualRuns(s)
}

// manualRuns reads inline's markup into runs.
func manualRuns(s string) []ManualRun {
	var runs []ManualRun
	var style ManualRun
	bold, italic := 0, 0
	text := func(t string) {
		if t == "" {
			return
		}
		r := style
		r.Bold, r.Italic, r.Text = bold > 0, italic > 0, manualUnescape.Replace(t)
		if n := len(runs); n > 0 {
			if last := runs[n-1]; last.Bold == r.Bold && last.Italic == r.Italic && last.Code == r.Code &&
				last.Key == r.Key && last.Topic == r.Topic && last.URL == r.URL {
				runs[n-1].Text += r.Text
				return
			}
		}
		runs = append(runs, r)
	}
	for s != "" {
		lt := strings.IndexByte(s, '<')
		if lt < 0 {
			text(s)
			break
		}
		text(s[:lt])
		gt := strings.IndexByte(s[lt:], '>')
		if gt < 0 {
			text(s[lt:])
			break
		}
		tag := s[lt+1 : lt+gt]
		s = s[lt+gt+1:]
		switch {
		case tag == "b":
			bold++
		case tag == "/b":
			bold--
		case tag == "i":
			italic++
		case tag == "/i":
			italic--
		case tag == "code" || tag == "/code":
			style.Code = tag == "code"
		case tag == "kbd" || tag == "/kbd":
			style.Key = tag == "kbd"
		case tag == "/a":
			style.Topic, style.URL = "", ""
		case strings.HasPrefix(tag, `a href="`):
			href := manualUnescape.Replace(strings.TrimSuffix(strings.TrimPrefix(tag, `a href="`), `"`))
			if id, ok := strings.CutPrefix(href, "#"); ok {
				style.Topic = id
			} else {
				style.URL = href
			}
		}
	}
	return runs
}

func manualIndented(l string) bool {
	return strings.HasPrefix(l, "  ") || strings.HasPrefix(l, "\t\t") || strings.HasPrefix(l, " \t") || strings.HasPrefix(l, "\t ")
}

// ManualParse reads a page's Markdown.
func ManualParse(src string, ctx ManualContext) ManualPage {
	var out ManualPage
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	trim := func(s string) string { return strings.Trim(s, " \t") }
	var para []string
	flush := func() {
		if len(para) > 0 {
			out.Blocks = append(out.Blocks, ManualBlock{Kind: ManualParagraph, Text: manualInline(strings.Join(para, " "), ctx, &out)})
		}
		para = nil
	}
	for i := 0; i < len(lines); {
		t := trim(lines[i])
		switch {
		case strings.HasPrefix(t, "```"):
			flush()
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(trim(lines[i]), "```"); i++ {
				code = append(code, lines[i])
			}
			out.Blocks = append(out.Blocks, ManualBlock{Kind: ManualCode, Code: strings.Join(code, "\n")})
			i++
		case t == "":
			flush()
			i++
		case manualHeadingRe.MatchString(t):
			flush()
			mark := manualHeadingRe.FindString(t)
			out.Blocks = append(out.Blocks, ManualBlock{Kind: ManualHeading, Level: strings.Count(mark, "#"), Text: manualInline(t[len(mark):], ctx, &out)})
			i++
		case strings.HasPrefix(t, "|"):
			flush()
			var rows [][]string
			for ; i < len(lines) && strings.HasPrefix(trim(lines[i]), "|"); i++ {
				row := strings.TrimSuffix(strings.TrimPrefix(trim(lines[i]), "|"), "|")
				cells := strings.Split(row, "|")
				rule := true
				for j := range cells {
					cells[j] = trim(cells[j])
					rule = rule && manualTableRule.MatchString(cells[j])
				}
				if !rule {
					rows = append(rows, cells)
				}
			}
			if len(rows) == 0 {
				continue
			}
			b := ManualBlock{Kind: ManualTable}
			for _, row := range rows {
				var cells [][]ManualRun
				for _, c := range row {
					cells = append(cells, manualInline(c, ctx, &out))
				}
				b.Rows = append(b.Rows, cells)
			}
			out.Blocks = append(out.Blocks, b)
		case manualListStart.MatchString(t):
			flush()
			ordered := !strings.HasPrefix(t, "-")
			var items []string
			for ; i < len(lines); i++ {
				l := lines[i]
				if m := manualListItem.FindStringSubmatch(l); m != nil && (m[1] != "-") == ordered {
					items = append(items, m[2])
				} else if trim(l) != "" && manualIndented(l) && len(items) > 0 {
					items[len(items)-1] += " " + trim(l)
				} else {
					break
				}
			}
			b := ManualBlock{Kind: ManualList, Ordered: ordered}
			for _, it := range items {
				b.Items = append(b.Items, manualInline(it, ctx, &out))
			}
			out.Blocks = append(out.Blocks, b)
		case strings.HasPrefix(t, ">"):
			flush()
			var tip []string
			for ; i < len(lines) && strings.HasPrefix(trim(lines[i]), ">"); i++ {
				l := trim(lines[i])[1:]
				tip = append(tip, strings.TrimPrefix(l, " "))
			}
			out.Blocks = append(out.Blocks, ManualBlock{Kind: ManualTip, Text: manualInline(strings.Join(tip, " "), ctx, &out)})
		default:
			para = append(para, t)
			i++
		}
	}
	flush()
	return out
}

var (
	manualCommandAny = regexp.MustCompile(`\{\{(cmd|key):[\w.]+\}\}`)
	manualMarks      = regexp.MustCompile("[#*`>|]")
	manualAnyLink    = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	manualSpace      = regexp.MustCompile(`\s+`)
)

// ManualPlainText is a page's words without markup, for searching.
func ManualPlainText(src string) string {
	s := manualReplace(src, manualCommandAny, func([]string) string { return " " })
	s = manualReplace(s, manualPlatform, func(g []string) string { return g[1] + " " + g[2] })
	s = manualMarks.ReplaceAllString(s, " ")
	s = manualReplace(s, manualAnyLink, func(g []string) string { return g[1] })
	s = manualSpace.ReplaceAllString(s, " ")
	return strings.ToLower(s)
}

// ManualSearchPage is a page to search: its topic and ManualPlainText.
type ManualSearchPage struct {
	Topic ManualTopic
	Text  string
}

// ManualSearch is the pages holding every word of the query (all pages for
// an empty one).
func ManualSearch(query string, pages []ManualSearchPage) []ManualTopic {
	words := strings.Fields(strings.ToLower(query))
	var out []ManualTopic
	for _, p := range pages {
		hay := strings.ToLower(p.Topic.Title) + " " + p.Text
		all := true
		for _, w := range words {
			all = all && strings.Contains(hay, w)
		}
		if all {
			out = append(out, p.Topic)
		}
	}
	return out
}

var missingTools = []struct {
	re   *regexp.Regexp
	tool string
}{
	{regexp.MustCompile(`\bkubectl isn't installed\b`), "kubectl"},
	{regexp.MustCompile(`\baws CLI isn't installed\b`), "aws"},
	{regexp.MustCompile(`\blsof isn't installed\b`), "lsof"},
}

// MissingTool is the tool an error says is missing (for its Install…
// link), or "".
func MissingTool(err string) string {
	for _, m := range missingTools {
		if m.re.MatchString(err) {
			return m.tool
		}
	}
	return ""
}
