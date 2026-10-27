package apiclient

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Curl writes a request as a curl command, every value in single quotes.
func Curl(r Request, multiline bool) string {
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method == "" {
		method = "GET"
	}
	var parts []string
	switch {
	case method == "HEAD":
		parts = append(parts, "--head")
	case method == "GET", method == "POST" && r.Body != "":
		// curl's own choice: GET, or POST with data.
	default:
		parts = append(parts, "-X "+method)
	}
	if r.Insecure {
		parts = append(parts, "-k")
	}
	for _, h := range r.Headers {
		name := strings.TrimSpace(h.Name)
		if h.Off || name == "" {
			continue
		}
		if h.Value == "" {
			// "Name;" is how curl sends a header with no value.
			parts = append(parts, "-H "+quote(name+";"))
		} else {
			parts = append(parts, "-H "+quote(name+": "+h.Value))
		}
	}
	if r.Body != "" {
		if !hasHeader(on(r.Headers), "Content-Type") {
			// curl would label the body a form; the tab sends it unlabelled.
			parts = append(parts, "-H "+quote("Content-Type:"))
		}
		parts = append(parts, "--data-raw "+quote(r.Body))
	}
	target := strings.TrimSpace(r.URL)
	if u, err := Target(target); err == nil {
		target = u.String()
	}
	parts = append(parts, quote(target))
	sep := " "
	if multiline {
		sep = " \\\n  "
	}
	return "curl " + strings.Join(parts, sep)
}

// quote puts a value in single quotes for sh, bash, zsh and fish.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// Options that take a value and don't change the request.
var ignoredWithValue = map[string]bool{
	"-o": true, "--output": true, "-m": true, "--max-time": true, "--connect-timeout": true,
	"-w": true, "--write-out": true, "--retry": true, "--retry-delay": true, "--retry-max-time": true,
	"-c": true, "--cookie-jar": true, "--cacert": true, "--capath": true, "-D": true, "--dump-header": true,
	"--resolve": true, "--connect-to": true, "--limit-rate": true, "--max-redirs": true,
	"--trace": true, "--trace-ascii": true, "--stderr": true, "--interface": true,
}

// Options without a value that don't change the request.
var ignoredFlags = map[string]bool{
	"-s": true, "--silent": true, "-S": true, "--show-error": true, "-L": true, "--location": true,
	"-v": true, "--verbose": true, "-i": true, "--include": true, "--compressed": true, "-f": true,
	"--fail": true, "--fail-with-body": true, "-N": true, "--no-buffer": true, "-g": true, "--globoff": true,
	"--http1.1": true, "--http2": true, "--http1.0": true, "-#": true, "--progress-bar": true,
	"-O": true, "--remote-name": true, "--location-trusted": true, "--no-progress-meter": true,
	"-4": true, "-6": true, "--ipv4": true, "--ipv6": true, "--path-as-is": true, "-l": true,
}

// Options that change the request but can't be represented, so the command
// is refused rather than sent differently.
var unsupported = map[string]string{
	"-F": "multipart forms (-F)", "--form": "multipart forms (--form)",
	"-T": "file uploads (-T)", "--upload-file": "file uploads (--upload-file)",
	"-E": "client certificates (-E)", "--cert": "client certificates (--cert)", "--key": "client keys (--key)",
	"-x": "proxies (-x)", "--proxy": "proxies (--proxy)",
	"--negotiate": "Negotiate auth", "--ntlm": "NTLM auth", "--digest": "digest auth", "--aws-sigv4": "AWS SigV4 signing",
	"-K": "config files (-K)", "--config": "config files (--config)",
}

// Short options that take a value.
var shortWithValue = "XHdubAeoFmwcDTExrK"

