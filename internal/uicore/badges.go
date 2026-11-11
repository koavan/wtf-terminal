// The address bar badges made from a shell's cloud and cluster context
// (infra.Resolve in internal/infra). A port of
// macos/Sources/WtfCore/Context.swift.
package uicore

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dayanraj/wtf/internal/infra"
)

// ClockState is how close a badge's credentials are to expiring.
type ClockState int

const (
	// ClockNone: no clock to show (nothing expires within a day).
	ClockNone ClockState = iota
	ClockOK
	// ClockSoon: under ten minutes left.
	ClockSoon
	ClockExpired
)

// BadgeClock is the time left on a badge's credentials; Text is "" when
// expired or none.
type BadgeClock struct {
	State ClockState
	Text  string
}

// Badge is one address bar badge.
type Badge struct {
	// kube, aws, gcp, tf, git, ssh.
	Kind  string
	Label string
	Title string
	Clock BadgeClock
}

// KindLabel is the kind as the badge shows it.
func (b Badge) KindLabel() string {
	if b.Kind == "kube" {
		return "K8S"
	}
	return strings.ToUpper(b.Kind)
}

// joinNonEmpty joins the parts that are not empty with sep.
func joinNonEmpty(sep string, parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}

func joinDot(parts ...string) string { return joinNonEmpty(" · ", parts...) }

var clusterNames = []*regexp.Regexp{
	regexp.MustCompile(`^arn:aws[\w-]*:eks:[^:]*:\d+:cluster/(.+)$`),
	regexp.MustCompile(`^gke_[^_]+_[^_]+_(.+)$`),
}

// ShortClusterName is the cluster part of an EKS ARN or a GKE name; others
// are unchanged.
func ShortClusterName(s string) string {
	for _, re := range clusterNames {
		if m := re.FindStringSubmatch(s); m != nil {
			return m[1]
		}
	}
	return s
}

// RelativeTime says when t is from now: "just now", "5 min ago", "in 3 h".
func RelativeTime(t, now time.Time) string {
	diff := t.Sub(now)
	a := math.Abs(diff.Seconds())
	var text string
	switch {
	case a < 45:
		return "just now"
	case a < 3600:
		text = fmt.Sprintf("%d min", int(math.Round(a/60)))
	case a < 86400:
		text = fmt.Sprintf("%d h", int(math.Round(a/3600)))
	default:
		text = fmt.Sprintf("%d d", int(math.Round(a/86400)))
	}
	if diff < 0 {
		return text + " ago"
	}
	return "in " + text
}

// TimeLeft is d as a clock shows it: "<1m", "42m", "3h 10m", "3h", "5d".
func TimeLeft(d time.Duration) string {
	m := d.Minutes()
	switch {
	case m < 1:
		return "<1m"
	case m < 60:
		return fmt.Sprintf("%dm", int(m))
	case m < 1440:
		h, mm := int(m/60), int(m)%60
		if mm == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh %dm", h, mm)
	}
	return fmt.Sprintf("%dd", int(m/1440))
}

func unixMilli(ms int64) time.Time { return time.UnixMilli(ms) }

// expiryClock is the clock for the soonest of list: none when nothing is
// due within a day.
func expiryClock(list []infra.Expiry, now time.Time) BadgeClock {
	if len(list) == 0 {
		return BadgeClock{}
	}
	at := list[0].ExpiresAt
	for _, e := range list[1:] {
		at = min(at, e.ExpiresAt)
	}
	left := unixMilli(at).Sub(now)
	switch {
	case left <= 0:
		return BadgeClock{State: ClockExpired}
	case left >= 24*time.Hour:
		return BadgeClock{}
	case left < 10*time.Minute:
		return BadgeClock{ClockSoon, TimeLeft(left)}
	}
	return BadgeClock{ClockOK, TimeLeft(left)}
}

// expiryLines are a badge title's lines about list, soonest first.
func expiryLines(list []infra.Expiry, now time.Time) []string {
	sorted := append([]infra.Expiry(nil), list...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ExpiresAt < sorted[j].ExpiresAt })
	lines := make([]string, 0, len(sorted))
	for _, e := range sorted {
		at := unixMilli(e.ExpiresAt)
		date := at.Format("Jan 2, 2006 at 3:04 PM")
		if at.After(now) {
			lines = append(lines, fmt.Sprintf("%s expires %s (in %s)", e.What, date, TimeLeft(at.Sub(now))))
		} else {
			lines = append(lines, fmt.Sprintf("%s expired %s (%s)", e.What, date, RelativeTime(at, now)))
		}
	}
	return lines
}

