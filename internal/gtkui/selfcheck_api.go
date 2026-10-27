//go:build linux

package gtkui

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/dayanraj/wtf/internal/apiclient"
	"github.com/dayanraj/wtf/internal/uicore"
)

// checkAPI (WTF_NATIVE_DEBUG=api): the API tab against a local server —
// opening one, its lists, masking, sending, curl in and out, saving without
// credentials, and Run in Shell. Local only.
func (w *Window) checkAPI() {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rw.Header().Set("Content-Type", "application/json")
		rw.Header().Set("X-Saw-Auth", map[bool]string{true: "yes", false: "no"}[r.Header.Get("Authorization") != ""])
		_, _ = io.WriteString(rw, `{"users":[{"id":12345678901234567890,"name":"ada"}],"method":"`+r.Method+`","body":"`+string(body)+`"}`)
	}))
	defer srv.Close()
	m := w.model
	time.Sleep(time.Second) // the first shell's prompt

	// A new tab, from its shortcut: empty lists still offer Add.
	_ = xkey("ctrl+alt+shift+a")
	var a *apiPane
	check(waitUntil(3*time.Second, func() bool { a = w.activeAPI(); return a != nil }), "Ctrl+Alt+Shift+A opens an API tab")
	if a == nil {
		w.finish()
		return
	}
	var title string
	var addShown bool
	onMain(func() {
		t, _ := m.Active()
		title = m.Title(t)
		addShown = a.params.add.Parent() != nil && a.headers.add.Parent() != nil && len(a.params.rows) == 0
	})
	check(title == "API", "it's called API (%q)", title)
	check(addShown, "an empty tab has Add Parameter and Add Header")
	w.shot("api-new")
	onMain(func() { a.headers.addRow() })
	var rows int
	onMain(func() { rows = len(a.headers.rows) })
	check(rows == 1, "Add Header adds a row (%d)", rows)
	onMain(m.CloseActivePane)

	// A request with a credential and a ticked-off header.
	onMain(func() {
		m.OpenAPI(apiclient.Request{Method: "GET", URL: srv.URL + "/users?page=2&limit=50", Headers: []apiclient.Header{
			{Name: "Authorization", Value: "Bearer abc.def"}, {Name: "Accept", Value: "application/json"}, {Name: "X-Debug", Value: "1", Off: true},
		}}, nil, 0)
	})
	check(waitUntil(2*time.Second, func() bool { a = w.activeAPI(); return a != nil }), "an API tab with a request")
	var params, masked, shownPlain int
	var inList bool
	onMain(func() {
		params = len(a.params.rows)
		inList = params > 0 && a.params.rows[0].box.Parent() != nil && sameWidget(a.params.box, gtk.BaseWidget(a.params.rows[0].box.Parent())) &&
			a.params.add.PrevSibling() != nil && sameWidget(a.params.rows[params-1].box, gtk.BaseWidget(a.params.add.PrevSibling()))
		for i, r := range a.headers.rows {
			if !r.value.Visibility() {
				masked++
				if i == 0 {
					a.headers.revealed[0] = true
					a.headers.mask(0)
					if r.value.Visibility() {
						shownPlain++
					}
					a.headers.revealed[0] = false
					a.headers.mask(0)
				}
			}
		}
	})
	check(params == 2, "the URL's query is two parameters (%d)", params)
	check(inList, "the rows are in the list, above Add Parameter")
	check(masked == 1 && shownPlain == 1, "only Authorization is masked (%d), and the eye shows it", masked)

	// Editing a parameter rewrites the URL.
	var url string
	onMain(func() {
		a.params.rows[1].value.SetText("10")
		url = a.url.Text()
	})
	check(strings.HasSuffix(url, "/users?page=2&limit=10"), "a parameter edit changes the URL (%s)", url)

	onMain(a.sendRequest)
	check(waitUntil(5*time.Second, func() bool { return a.res != nil || a.failure != "" }), "a response arrives (%s)", a.failure)
	var status, text, headers string
	onMain(func() {
		status = a.status.Text()
		text = bufferText(a.respView.Buffer())
		for _, h := range a.res.Headers {
			if h.Name == "X-Saw-Auth" {
				headers = h.Value
			}
		}
	})
	check(status == "200 OK", "status %q", status)
	check(strings.Contains(text, `"id": 12345678901234567890`) && strings.Contains(text, "\n  \"users\""),
		"JSON is indented, its numbers kept")
	check(headers == "yes", "the Authorization header was sent")
	w.shot("api-response")
	onMain(func() { a.reqStack.SetVisibleChildName("headers"); a.respHeaders.SetActive(true) })
	time.Sleep(300 * time.Millisecond)
	w.shot("api-headers")
	onMain(func() { a.respBody.SetActive(true) })

	// Find in the response.
	var found string
	onMain(func() {
		a.showFind()
		a.findEntry.SetText("ada")
	})
	time.Sleep(400 * time.Millisecond)
	onMain(func() {
		b := a.respView.Buffer()
		s, e, _ := b.SelectionBounds()
		found = b.Text(s, e, false)
		a.findBar.SetRevealChild(false)
	})
	check(strings.EqualFold(found, "ada"), "find selects the match (%q)", found)

	// Saved without the credential.
	var saved string
	onMain(func() { saved = w.sessionJSON() })
	check(strings.Contains(saved, `"api"`) && strings.Contains(saved, "/users?page=2") && !strings.Contains(saved, "abc.def"),
		"the session keeps the request but not the token")

	// A curl command pasted into the URL field fills in the request.
	onMain(func() {
		a.lastURLLen = 0
		a.url.SetText(`curl -X POST ` + srv.URL + `/echo -H 'Content-Type: text/plain' --data-raw 'hi there'`)
	})
	var method, body string
	ok := waitUntil(2*time.Second, func() bool {
		method, body = a.req.Method, bufferText(a.body.Buffer())
		return method == "POST"
	})
	check(ok && body == "hi there" && strings.HasSuffix(a.url.Text(), "/echo"), "a pasted curl command fills it in (%s, %q)", method, body)
	w.shot("api-curl")
	var curl string
	onMain(func() { curl = apiclient.Curl(a.req, false) })
	check(strings.Contains(curl, "--data-raw 'hi there'") && strings.Contains(curl, "-H 'Content-Type: text/plain'"), "Copy as curl: %s", curl)

	// Run in Shell types the command into the tab's idle shell.
	onMain(func() { m.Split(uicore.SplitRow, nil, "") })
	var term *Pane
	check(waitUntil(5*time.Second, func() bool { term = w.active(); return term != nil && term.sess != nil }), "a shell split beside it")
	time.Sleep(time.Second)
	var mu sync.Mutex
	var out bytes.Buffer
	onMain(func() {
		term.tap = func(b []byte) {
			mu.Lock()
			out.Write(b)
			mu.Unlock()
		}
		w.runFromAPI(a.id, "echo "+uicore.ShellQuote("ran-from-api"))
	})
	check(waitUntil(3*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return bytes.Contains(out.Bytes(), []byte("ran-from-api\r\n"))
	}), "Run in Shell types into the idle shell in the same tab")
	var tabs int
	onMain(func() { tabs = len(m.Tabs()) })
	check(tabs == 2, "without opening a tab (%d tabs)", tabs)
	w.shot("api-split")
	w.finish()
}
