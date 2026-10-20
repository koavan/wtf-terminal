package uicore

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// JSON that keeps its keys in order and prints like JavaScript's
// JSON.stringify(v, null, 2) — encoding/json reorders keys and escapes <,
// > and &. For the JSON and JWT viewers. Port of the JSON parts of
// lib/logs.ts (macos/Sources/WtfCore/OrderedJSON.swift).

// JSONKind is what a JSONValue holds.
type JSONKind int

const (
	JSONNull JSONKind = iota
	JSONBool
	JSONNumber
	JSONString
	JSONArray
	JSONObject
)

// JSONValue is a JSON value; only the field for its Kind is set.
type JSONValue struct {
	Kind    JSONKind
	Bool    bool
	Num     float64
	Str     string
	Items   []JSONValue
	Members []JSONMember
}

// JSONMember is one key of an object, in the order the text had it.
type JSONMember struct {
	Key   string
	Value JSONValue
}

// ErrJSONParse is any JSON syntax error.
var ErrJSONParse = errors.New("not valid JSON")

// Equal compares two values, keys in order.
func (v JSONValue) Equal(o JSONValue) bool {
	if v.Kind != o.Kind {
		return false
	}
	switch v.Kind {
	case JSONBool:
		return v.Bool == o.Bool
	case JSONNumber:
		return v.Num == o.Num
	case JSONString:
		return v.Str == o.Str
	case JSONArray:
		if len(v.Items) != len(o.Items) {
			return false
		}
		for i := range v.Items {
			if !v.Items[i].Equal(o.Items[i]) {
				return false
			}
		}
	case JSONObject:
		if len(v.Members) != len(o.Members) {
			return false
		}
		for i := range v.Members {
			if v.Members[i].Key != o.Members[i].Key || !v.Members[i].Value.Equal(o.Members[i].Value) {
				return false
			}
		}
	}
	return true
}

// Get is an object's value for a key.
func (v JSONValue) Get(key string) (JSONValue, bool) {
	for _, m := range v.Members {
		if m.Key == key {
			return m.Value, true
		}
	}
	return JSONValue{}, false
}

// IsContainer says it's an array or an object.
func (v JSONValue) IsContainer() bool { return v.Kind == JSONArray || v.Kind == JSONObject }

// ParseJSON reads one JSON value, with nothing but white space around it.
func ParseJSON(text string) (JSONValue, error) {
	p := jsonParser{s: []rune(text)}
	p.space()
	v, err := p.value()
	if err != nil {
		return JSONValue{}, err
	}
	p.space()
	if p.i != len(p.s) {
		return JSONValue{}, ErrJSONParse
	}
	return v, nil
}

type jsonParser struct {
	s []rune
	i int
}

func (p *jsonParser) space() {
	for p.i < len(p.s) && strings.ContainsRune(" \t\n\r", p.s[p.i]) {
		p.i++
	}
}

func (p *jsonParser) expect(word string) error {
	for _, c := range word {
		if p.i >= len(p.s) || p.s[p.i] != c {
			return ErrJSONParse
		}
		p.i++
	}
	return nil
}

func (p *jsonParser) at(c rune) bool { return p.i < len(p.s) && p.s[p.i] == c }

func (p *jsonParser) digit() bool { return p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' }

func (p *jsonParser) value() (JSONValue, error) {
	if p.i >= len(p.s) {
		return JSONValue{}, ErrJSONParse
	}
	switch p.s[p.i] {
	case '{':
		p.i++
		out := JSONValue{Kind: JSONObject, Members: []JSONMember{}}
		p.space()
		if p.at('}') {
			p.i++
			return out, nil
		}
		for {
			p.space()
			if !p.at('"') {
				return JSONValue{}, ErrJSONParse
			}
			k, err := p.string()
			if err != nil {
				return JSONValue{}, err
			}
			p.space()
			if err := p.expect(":"); err != nil {
				return JSONValue{}, err
			}
			p.space()
			v, err := p.value()
			if err != nil {
				return JSONValue{}, err
			}
			// A repeated key keeps its first place and its last value.
			found := false
			for j := range out.Members {
				if out.Members[j].Key == k {
					out.Members[j].Value, found = v, true
					break
				}
			}
			if !found {
				out.Members = append(out.Members, JSONMember{k, v})
			}
			p.space()
			switch {
			case p.at(','):
				p.i++
			case p.at('}'):
				p.i++
				return out, nil
			default:
				return JSONValue{}, ErrJSONParse
			}
		}
	case '[':
		p.i++
		out := JSONValue{Kind: JSONArray, Items: []JSONValue{}}
		p.space()
		if p.at(']') {
			p.i++
			return out, nil
		}
		for {
			p.space()
			v, err := p.value()
			if err != nil {
				return JSONValue{}, err
			}
			out.Items = append(out.Items, v)
			p.space()
			switch {
			case p.at(','):
				p.i++
			case p.at(']'):
				p.i++
				return out, nil
			default:
				return JSONValue{}, ErrJSONParse
			}
		}
	case '"':
		s, err := p.string()
		return JSONValue{Kind: JSONString, Str: s}, err
	case 't':
		return JSONValue{Kind: JSONBool, Bool: true}, p.expect("true")
	case 'f':
		return JSONValue{Kind: JSONBool}, p.expect("false")
	case 'n':
		return JSONValue{}, p.expect("null")
	}
	n, err := p.number()
	return JSONValue{Kind: JSONNumber, Num: n}, err
}

func (p *jsonParser) number() (float64, error) {
	start := p.i
	if p.at('-') {
		p.i++
	}
	if !p.digit() {
		return 0, ErrJSONParse
	}
	if p.at('0') {
		p.i++
	} else {
		for p.digit() {
			p.i++
		}
	}
	if p.at('.') {
		p.i++
		if !p.digit() {
			return 0, ErrJSONParse
		}
		for p.digit() {
			p.i++
		}
	}
	if p.at('e') || p.at('E') {
		p.i++
		if p.at('+') || p.at('-') {
			p.i++
		}
		if !p.digit() {
			return 0, ErrJSONParse
		}
		for p.digit() {
			p.i++
		}
	}
	d, err := strconv.ParseFloat(string(p.s[start:p.i]), 64)
	// Too big is ±Inf, as in JavaScript.
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, ErrJSONParse
	}
	return d, nil
}

