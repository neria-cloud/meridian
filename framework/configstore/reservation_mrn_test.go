package configstore

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/neria-cloud/meridian/core/schemas"
)

func TestReservation_UnmarshalJSON_PositiveNumber(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"0.05", 0.05},
		{"1", 1},
		{"1000.5", 1000.5},
	}
	for _, c := range cases {
		var r Reservation
		if err := json.Unmarshal([]byte(c.in), &r); err != nil {
			t.Fatalf("unmarshal %s: %v", c.in, err)
		}
		if r.WorstCase || r.Amount != c.want {
			t.Errorf("input %s: got %+v, want Amount=%v", c.in, r, c.want)
		}
	}
}

func TestReservation_UnmarshalJSON_WorstCaseString(t *testing.T) {
	var r Reservation
	if err := json.Unmarshal([]byte(`"worst_case"`), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !r.WorstCase || r.Amount != 0 {
		t.Errorf("got %+v, want {0, true}", r)
	}
}

func TestReservation_UnmarshalJSON_RejectsZero(t *testing.T) {
	var r Reservation
	if err := json.Unmarshal([]byte(`0`), &r); err == nil {
		t.Fatal("expected error for 0")
	}
}

func TestReservation_UnmarshalJSON_RejectsNegative(t *testing.T) {
	var r Reservation
	if err := json.Unmarshal([]byte(`-0.05`), &r); err == nil {
		t.Fatal("expected error for negative")
	}
}

func TestReservation_UnmarshalJSON_RejectsOtherStrings(t *testing.T) {
	for _, s := range []string{`"auto"`, `"none"`, `""`} {
		var r Reservation
		if err := json.Unmarshal([]byte(s), &r); err == nil {
			t.Errorf("expected error for %s", s)
		}
	}
}

func TestReservation_UnmarshalJSON_RejectsGarbage(t *testing.T) {
	for _, s := range []string{`{"foo":1}`, `true`, `null`, `[1,2]`} {
		var r Reservation
		if err := json.Unmarshal([]byte(s), &r); err == nil {
			t.Errorf("expected error for %s", s)
		}
	}
}

func TestReservation_MarshalJSON_RoundTrip(t *testing.T) {
	cases := []Reservation{
		{Amount: 0.05},
		{Amount: 12.5},
		{WorstCase: true},
	}
	for _, c := range cases {
		data, err := json.Marshal(c)
		if err != nil {
			t.Fatalf("marshal %+v: %v", c, err)
		}
		var got Reservation
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", string(data), err)
		}
		if got != c {
			t.Errorf("round-trip %+v -> %s -> %+v", c, string(data), got)
		}
	}
}

func TestReservationConfig_ApplyDefaults(t *testing.T) {
	var rc ReservationConfig
	rc.ApplyDefaults()
	if !rc.enabled() {
		t.Errorf("Enabled should default to true")
	}
	if rc.DefaultMandatory.Amount != 0.05 || rc.DefaultMandatory.WorstCase {
		t.Errorf("DefaultMandatory should default to {0.05, false}, got %+v", rc.DefaultMandatory)
	}
	if rc.ModelOverrides != nil {
		t.Errorf("ModelOverrides should remain nil, got %+v", rc.ModelOverrides)
	}
}

// captureLogger is a schemas.Logger that records the format strings passed to Warn,
// so tests can assert a WARN was emitted (and what it named).
type captureLogger struct {
	warns  []string
	others []string
}

func (l *captureLogger) Warn(format string, args ...any)        { l.warns = append(l.warns, format) }
func (l *captureLogger) Debug(format string, args ...any)       { l.others = append(l.others, format) }
func (l *captureLogger) Info(format string, args ...any)        { l.others = append(l.others, format) }
func (l *captureLogger) Error(format string, args ...any)       { l.others = append(l.others, format) }
func (l *captureLogger) Fatal(format string, args ...any)       { l.others = append(l.others, format) }
func (l *captureLogger) SetLevel(schemas.LogLevel)              {}
func (l *captureLogger) SetOutputType(schemas.LoggerOutputType) {}
func (l *captureLogger) LogHTTPRequest(schemas.LogLevel, string) schemas.LogEventBuilder {
	return schemas.NoopLogEvent
}

