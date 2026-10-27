package apiclient

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Seen", r.Method+" "+r.Header.Get("X-Token")+" "+r.Header.Get("X-Off")+" "+r.UserAgent())
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"b":12345678901234567890,"a":`+string(body)+`}`)
	}))
	defer srv.Close()

	s := NewService()
	res, err := s.Send("t1", Request{
		Method: "post", URL: srv.URL + "/x",
		Headers: []Header{{Name: "X-Token", Value: "abc"}, {Name: "X-Off", Value: "no", Off: true}, {Name: " "}},
		Body:    `[1]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 201 || res.StatusText != "Created" {
		t.Errorf("status %d %q", res.Status, res.StatusText)
	}
	if res.Body != `{"b":12345678901234567890,"a":[1]}` {
		t.Errorf("body %q", res.Body)
	}
	// Numbers and key order as sent.
	want := "{\n  \"b\": 12345678901234567890,\n  \"a\": [\n    1\n  ]\n}"
	if res.Pretty != want {
		t.Errorf("pretty %q", res.Pretty)
	}
	var seen string
	for _, h := range res.Headers {
		if h.Name == "X-Seen" {
			seen = h.Value
		}
	}
	if seen != "POST abc  wtf" {
		t.Errorf("server saw %q", seen)
	}
	if res.ContentType != "application/json" || res.Size != int64(len(res.Body)) || res.Binary {
		t.Errorf("response %+v", res)
	}
}

func TestSendBinaryAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte{0x89, 'P', 'N', 'G', 0, 1})
	}))
	defer srv.Close()
	s := NewService()
	res, err := s.Send("b", Request{URL: srv.URL})
	if err != nil || !res.Binary || res.Body != "" || res.Size != 6 {
		t.Errorf("binary: %+v %v", res, err)
	}
	for _, u := range []string{"", "ftp://x", "http://"} {
		if _, err := s.Send("e", Request{URL: u}); err == nil {
			t.Errorf("%q: no error", u)
		}
	}
}

