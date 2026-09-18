package estimate

import (
	"testing"

	"github.com/neria-cloud/meridian/core/schemas"
	"github.com/neria-cloud/meridian/framework/modelcatalog"
	"github.com/neria-cloud/meridian/tests/functional/internal/fixtures"
	"github.com/stretchr/testify/assert"
)

func TestOverrides_ScopesOnTier1(t *testing.T) {
	t.Parallel()
	mc := chatCatalog(t, chatRow(1e-6, 2e-6, 1000, 100))
	vk := fixtures.Override("vk", modelcatalog.ScopeKindVirtualKey, "gpt-test", schemas.ChatCompletionRequest, map[string]any{"input_cost_per_token": 2e-6})
	vk.VirtualKeyID = fixtures.Ptr("vk-a")
	user := fixtures.Override("user", modelcatalog.ScopeKindUser, "gpt-test", schemas.ChatCompletionRequest, map[string]any{"input_cost_per_token": 3e-6})
	user.UserID = fixtures.Ptr("u-1")
	provider := fixtures.Override("prov", modelcatalog.ScopeKindProvider, "gpt-test", schemas.ChatCompletionRequest, map[string]any{"input_cost_per_token": 4e-6})
	provider.ProviderID = fixtures.Ptr("openai")
	global := fixtures.Override("glob", modelcatalog.ScopeKindGlobal, "gpt-test", schemas.ChatCompletionRequest, map[string]any{"input_cost_per_token": 5e-6})
	fixtures.WithOverrides(t, mc, vk, user, provider, global)
	req := fixtures.Chat(openai, "gpt-test", nil)
	cost := func(scopes *modelcatalog.PricingLookupScopes) float64 {
		return mc.EstimateMaxCost(req, "openai", "gpt-test", scopes, none).Cost
	}
	out := 100 * 2e-6
	assert.InDelta(t, 1000*2e-6+out, cost(&modelcatalog.PricingLookupScopes{VirtualKeyID: "vk-a", UserID: "u-1", Provider: "openai"}), 1e-12, "virtual key wins")
	assert.InDelta(t, 1000*3e-6+out, cost(&modelcatalog.PricingLookupScopes{UserID: "u-1", Provider: "openai"}), 1e-12, "then user")
	assert.InDelta(t, 1000*4e-6+out, cost(&modelcatalog.PricingLookupScopes{Provider: "openai"}), 1e-12, "then provider")
	assert.InDelta(t, 1000*4e-6+out, cost(nil), 1e-12, "nil scopes still carry the provider")
	assert.InDelta(t, 1000*5e-6+out, cost(&modelcatalog.PricingLookupScopes{Provider: "other"}), 1e-12, "global last")
}

func TestOverrides_OnTier2(t *testing.T) {
	t.Parallel()
	mc := fixtures.NewCatalog(t, fixtures.Row{Model: "img", Provider: "openai", RequestType: schemas.ImageGenerationRequest, Pricing: imageRow()})
	vk := fixtures.Override("vk", modelcatalog.ScopeKindVirtualKey, "img", schemas.ImageGenerationRequest, map[string]any{"output_cost_per_image": 0.40})
	vk.VirtualKeyID = fixtures.Ptr("vk-a")
	fixtures.WithOverrides(t, mc, vk)
	req := fixtures.Image(openai, "img", 2, "256x256", "")
	assert.InDelta(t, 0.80, mc.EstimateMaxCost(req, "openai", "img", &modelcatalog.PricingLookupScopes{VirtualKeyID: "vk-a", Provider: "openai"}, none).Cost, 1e-9)
	assert.InDelta(t, 0.08, mc.EstimateMaxCost(req, "openai", "img", &modelcatalog.PricingLookupScopes{VirtualKeyID: "vk-b", Provider: "openai"}, none).Cost, 1e-9)
}

func TestOverrides_CustomPricedModelWithoutDatasheetRow(t *testing.T) {
	t.Parallel()
	mc := fixtures.NewCatalog(t)
	fixtures.WithOverrides(t, mc, fixtures.Override("g", modelcatalog.ScopeKindGlobal, "llama-local", schemas.ImageGenerationRequest, map[string]any{"output_cost_per_image": 0.01}))
	est := mc.EstimateMaxCost(fixtures.Image(schemas.ModelProvider("custom"), "llama-local", 3, "", ""), "custom", "llama-local", nil, none)
	assert.Equal(t, modelcatalog.EstimatePerUnit, est.Source)
	assert.InDelta(t, 0.03, est.Cost, 1e-9)
}

func TestDatasheetLoadedFromURL(t *testing.T) {
	t.Parallel()
	mc := fixtures.PricingServer(t, map[string]any{
		"gpt-sheet": map[string]any{"provider": "openai", "mode": "chat", "input_cost_per_token": 1e-6, "output_cost_per_token": 2e-6, "max_input_tokens": 1000, "max_output_tokens": 100},
		"img-sheet": map[string]any{"provider": "openai", "mode": "image_generation", "output_cost_per_image": 0.05},
	})
	chat := mc.EstimateMaxCost(fixtures.Chat(openai, "gpt-sheet", nil), "openai", "gpt-sheet", nil, none)
	assert.InDelta(t, 1000*1e-6+100*2e-6, chat.Cost, 1e-12)
	assert.True(t, chat.BoundsFromEntry)
	img := mc.EstimateMaxCost(fixtures.Image(openai, "img-sheet", 2, "", ""), "openai", "img-sheet", nil, none)
	assert.InDelta(t, 0.10, img.Cost, 1e-9)
}
