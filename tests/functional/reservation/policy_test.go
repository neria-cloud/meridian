// Package reservation exercises the full chain catalog estimate -> ReserveAmount.
package reservation

import (
	"testing"

	"github.com/neria-cloud/meridian/core/schemas"
	"github.com/neria-cloud/meridian/framework/configstore"
	"github.com/neria-cloud/meridian/framework/configstore/tables"
	"github.com/neria-cloud/meridian/framework/modelcatalog"
	"github.com/neria-cloud/meridian/tests/functional/internal/fixtures"
	"github.com/stretchr/testify/assert"
)

func f(v float64) *float64 { return &v }
func i(v int) *int         { return &v }

func policy(mutate func(*configstore.ReservationConfig)) *configstore.ReservationConfig {
	rc := &configstore.ReservationConfig{}
	if mutate != nil {
		mutate(rc)
	}
	rc.ApplyDefaults()
	return rc
}

func catalog(t *testing.T) *modelcatalog.ModelCatalog {
	t.Helper()
	return fixtures.NewCatalog(t,
		fixtures.Row{Model: "chat", Provider: "openai", RequestType: schemas.ChatCompletionRequest,
			Pricing: tables.TableModelPricing{Mode: "chat", InputCostPerToken: f(1e-6), OutputCostPerToken: f(2e-6), MaxInputTokens: i(1000), MaxOutputTokens: i(100)}},
		fixtures.Row{Model: "img", Provider: "openai", RequestType: schemas.ImageGenerationRequest,
			Pricing: tables.TableModelPricing{Mode: "image_generation", OutputCostPerImage: f(0.04)}},
		fixtures.Row{Model: "stt", Provider: "openai", RequestType: schemas.TranscriptionRequest,
			Pricing: tables.TableModelPricing{Mode: "audio_transcription", InputCostPerAudioPerSecond: f(1e-4)}},
		fixtures.Row{Model: "free", Provider: "openai", RequestType: schemas.ChatCompletionRequest, Pricing: tables.TableModelPricing{Mode: "chat"}},
	)
}

func reserve(t *testing.T, mc *modelcatalog.ModelCatalog, cfg *configstore.ReservationConfig, req *schemas.BifrostRequest, model string) (float64, configstore.ReserveReason) {
	t.Helper()
	est := mc.EstimateMaxCost(req, "openai", model, nil, modelcatalog.EstimateOptionsFor(cfg))
	return modelcatalog.ReserveAmount(cfg, model, est, req.RequestType)
}

func TestReserve_WorstCaseChain(t *testing.T) {
	t.Parallel()
	mc := catalog(t)
	cfg := policy(func(rc *configstore.ReservationConfig) {
		rc.DefaultMandatory = configstore.Reservation{WorstCase: true}
	})
	cases := []struct {
		name   string
		req    *schemas.BifrostRequest
		model  string
		amount float64
		reason configstore.ReserveReason
	}{
		{"chat worst case", fixtures.Chat(schemas.OpenAI, "chat", nil), "chat", 1000*1e-6 + 100*2e-6, configstore.ReserveWorstCase},
		{"image per unit", fixtures.Image(schemas.OpenAI, "img", 2, "", ""), "img", 0.08, configstore.ReserveWorstCase},
		{"stt sentinel", fixtures.Transcription(schemas.OpenAI, "stt"), "stt", 0.25, configstore.ReserveNonText},
		{"unknown text model", fixtures.Chat(schemas.OpenAI, "nope", nil), "nope", 0.01, configstore.ReserveNoEntry},
		{"unknown image model", fixtures.Image(schemas.OpenAI, "nope", 1, "", ""), "nope", 0.25, configstore.ReserveNonText},
		{"zero-rate text row", fixtures.Chat(schemas.OpenAI, "free", nil), "free", 0.01, configstore.ReserveZeroRate},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			amount, reason := reserve(t, mc, cfg, c.req, c.model)
			assert.InDelta(t, c.amount, amount, 1e-12)
			assert.Equal(t, c.reason, reason)
		})
	}
}

func TestReserve_FlatAndOverrides(t *testing.T) {
	t.Parallel()
	mc := catalog(t)
	req := fixtures.Chat(schemas.OpenAI, "chat", nil)
	amount, reason := reserve(t, mc, policy(nil), req, "chat")
	assert.InDelta(t, 0.05, amount, 1e-12)
	assert.Equal(t, configstore.ReserveFlat, reason)

	cfg := policy(func(rc *configstore.ReservationConfig) {
		rc.ModelOverrides = map[string]configstore.Reservation{"chat": {Amount: 0.30}, "img": {WorstCase: true}}
	})
	amount, reason = reserve(t, mc, cfg, req, "chat")
	assert.InDelta(t, 0.30, amount, 1e-12)
	assert.Equal(t, configstore.ReserveOverrideFlat, reason)
	amount, reason = reserve(t, mc, cfg, fixtures.Image(schemas.OpenAI, "img", 1, "", ""), "img")
	assert.InDelta(t, 0.04, amount, 1e-12)
	assert.Equal(t, configstore.ReserveWorstCase, reason)

	off := false
	amount, reason = reserve(t, mc, policy(func(rc *configstore.ReservationConfig) { rc.Enabled = &off }), req, "chat")
	assert.InDelta(t, 1e-9, amount, 1e-15)
	assert.Equal(t, configstore.ReserveDisabled, reason)
}

func TestReserve_UnknownModelBoundsFlowIntoEstimate(t *testing.T) {
	t.Parallel()
	mc := fixtures.NewCatalog(t)
	fixtures.WithOverrides(t, mc, fixtures.Override("g", modelcatalog.ScopeKindGlobal, "self-hosted", schemas.ChatCompletionRequest,
		map[string]any{"input_cost_per_token": 1e-6, "output_cost_per_token": 2e-6}))
	req := fixtures.Chat(schemas.ModelProvider("custom"), "self-hosted", nil)
	worst := func(rc *configstore.ReservationConfig) {
		rc.DefaultMandatory = configstore.Reservation{WorstCase: true}
	}
	est := mc.EstimateMaxCost(req, "custom", "self-hosted", nil, modelcatalog.EstimateOptionsFor(policy(worst)))
	amount, _ := modelcatalog.ReserveAmount(policy(worst), "self-hosted", est, req.RequestType)
	assert.InDelta(t, 4096*1e-6+4096*2e-6, amount, 1e-12)

	big := policy(func(rc *configstore.ReservationConfig) {
		worst(rc)
		rc.UnknownModelBounds = &configstore.UnknownModelBounds{InputTokens: 131072, OutputTokens: 8192}
	})
	est = mc.EstimateMaxCost(req, "custom", "self-hosted", nil, modelcatalog.EstimateOptionsFor(big))
	amount, reason := modelcatalog.ReserveAmount(big, "self-hosted", est, req.RequestType)
	assert.InDelta(t, 131072*1e-6+8192*2e-6, amount, 1e-12)
	assert.Equal(t, configstore.ReserveWorstCase, reason)
}
