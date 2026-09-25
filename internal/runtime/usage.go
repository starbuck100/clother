package runtime

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/kilo"
	"github.com/jolehuit/clother/internal/openrouter"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/usage"
)

func gatewayUsageTransport(ctx context.Context, paths config.Paths, target profiles.Target, env []string) (http.RoundTripper, error) {
	if target.Family != providers.FamilyOpenRouter && target.Family != providers.FamilyKilo {
		return nil, nil
	}
	var catalog openrouter.Catalog
	var err error
	provider := string(target.Family)
	if provider == "kilo" {
		catalog, err = kilo.Load(ctx, target.BaseURL, paths.CacheDir, false)
	} else {
		catalog, err = openrouter.Load(ctx, target.BaseURL, paths.CacheDir, false)
	}
	if err != nil {
		return nil, err
	}
	key := envSliceToMap(env)["ANTHROPIC_AUTH_TOKEN"]
	return &usage.Transport{
		Store: usage.NewStore(paths.DataDir), Provider: provider, AccountScope: usage.Scope(provider, target.BaseURL, key),
		IsFree: func(id string) bool { model, err := catalog.Find(id); return err == nil && model.Free() },
		Warn:   func(message string) { fmt.Fprintln(os.Stderr, "Clother:", message) },
	}, nil
}