// TestReservationConfig_ApplyDefaults_UnknownOnCancel_WarnsAndNormalizes proves M11:
// a NON-EMPTY unrecognized on_cancel value (a typo) is normalized to the safe default
// AND surfaces a WARN naming the field, rather than being silently coerced into
// charging the full reserved MaxCost per cancel.
func TestReservationConfig_ApplyDefaults_UnknownOnCancel_WarnsAndNormalizes(t *testing.T) {
	logger := &captureLogger{}
	rc := &ReservationConfig{OnCancel: "releasee"}
	rc.ApplyDefaults(logger)

	if rc.OnCancel != OnCancelChargeReserved {
		t.Fatalf("unknown on_cancel should normalize to %q, got %q", OnCancelChargeReserved, rc.OnCancel)
	}
	if len(logger.warns) != 1 {
		t.Fatalf("expected exactly one WARN for the unrecognized on_cancel, got %d: %v", len(logger.warns), logger.warns)
	}
	if !strings.Contains(logger.warns[0], "on_cancel") {
		t.Errorf("WARN must name the on_cancel field, got: %q", logger.warns[0])
	}
}

// TestReservationConfig_ApplyDefaults_OnCancel_NoWarnOnLegitimateValues proves the
// WARN fires ONLY on a non-empty unrecognized value: empty/unset (the legitimate
// default) and the two recognized values must normalize quietly.
func TestReservationConfig_ApplyDefaults_OnCancel_NoWarnOnLegitimateValues(t *testing.T) {
	cases := []struct {
		name       string
		onCancel   string
		wantResult string
	}{
		{"empty defaults quietly", "", OnCancelChargeReserved},
		{"charge_reserved stays", OnCancelChargeReserved, OnCancelChargeReserved},
		{"release stays", OnCancelRelease, OnCancelRelease},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger := &captureLogger{}
			rc := &ReservationConfig{OnCancel: tc.onCancel}
			rc.ApplyDefaults(logger)
			if rc.OnCancel != tc.wantResult {
				t.Fatalf("on_cancel %q should resolve to %q, got %q", tc.onCancel, tc.wantResult, rc.OnCancel)
			}
			if len(logger.warns) != 0 {
				t.Errorf("no WARN expected for %q, got: %v", tc.onCancel, logger.warns)
			}
		})
	}
}

func TestResolveReservation_Disabled(t *testing.T) {
	f := false
	rc := &ReservationConfig{Enabled: &f}
	rc.ApplyDefaults()
	// Force Enabled back to false (ApplyDefaults preserves explicit false).
	if rc.enabled() {
		t.Fatalf("expected disabled")
	}
	amount, wc := rc.ResolveReservation("any-model")
	if amount != 1e-9 || wc {
		t.Errorf("got (%v, %v), want (1e-9, false)", amount, wc)
	}
}

func enabledConfig() *ReservationConfig {
	rc := &ReservationConfig{}
	rc.ApplyDefaults()
	return rc
}

func TestResolveReservation_Default_Flat(t *testing.T) {
	rc := enabledConfig()
	amount, wc := rc.ResolveReservation("anything")
	if amount != 0.05 || wc {
		t.Errorf("got (%v, %v), want (0.05, false)", amount, wc)
	}
}

func TestResolveReservation_Default_WorstCase(t *testing.T) {
	rc := &ReservationConfig{DefaultMandatory: Reservation{WorstCase: true}}
	rc.ApplyDefaults()
	amount, wc := rc.ResolveReservation("anything")
	if amount != 0 || !wc {
		t.Errorf("got (%v, %v), want (0, true)", amount, wc)
	}
}

func TestResolveReservation_Override_Flat(t *testing.T) {
	rc := &ReservationConfig{
		ModelOverrides: map[string]Reservation{
			"claude-opus-*": {Amount: 0.30},
		},
	}
	rc.ApplyDefaults()
	amount, wc := rc.ResolveReservation("claude-opus-4.7")
	if amount != 0.30 || wc {
		t.Errorf("got (%v, %v), want (0.30, false)", amount, wc)
	}
}

func TestResolveReservation_Override_WorstCase(t *testing.T) {
	rc := &ReservationConfig{
		ModelOverrides: map[string]Reservation{
			"claude-opus-*": {WorstCase: true},
		},
	}
	rc.ApplyDefaults()
	amount, wc := rc.ResolveReservation("claude-opus-4.7")
	if amount != 0 || !wc {
		t.Errorf("got (%v, %v), want (0, true)", amount, wc)
	}
}

func TestMatchOverride_NoOverrides(t *testing.T) {
	rc := enabledConfig()
	if _, ok := rc.matchOverride("anything"); ok {
		t.Errorf("expected no match")
	}
}

