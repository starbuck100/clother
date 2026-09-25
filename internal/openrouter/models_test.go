package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testModel = `{"id":"vendor/free","context_length":32768,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"supported_parameters":["tools"],"top_provider":{"context_length":16384,"max_completion_tokens":2048},"pricing":{"prompt":"0","completion":"0.0000","overrides":[]}}`

func TestFreeAndCompatible(t *testing.T) {
	var model Model
	if err := json.Unmarshal([]byte(testModel), &model); err != nil {
		t.Fatal(err)
	}
	if !model.Free() || model.Validate() != nil || model.ContextLimit() != 16384 {
		t.Fatal("free model or conservative context limit not recognized")
	}
	for _, pricing := range []string{
		`{"prompt":"0","completion":"0.01"}`,
		`{"prompt":"0","completion":"0","request":"0.01"}`,
		`{"prompt":"0"}`,
		`{"prompt":"bad","completion":"0"}`,
		`{"prompt":"-1","completion":"0"}`,
		`{"prompt":"0","completion":"0","overrides":[{"completion":"1"}]}`,
	} {
		model.Pricing = nil
		if err := json.Unmarshal([]byte(pricing), &model.Pricing); err != nil {
			t.Fatal(err)
		}
		if model.Free() {
			t.Errorf("must not label %s free", pricing)
		}
	}
	model.SupportedParameters = nil
	if model.Validate() == nil {
		t.Fatal("model without tools accepted")
	}
	model.SupportedParameters = []string{"tools"}
	model.Architecture.OutputModalities = []string{"audio"}
	if model.Validate() == nil {
		t.Fatal("non-text output accepted")
	}
}

func TestCatalogRefreshCacheAndFailure(t *testing.T) {
	calls := 0
	status := 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/models" || r.Header.Get("Authorization") != "" {
			t.Error("wrong endpoint or credentials sent to public catalog")
		}
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"data":[%s]}`, testModel)
	}))
	defer server.Close()
	cache := t.TempDir()
	base := server.URL + "/api"
	for _, refresh := range []bool{true, false, true} {
		catalog, err := Load(context.Background(), base, cache, refresh)
		if err != nil || len(catalog.FreeModels()) != 1 {
			t.Fatalf("catalog=%+v err=%v", catalog, err)
		}
		if _, err := catalog.Find("absent/model"); err == nil {
			t.Fatal("unknown model accepted")
		}
	}
	if calls != 2 {
		t.Fatalf("expected live, cache, live; got %d requests", calls)
	}
	status = 503
	if _, err := Load(context.Background(), base, cache, true); err == nil {
		t.Fatal("refresh silently used cached prices")
	}
	for _, timestamp := range []time.Time{time.Now().Add(-CacheTTL - time.Minute), time.Now().Add(time.Hour)} {
		var model Model
		_ = json.Unmarshal([]byte(testModel), &model)
		data, _ := json.Marshal(Catalog{BaseURL: base, FetchedAt: timestamp, Models: []Model{model}})
		if err := os.WriteFile(filepath.Join(cache, "openrouter-models.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(context.Background(), base, cache, false); err == nil {
			t.Fatal("invalid cache age accepted")
		}
	}
}

func TestNumericZeroPricing(t *testing.T) {
	for _, tc := range []struct {
		pricing string
		free    bool
	}{
		{`{"prompt":0,"completion":0,"discount":0}`, true},
		{`{"prompt":"0","completion":"0","discount":0}`, true},
		{`{"prompt":null,"completion":0}`, false},
		{`{"prompt":0,"completion":0.01}`, false},
		{`{"prompt":0,"completion":false}`, false},
	} {
		var model Model
		if err := json.Unmarshal([]byte(tc.pricing), &model.Pricing); err != nil {
			t.Fatal(err)
		}
		if model.Free() != tc.free {
			t.Errorf("pricing %s free=%v", tc.pricing, model.Free())
		}
	}
}
