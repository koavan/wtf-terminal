package uicore

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dayanraj/wtf/internal/infra"
)

// The cases of the Svelte build's infra tests.

func TestShortClusterNames(t *testing.T) {
	for in, want := range map[string]string{
		"arn:aws:eks:us-east-1:123456789012:cluster/payments": "payments",
		"gke_my-proj_europe-west1-b_shop":                     "shop",
		"minikube":                                            "minikube",
	} {
		if got := ShortClusterName(in); got != want {
			t.Errorf("ShortClusterName(%q) = %q, want %q", in, got, want)
		}
	}
}

func kindsAndLabels(b []Badge) [][2]string {
	var out [][2]string
	for _, x := range b {
		out = append(out, [2]string{x.Kind, x.Label})
	}
	return out
}

func TestBadgesShowClusterAccountProjectWorkspace(t *testing.T) {
	b := BadgesOf(infra.Context{
		KubeContext: "work", KubeCluster: "arn:aws:eks:us-east-1:123456789012:cluster/payments", KubeNamespace: "api",
		AWSProfile: "admin", AWSAccount: "210987654321", AWSRegion: "us-east-1", GCPProject: "shop", TFWorkspace: "staging",
	}, time.Now())
	want := [][2]string{{"kube", "payments · api"}, {"aws", "210987654321 · admin · us-east-1"}, {"gcp", "shop"}, {"tf", "staging"}}
	if got := kindsAndLabels(b); !reflect.DeepEqual(got, want) {
		t.Fatalf("badges = %v, want %v", got, want)
	}
	if !strings.Contains(b[0].Title, "Context: work") || !strings.Contains(b[0].Title, "Cluster: arn:aws:eks:us-east-1:123456789012:cluster/payments") {
		t.Errorf("kube title = %q", b[0].Title)
	}
	if b[0].KindLabel() != "K8S" {
		t.Errorf("KindLabel = %q", b[0].KindLabel())
	}
}

func TestBadgesFallBackAndLeaveOutTheUnknown(t *testing.T) {
	b := BadgesOf(infra.Context{KubeContext: "minikube", AWSProfile: "static", AWSAccountNote: "asking AWS…"}, time.Now())
	if got := kindsAndLabels(b); !reflect.DeepEqual(got, [][2]string{{"kube", "minikube"}, {"aws", "static"}}) {
		t.Fatalf("badges = %v", got)
	}
	if !strings.Contains(b[1].Title, "Account: asking AWS…") {
		t.Errorf("aws title = %q", b[1].Title)
	}
	if b := BadgesOf(infra.Context{}, time.Now()); len(b) != 0 {
		t.Errorf("empty context gave %v", b)
	}
}

func testGit(f func(*infra.Git)) infra.Git {
	g := infra.Git{Root: "/src/engine", Branch: "main", Upstream: "origin/main", Remote: "origin",
		RemoteURL: "git@github.com:ada/engine.git", AuthorName: "Ada", AuthorEmail: "ada@example.com"}
	if f != nil {
		f(&g)
	}
	return g
}

func TestGitBadge(t *testing.T) {
	now := time.UnixMilli(10 * 3_600_000)
	b := gitBadge(testGit(func(g *infra.Git) {
		g.Ahead, g.Behind, g.FetchedAt = 2, 1, now.Add(-3*time.Hour).UnixMilli()
	}), now)
	if b.Label != "Ada · main → origin ↑2 ↓1" {
		t.Errorf("label = %q", b.Label)
	}
	for _, s := range []string{"Remote: origin git@github.com:ada/engine.git", "2 commits to push, 1 commit to pull",
		"As of the last fetch (3 h ago)", "Author: Ada <ada@example.com>"} {
		if !strings.Contains(b.Title, s) {
			t.Errorf("title %q lacks %q", b.Title, s)
		}
	}
	labels := []struct {
		f    func(*infra.Git)
		want string
	}{
		{func(g *infra.Git) { g.Upstream = "origin/release/2.0" }, "Ada · main → origin/release/2.0"},
		{func(g *infra.Git) { g.Upstream = "upstream/main" }, "Ada · main → upstream"},
		{func(g *infra.Git) { g.Upstream = "" }, "Ada · main"},
		{func(g *infra.Git) { g.UpstreamGone = true }, "Ada · main → origin gone"},
	}
	for _, c := range labels {
		if got := gitBadge(testGit(c.f), now).Label; got != c.want {
			t.Errorf("label = %q, want %q", got, c.want)
		}
	}
	titles := []struct {
		f    func(*infra.Git)
		want string
	}{
		{func(g *infra.Git) { g.Upstream = "" }, "No upstream branch"},
		{func(g *infra.Git) { g.Upstream, g.Detached, g.Branch = "", true, "abc1234" }, "Commit: abc1234"},
		{func(g *infra.Git) { g.AuthorName, g.AuthorEmail = "", "" }, "Author: not set"},
	}
	for _, c := range titles {
		if got := gitBadge(testGit(c.f), now).Title; !strings.Contains(got, c.want) {
			t.Errorf("title %q lacks %q", got, c.want)
		}
	}
	w := gitBadge(testGit(func(g *infra.Git) { g.Identity = "work" }), now)
	if w.Label != "work · main → origin" || !strings.Contains(w.Title, "Git identity: work") || !strings.Contains(w.Title, "Author: Ada <ada@example.com>") {
		t.Errorf("identity badge = %+v", w)
	}
	g := testGit(nil)
	if b := BadgesOf(infra.Context{Git: &g}, time.Now()); len(b) != 1 || b[0].Kind != "git" {
		t.Errorf("git context gave %v", b)
	}
}

