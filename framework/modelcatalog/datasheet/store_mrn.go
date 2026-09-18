package datasheet

import (
	"github.com/neria-cloud/meridian/core/schemas"
	configstoreTables "github.com/neria-cloud/meridian/framework/configstore/tables"
)

// SeedPricingForTest installs a pricing row into the in-memory catalog (test-only).
func (s *Store) SeedPricingForTest(model, provider string, requestType schemas.RequestType, pricing configstoreTables.TableModelPricing) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pricingData[makeKey(model, provider, normalizeRequestType(requestType))] = pricing
}

// SeedOverridesForTest replaces the pricing override index from rows (test-only).
func (s *Store) SeedOverridesForTest(rows ...*configstoreTables.TablePricingOverride) error {
	return s.UpsertOverrides(rows...)
}
