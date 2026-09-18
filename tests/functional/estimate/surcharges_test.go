package estimate

import (
	"testing"

	"github.com/neria-cloud/meridian/core/schemas"
	"github.com/neria-cloud/meridian/tests/functional/internal/fixtures"
	"github.com/stretchr/testify/assert"
)

func TestSurcharges_WebSearch(t *testing.T) {
	t.Parallel()
	row := chatRow(1e-6, 2e-6, 1000, 100)
	row.SearchContextCostPerQuery = f(0.01)
	mc := chatCatalog(t, row)
	base := 1000*1e-6 + 100*2e-6
	plain := mc.EstimateMaxCost(fixtures.Chat(openai, "gpt-test", nil), "openai", "gpt-test", nil, none)
	assert.InDelta(t, base, plain.Cost, 1e-12)
	assert.Zero(t, plain.Surcharges)
	withOpts := mc.EstimateMaxCost(fixtures.Chat(openai, "gpt-test", &schemas.ChatParameters{WebSearchOptions: &schemas.ChatWebSearchOptions{}}), "openai", "gpt-test", nil, none)
	assert.InDelta(t, base+0.01, withOpts.Cost, 1e-12)
	withTool := mc.EstimateMaxCost(fixtures.Chat(openai, "gpt-test", &schemas.ChatParameters{Tools: []schemas.ChatTool{{Type: schemas.ChatToolType("web_search_20260209")}}}), "openai", "gpt-test", nil, none)
	assert.InDelta(t, 0.01, withTool.Surcharges, 1e-12)
	resp := mc.EstimateMaxCost(fixtures.Responses(openai, "gpt-test", &schemas.ResponsesParameters{Tools: []schemas.ResponsesTool{{Type: schemas.ResponsesToolType("web_search_preview")}}}), "openai", "gpt-test", nil, none)
	assert.InDelta(t, base+0.01, resp.Cost, 1e-12)
}

func TestSurcharges_ImageParts(t *testing.T) {
	t.Parallel()
	row := chatRow(1e-6, 2e-6, 1000, 100)
	row.InputCostPerImage = f(0.002)
	mc := chatCatalog(t, row)
	est := mc.EstimateMaxCost(fixtures.ChatWithImages(openai, "gpt-test", 3), "openai", "gpt-test", nil, none)
	assert.InDelta(t, 1000*1e-6+100*2e-6+3*0.002, est.Cost, 1e-12)
	assert.InDelta(t, 0.006, est.Surcharges, 1e-12)
}