// ParseCurl reads a curl command, as copied from docs or a browser's
// "Copy as cURL". Options it can't represent are an error, never dropped.
func ParseCurl(command string) (Request, error) {
	words, err := shellWords(command)
	if err != nil {
		return Request{}, err
	}
	if len(words) > 0 && words[0] == "$" {
		words = words[1:]
	}
	if len(words) == 0 || words[0] != "curl" {
		return Request{}, errors.New("not a curl command")
	}
	words = words[1:]

	var (
		r      Request
		method string
		data   []string
		get    bool
		json   bool
		urls   []string
		noMore bool
		// Headers curl is told not to add ("-H 'Name:'").
		removed = map[string]bool{}
	)
	// Short option clusters (-sSL, -XPOST) become separate words.
	var args []string
	for _, w := range words {
		if noMore || !strings.HasPrefix(w, "-") || strings.HasPrefix(w, "--") || len(w) <= 2 {
			args = append(args, w)
			if w == "--" {
				noMore = true
			}
			continue
		}
		for i := 1; i < len(w); i++ {
			opt := "-" + string(w[i])
			if strings.ContainsRune(shortWithValue, rune(w[i])) && i+1 < len(w) {
				args = append(args, opt, w[i+1:])
				break
			}
			args = append(args, opt)
		}
	}
	noMore = false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if noMore || !strings.HasPrefix(a, "-") || a == "-" {
			urls = append(urls, a)
			continue
		}
		if a == "--" {
			noMore = true
			continue
		}
		name, value, hasValue := a, "", false
		if strings.HasPrefix(a, "--") {
			if k, v, ok := strings.Cut(a, "="); ok {
				name, value, hasValue = k, v, true
			}
		}
		next := func() (string, error) {
			if hasValue {
				return value, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", name)
			}
			i++
			return args[i], nil
		}
		if what, ok := unsupported[name]; ok {
			return Request{}, fmt.Errorf("%s can't be used in the API tab yet", what)
		}
		switch {
		case name == "-X" || name == "--request":
			v, err := next()
			if err != nil {
				return Request{}, err
			}
			method = strings.ToUpper(v)
		case name == "-H" || name == "--header":
			v, err := next()
			if err != nil {
				return Request{}, err
			}
			if strings.HasPrefix(v, "@") {
				return Request{}, errors.New("headers from a file (-H @file) can't be used in the API tab")
			}
			if n, val, ok := strings.Cut(v, ":"); ok {
				val = strings.TrimSpace(val)
				if val == "" {
					// "Name:" stops curl adding that header.
					removed[strings.ToLower(strings.TrimSpace(n))] = true
					continue
				}
				r.Headers = append(r.Headers, Header{Name: strings.TrimSpace(n), Value: val})
			} else if n, ok := strings.CutSuffix(v, ";"); ok {
				r.Headers = append(r.Headers, Header{Name: strings.TrimSpace(n)})
			} else {
				return Request{}, fmt.Errorf("%q isn't a header", v)
			}
		case name == "-d" || name == "--data" || name == "--data-ascii" || name == "--data-binary":
			v, err := next()
			if err != nil {
				return Request{}, err
			}
			if strings.HasPrefix(v, "@") {
				return Request{}, fmt.Errorf("%s reads a file (%s): paste the body instead", name, v)
			}
			if name != "--data-binary" {
				// curl drops newlines from -d data.
				v = strings.NewReplacer("\r", "", "\n", "").Replace(v)
			}
			data = append(data, v)
		case name == "--data-raw":
			v, err := next()
			if err != nil {
				return Request{}, err
			}
			data = append(data, v)
		case name == "--data-urlencode":
			v, err := next()
			if err != nil {
				return Request{}, err
			}
			enc, err := urlencodeData(v)
			if err != nil {
				return Request{}, err
			}
			data = append(data, enc)
		case name == "--json":
			v, err := next()
			if err != nil {
				return Request{}, err
			}
			if strings.HasPrefix(v, "@") {
				return Request{}, fmt.Errorf("--json reads a file (%s): paste the body instead", v)
			}
			data = append(data, v)
			json = true
		case name == "-u" || name == "--user":
			v, err := next()
			if err != nil {
				return Request{}, err
			}
			if !strings.Contains(v, ":") {
				return Request{}, errors.New("-u without a password asks for one: include it as user:password")
			}
			r.Headers = append(r.Headers, Header{Name: "Authorization", Value: "Basic " + base64.StdEncoding.EncodeToString([]byte(v))})
		case name == "-A" || name == "--user-agent":
			v, err := next()
			if err != nil {
				return Request{}, err
			}
			r.Headers = append(r.Headers, Header{Name: "User-Agent", Value: v})
		case name == "-e" || name == "--referer":
			v, err := next()
			if err != nil {
				return Request{}, err
			}
			r.Headers = append(r.Headers, Header{Name: "Referer", Value: v})
		case name == "-b" || name == "--cookie":
			v, err := next()
			if err != nil {
				return Request{}, err
			}
			if !strings.Contains(v, "=") {
				return Request{}, fmt.Errorf("cookies from a file (-b %s) can't be used in the API tab", v)
			}
			r.Headers = append(r.Headers, Header{Name: "Cookie", Value: v})
		case name == "-k" || name == "--insecure":
			r.Insecure = true
		case name == "-I" || name == "--head":
			method = "HEAD"
		case name == "-G" || name == "--get":
			get = true
		case name == "--url":
			v, err := next()
			if err != nil {
				return Request{}, err
			}
			urls = append(urls, v)
		case name == "--basic":
			// The default for -u.
		case ignoredFlags[name]:
		case ignoredWithValue[name]:
			if _, err := next(); err != nil {
				return Request{}, err
			}
		default:
			return Request{}, fmt.Errorf("the curl option %s isn't supported in the API tab", name)
		}
	}
	switch len(urls) {
	case 0:
		return Request{}, errors.New("the curl command has no URL")
	case 1:
		r.URL = urls[0]
	default:
		return Request{}, errors.New("the curl command has more than one URL")
	}
	body := strings.Join(data, "&")
	if get && body != "" {
		sep := "?"
		if strings.Contains(r.URL, "?") {
			sep = "&"
		}
		r.URL += sep + body
		body = ""
	}
	r.Body = body
	if json {
		if !hasHeader(r.Headers, "Content-Type") {
			r.Headers = append(r.Headers, Header{Name: "Content-Type", Value: "application/json"})
		}
		if !hasHeader(r.Headers, "Accept") {
			r.Headers = append(r.Headers, Header{Name: "Accept", Value: "application/json"})
		}
	} else if body != "" && !hasHeader(r.Headers, "Content-Type") && !removed["content-type"] {
		// What curl sends with -d.
		r.Headers = append(r.Headers, Header{Name: "Content-Type", Value: "application/x-www-form-urlencoded"})
	}
	switch {
	case method != "":
		r.Method = method
	case get:
		r.Method = "GET"
	case body != "":
		r.Method = "POST"
	default:
		r.Method = "GET"
	}
	return r, nil
}

