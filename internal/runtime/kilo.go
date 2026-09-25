package runtime

import (
	"context"
	"github.com/jolehuit/clother/internal/kilo"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

func PrepareKiloProxy(ctx context.Context, cache string, target profiles.Target, env []string) ([]string, func(), error) {
	if target.Family != providers.FamilyKilo {
		return env, func() {}, nil
	}
	catalog, err := kilo.Load(ctx, target.BaseURL, cache, false)
	if err != nil {
		return nil, nil, err
	}
	values := envSliceToMap(env)
	endpoint, token, cleanup, err := kilo.Start(ctx, target.BaseURL, values["ANTHROPIC_AUTH_TOKEN"], catalog)
	if err != nil {
		return nil, nil, err
	}
	values["ANTHROPIC_BASE_URL"] = endpoint
	values["ANTHROPIC_AUTH_TOKEN"] = token
	values["ANTHROPIC_API_KEY"] = ""
	return flattenEnv(values), cleanup, nil
}