func TestExpiryClocks(t *testing.T) {
	now := time.Unix(1_791_806_400, 0)
	for d, want := range map[time.Duration]string{
		30 * time.Second:     "<1m",
		42 * time.Minute:     "42m",
		190 * time.Minute:    "3h 10m",
		120 * time.Minute:    "2h",
		5 * 24 * time.Hour:   "5d",
		-5 * time.Minute:     "<1m",
		1439 * time.Minute:   "23h 59m",
		90*time.Second + 1e8: "1m",
	} {
		if got := TimeLeft(d); got != want {
			t.Errorf("TimeLeft(%v) = %q, want %q", d, got, want)
		}
	}
	e := func(m time.Duration) []infra.Expiry {
		return []infra.Expiry{{Kind: "aws", What: "x", ExpiresAt: now.Add(m * time.Minute).UnixMilli()}}
	}
	for _, c := range []struct {
		list []infra.Expiry
		want BadgeClock
	}{
		{nil, BadgeClock{}},
		{e(25 * 60), BadgeClock{}},
		{e(42), BadgeClock{ClockOK, "42m"}},
		{e(9), BadgeClock{ClockSoon, "9m"}},
		{e(-1), BadgeClock{State: ClockExpired}},
	} {
		if got := expiryClock(c.list, now); got != c.want {
			t.Errorf("expiryClock(%v) = %+v, want %+v", c.list, got, c.want)
		}
	}

	min := func(m int64) int64 { return now.Add(time.Duration(m) * time.Minute).UnixMilli() }
	b := BadgesOf(infra.Context{
		KubeContext: "dev", AWSProfile: "sso", AWSAccount: "111111111111",
		Expiries: []infra.Expiry{
			{Kind: "kube", What: "Kubernetes token of user dev", ExpiresAt: min(300)},
			{Kind: "kube", What: "Kubernetes client certificate of user dev", ExpiresAt: min(30)},
			{Kind: "aws", What: "AWS SSO session acme", ExpiresAt: min(-5)},
			{Kind: "ssh", What: "SSH certificate ada@corp (ada)", ExpiresAt: min(9 * 1440)},
		},
	}, now)
	matches := func(pattern, s string) bool { return regexp.MustCompile(pattern).MatchString(s) }
	if b[0].Clock != (BadgeClock{ClockOK, "30m"}) {
		t.Errorf("kube clock = %+v", b[0].Clock)
	}
	if !matches(`client certificate of user dev expires .* \(in 30m\)\n.*token of user dev expires .* \(in 5h\)$`, b[0].Title) {
		t.Errorf("kube title = %q", b[0].Title)
	}
	if b[1].Clock.State != ClockExpired {
		t.Errorf("aws clock = %+v", b[1].Clock)
	}
	if !matches(`AWS SSO session acme expired .* \(5 min ago\)$`, b[1].Title) {
		t.Errorf("aws title = %q", b[1].Title)
	}
	if b[2].Kind != "ssh" || b[2].Label != "certificate" || b[2].Clock != (BadgeClock{}) {
		t.Errorf("ssh badge = %+v", b[2])
	}
	if !matches(`ada@corp \(ada\) expires .* \(in 9d\)$`, b[2].Title) {
		t.Errorf("ssh title = %q", b[2].Title)
	}
	if c := BadgesOf(infra.Context{AWSProfile: "p"}, now)[0].Clock; c != (BadgeClock{}) {
		t.Errorf("no-expiry clock = %+v", c)
	}
}

func TestRelativeTime(t *testing.T) {
	now := time.Unix(1_791_806_400, 0)
	for d, want := range map[time.Duration]string{
		0:                   "just now",
		-44 * time.Second:   "just now",
		-5 * time.Minute:    "5 min ago",
		90 * time.Second:    "in 2 min",
		3 * time.Hour:       "in 3 h",
		-150 * time.Minute:  "3 h ago",
		-36 * time.Hour:     "2 d ago",
		10 * 24 * time.Hour: "in 10 d",
	} {
		if got := RelativeTime(now.Add(d), now); got != want {
			t.Errorf("RelativeTime(%v) = %q, want %q", d, got, want)
		}
	}
}
