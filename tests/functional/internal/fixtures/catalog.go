// Package fixtures builds catalogs and requests for the functional tests.
package fixtures

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	meridian "github.com/neria-cloud/meridian/core"
	"github.com/neria-cloud/meridian/core/schemas"
	"github.com/neria-cloud/meridian/framework/configstore/tables"
	"github.com/neria-cloud/meridian/framework/modelcatalog"
	"github.com/neria-cloud/meridian/framework/modelcatalog/datasheet"
	"github.com/stretchr/testify/require"
)

// Row is one pricing row seeded into a catalog.
type Row struct {
	Model, Provider string
	RequestType     schemas.RequestType
	Pricing         tables.TableModelPricing
}

// NewCatalog builds an in-memory catalog holding rows.
func NewCatalog(t *testing.T, rows ...Row) *modelcatalog.ModelCatalog {
	t.Helper()
	ds := datasheet.NewTestStore(nil)
	for _, r := range rows {
		p := r.Pricing
		p.Model, p.Provider = r.Model, r.Provider
		ds.SeedPricingForTest(r.Model, r.Provider, r.RequestType, p)
	}
	return modelcatalog.NewTestCatalogWithDatasheet(ds)
}

// WithOverrides installs pricing overrides (custom prices) into mc.
func WithOverrides(t *testing.T, mc *modelcatalog.ModelCatalog, rows ...*tables.TablePricingOverride) {
	t.Helper()
	require.NoError(t, mc.SeedOverridesForTest(rows...))
}

// Override builds one pricing override row.
func Override(id string, scope modelcatalog.ScopeKind, pattern string, rt schemas.RequestType, patch map[string]any) *tables.TablePricingOverride {
	b, _ := json.Marshal(patch)
	return &tables.TablePricingOverride{ID: id, Name: id, ScopeKind: string(scope), MatchType: string(modelcatalog.MatchTypeExact),
		Pattern: pattern, RequestTypes: []schemas.RequestType{rt}, PricingPatchJSON: string(b)}
}

// PricingServer serves sheet (a datasheet JSON keyed by model) over HTTP and
// returns a catalog loaded from it, the way the gateway loads its pricing.
func PricingServer(t *testing.T, sheet map[string]any) *modelcatalog.ModelCatalog {
	t.Helper()
	body, err := json.Marshal(sheet)
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	ds := datasheet.New(nil, meridian.NewNoOpLogger(), datasheet.Config{URL: srv.URL, ModelParametersURL: srv.URL + "/model-parameters"})
	require.NoError(t, ds.LoadFromURLIntoMemory(context.Background()))
	return modelcatalog.NewTestCatalogWithDatasheet(ds)
}

// Ptr returns a pointer to v.
func Ptr[T any](v T) *T { return &v }
