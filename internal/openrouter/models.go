// Package openrouter reads the public model catalog without sending credentials.
package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jolehuit/clother/internal/platform"
)

const CacheTTL = 15 * time.Minute

type Model struct {
	ID                  string                     `json:"id"`
	Name                string                     `json:"name"`
	ContextLength       int                        `json:"context_length"`
	Pricing             map[string]json.RawMessage `json:"pricing"`
	SupportedParameters []string                   `json:"supported_parameters"`
	MayTrainOnPrompts   bool                       `json:"mayTrainOnYourPrompts"`
	Architecture        struct {
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
	TopProvider struct {
		ContextLength       int `json:"context_length"`
		MaxCompletionTokens int `json:"max_completion_tokens"`
	} `json:"top_provider"`
}

type Catalog struct {
	BaseURL   string    `json:"base_url"`
	FetchedAt time.Time `json:"fetched_at"`
	Models    []Model   `json:"data"`
}

func (m Model) Supports(parameter string) bool {
	return slices.Contains(m.SupportedParameters, parameter)
}

// Missing, malformed, negative and conditional prices are not evidence of free
// usage. Require known zero input/output prices and no other nonzero charge.
func (m Model) Free() bool {
	for _, key := range []string{"prompt", "completion"} {
		if _, ok := m.Pricing[key]; !ok {
			return false
		}
	}
	for key, raw := range m.Pricing {
		if key == "overrides" && string(raw) == "[]" {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			var number json.Number
			if json.Unmarshal(raw, &number) != nil || number == "" {
				return false
			}
			value = number.String()
		}
		price, ok := new(big.Rat).SetString(value)
		if !ok || price.Sign() != 0 {
			return false
		}
	}
	return true
}

func (m Model) ContextLimit() int {
	limit := m.ContextLength
	if top := m.TopProvider.ContextLength; top > 0 && (limit <= 0 || top < limit) {
		limit = top
	}
	return limit
}

func (m Model) Validate() error {
	if !slices.Contains(m.Architecture.InputModalities, "text") || !slices.Contains(m.Architecture.OutputModalities, "text") {
		return fmt.Errorf("%s does not support text input and output", m.ID)
	}
	if !m.Supports("tools") {
		return fmt.Errorf("%s does not advertise tool calling required by Claude Code", m.ID)
	}
	if m.ContextLimit() < 2 {
		return fmt.Errorf("%s has no usable context limit in the catalog", m.ID)
	}
	return nil
}

func (c Catalog) Find(id string) (Model, error) {
	for _, model := range c.Models {
		if model.ID == id {
			return model, nil
		}
	}
	return Model{}, fmt.Errorf("model %q is not in the current catalog; run clother config for this provider", id)
}

func (c Catalog) FreeModels() []Model {
	var models []Model
	for _, model := range c.Models {
		if model.Free() && model.Validate() == nil {
			models = append(models, model)
		}
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models
}

// Configuration refreshes prices live. Launches reuse a short-lived catalog,
// scoped to the endpoint. Expired/malformed data is never silently accepted.
func Load(ctx context.Context, baseURL, cacheDir string, refresh bool) (Catalog, error) {
	return LoadAt(ctx, baseURL, "/v1/models", cacheDir, "openrouter-models.json", refresh)
}

// LoadAt shares the model format with gateways such as Kilo while keeping
// endpoint-specific caches separate.
func LoadAt(ctx context.Context, baseURL, modelPath, cacheDir, cacheName string, refresh bool) (Catalog, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	path := filepath.Join(cacheDir, cacheName)
	if !refresh && cacheDir != "" {
		if data, err := os.ReadFile(path); err == nil {
			var cached Catalog
			if json.Unmarshal(data, &cached) == nil && cached.BaseURL == baseURL && len(cached.Models) > 0 {
				age := time.Since(cached.FetchedAt)
				if age >= 0 && age < CacheTTL {
					return cached, nil
				}
			}
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+modelPath, nil)
	if err != nil {
		return Catalog{}, fmt.Errorf("invalid model catalog endpoint")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Catalog{}, fmt.Errorf("could not fetch model catalog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Catalog{}, fmt.Errorf("model catalog returned HTTP %d", resp.StatusCode)
	}
	var catalog Catalog
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&catalog); err != nil || len(catalog.Models) == 0 {
		return Catalog{}, fmt.Errorf("invalid or empty model catalog")
	}
	catalog.BaseURL = baseURL
	catalog.FetchedAt = time.Now()
	if data, err := json.Marshal(catalog); err == nil && cacheDir != "" {
		if os.MkdirAll(cacheDir, 0o755) == nil {
			// A read-only cache must not make a successful live lookup fail.
			_ = platform.AtomicWrite(path, data, 0o644)
		}
	}
	return catalog, nil
}
