package benchmarks

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchDiscoversLatestDatasetAndSeparatesVersions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<script src="./static/js/main.abc.js"></script>`)
		case "/static/js/main.abc.js":
			fmt.Fprint(w, `const versions=["2025-01-01","2025-02-01","2026-01-01","2026-06-25"];`)
		case "/table_2026_06_25.csv":
			w.Header().Set("Last-Modified", "Tue, 22 Sep 2026 20:51:19 GMT")
			fmt.Fprint(w, "model,code,math,logic\nqwen3.8-27b,90,80,60\nqwen3.8-27b-high,95,85,70\nunknown,,10,20\n")
		case "/categories_2026_06_25.json":
			fmt.Fprint(w, `{"Coding":["code"],"Mathematics":["math"],"Reasoning":["logic"]}`)
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	r, e := Fetch(context.Background(), server.URL)
	if e != nil {
		t.Fatal(e)
	}
	match := r.Match("qwen/qwen3.8-27b:free")
	if r.Dataset != "2026-06-25" || r.Modified == "" || len(match) != 1 || match[0].Categories["Coding"] != 90 {
		t.Fatal(r)
	}
	if len(r.Match("stealth/space-bunny-alpha")) != 0 {
		t.Fatal("fabricated stealth identity")
	}
	if _, ok := r.Rows[2].Categories["Coding"]; ok {
		t.Fatal("missing score converted to zero")
	}
}
