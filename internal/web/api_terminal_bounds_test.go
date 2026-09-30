package web

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"
)

func TestTermDimBounds(t *testing.T) {
	for in, want := range map[string]uint16{"": 80, "x": 80, "70000": 80, "-1": 80, "0": 10, "9999": 500, "120": 120} {
		if got := termDim(in, 80, 10, 500); got != want {
			t.Errorf("termDim(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestSecureRequest(t *testing.T) {
	for host, want := range map[string]bool{"localhost:8080": true, "127.0.0.1:1": true, "[::1]:1": true, "example.com": false} {
		r := httptest.NewRequest("GET", "http://"+host+"/", nil)
		if got := secureRequest(r); got != want {
			t.Errorf("%s = %v", host, got)
		}
	}
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	r.TLS = &tls.ConnectionState{}
	if !secureRequest(r) {
		t.Error("tls")
	}
	r = httptest.NewRequest("GET", "http://example.com/", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	if !secureRequest(r) {
		t.Error("proxy")
	}
}

func TestSafeArg(t *testing.T) {
	for _, s := range []string{"-x", "a b", "a;b", "", "$(x)"} {
		if safeArg.MatchString(s) {
			t.Errorf("%q passed", s)
		}
	}
	if !safeArg.MatchString("issue/LAN-1-x") {
		t.Error("branch refused")
	}
}
