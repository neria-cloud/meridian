//go:build pg

// Package pg runs the override path through a real PostgreSQL config store.
package pg

import (
	"context"
	"testing"
	"time"

	meridian "github.com/neria-cloud/meridian/core"
	"github.com/neria-cloud/meridian/core/schemas"
	"github.com/neria-cloud/meridian/framework/configstore"
	"github.com/neria-cloud/meridian/framework/configstore/tables"
	"github.com/neria-cloud/meridian/framework/modelcatalog"
	"github.com/neria-cloud/meridian/framework/modelcatalog/datasheet"
	"github.com/neria-cloud/meridian/tests/functional/internal/fixtures"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestOverridesLoadedFromPostgres(t *testing.T) {
	ctx := context.Background()
	pg, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("functional"), postgres.WithUsername("test"), postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pg.Terminate(context.Background()) })
	host, err := pg.Host(ctx)
	require.NoError(t, err)
	port, err := pg.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)

	logger := meridian.NewNoOpLogger()
	cs, err := configstore.NewConfigStore(ctx, &configstore.Config{Enabled: true, Type: configstore.ConfigStoreTypePostgres, Config: &configstore.PostgresConfig{
		Host: schemas.NewSecretVar(host), Port: schemas.NewSecretVar(port.Port()), User: schemas.NewSecretVar("test"),
		Password: schemas.NewSecretVar("test"), DBName: schemas.NewSecretVar("functional"), SSLMode: schemas.NewSecretVar("disable"),
	}}, logger)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close(ctx) })

	row := fixtures.Override("pg-1", modelcatalog.ScopeKindGlobal, "img", schemas.ImageGenerationRequest, map[string]any{"output_cost_per_image": 0.40})
	require.NoError(t, cs.CreatePricingOverride(ctx, row))

	ds := datasheet.New(cs, logger, datasheet.Config{})
	ds.SeedPricingForTest("img", "openai", schemas.ImageGenerationRequest, tables.TableModelPricing{Model: "img", Provider: "openai", Mode: "image_generation", OutputCostPerImage: fixtures.Ptr(0.04)})
	require.NoError(t, ds.LoadOverridesFromStore(ctx))
	mc := modelcatalog.NewTestCatalogWithDatasheet(ds)
	est := mc.EstimateMaxCost(fixtures.Image(schemas.OpenAI, "img", 2, "", ""), "openai", "img", nil, modelcatalog.EstimateOptions{})
	assert.InDelta(t, 0.80, est.Cost, 1e-9, "the persisted override prices the estimate")
}