// on: the headers that are sent.
func on(hs []Header) []Header {
	var out []Header
	for _, h := range hs {
		if !h.Off && strings.TrimSpace(h.Name) != "" {
			out = append(out, h)
		}
	}
	return out
}

func hasHeader(hs []Header, name string) bool {
	for _, h := range hs {
		if strings.EqualFold(h.Name, name) {
			return true
		}
	}
	return false
}

// urlencodeData follows curl's --data-urlencode forms: "content",
// "=content", "name=content" (and @file forms, which are refused).
func urlencodeData(v string) (string, error) {
	if strings.HasPrefix(v, "@") || strings.Contains(v, "@") && !strings.Contains(v, "=") {
		return "", fmt.Errorf("--data-urlencode reads a file (%s): paste the value instead", v)
	}
	name, content, ok := strings.Cut(v, "=")
	if !ok {
		return url.QueryEscape(v), nil
	}
	if name == "" {
		return url.QueryEscape(content), nil
	}
	return name + "=" + url.QueryEscape(content), nil
}

// shellWords splits a command line as sh would: quotes, backslashes and
// line continuations, plus bash's $'…'. No variables or substitutions.
func shellWords(s string) ([]string, error) {
	var (
		words []string
		cur   strings.Builder
		in    bool
	)
	end := func() {
		if in {
			words = append(words, cur.String())
			cur.Reset()
			in = false
		}
	}
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			end()
		case c == '\\':
			if i+1 < len(rs) {
				i++
				if rs[i] == '\n' {
					continue
				}
				if rs[i] == '\r' && i+1 < len(rs) && rs[i+1] == '\n' {
					i++
					continue
				}
				cur.WriteRune(rs[i])
				in = true
			}
		case c == '\'':
			j := i + 1
			for j < len(rs) && rs[j] != '\'' {
				j++
			}
			if j >= len(rs) {
				return nil, errors.New("a quote (') isn't closed")
			}
			cur.WriteString(string(rs[i+1 : j]))
			in = true
			i = j
		case c == '"':
			j := i + 1
			for ; j < len(rs) && rs[j] != '"'; j++ {
				if rs[j] == '\\' && j+1 < len(rs) && strings.ContainsRune("\"\\$`\n", rs[j+1]) {
					j++
					if rs[j] != '\n' {
						cur.WriteRune(rs[j])
					}
					continue
				}
				cur.WriteRune(rs[j])
			}
			if j >= len(rs) {
				return nil, errors.New(`a quote (") isn't closed`)
			}
			in = true
			i = j
		case c == '$' && i+1 < len(rs) && rs[i+1] == '\'':
			j := i + 2
			for ; j < len(rs) && rs[j] != '\''; j++ {
				if rs[j] == '\\' && j+1 < len(rs) {
					j++
					switch rs[j] {
					case 'n':
						cur.WriteRune('\n')
					case 't':
						cur.WriteRune('\t')
					case 'r':
						cur.WriteRune('\r')
					default:
						cur.WriteRune(rs[j])
					}
					continue
				}
				cur.WriteRune(rs[j])
			}
			if j >= len(rs) {
				return nil, errors.New("a quote ($') isn't closed")
			}
			in = true
			i = j
		case c == '|' || c == ';' || c == '&' || c == '>' || c == '<' || c == '`' || c == '$' && i+1 < len(rs) && rs[i+1] == '(':
			return nil, fmt.Errorf("only a single curl command can be pasted (found %q)", string(c))
		default:
			cur.WriteRune(c)
			in = true
		}
	}
	end()
	return words, nil
}
