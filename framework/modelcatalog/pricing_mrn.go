package modelcatalog

import (
	"github.com/neria-cloud/meridian/core/schemas"
	configstoreTables "github.com/neria-cloud/meridian/framework/configstore/tables"
	"github.com/neria-cloud/meridian/framework/modelcatalog/datasheet"
)

// Estimation re-exports: the worst-case pricing lives in the datasheet store.
type (
	Estimate        = datasheet.Estimate
	EstimateOptions = datasheet.EstimateOptions
	EstimateSource  = datasheet.EstimateSource
)

const (
	EstimateNone    = datasheet.EstimateNone
	EstimateTokens  = datasheet.EstimateTokens
	EstimatePerUnit = datasheet.EstimatePerUnit
	EstimateZero    = datasheet.EstimateZero
)

// EstimateMaxCost prices the worst case of request (tiers 1 and 2) through the
// override-aware pricing lookup. See datasheet.Store.EstimateMaxCost.
func (mc *ModelCatalog) EstimateMaxCost(request *schemas.BifrostRequest, provider, model string, scopes *PricingLookupScopes, opts EstimateOptions) Estimate {
	return mc.datasheet.EstimateMaxCost(request, provider, model, scopes, opts)
}

// EstimateRequestCost is the per-unit (image / speech / video) estimate alone.
func (mc *ModelCatalog) EstimateRequestCost(request *schemas.BifrostRequest, provider, model string, scopes *PricingLookupScopes) float64 {
	return mc.datasheet.EstimateRequestCost(request, provider, model, scopes)
}

// SeedPricingForTest installs a pricing row into the catalog (test-only).
func (mc *ModelCatalog) SeedPricingForTest(model, provider string, requestType schemas.RequestType, pricing configstoreTables.TableModelPricing) {
	mc.datasheet.SeedPricingForTest(model, provider, requestType, pricing)
}

// SeedOverridesForTest replaces the pricing override index (test-only).
func (mc *ModelCatalog) SeedOverridesForTest(rows ...*configstoreTables.TablePricingOverride) error {
	return mc.datasheet.SeedOverridesForTest(rows...)
}
