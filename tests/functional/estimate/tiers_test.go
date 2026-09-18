package estimate

import (
	"testing"

	"github.com/neria-cloud/meridian/core/schemas"
	"github.com/neria-cloud/meridian/framework/configstore/tables"
	"github.com/neria-cloud/meridian/tests/functional/internal/fixtures"
	"github.com/stretchr/testify/assert"
)

func tierRow() tables.TableModelPricing {
	row := chatRow(1e-6, 2e-6, 1000, 100)
	row.InputCostPerTokenPriority, row.OutputCostPerTokenPriority = f(2e-6), f(4e-6)
	row.InputCostPerTokenFlex, row.OutputCostPerTokenFlex = f(5e-7), f(1e-6)
	row.InputCostPerTokenFast, row.OutputCostPerTokenFast = f(6e-6), f(3e-5)
	row.InferenceGeoUSMultiplier = f(1.1)
	return row
}

func TestTierCandidates(t *testing.T) {
	t.Parallel()
	mc := chatCatalog(t, tierRow())
	base := 1000*1e-6 + 100*2e-6
	priority := 1000*2e-6 + 100*4e-6
	fast := 1000*6e-6 + 100*3e-5
	st := func(v schemas.BifrostServiceTier) *schemas.BifrostServiceTier { return &v }
	cases := []struct {
		name   string
		params *schemas.ChatParameters
		want   float64
		tier   string
	}{
		{"no params", nil, base, "base"},
		{"default", &schemas.ChatParameters{ServiceTier: st(schemas.BifrostServiceTierDefault)}, base, "base"},
		{"priority", &schemas.ChatParameters{ServiceTier: st(schemas.BifrostServiceTierPriority)}, priority, "priority"},
		{"auto", &schemas.ChatParameters{ServiceTier: st(schemas.BifrostServiceTierAuto)}, priority, "priority"},
		{"flex", &schemas.ChatParameters{ServiceTier: st(schemas.BifrostServiceTierFlex)}, base, "base"},
		{"fast", &schemas.ChatParameters{Speed: fixtures.Ptr("fast")}, fast, "fast"},
		{"us geo", &schemas.ChatParameters{InferenceGeo: fixtures.Ptr("us")}, base * 1.1, "base+us"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			est := mc.EstimateMaxCost(fixtures.Chat(openai, "gpt-test", c.params), "openai", "gpt-test", nil, none)
			assert.InDelta(t, c.want, est.Cost, 1e-12)
			assert.Equal(t, c.tier, est.Tier)
		})
	}
}

func TestTierCandidates_Responses(t *testing.T) {
	t.Parallel()
	mc := chatCatalog(t, tierRow())
	p := schemas.BifrostServiceTierPriority
	est := mc.EstimateMaxCost(fixtures.Responses(openai, "gpt-test", &schemas.ResponsesParameters{ServiceTier: &p}), "openai", "gpt-test", nil, none)
	assert.InDelta(t, 1000*2e-6+100*4e-6, est.Cost, 1e-12)
	assert.Equal(t, "priority", est.Tier)
}
