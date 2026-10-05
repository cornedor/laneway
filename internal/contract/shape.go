// Package contract checks the demo Jira (internal/demo) against a real one.
// A probe makes the app's own reads through a proxy that keeps only the
// outline of each JSON answer: its key paths and the types seen at them,
// never a value. Jira's outline is recorded once into testdata; the test
// takes the demo's and fails where the demo says something Jira does not.
package contract

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// kinds is a set of JSON types.
type kinds uint8

const (
	kObject kinds = 1 << iota
	kArray
	kString
	kNumber
	kBool
	kNull
)

var kindNames = []struct {
	k    kinds
	name string
}{{kObject, "object"}, {kArray, "array"}, {kString, "string"}, {kNumber, "number"}, {kBool, "bool"}, {kNull, "null"}}

func (k kinds) String() string {
	var out []string
	for _, n := range kindNames {
		if k&n.k != 0 {
			out = append(out, n.name)
		}
	}
	return strings.Join(out, "|")
}

func parseKinds(s string) (kinds, error) {
	var k kinds
next:
	for part := range strings.SplitSeq(s, "|") {
		for _, n := range kindNames {
			if n.name == part {
				k |= n.k
				continue next
			}
		}
		return 0, fmt.Errorf("unknown type %q", part)
	}
	return k, nil
}

// Shape is one endpoint's outline: each key path ("." the document itself,
// "a.b", "a[]" an element of a) and the types seen there.
type Shape map[string]kinds

// Doc is the outline of every endpoint, keyed "METHOD /path/{id} status".
type Doc map[string]Shape

// add merges v, a value decoded with UseNumber, in at path.
func (s Shape) add(path string, v any) {
	switch v := v.(type) {
	case map[string]any:
		s[path] |= kObject
		for k, e := range v {
			s.add(child(path, normKey(k)), e)
		}
	case []any:
		s[path] |= kArray
		for _, e := range v {
			s.add(path+"[]", e)
		}
	case string:
		s[path] |= kString
	case json.Number:
		s[path] |= kNumber
	case bool:
		s[path] |= kBool
	case nil:
		s[path] |= kNull
	}
}

func child(path, key string) string {
	if path == "." {
		return key
	}
	return path + "." + key
}

// parent is the path holding path, "" for the document itself.
func parent(path string) string {
	if path == "." {
		return ""
	}
	if p, ok := strings.CutSuffix(path, "[]"); ok {
		return p
	}
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[:i]
	}
	return "."
}

var (
	customRe = regexp.MustCompile(`^customfield_\d+$`)
	// An id as a map key: a number, an issue key, an account id.
	idKeyRe = regexp.MustCompile(`^(\d+|[A-Z][A-Z0-9_]*-\d+|[0-9a-f]{24}|\d+:[0-9a-f-]{36})$`)
)

// normKey folds the keys that differ between sites but not in meaning.
// Custom fields stay as they are until named (Proxy.Doc).
func normKey(k string) string {
	if idKeyRe.MatchString(k) && !customRe.MatchString(k) {
		return "*"
	}
	return k
}

// named is d with each custom field id in its paths swapped for the
// field's type, "customfield[gh-sprint]", or "customfield_*" if kinds lacks
// it. Sites number and name (in their language) custom fields differently,
// but a type is the same everywhere.
func (d Doc) named(kinds map[string]string) Doc {
	out := Doc{}
	for ep, s := range d {
		ns := Shape{}
		for p, k := range s {
			segs := strings.Split(p, ".")
			for i, seg := range segs {
				id, arr := strings.CutSuffix(seg, "[]")
				if !customRe.MatchString(id) {
					continue
				}
				seg = "customfield_*"
				if typ, ok := kinds[id]; ok {
					seg = "customfield[" + typ + "]"
				}
				if arr {
					seg += "[]"
				}
				segs[i] = seg
			}
			ns[strings.Join(segs, ".")] |= k
		}
		out[ep] = ns
	}
	return out
}

var (
	issueKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*-\d+$`)
	projectRe  = regexp.MustCompile(`^[A-Z][A-Z0-9_]+$`)
	numberRe   = regexp.MustCompile(`^\d+$`)
)

// endpoint names a request by its method, its path with ids folded, and
// the status it got. The API's version ("/rest/api/3") stays.
func endpoint(method, path string, status int) string {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		switch {
		case i <= 3:
		case numberRe.MatchString(s):
			segs[i] = "{id}"
		case issueKeyRe.MatchString(s):
			segs[i] = "{key}"
		case projectRe.MatchString(s):
			segs[i] = "{project}"
		}
	}
	return fmt.Sprintf("%s %s %d", method, strings.Join(segs, "/"), status)
}

// Write prints d one endpoint at a time, sorted, each path on a tab-indented
// line, so a re-recording diffs line by line.
func (d Doc) Write(w io.Writer, header string) error {
	bw := bufio.NewWriter(w)
	for line := range strings.SplitSeq(strings.TrimSpace(header), "\n") {
		fmt.Fprintf(bw, "# %s\n", line)
	}
	for _, ep := range slices.Sorted(maps.Keys(d)) {
		fmt.Fprintf(bw, "\n%s\n", ep)
		for _, p := range slices.Sorted(maps.Keys(d[ep])) {
			fmt.Fprintf(bw, "\t%s %s\n", p, d[ep][p])
		}
	}
	return bw.Flush()
}

// ReadDoc parses what Write printed.
func ReadDoc(r io.Reader) (Doc, error) {
	d := Doc{}
	var cur Shape
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "\t"):
			path, typ, ok := strings.Cut(strings.TrimPrefix(line, "\t"), " ")
			k, err := parseKinds(typ)
			if !ok || err != nil || cur == nil {
				return nil, fmt.Errorf("line %d: %q", n, line)
			}
			cur[path] = k
		default:
			cur = Shape{}
			d[line] = cur
		}
	}
	return d, sc.Err()
}
