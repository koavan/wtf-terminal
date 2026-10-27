// Package apiclient sends the API tab's HTTP requests, and turns them into
// curl commands and back. Requests go out only when the user presses Send;
// nothing here runs in the background.
package apiclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// Bodies past this are cut off: the tab shows text, not downloads.
	maxBody = 20 << 20
	// JSON past this is shown as it came, not indented.
	maxPretty = 5 << 20
	// A request nobody cancels still ends.
	requestTimeout = 5 * time.Minute
)

// Header is one header line. Off headers are kept in the tab but not sent.
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Off   bool   `json:"off"`
}

// Request is what the tab sends.
type Request struct {
	Method  string   `json:"method"`
	URL     string   `json:"url"`
	Headers []Header `json:"headers"`
	Body    string   `json:"body"`
	// Skip TLS certificate checks (curl -k).
	Insecure bool `json:"insecure"`
}

// Response is what came back.
type Response struct {
	Status     int    `json:"status"`
	StatusText string `json:"statusText"`
	Proto      string `json:"proto"`
	// Where the response came from, after redirects.
	URL         string   `json:"url"`
	Headers     []Header `json:"headers"`
	ContentType string   `json:"contentType"`
	// The body as text; empty when it isn't text (Binary).
	Body string `json:"body"`
	// The body indented, when it's JSON.
	Pretty    string `json:"pretty"`
	Binary    bool   `json:"binary"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
	Millis    int64  `json:"millis"`
}

type Service struct {
	mu       sync.Mutex
	inFlight map[string]*flight
	secure   *http.Client
	insecure *http.Client
}

func NewService() *Service {
	secure := http.DefaultTransport.(*http.Transport).Clone()
	insecure := http.DefaultTransport.(*http.Transport).Clone()
	insecure.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	return &Service{
		inFlight: map[string]*flight{},
		secure:   &http.Client{Transport: secure},
		insecure: &http.Client{Transport: insecure},
	}
}

// Target is the URL a request goes to: http:// when no scheme is given, as
// with curl.
func Target(raw string) (*url.URL, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, errors.New("enter a URL")
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("%q isn't a URL", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("only http and https URLs can be sent, not %s", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("%q has no host", raw)
	}
	return u, nil
}

// Send makes the request and waits for its response. id names it for Cancel.
func (s *Service) Send(id string, r Request) (Response, error) {
	u, err := Target(r.URL)
	if err != nil {
		return Response{}, err
	}
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method == "" {
		method = http.MethodGet
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	f := &flight{cancel}
	s.mu.Lock()
	if old := s.inFlight[id]; old != nil {
		old.cancel()
	}
	s.inFlight[id] = f
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		// A newer Send under the same id has its own entry.
		if s.inFlight[id] == f {
			delete(s.inFlight, id)
		}
		s.mu.Unlock()
	}()

	var body io.Reader
	if r.Body != "" {
		body = strings.NewReader(r.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return Response{}, err
	}
	for _, h := range r.Headers {
		name := strings.TrimSpace(h.Name)
		if h.Off || name == "" {
			continue
		}
		if strings.EqualFold(name, "Host") {
			req.Host = h.Value
			continue
		}
		req.Header.Add(name, h.Value)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "wtf")
	}
	client := s.secure
	if r.Insecure {
		client = s.insecure
	}

	start := time.Now()
	res, err := client.Do(req)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			return Response{}, errors.New("canceled")
		case errors.Is(err, context.DeadlineExceeded):
			return Response{}, fmt.Errorf("no response after %s", requestTimeout)
		}
		return Response{}, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return Response{}, errors.New("canceled")
		}
		return Response{}, fmt.Errorf("reading the response: %w", err)
	}
	out := Response{
		Status:      res.StatusCode,
		StatusText:  http.StatusText(res.StatusCode),
		Proto:       res.Proto,
		URL:         res.Request.URL.String(),
		ContentType: res.Header.Get("Content-Type"),
		Millis:      time.Since(start).Milliseconds(),
	}
	if len(data) > maxBody {
		data = data[:maxBody]
		out.Truncated = true
	}
	out.Size = int64(len(data))
	out.Headers = sortedHeaders(res.Header)
	if isText(data) {
		out.Body = string(data)
		if !out.Truncated && len(data) <= maxPretty {
			out.Pretty = pretty(data)
		}
	} else {
		out.Binary = true
	}
	return out, nil
}

// Cancel stops a request Send is waiting on.
func (s *Service) Cancel(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f := s.inFlight[id]; f != nil {
		f.cancel()
		delete(s.inFlight, id)
	}
}

// flight is a request in progress.
type flight struct{ cancel context.CancelFunc }

// FormatJSON indents JSON, keeping its keys' order and its numbers as written.
func (s *Service) FormatJSON(text string) (string, error) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(strings.TrimSpace(text)), "", "  "); err != nil {
		return "", fmt.Errorf("not valid JSON: %w", err)
	}
	return buf.String(), nil
}

// Curl is the request as a curl command; multiline puts each option on its
// own line.
func (s *Service) Curl(r Request, multiline bool) string { return Curl(r, multiline) }

// ParseCurl reads a curl command into a request.
func (s *Service) ParseCurl(command string) (Request, error) { return ParseCurl(command) }

func sortedHeaders(h http.Header) []Header {
	names := make([]string, 0, len(h))
	for n := range h {
		names = append(names, n)
	}
	sort.Strings(names)
	out := []Header{}
	for _, n := range names {
		for _, v := range h[n] {
			out = append(out, Header{Name: n, Value: v})
		}
	}
	return out
}

// isText: valid UTF-8 without NUL bytes.
func isText(b []byte) bool { return utf8.Valid(b) && !bytes.Contains(b, []byte{0}) }

func pretty(b []byte) string {
	t := bytes.TrimSpace(b)
	if len(t) == 0 || (t[0] != '{' && t[0] != '[') || !json.Valid(t) {
		return ""
	}
	var buf bytes.Buffer
	if json.Indent(&buf, t, "", "  ") != nil {
		return ""
	}
	return buf.String()
}
