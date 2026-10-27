package uicore

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/dayanraj/wtf/internal/apiclient"
)

// The API tab's request, apart from its views: titles, which headers hold
// credentials, what is kept in the saved session, and the query string
// shown as a list of parameters. A port of macos/Sources/WtfCore/Api.swift.

// APIMethods are the methods offered in the method menu.
var APIMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

// APIBlank is a new tab's request.
func APIBlank() apiclient.Request {
	return apiclient.Request{Method: "GET", Headers: []apiclient.Header{}}
}

func apiOrBlank(r *apiclient.Request) apiclient.Request {
	if r == nil {
		return APIBlank()
	}
	return *r
}

// APITitle is what the tab is called: the method and where it goes.
func APITitle(r *apiclient.Request) string {
	if r == nil {
		return "API"
	}
	target := strings.TrimSpace(r.URL)
	if _, after, ok := strings.Cut(target, "://"); ok {
		target = after
	}
	if i := strings.IndexAny(target, "?#"); i >= 0 {
		target = target[:i]
	}
	if len(target) > 1 {
		target = strings.TrimSuffix(target, "/")
	}
	if target == "" {
		return "API"
	}
	m := strings.ToUpper(r.Method)
	if m == "" {
		m = "GET"
	}
	return m + " " + target
}

// APIIsSecret: header names whose values are credentials by definition or
// by name, shown masked and never written to the saved session.
func APIIsSecret(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "authorization", "proxy-authorization", "cookie", "set-cookie":
		return true
	}
	for _, w := range []string{"token", "secret", "password", "api-key", "apikey", "api_key", "session"} {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

// APIForSaving is the request as written to the saved session: credentials
// left out.
func APIForSaving(r apiclient.Request) apiclient.Request {
	hs := make([]apiclient.Header, len(r.Headers))
	for i, h := range r.Headers {
		if APIIsSecret(h.Name) {
			h.Value = ""
		}
		hs[i] = h
	}
	r.Headers = hs
	return r
}

// splitURL is the URL before its "?", the query, and the fragment.
func splitURL(u string) (base string, query *string, fragment *string) {
	if i := strings.IndexByte(u, '#'); i >= 0 {
		f := u[i+1:]
		fragment = &f
		u = u[:i]
	}
	if i := strings.IndexByte(u, '?'); i >= 0 {
		q := u[i+1:]
		return u[:i], &q, fragment
	}
	return u, nil, fragment
}

func unescape(s string) string {
	if v, err := url.PathUnescape(s); err == nil {
		return v
	}
	return s
}

// APIParams is the URL's query as name/value pairs, decoded.
func APIParams(u string) []apiclient.Header {
	out := []apiclient.Header{}
	_, q, _ := splitURL(u)
	if q == nil {
		return out
	}
	for _, part := range strings.Split(*q, "&") {
		if part == "" {
			continue
		}
		name, value, _ := strings.Cut(part, "=")
		out = append(out, apiclient.Header{Name: unescape(name), Value: unescape(value)})
	}
	return out
}

// APIURL is the URL with its query rebuilt from params (off ones left out).
func APIURL(u string, params []apiclient.Header) string {
	base, _, fragment := splitURL(u)
	var parts []string
	for _, p := range params {
		if p.Off || (p.Name == "" && p.Value == "") {
			continue
		}
		if p.Value == "" && p.Name != "" {
			parts = append(parts, queryEscape(p.Name))
		} else {
			parts = append(parts, queryEscape(p.Name)+"="+queryEscape(p.Value))
		}
	}
	out := base
	if len(parts) > 0 {
		out += "?" + strings.Join(parts, "&")
	}
	if fragment != nil {
		out += "#" + *fragment
	}
	return out
}

// queryEscape keeps what a query may hold as it is (as Foundation's
// urlQueryAllowed less &=+#?), and escapes the rest.
func queryEscape(s string) string {
	const keep = "-._~!$'()*,;:@/"
	var b strings.Builder
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', strings.IndexByte(keep, c) >= 0:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// APISize is "1.2 KB": the size of a response body.
func APISize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	kb := float64(bytes) / 1024
	if kb < 1024 {
		if kb < 10 {
			return fmt.Sprintf("%.1f KB", kb)
		}
		return fmt.Sprintf("%.0f KB", kb)
	}
	mb := kb / 1024
	if mb < 10 {
		return fmt.Sprintf("%.1f MB", mb)
	}
	return fmt.Sprintf("%.0f MB", mb)
}

// APIDuration is "240 ms" or "1.3 s": how long a request took.
func APIDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%d ms", ms)
	}
	return fmt.Sprintf("%.1f s", float64(ms)/1000)
}
