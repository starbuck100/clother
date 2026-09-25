package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/ui"
)

func TestBudgetConfigurationRejectsInvalidValues(t *testing.T) {
	c, _ := testSessionContext(t)
	for _, value := range []string{"NaN", "+Inf", "-1", "1000001", "bad"} {
		if code, e := runConfig(context.Background(), c, []string{"budget", value, "1"}); code != 2 || e == nil {
			t.Fatalf("accepted %s", value)
		}
	}
	configNotWritten(t, c)
	if code, e := runConfig(context.Background(), c, []string{"budget", "0", "0.25"}); code != 0 || e != nil {
		t.Fatal(code, e)
	}
	saved, e := config.LoadConfig(c.Paths.ConfigFile)
	if e != nil || saved.Budget.Daily != 0 || saved.Budget.Session != 0.25 {
		t.Fatal("budget not preserved")
	}
}
func TestControlCannotSendTokenToExternalHost(t *testing.T) {
	c, _ := testSessionContext(t)
	t.Setenv("CLOTHER_CONTROL_TOKEN", strings.Repeat("a", 64))
	for _, url := range []string{"https://example.com", "http://127.0.0.1:80@evil.invalid", "http://127.0.0.1:80/path", "http://127.0.0.1:80?redirect=evil"} {
		t.Setenv("CLOTHER_CONTROL_URL", url)
		if code, e := sessionControl(context.Background(), c, "free"); code != 2 || e == nil {
			t.Fatal("unsafe control URL accepted")
		}
	}
}
func TestDoctorJSONIsMetadataOnly(t *testing.T) {
	c, out := testSessionContext(t)
	c.Output.Format = ui.FormatJSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"test/free","context_length":100000,"pricing":{"prompt":"0","completion":"0"},"supported_parameters":["tools"],"architecture":{"input_modalities":["text"],"output_modalities":["text"]}}]}`)
	}))
	defer server.Close()
	c.Config.ProviderOverrides["kilo"] = config.ProviderOverride{BaseURL: server.URL, Model: "test/free"}
	c.Secrets["KILO_API_KEY"] = "SECRET-MUST-NOT-LEAK"
	_, e := runDoctor(context.Background(), c, []string{"kilo"})
	if e != nil {
		t.Fatal(e)
	}
	var report map[string]any
	if json.Unmarshal(out.Bytes(), &report) != nil {
		t.Fatal("--json returned prose")
	}
	if strings.Contains(out.String(), "SECRET-MUST-NOT-LEAK") {
		t.Fatal("credential in diagnostics")
	}
	providers := report["providers"].([]any)
	p := providers[0].(map[string]any)
	if p["tool_probe"] != "not run" || p["catalog_reachable"] != true {
		t.Fatal("doctor falsely claimed inference")
	}
}