// BadgesOf is the badges for a context, in order: kube, aws, gcp, tf, git,
// ssh.
func BadgesOf(c infra.Context, now time.Time) []Badge {
	ofKind := func(k string) []infra.Expiry {
		var l []infra.Expiry
		for _, e := range c.Expiries {
			if e.Kind == k {
				l = append(l, e)
			}
		}
		return l
	}
	title := func(lines []string, k string) string {
		if k != "" {
			lines = append(lines, expiryLines(ofKind(k), now)...)
		}
		return joinNonEmpty("\n", lines...)
	}
	ifSet := func(v, text string) string {
		if v == "" {
			return ""
		}
		return text
	}
	var out []Badge
	if c.KubeContext != "" {
		cluster := c.KubeCluster
		if cluster == "" {
			cluster = c.KubeContext
		}
		out = append(out, Badge{
			Kind:  "kube",
			Label: joinDot(ShortClusterName(cluster), c.KubeNamespace),
			Title: title([]string{ifSet(c.KubeCluster, "Cluster: "+c.KubeCluster), "Context: " + c.KubeContext,
				ifSet(c.KubeNamespace, "Namespace: "+c.KubeNamespace)}, "kube"),
			Clock: expiryClock(ofKind("kube"), now),
		})
	}
	if c.AWSProfile != "" {
		account := c.AWSAccount
		if account == "" {
			account = c.AWSAccountNote
		}
		if account == "" {
			account = "unknown"
		}
		out = append(out, Badge{
			Kind:  "aws",
			Label: joinDot(c.AWSAccount, c.AWSProfile, c.AWSRegion),
			Title: title([]string{"Account: " + account, "Profile: " + c.AWSProfile, ifSet(c.AWSRegion, "Region: "+c.AWSRegion)}, "aws"),
			Clock: expiryClock(ofKind("aws"), now),
		})
	}
	if c.GCPProject != "" {
		out = append(out, Badge{Kind: "gcp", Label: c.GCPProject, Title: "Google Cloud project: " + c.GCPProject})
	}
	if c.TFWorkspace != "" {
		out = append(out, Badge{Kind: "tf", Label: c.TFWorkspace, Title: "Terraform workspace: " + c.TFWorkspace})
	}
	if c.Git != nil {
		out = append(out, gitBadge(*c.Git, now))
	}
	if ssh := ofKind("ssh"); len(ssh) > 0 {
		label := "certificate"
		if len(ssh) > 1 {
			label = fmt.Sprintf("%d certificates", len(ssh))
		}
		out = append(out, Badge{Kind: "ssh", Label: label,
			Title: title([]string{"Certificates in the shell's ssh-agent"}, "ssh"), Clock: expiryClock(ssh, now)})
	}
	return out
}

// gitBadge is the badge for a repository.
func gitBadge(g infra.Git, now time.Time) Badge {
	branch := g.Branch
	if branch == "" {
		branch = "(no commits)"
	}
	upstream := g.Upstream
	if remote, rest, ok := strings.Cut(upstream, "/"); ok && rest == branch {
		upstream = remote
	}
	var diff string
	if g.UpstreamGone {
		diff = "gone"
	} else {
		var ahead, behind string
		if g.Ahead > 0 {
			ahead = fmt.Sprintf("↑%d", g.Ahead)
		}
		if g.Behind > 0 {
			behind = fmt.Sprintf("↓%d", g.Behind)
		}
		diff = joinNonEmpty(" ", ahead, behind)
	}
	author := g.Identity
	if author == "" {
		author = g.AuthorName
	}
	if author == "" {
		author = g.AuthorEmail
	}
	to := ""
	if upstream != "" {
		to = "→ " + upstream
	}
	label := joinDot(author, joinNonEmpty(" ", branch, to, diff))

	commits := func(n int, what string) string {
		if n == 1 {
			return fmt.Sprintf("%d commit to %s", n, what)
		}
		return fmt.Sprintf("%d commits to %s", n, what)
	}
	var sync string
	switch {
	case g.Detached:
		sync = "Detached HEAD: not on a branch"
	case g.Upstream == "":
		sync = "No upstream branch: nothing to compare with"
	case g.UpstreamGone:
		sync = g.Upstream + " is gone (deleted on the remote, or not fetched yet)"
	case g.Ahead == 0 && g.Behind == 0:
		sync = "Up to date with " + g.Upstream
	default:
		var push, pull string
		if g.Ahead > 0 {
			push = commits(g.Ahead, "push")
		}
		if g.Behind > 0 {
			pull = commits(g.Behind, "pull")
		}
		sync = joinNonEmpty(", ", push, pull)
	}
	head := "Branch: " + branch
	if g.Detached {
		head = "Commit: " + branch
	}
	lines := []string{"Repository: " + g.Root, head}
	if g.Upstream != "" {
		lines = append(lines, "Upstream: "+g.Upstream)
	}
	if g.RemoteURL != "" {
		lines = append(lines, "Remote: "+g.Remote+" "+g.RemoteURL)
	}
	lines = append(lines, sync)
	if g.Upstream != "" && !g.UpstreamGone {
		asOf := "As of the last fetch"
		if g.FetchedAt > 0 {
			asOf += " (" + RelativeTime(unixMilli(g.FetchedAt), now) + ")"
		}
		lines = append(lines, asOf)
	}
	if g.Identity != "" {
		lines = append(lines, "Git identity: "+g.Identity+" (its SSH key only)")
	}
	email := ""
	if g.AuthorEmail != "" {
		email = "<" + g.AuthorEmail + ">"
	}
	if who := joinNonEmpty(" ", g.AuthorName, email); who != "" {
		lines = append(lines, "Author: "+who)
	} else {
		lines = append(lines, "Author: not set (git config user.name)")
	}
	return Badge{Kind: "git", Label: label, Title: joinNonEmpty("\n", lines...)}
}
