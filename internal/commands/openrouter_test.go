package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/ui"
)

func TestConfigOpenRouterDefaultAndAliases(t *testing.T) {
	c, _ := testSessionContext(t)
	setTestOpenRouterCatalog(t, c)
	c.Prompt = ui.NewPrompter(strings.NewReader("test-key\nvendor/default:free\nvendor/other\nother\n\n"), io.Discard)
	code, err := configOpenRouter(context.Background(), c)
	if err != nil || code != 0 {
		t.Fatalf("configure: code=%d err=%v", code, err)
	}
	stored, err := config.LoadConfig(c.Paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ProviderOverrides["openrouter"].Model != "vendor/default:free" || stored.OpenRouterAliases["other"] != "vendor/other" || stored.OpenRouterAliases["kimi"] == "" {
		t.Fatalf("default or aliases lost: %+v", stored)
	}
	secrets, err := config.LoadSecrets(c.Paths.SecretsFile)
	if err != nil || secrets["OPENROUTER_API_KEY"] != "test-key" {
		t.Fatal("key not persisted")
	}
	c.Prompt = ui.NewPrompter(strings.NewReader("\n\n\n"), io.Discard)
	if code, err := configOpenRouter(context.Background(), c); err != nil || code != 0 {
		t.Fatalf("keep defaults: code=%d err=%v", code, err)
	}
	if c.Config.ProviderOverrides["openrouter"].Model != "vendor/default:free" {
		t.Fatal("reconfiguration lost the default")
	}
	c.Prompt = ui.NewPrompter(strings.NewReader("\n1\n\n"), io.Discard)
	if code, err := configOpenRouter(context.Background(), c); err != nil || code != 0 {
		t.Fatalf("numeric choice: code=%d err=%v", code, err)
	}
	if c.Config.ProviderOverrides["openrouter"].Model != "vendor/default:free" {
		t.Fatal("numeric choice not resolved")
	}
}

func TestConfigOpenRouterRejectsIncompleteInput(t *testing.T) {
	for _, input := range []string{"\n", "test-key\ninvalid\n", "test-key\nvendor/model\ninvalid\n"} {
		t.Run(input, func(t *testing.T) {
			c, _ := testSessionContext(t)
			setTestOpenRouterCatalog(t, c)
			c.Prompt = ui.NewPrompter(strings.NewReader(input), io.Discard)
			if code, err := configOpenRouter(context.Background(), c); code == 0 || err == nil {
				t.Fatal("expected invalid configuration to fail")
			}
			if _, err := os.Stat(c.Paths.ConfigFile); !os.IsNotExist(err) {
				t.Fatal("invalid configuration was persisted")
			}
		})
	}
}

func TestOpenRouterAuthenticationCheck(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		key    string
		wantOK bool
	}{
		{"valid", 200, `{"data":{"label":"test"}}`, "test-key", true},
		{"unauthorized", 401, `{"error":"secret must not be printed"}`, "test-key", false},
		{"server error", 500, "oops", "test-key", false},
		{"html", 200, "<html>ok</html>", "test-key", false},
		{"missing data", 200, `{}`, "test-key", false},
		{"missing key", 200, `{"data":{}}`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/v1/key" || r.Method != "GET" || r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("wrong authentication request")
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			err := testOpenRouter(context.Background(), server.Client(), profiles.Target{BaseURL: server.URL + "/api"}, tc.key)
			if (err == nil) != tc.wantOK {
				t.Fatalf("error=%v, wantOK=%v", err, tc.wantOK)
			}
			if tc.key == "" && calls != 0 {
				t.Fatal("request sent without a key")
			}
			if err != nil && strings.Contains(err.Error(), "secret must not be printed") {
				t.Fatal("response body leaked")
			}
		})
	}
}

func TestRunTestOpenRouterFailureExitCode(t *testing.T) {
	c, out := testSessionContext(t)
	code, err := runTest(context.Background(), c, []string{"openrouter"})
	if err != nil || code != 1 || !strings.Contains(out.String(), "OPENROUTER_API_KEY not configured") {
		t.Fatalf("code=%d err=%v output=%s", code, err, out.String())
	}
}

func setTestOpenRouterCatalog(t *testing.T, c Context) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			t.Error("wrong model catalog URL")
		}
		fmt.Fprint(w, `{"data":[
		{"id":"vendor/default:free","context_length":32768,"pricing":{"prompt":"0","completion":"0"},"supported_parameters":["tools"],"architecture":{"input_modalities":["text"],"output_modalities":["text"]}},
		{"id":"vendor/other","context_length":32768,"pricing":{"prompt":"0.01","completion":"0.01"},"supported_parameters":["tools"],"architecture":{"input_modalities":["text"],"output_modalities":["text"]}},
		{"id":"vendor/model","context_length":32768,"pricing":{"prompt":"0","completion":"0"},"supported_parameters":["tools"],"architecture":{"input_modalities":["text"],"output_modalities":["text"]}}
		]}`)
	}))
	t.Cleanup(server.Close)
	c.Config.ProviderOverrides["openrouter"] = config.ProviderOverride{BaseURL: server.URL + "/api"}
}
