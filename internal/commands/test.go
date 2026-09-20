package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

func runTest(ctx context.Context, c Context, args []string) (int, error) {
	var targets []profiles.Target
	if len(args) > 0 {
		target, err := profiles.Resolve(args[0], c.Catalog, c.Config)
		if err != nil {
			return 1, err
		}
		targets = []profiles.Target{target}
	} else {
		targets = profiles.All(c.Catalog, c.Config)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	okCount, failCount := 0, 0
	for _, target := range targets {
		if target.Profile == "native" {
			continue
		}
		if target.Family == providers.FamilyOpenRouter {
			if err := testOpenRouter(ctx, client, target, c.Secrets[target.SecretKey]); err != nil {
				fmt.Fprintf(c.Output.Stdout, "  %-18s failed: %v\n", target.Profile, err)
				failCount++
			} else {
				fmt.Fprintf(c.Output.Stdout, "  %-18s authenticated (model inference not tested)\n", target.Profile)
				okCount++
			}
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.TestURL, nil)
		if err != nil {
			fmt.Fprintf(c.Output.Stdout, "  %-18s invalid test URL\n", target.Profile)
			failCount++
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(c.Output.Stdout, "  %-18s unreachable\n", target.Profile)
			failCount++
			continue
		}
		_ = resp.Body.Close()
		fmt.Fprintf(c.Output.Stdout, "  %-18s reachable (HTTP %d)\n", target.Profile, resp.StatusCode)
		okCount++
	}
	fmt.Fprintf(c.Output.Stdout, "\nResults: %d reachable, %d failed\n", okCount, failCount)
	if failCount > 0 {
		return 1, nil
	}
	return 0, nil
}

// Authenticate without spending inference credits. Use the configured endpoint
// so an explicit base URL override is tested rather than silently ignored.
func testOpenRouter(ctx context.Context, client *http.Client, target profiles.Target, key string) error {
	if key == "" {
		return fmt.Errorf("OPENROUTER_API_KEY not configured; run clother config openrouter")
	}
	endpoint := strings.TrimRight(target.BaseURL, "/") + "/v1/key"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("invalid OpenRouter endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("OpenRouter key check failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OpenRouter key check returned HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&payload); err != nil || payload.Data == nil {
		return fmt.Errorf("invalid OpenRouter key response")
	}
	return nil
}
