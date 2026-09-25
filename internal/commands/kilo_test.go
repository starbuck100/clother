package commands

import (
	"context"
	"fmt"
	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/platform"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/ui"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigKiloLiveSelection(t *testing.T) {
	for _, tc := range []struct {
		input, model, key string
		valid             bool
	}{
		{"\n1\n", "stealth/free", "", true},
		{"optional-key\npaid/model\n", "paid/model", "optional-key", true},
		{"-\n1\n", "stealth/free", "", true},
		{"\npaid/model\n", "", "", false},
		{"\nunknown\n", "", "", false},
	} {
		t.Run(tc.input, func(t *testing.T) {
			c, out := testSessionContext(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/models" || r.Header.Get("Authorization") != "" {
					t.Error("catalog should be public and use Kilo route")
				}
				fmt.Fprint(w, `{"data":[{"id":"stealth/free","context_length":32768,"pricing":{"prompt":"0","completion":"0","discount":0},"mayTrainOnYourPrompts":true,"supported_parameters":["tools"],"architecture":{"input_modalities":["text"],"output_modalities":["text"]}},{"id":"paid/model","context_length":32768,"pricing":{"prompt":"1","completion":"1"},"supported_parameters":["tools"],"architecture":{"input_modalities":["text"],"output_modalities":["text"]}}]}`)
			}))
			defer server.Close()
			c.Config.ProviderOverrides["kilo"] = config.ProviderOverride{BaseURL: server.URL}
			if strings.HasPrefix(tc.input, "-") {
				c.Secrets["KILO_API_KEY"] = "old-key"
			}
			c.Prompt = ui.NewPrompter(strings.NewReader(tc.input), io.Discard)
			code, err := runConfig(context.Background(), c, []string{"kilo"})
			if !tc.valid {
				if code == 0 || err == nil {
					t.Fatal("invalid selection accepted")
				}
				configNotWritten(t, c)
				return
			}
			if code != 0 || err != nil {
				t.Fatalf("configure: %d %v", code, err)
			}
			if _, err := os.Stat(filepath.Join(c.Paths.BinDir, platform.LauncherName("kilo"))); err != nil {
				t.Fatal("Kilo launcher missing:", err)
			}
			stored, err := config.LoadConfig(c.Paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			if stored.ProviderOverrides["kilo"].Model != tc.model {
				t.Fatal("selection not saved")
			}
			secrets, err := config.LoadSecrets(c.Paths.SecretsFile)
			if err != nil || secrets["KILO_API_KEY"] != tc.key {
				t.Fatal("optional key not persisted correctly")
			}
			target, err := profiles.Resolve("kilo", c.Catalog, stored)
			if err != nil || target.ModelTiers["haiku"] != tc.model {
				t.Fatal("model tiers not resolved")
			}
			if !strings.Contains(out.String(), "provider may train on prompts") {
				t.Fatal("missing catalog data policy")
			}
		})
	}
}