func (p *jsonParser) string() (string, error) {
	p.i++
	var out strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		p.i++
		if c == '"' {
			return out.String(), nil
		}
		if c < 0x20 {
			return "", ErrJSONParse
		}
		if c != '\\' {
			out.WriteRune(c)
			continue
		}
		if p.i >= len(p.s) {
			return "", ErrJSONParse
		}
		e := p.s[p.i]
		p.i++
		switch e {
		case '"', '\\', '/':
			out.WriteRune(e)
		case 'b':
			out.WriteRune('\b')
		case 'f':
			out.WriteRune('\f')
		case 'n':
			out.WriteRune('\n')
		case 'r':
			out.WriteRune('\r')
		case 't':
			out.WriteRune('\t')
		case 'u':
			u, err := p.hex4()
			if err != nil {
				return "", err
			}
			if u >= 0xD800 && u <= 0xDBFF && p.i+1 < len(p.s) && p.s[p.i] == '\\' && p.s[p.i+1] == 'u' {
				save := p.i
				p.i += 2
				lo, err := p.hex4()
				if err != nil {
					return "", err
				}
				if lo >= 0xDC00 && lo <= 0xDFFF {
					u = 0x10000 + (u-0xD800)<<10 + (lo - 0xDC00)
				} else {
					p.i = save
				}
			}
			// A lone surrogate can't live in a Go string as text.
			if !utf8.ValidRune(u) {
				u = utf8.RuneError
			}
			out.WriteRune(u)
		default:
			return "", ErrJSONParse
		}
	}
	return "", ErrJSONParse
}

func (p *jsonParser) hex4() (rune, error) {
	if p.i+4 > len(p.s) {
		return 0, ErrJSONParse
	}
	v, err := strconv.ParseUint(string(p.s[p.i:p.i+4]), 16, 32)
	if err != nil {
		return 0, ErrJSONParse
	}
	p.i += 4
	return rune(v), nil
}

// Pretty is JSON.stringify(v, null, 2).
func (v JSONValue) Pretty() string { return v.pretty("") }

func (v JSONValue) pretty(indent string) string {
	inner := indent + "  "
	switch v.Kind {
	case JSONBool:
		if v.Bool {
			return "true"
		}
		return "false"
	case JSONNumber:
		return JSNumber(v.Num)
	case JSONString:
		return jsonQuote(v.Str)
	case JSONArray:
		if len(v.Items) == 0 {
			return "[]"
		}
		parts := make([]string, len(v.Items))
		for i, x := range v.Items {
			parts[i] = inner + x.pretty(inner)
		}
		return "[\n" + strings.Join(parts, ",\n") + "\n" + indent + "]"
	case JSONObject:
		if len(v.Members) == 0 {
			return "{}"
		}
		parts := make([]string, len(v.Members))
		for i, m := range v.Members {
			parts[i] = inner + jsonQuote(m.Key) + ": " + m.Value.pretty(inner)
		}
		return "{\n" + strings.Join(parts, ",\n") + "\n" + indent + "}"
	}
	return "null"
}

func jsonQuote(s string) string {
	var out strings.Builder
	out.WriteByte('"')
	for _, c := range s {
		switch c {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if c < 0x20 {
				fmt.Fprintf(&out, `\u%04x`, c)
			} else {
				out.WriteRune(c)
			}
		}
	}
	out.WriteByte('"')
	return out.String()
}

// JSNumber is a number as JavaScript prints it: shortest round trip, 1 not
// 1.0, 1e+21, 1e-7.
func JSNumber(d float64) string {
	if math.IsInf(d, 0) || math.IsNaN(d) {
		return "null"
	}
	if d == 0 {
		return "0"
	}
	sign := ""
	if d < 0 {
		sign = "-"
	}
	// The shortest round-trip form, as d.ddde±x: take its digits and exponent.
	e := strconv.FormatFloat(math.Abs(d), 'e', -1, 64)
	mant, exp, _ := strings.Cut(e, "e")
	x, _ := strconv.Atoi(exp)
	digits := strings.TrimRight(strings.Replace(mant, ".", "", 1), "0")
	// value = 0.digits × 10^n.
	k, n := len(digits), x+1
	switch {
	case k <= n && n <= 21:
		return sign + digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return sign + digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return sign + "0." + strings.Repeat("0", -n) + digits
	}
	m := digits
	if k > 1 {
		m = digits[:1] + "." + digits[1:]
	}
	ex := n - 1
	if ex >= 0 {
		return sign + m + "e+" + strconv.Itoa(ex)
	}
	return sign + m + "e-" + strconv.Itoa(-ex)
}

// FindJSON is the first JSON object or array in some text, pretty-printed
// (log lines with a payload), or "". Tries at most 20 brackets.
func FindJSON(text string) string {
	tries := 0
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c != '{' && c != '[' {
			continue
		}
		tries++
		if tries > 20 {
			break
		}
		closer := "}"
		if c == '[' {
			closer = "]"
		}
		end := strings.LastIndex(text, closer)
		if end <= i {
			continue
		}
		if v, err := ParseJSON(text[i : end+1]); err == nil && v.IsContainer() {
			return v.Pretty()
		}
	}
	return ""
}