func TestMatchOverride_LiteralBeatsGlob(t *testing.T) {
	// Use ordered unmarshal so insertion order is deterministic.
	raw := `{"model_overrides":{"claude-opus-4.7":0.50,"claude-opus-*":0.30}}`
	var rc ReservationConfig
	if err := json.Unmarshal([]byte(raw), &rc); err != nil {
		t.Fatal(err)
	}
	rc.ApplyDefaults()
	got, ok := rc.matchOverride("claude-opus-4.7")
	if !ok || got.Amount != 0.50 {
		t.Errorf("got (%+v, %v), want amount=0.50", got, ok)
	}
}

func TestMatchOverride_LongerPrefixWins(t *testing.T) {
	raw := `{"model_overrides":{"claude-*":0.10,"claude-opus-*":0.30}}`
	var rc ReservationConfig
	if err := json.Unmarshal([]byte(raw), &rc); err != nil {
		t.Fatal(err)
	}
	rc.ApplyDefaults()
	got, ok := rc.matchOverride("claude-opus-4.7")
	if !ok || got.Amount != 0.30 {
		t.Errorf("got (%+v, %v), want amount=0.30", got, ok)
	}
}

func TestMatchOverride_InsertionOrderTiebreak(t *testing.T) {
	// Two equally-specific patterns (both "*-bar"); first-inserted wins.
	raw := `{"model_overrides":{"foo-*":0.10,"foo-bar":0.20,"*-bar":0.30}}`
	var rc ReservationConfig
	if err := json.Unmarshal([]byte(raw), &rc); err != nil {
		t.Fatal(err)
	}
	rc.ApplyDefaults()
	// "foo-bar" is most-specific (literal); ensure literal wins overall.
	got, ok := rc.matchOverride("foo-bar")
	if !ok || got.Amount != 0.20 {
		t.Errorf("got (%+v, %v), want amount=0.20 (literal wins)", got, ok)
	}

	// Now test pure tiebreak between two equally-specific globs.
	raw2 := `{"model_overrides":{"a-*":0.10,"*-b":0.20}}`
	var rc2 ReservationConfig
	if err := json.Unmarshal([]byte(raw2), &rc2); err != nil {
		t.Fatal(err)
	}
	rc2.ApplyDefaults()
	got2, ok2 := rc2.matchOverride("a-b")
	if !ok2 {
		t.Fatal("expected match")
	}
	// "a-*" has prefix 2, "*-b" has prefix 0. "a-*" wins by longer prefix.
	if got2.Amount != 0.10 {
		t.Errorf("got %+v, want amount=0.10 (longer literal prefix wins)", got2)
	}

	// True tiebreak: two patterns with identical scores.
	raw3 := `{"model_overrides":{"*-x":0.10,"*-y":0.20}}`
	var rc3 ReservationConfig
	if err := json.Unmarshal([]byte(raw3), &rc3); err != nil {
		t.Fatal(err)
	}
	rc3.ApplyDefaults()
	// Both have prefix=0, nonGlob=2; insertion-order tiebreak makes "*-x" win for "a-x".
	got3, ok3 := rc3.matchOverride("a-x")
	if !ok3 || got3.Amount != 0.10 {
		t.Errorf("got (%+v, %v), want amount=0.10 (first-inserted wins on tie)", got3, ok3)
	}
}

func TestMatchOverride_NoMatch(t *testing.T) {
	raw := `{"model_overrides":{"claude-*":0.10}}`
	var rc ReservationConfig
	if err := json.Unmarshal([]byte(raw), &rc); err != nil {
		t.Fatal(err)
	}
	rc.ApplyDefaults()
	if _, ok := rc.matchOverride("gpt-4o"); ok {
		t.Errorf("expected no match")
	}
}

func TestValidate_RejectsMalformedGlob(t *testing.T) {
	// Unterminated character class — filepath.Match returns ErrBadPattern.
	// Without Validate(), matchOverride would silently drop this and the
	// operator's intended override would never apply.
	raw := `{"model_overrides":{"claude-[opus":0.30}}`
	var rc ReservationConfig
	if err := json.Unmarshal([]byte(raw), &rc); err != nil {
		t.Fatal(err)
	}
	rc.ApplyDefaults()
	if err := rc.Validate(); err == nil {
		t.Errorf("expected Validate to reject malformed pattern, got nil")
	}
}

