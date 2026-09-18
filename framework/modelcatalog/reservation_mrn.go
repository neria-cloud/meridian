package modelcatalog

import (
	"github.com/neria-cloud/meridian/core/schemas"
	"github.com/neria-cloud/meridian/framework/configstore"
)

// EstimateOptionsFor derives the estimator fallbacks from the reservation
// policy; a nil config or nil bounds yield the datasheet's 4096 defaults.
func EstimateOptionsFor(cfg *configstore.ReservationConfig) EstimateOptions {
	if cfg == nil || cfg.UnknownModelBounds == nil {
		return EstimateOptions{}
	}
	return EstimateOptions{FallbackInputTokens: cfg.UnknownModelBounds.InputTokens, FallbackOutputTokens: cfg.UnknownModelBounds.OutputTokens}
}

// ReserveAmount applies the reservation policy to a catalog estimate. Pure: it
// reads no store. A nil cfg means the worst-case path. The sentinels keep
// RESERVE alive so FINALIZE settles every request to its actual cost.
func ReserveAmount(cfg *configstore.ReservationConfig, model string, est Estimate, requestType schemas.RequestType) (float64, configstore.ReserveReason) {
	if cfg != nil {
		if !cfg.IsEnabled() {
			return configstore.ReserveDisabledAmount, configstore.ReserveDisabled
		}
		if amount, worstCase := cfg.ResolveReservation(model); !worstCase {
			if cfg.IsOverride(model) {
				return amount, configstore.ReserveOverrideFlat
			}
			return amount, configstore.ReserveFlat
		}
	}
	if est.Cost > 0 {
		return est.Cost, configstore.ReserveWorstCase
	}
	nonText := configstore.IsNonTextRequestType(requestType)
	switch {
	case nonText:
		return configstore.ReserveNonTextAmount, configstore.ReserveNonText
	case est.Source == EstimateNone:
		return configstore.ReserveNoEntryAmount, configstore.ReserveNoEntry
	default:
		return configstore.ReserveNoEntryAmount, configstore.ReserveZeroRate
	}
}