func TestCancel(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	s := NewService()
	done := make(chan error, 1)
	go func() {
		_, err := s.Send("slow", Request{URL: srv.URL})
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	s.Cancel("slow")
	select {
	case err := <-done:
		if err == nil || err.Error() != "canceled" {
			t.Errorf("got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Cancel didn't stop the request")
	}
}

func TestTarget(t *testing.T) {
	u, err := Target(" example.com/a?b=1 ")
	if err != nil || u.String() != "http://example.com/a?b=1" {
		t.Errorf("%v %v", u, err)
	}
}

func TestFormatJSON(t *testing.T) {
	s := NewService()
	out, err := s.FormatJSON(` {"z":1.50,"a":[]} `)
	if err != nil || out != "{\n  \"z\": 1.50,\n  \"a\": []\n}" {
		t.Errorf("%q %v", out, err)
	}
	if _, err := s.FormatJSON("{"); err == nil {
		t.Error("no error for bad JSON")
	}
}

func TestCurl(t *testing.T) {
	r := Request{
		Method: "PUT", URL: "api.example.com/v1/x?q=1",
		Headers:  []Header{{Name: "Authorization", Value: "Bearer it's"}, {Name: "X-Empty"}, {Name: "X-Off", Value: "1", Off: true}},
		Body:     `{"a":1}`,
		Insecure: true,
	}
	got := Curl(r, false)
	want := `curl -X PUT -k -H 'Authorization: Bearer it'\''s' -H 'X-Empty;' -H 'Content-Type:' --data-raw '{"a":1}' 'http://api.example.com/v1/x?q=1'`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := Curl(Request{Method: "POST", URL: "https://x", Body: "a=1"}, true); got != "curl -H 'Content-Type:' \\\n  --data-raw 'a=1' \\\n  'https://x'" {
		t.Errorf("multiline: %q", got)
	}
	head := Curl(Request{Method: "HEAD", URL: "https://x"}, false)
	if head != "curl --head 'https://x'" {
		t.Errorf("head: %q", head)
	}
	// What Curl writes, ParseCurl reads back.
	back, err := ParseCurl(head)
	if err != nil || back.Method != "HEAD" || back.URL != "https://x" {
		t.Errorf("round trip: %+v %v", back, err)
	}
	back, err = ParseCurl(Curl(r, true))
	if err != nil {
		t.Fatal(err)
	}
	r.URL = "http://api.example.com/v1/x?q=1"
	r.Headers = r.Headers[:2]
	if !reflect.DeepEqual(back, r) {
		t.Errorf("round trip:\n%+v\n%+v", back, r)
	}
}

func TestParseCurl(t *testing.T) {
	cases := []struct {
		in   string
		want Request
	}{
		{`curl https://x.io`, Request{Method: "GET", URL: "https://x.io"}},
		{`$ curl -sSL -XDELETE "https://x.io/a b"`, Request{Method: "DELETE", URL: "https://x.io/a b"}},
		{"curl 'https://x.io' \\\n  -H 'accept: */*' \\\n  --data-raw $'{\"a\":\\n1}' --compressed",
			Request{Method: "POST", URL: "https://x.io", Body: "{\"a\":\n1}",
				Headers: []Header{{Name: "accept", Value: "*/*"}, {Name: "Content-Type", Value: "application/x-www-form-urlencoded"}}}},
		{`curl -d a=1 -d b=2 -G https://x.io?z=0`, Request{Method: "GET", URL: "https://x.io?z=0&a=1&b=2"}},
		{`curl --json '{"a":1}' https://x.io`, Request{Method: "POST", URL: "https://x.io", Body: `{"a":1}`,
			Headers: []Header{{Name: "Content-Type", Value: "application/json"}, {Name: "Accept", Value: "application/json"}}}},
		{`curl -u me:pw -k --url=https://x.io -H 'X-Gone:' -H 'X-Empty;'`, Request{Method: "GET", URL: "https://x.io", Insecure: true,
			Headers: []Header{{Name: "Authorization", Value: "Basic bWU6cHc="}, {Name: "X-Empty"}}}},
		{`curl --data-urlencode 'q=a b&c' -H "Content-Type: text/plain" -m 5 x.io`, Request{Method: "POST", URL: "x.io", Body: "q=a+b%26c",
			Headers: []Header{{Name: "Content-Type", Value: "text/plain"}}}},
		{`curl -b 'a=1; b=2' -A ua -e ref -I x.io`, Request{Method: "HEAD", URL: "x.io",
			Headers: []Header{{Name: "Cookie", Value: "a=1; b=2"}, {Name: "User-Agent", Value: "ua"}, {Name: "Referer", Value: "ref"}}}},
	}
	for _, c := range cases {
		got, err := ParseCurl(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", c.in, got, c.want)
		}
	}
}

func TestParseCurlRefuses(t *testing.T) {
	for in, why := range map[string]string{
		`wget x.io`:                     "not a curl",
		`curl`:                          "no URL",
		`curl a.io b.io`:                "more than one URL",
		`curl -F f=@x x.io`:             "multipart",
		`curl -d @body.json x.io`:       "reads a file",
		`curl --frobnicate x.io`:        "--frobnicate",
		`curl x.io | jq .`:              "single curl command",
		`curl 'x.io`:                    "isn't closed",
		`curl -u me x.io`:               "password",
		`curl -x http://proxy:1 x.io`:   "proxies",
		`curl -H`:                       "needs a value",
		`curl $(cat url)`:               "single curl command",
		`curl -b cookies.txt x.io`:      "from a file",
		`curl -H 'no colon here' x.io`:  "isn't a header",
		`curl --data-urlencode @f x.io`: "reads a file",
	} {
		_, err := ParseCurl(in)
		if err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("%s: got %v, want %q", in, err, why)
		}
	}
}