func TestValidate_AcceptsValidGlobs(t *testing.T) {
	raw := `{"model_overrides":{"claude-*":0.10,"gpt-4o":0.20,"o[1-3]-mini":0.30}}`
	var rc ReservationConfig
	if err := json.Unmarshal([]byte(raw), &rc); err != nil {
		t.Fatal(err)
	}
	rc.ApplyDefaults()
	if err := rc.Validate(); err != nil {
		t.Errorf("expected Validate to accept well-formed patterns, got: %v", err)
	}
}

func TestValidate_NilSafe(t *testing.T) {
	var rc *ReservationConfig
	if err := rc.Validate(); err != nil {
		t.Errorf("nil receiver should not error, got: %v", err)
	}
}

func TestValidate_RejectsMalformedGlob_InCodeConstructed(t *testing.T) {
	// Patterns set directly (not via UnmarshalJSON) skip overrideOrder
	// population; Validate must still catch them.
	rc := &ReservationConfig{
		ModelOverrides: map[string]Reservation{
			"claude-[bad": {Amount: 0.30},
		},
	}
	rc.ApplyDefaults()
	if err := rc.Validate(); err == nil {
		t.Errorf("expected Validate to reject malformed in-code pattern")
	}
}

func TestReservationConfig_UnknownModelBounds_Defaults(t *testing.T) {
	var rc ReservationConfig
	rc.ApplyDefaults()
	if rc.UnknownModelBounds == nil || rc.UnknownModelBounds.InputTokens != 4096 || rc.UnknownModelBounds.OutputTokens != 4096 {
		t.Fatalf("unknown_model_bounds default: %+v", rc.UnknownModelBounds)
	}
	if err := rc.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestReservationConfig_UnknownModelBounds_RejectsZero(t *testing.T) {
	for _, raw := range []string{`{"unknown_model_bounds":{"input_tokens":0,"output_tokens":10}}`, `{"unknown_model_bounds":{"input_tokens":10,"output_tokens":-1}}`} {
		var rc ReservationConfig
		if err := json.Unmarshal([]byte(raw), &rc); err != nil {
			t.Fatal(err)
		}
		rc.ApplyDefaults()
		if err := rc.Validate(); err == nil || !strings.Contains(err.Error(), "unknown_model_bounds") {
			t.Errorf("%s: expected a bounds error, got %v", raw, err)
		}
	}
}

func TestReservationConfig_UnknownModelBounds_RoundTrip(t *testing.T) {
	raw := `{"enabled":true,"default_mandatory":"worst_case","unknown_model_bounds":{"input_tokens":8192,"output_tokens":2048}}`
	var rc ReservationConfig
	if err := json.Unmarshal([]byte(raw), &rc); err != nil {
		t.Fatal(err)
	}
	rc.ApplyDefaults()
	if rc.UnknownModelBounds.InputTokens != 8192 || rc.UnknownModelBounds.OutputTokens != 2048 {
		t.Fatalf("bounds: %+v", rc.UnknownModelBounds)
	}
	out, err := json.Marshal(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"unknown_model_bounds":{"input_tokens":8192,"output_tokens":2048}`) {
		t.Errorf("marshal: %s", out)
	}
}

func TestReservationConfig_EnabledAndIsOverride(t *testing.T) {
	var nilCfg *ReservationConfig
	if nilCfg.IsEnabled() || nilCfg.IsOverride("x") {
		t.Fatal("nil config must be disabled and match nothing")
	}
	rc := &ReservationConfig{ModelOverrides: map[string]Reservation{"claude-*": {WorstCase: true}}}
	rc.ApplyDefaults()
	if !rc.IsEnabled() || !rc.IsOverride("claude-opus") || rc.IsOverride("gpt-4o") {
		t.Errorf("enabled=%v override(claude-opus)=%v override(gpt-4o)=%v", rc.IsEnabled(), rc.IsOverride("claude-opus"), rc.IsOverride("gpt-4o"))
	}
}

func TestIsNonTextRequestType(t *testing.T) {
	for _, rt := range []schemas.RequestType{schemas.ImageGenerationRequest, schemas.SpeechStreamRequest, schemas.TranscriptionRequest, schemas.VideoRemixRequest} {
		if !IsNonTextRequestType(rt) {
			t.Errorf("%s must be non-text", rt)
		}
	}
	for _, rt := range []schemas.RequestType{schemas.ChatCompletionRequest, schemas.EmbeddingRequest, schemas.RerankRequest, schemas.ResponsesRequest} {
		if IsNonTextRequestType(rt) {
			t.Errorf("%s must be text", rt)
		}
	}
}
