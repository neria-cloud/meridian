package modelcatalog

import (
	"testing"

	"github.com/neria-cloud/meridian/core/schemas"
	"github.com/neria-cloud/meridian/framework/configstore"
	"github.com/stretchr/testify/assert"
)

func policy(mutate func(*configstore.ReservationConfig)) *configstore.ReservationConfig {
	rc := &configstore.ReservationConfig{}
	if mutate != nil {
		mutate(rc)
	}
	rc.ApplyDefaults()
	return rc
}

func TestReserveAmount_Matrix(t *testing.T) {
	t.Parallel()
	off := false
	configs := map[string]*configstore.ReservationConfig{
		"nil":      nil,
		"disabled": policy(func(rc *configstore.ReservationConfig) { rc.Enabled = &off }),
		"flat":     policy(nil), // default_mandatory 0.05
		"override_flat": policy(func(rc *configstore.ReservationConfig) {
			rc.ModelOverrides = map[string]configstore.Reservation{"m": {Amount: 0.30}}
		}),
		"override_worstcase": policy(func(rc *configstore.ReservationConfig) {
			rc.ModelOverrides = map[string]configstore.Reservation{"m": {WorstCase: true}}
		}),
		"default_worstcase": policy(func(rc *configstore.ReservationConfig) {
			rc.DefaultMandatory = configstore.Reservation{WorstCase: true}
		}),
	}
	estimates := map[string]struct {
		est Estimate
		rt  schemas.RequestType
	}{
		"tokens":     {Estimate{Cost: 1.23, Source: EstimateTokens}, schemas.ChatCompletionRequest},
		"per_unit":   {Estimate{Cost: 0.16, Source: EstimatePerUnit}, schemas.ImageGenerationRequest},
		"zero_text":  {Estimate{Source: EstimateZero}, schemas.ChatCompletionRequest},
		"zero_image": {Estimate{Source: EstimateZero}, schemas.ImageGenerationRequest},
		"none_text":  {Estimate{}, schemas.ChatCompletionRequest},
		"none_image": {Estimate{}, schemas.VideoGenerationRequest},
	}
	type want struct {
		amount float64
		reason configstore.ReserveReason
	}
	worst := map[string]want{
		"tokens":     {1.23, configstore.ReserveWorstCase},
		"per_unit":   {0.16, configstore.ReserveWorstCase},
		"zero_text":  {0.01, configstore.ReserveZeroRate},
		"zero_image": {0.25, configstore.ReserveNonText},
		"none_text":  {0.01, configstore.ReserveNoEntry},
		"none_image": {0.25, configstore.ReserveNonText},
	}
	expect := map[string]map[string]want{
		"nil":                worst,
		"default_worstcase":  worst,
		"override_worstcase": worst,
	}
	for name, cfg := range configs {
		for ename, e := range estimates {
			got, reason := ReserveAmount(cfg, "m", e.est, e.rt)
			var w want
			switch name {
			case "disabled":
				w = want{1e-9, configstore.ReserveDisabled}
			case "flat":
				w = want{0.05, configstore.ReserveFlat}
			case "override_flat":
				w = want{0.30, configstore.ReserveOverrideFlat}
			default:
				w = expect[name][ename]
			}
			assert.InDelta(t, w.amount, got, 1e-12, "%s/%s amount", name, ename)
			assert.Equal(t, w.reason, reason, "%s/%s reason", name, ename)
		}
	}
	// An override for another model falls through to the default.
	got, reason := ReserveAmount(configs["override_flat"], "other", estimates["tokens"].est, schemas.ChatCompletionRequest)
	assert.InDelta(t, 0.05, got, 1e-12)
	assert.Equal(t, configstore.ReserveFlat, reason)
}

func TestEstimateOptionsFor(t *testing.T) {
	t.Parallel()
	assert.Equal(t, EstimateOptions{}, EstimateOptionsFor(nil))
	assert.Equal(t, EstimateOptions{}, EstimateOptionsFor(&configstore.ReservationConfig{}))
	rc := policy(func(rc *configstore.ReservationConfig) {
		rc.UnknownModelBounds = &configstore.UnknownModelBounds{InputTokens: 8192, OutputTokens: 2048}
	})
	assert.Equal(t, EstimateOptions{FallbackInputTokens: 8192, FallbackOutputTokens: 2048}, EstimateOptionsFor(rc))
	assert.Equal(t, EstimateOptions{FallbackInputTokens: 4096, FallbackOutputTokens: 4096}, EstimateOptionsFor(policy(nil)))
}
