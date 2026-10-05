package contract

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
)

// readPOSTs are the POSTs that only read; any other write is refused before
// it leaves, so a recording can't change the site.
var readPOSTs = map[string]bool{
	"/rest/api/3/search/jql":               true,
	"/rest/api/3/search/approximate-count": true,
}

// Proxy forwards the app's requests to a Jira and keeps the outline of
// each JSON answer.
type Proxy struct {
	rp *httputil.ReverseProxy

	mu  sync.Mutex
	doc Doc
	// kinds are the custom fields' types by id, from the field list.
	kinds map[string]string
	// Refused are the writes kept from the site.
	refused []string
}

// NewProxy proxies to the Jira at target.
func NewProxy(target string) (*Proxy, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	p := &Proxy{doc: Doc{}, kinds: map[string]string{}}
	p.rp = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(u)
			// The transport's own gzip, undone before ModifyResponse reads it.
			pr.Out.Header.Del("Accept-Encoding")
		},
		ModifyResponse: p.keep,
	}
	return p, nil
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && !(r.Method == http.MethodPost && readPOSTs[r.URL.Path]) {
		p.mu.Lock()
		p.refused = append(p.refused, r.Method+" "+r.URL.Path)
		p.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errorMessages":["contract: a recording only reads"]}`))
		return
	}
	p.rp.ServeHTTP(w, r)
}

func (p *Proxy) keep(resp *http.Response) error {
	if !strings.Contains(resp.Header.Get("Content-Type"), "json") {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil || len(bytes.TrimSpace(body)) == 0 {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return nil
	}
	ep := endpoint(resp.Request.Method, resp.Request.URL.Path, resp.StatusCode)
	p.mu.Lock()
	defer p.mu.Unlock()
	if ep == "GET /rest/api/3/field 200" {
		var fields []struct {
			ID     string
			Schema struct{ Custom string }
		}
		_ = json.Unmarshal(body, &fields)
		for _, f := range fields {
			if _, typ, ok := strings.Cut(f.Schema.Custom, ":"); ok {
				p.kinds[f.ID] = typ
			}
		}
	}
	s := p.doc[ep]
	if s == nil {
		s = Shape{}
		p.doc[ep] = s
	}
	s.add(".", v)
	return nil
}

// Doc is the outline kept so far, custom fields by type.
func (p *Proxy) Doc() Doc {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.doc.named(p.kinds)
}

// Refused are the writes kept from the site.
func (p *Proxy) Refused() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.refused
}
