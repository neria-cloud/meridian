package reservation

import (
	"encoding/json"
	"testing"

	"github.com/neria-cloud/meridian/framework/configstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	raw := `{"enabled":true,"default_mandatory":"worst_case","model_overrides":{"gpt-4o-mini":0.01,"claude-opus-*":"worst_case"},"on_cancel":"release","unknown_model_bounds":{"input_tokens":8192,"output_tokens":2048}}`
	var rc configstore.ReservationConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &rc))
	rc.ApplyDefaults()
	require.NoError(t, rc.Validate())
	assert.True(t, rc.IsEnabled())
	assert.True(t, rc.DefaultMandatory.WorstCase)
	assert.Equal(t, configstore.OnCancelRelease, rc.OnCancel)
	assert.Equal(t, 8192, rc.UnknownModelBounds.InputTokens)
	amount, worst := rc.ResolveReservation("gpt-4o-mini")
	assert.InDelta(t, 0.01, amount, 1e-12)
	assert.False(t, worst)
	_, worst = rc.ResolveReservation("claude-opus-4")
	assert.True(t, worst)
	out, err := json.Marshal(rc)
	require.NoError(t, err)
	var again configstore.ReservationConfig
	require.NoError(t, json.Unmarshal(out, &again))
	again.ApplyDefaults()
	assert.Equal(t, rc.ModelOverrides, again.ModelOverrides)
	assert.Equal(t, rc.UnknownModelBounds, again.UnknownModelBounds)
}

func TestConfig_Defaults(t *testing.T) {
	t.Parallel()
	var rc configstore.ReservationConfig
	require.NoError(t, json.Unmarshal([]byte(`{}`), &rc))
	rc.ApplyDefaults()
	require.NoError(t, rc.Validate())
	assert.True(t, rc.IsEnabled())
	assert.InDelta(t, 0.05, rc.DefaultMandatory.Amount, 1e-12)
	assert.Equal(t, configstore.OnCancelChargeReserved, rc.OnCancel)
	assert.Equal(t, &configstore.UnknownModelBounds{InputTokens: 4096, OutputTokens: 4096}, rc.UnknownModelBounds)
}

func TestConfig_Rejects(t *testing.T) {
	t.Parallel()
	var rc configstore.ReservationConfig
	assert.Error(t, json.Unmarshal([]byte(`{"default_mandatory":0}`), &rc))
	assert.Error(t, json.Unmarshal([]byte(`{"default_mandatory":"auto"}`), &rc))
	require.NoError(t, json.Unmarshal([]byte(`{"model_overrides":{"claude-[opus":0.3}}`), &rc))
	rc.ApplyDefaults()
	assert.Error(t, rc.Validate())
	var bounds configstore.ReservationConfig
	require.NoError(t, json.Unmarshal([]byte(`{"unknown_model_bounds":{"input_tokens":0,"output_tokens":1}}`), &bounds))
	bounds.ApplyDefaults()
	assert.Error(t, bounds.Validate())
}
