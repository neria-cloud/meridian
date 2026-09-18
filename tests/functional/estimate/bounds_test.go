package estimate

import (
	"testing"

	"github.com/neria-cloud/meridian/core/schemas"
	"github.com/neria-cloud/meridian/framework/configstore/tables"
	"github.com/neria-cloud/meridian/framework/modelcatalog"
	"github.com/neria-cloud/meridian/tests/functional/internal/fixtures"
	"github.com/stretchr/testify/assert"
)

func TestBounds_FromEntry(t *testing.T) {
	t.Parallel()
	mc := chatCatalog(t, chatRow(1e-6, 2e-6, 8192, 2048))
	est := mc.EstimateMaxCost(fixtures.Chat(openai, "gpt-test", nil), "openai", "gpt-test", nil, none)
	assert.Equal(t, modelcatalog.EstimateTokens, est.Source)
	assert.True(t, est.BoundsFromEntry)
	assert.Equal(t, 8192, est.MaxInputTokens)
	assert.Equal(t, 2048, est.MaxOutputTokens)
	assert.InDelta(t, 8192*1e-6+2048*2e-6, est.Cost, 1e-12)
}

func TestBounds_CallerCapsPerRequestType(t *testing.T) {
	t.Parallel()
	mc := fixtures.NewCatalog(t,
		fixtures.Row{Model: "gpt-test", Provider: "openai", RequestType: schemas.ChatCompletionRequest, Pricing: chatRow(1e-6, 2e-6, 8192, 2048)},
		fixtures.Row{Model: "gpt-test", Provider: "openai", RequestType: schemas.TextCompletionRequest, Pricing: tables.TableModelPricing{Mode: "completion", InputCostPerToken: f(1e-6), OutputCostPerToken: f(2e-6)}},
		fixtures.Row{Model: "emb-test", Provider: "openai", RequestType: schemas.EmbeddingRequest, Pricing: tables.TableModelPricing{Mode: "embedding", InputCostPerToken: f(1e-6), OutputCostPerToken: f(9)}},
	)
	chat := mc.EstimateMaxCost(fixtures.Chat(openai, "gpt-test", &schemas.ChatParameters{MaxCompletionTokens: i(100)}), "openai", "gpt-test", nil, none)
	assert.Equal(t, 100, chat.MaxOutputTokens)
	assert.InDelta(t, 8192*1e-6+100*2e-6, chat.Cost, 1e-12)

	resp := mc.EstimateMaxCost(fixtures.Responses(openai, "gpt-test", &schemas.ResponsesParameters{MaxOutputTokens: i(50)}), "openai", "gpt-test", nil, none)
	assert.Equal(t, 50, resp.MaxOutputTokens)

	text := mc.EstimateMaxCost(fixtures.TextCompletion(openai, "gpt-test", &schemas.TextCompletionParameters{MaxTokens: i(7)}), "openai", "gpt-test", nil, none)
	assert.Equal(t, 7, text.MaxOutputTokens)
	assert.False(t, text.BoundsFromEntry)

	emb := mc.EstimateMaxCost(fixtures.Embedding(openai, "emb-test"), "openai", "emb-test", nil, none)
	assert.Equal(t, 0, emb.MaxOutputTokens)
	assert.InDelta(t, 4096*1e-6, emb.Cost, 1e-12)
}

func TestBounds_FallbacksForCustomPricedModel(t *testing.T) {
	t.Parallel()
	mc := fixtures.NewCatalog(t)
	fixtures.WithOverrides(t, mc, fixtures.Override("g1", modelcatalog.ScopeKindGlobal, "self-hosted", schemas.ChatCompletionRequest,
		map[string]any{"input_cost_per_token": 1e-6, "output_cost_per_token": 2e-6}))
	req := fixtures.Chat(schemas.ModelProvider("custom"), "self-hosted", nil)
	est := mc.EstimateMaxCost(req, "custom", "self-hosted", nil, none)
	assert.Equal(t, modelcatalog.EstimateTokens, est.Source)
	assert.False(t, est.BoundsFromEntry)
	assert.InDelta(t, 4096*1e-6+4096*2e-6, est.Cost, 1e-12)

	est = mc.EstimateMaxCost(req, "custom", "self-hosted", nil, modelcatalog.EstimateOptions{FallbackInputTokens: 32768, FallbackOutputTokens: 8192})
	assert.Equal(t, 32768, est.MaxInputTokens)
	assert.InDelta(t, 32768*1e-6+8192*2e-6, est.Cost, 1e-12)
}

func TestBounds_NoRowIsNone(t *testing.T) {
	t.Parallel()
	mc := fixtures.NewCatalog(t)
	est := mc.EstimateMaxCost(fixtures.Chat(openai, "unknown", nil), "openai", "unknown", nil, none)
	assert.Equal(t, modelcatalog.EstimateNone, est.Source)
	assert.Equal(t, 0.0, est.Cost)
}
