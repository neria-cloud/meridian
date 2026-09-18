package configstore

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/neria-cloud/meridian/core/schemas"
)

// Reservation policy for per-request budget reservations. Amounts are dollars.
// The catalog prices the worst case; ReserveAmount (framework/modelcatalog)
// turns an Estimate and this config into the reserved amount.

// DefaultUnknownModelTokens bounds either side of a model without a base entry.
const DefaultUnknownModelTokens = 4096

// UnknownModelBounds are the Tier-1 token bounds used when the catalog has no
// base entry for the model (custom prices only).
type UnknownModelBounds struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ReserveReason says which rule produced a reserved amount.
type ReserveReason string

const (
	ReserveDisabled     ReserveReason = "disabled"      // reservations off: ReserveDisabledAmount
	ReserveFlat         ReserveReason = "flat"          // default_mandatory amount
	ReserveOverrideFlat ReserveReason = "override_flat" // model_overrides amount
	ReserveWorstCase    ReserveReason = "worst_case"    // the catalog estimate
	ReserveNoEntry      ReserveReason = "no_entry"      // no pricing row: ReserveNoEntryAmount
	ReserveNonText      ReserveReason = "non_text"      // image/audio/video without an estimate: ReserveNonTextAmount
	ReserveZeroRate     ReserveReason = "zero_rate"     // text row with zero rates: ReserveNoEntryAmount
)

// Sentinel amounts keep RESERVE alive so FINALIZE settles to the actual cost.
const (
	ReserveDisabledAmount = 1e-9
	ReserveNoEntryAmount  = 0.01
	ReserveNonTextAmount  = 0.25
)

// IsNonTextRequestType reports whether the request type bills per non-token
// unit (per-image, per-character, per-second).
func IsNonTextRequestType(t schemas.RequestType) bool {
	switch t {
	case schemas.ImageGenerationRequest, schemas.ImageEditRequest, schemas.ImageVariationRequest,
		schemas.SpeechRequest, schemas.SpeechStreamRequest,
		schemas.TranscriptionRequest, schemas.TranscriptionStreamRequest,
		schemas.VideoGenerationRequest, schemas.VideoRemixRequest:
		return true
	default:
		return false
	}
}

// Reservation is one reservation value: a flat dollar amount or the string
// "worst_case"; set ReservationConfig.Enabled=false for zero reservation.
//
// TODO(not planned): historical-actual-based reservation (e.g., K × p95 of
// recent actual cost). Complex; deferred.
type Reservation struct {
	Amount    float64
	WorstCase bool
}

const reservationInvalidMsg = `invalid reservation value: must be a positive number or the string "worst_case" (use enabled: false for zero reservation)`

// UnmarshalJSON accepts either a positive number or the literal "worst_case".
func (r *Reservation) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		if s != "worst_case" {
			return fmt.Errorf("%s; got %q", reservationInvalidMsg, s)
		}
		r.Amount = 0
		r.WorstCase = true
		return nil
	}
	var f float64
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("%s; got %s", reservationInvalidMsg, string(data))
	}
	if f <= 0 {
		return fmt.Errorf("%s; got %v", reservationInvalidMsg, f)
	}
	r.Amount = f
	r.WorstCase = false
	return nil
}

// MarshalJSON emits "worst_case" when set, otherwise the numeric amount.
func (r Reservation) MarshalJSON() ([]byte, error) {
	if r.WorstCase {
		return json.Marshal("worst_case")
	}
	return json.Marshal(r.Amount)
}

// ReservationConfig configures per-request budget reservation behaviour.
//
// TODO(not planned): move ReservationConfig to a PG-backed singleton table for
// runtime editing without restart. AuthConfig provides the precedent.
// TODO(not planned): per-VK reservation policy.
// OnCancel policy values for ReservationConfig.OnCancel — how a mid-stream-cancelled
// request's budget reservation is settled synchronously (Meridian-only; the reservation
// model is Meridian's raft addition).
const (
	// OnCancelChargeReserved (default) settles the reserved MaxCost immediately + a charge
	// log record — same charge amount as today's ≤1h TTL sweep, but prompt and logged.
	OnCancelChargeReserved = "charge_reserved"
	// OnCancelRelease settles the reservation to a $1e-9 sentinel → effectively $0 (platform
	// absorbs the cancelled-stream provider cost). Uses the normal FinalizeBudget path; no
	// separate release op.
	OnCancelRelease = "release"
)

type ReservationConfig struct {
	// HideInFlight removed in 20260519 cleanup — settled-only accounting
	// makes the read-side projection unnecessary. Operators must remove
	// hide_in_flight from config.json on upgrade or schema validation fails.
	Enabled          *bool                  `json:"enabled,omitempty"` // nil → default true
	DefaultMandatory Reservation            `json:"default_mandatory"`
	ModelOverrides   map[string]Reservation `json:"model_overrides,omitempty"`
	// OnCancel controls synchronous settlement of a mid-stream-cancelled request's
	// reservation: "charge_reserved" (default) | "release". See the OnCancel* constants.
	OnCancel string `json:"on_cancel,omitempty"`
	// UnknownModelBounds are the worst-case token bounds for a model the catalog
	// has no base entry for (custom prices only); nil → 4096/4096.
	UnknownModelBounds *UnknownModelBounds `json:"unknown_model_bounds,omitempty"`

	// overrideOrder records insertion order of ModelOverrides keys for
	// deterministic tiebreaks in matchOverride. Populated by UnmarshalJSON
	// and ApplyDefaults; not serialised.
	overrideOrder []string
}

// UnmarshalJSON preserves insertion order of ModelOverrides for tiebreaks.
func (rc *ReservationConfig) UnmarshalJSON(data []byte) error {
	type wire struct {
		Enabled            *bool               `json:"enabled,omitempty"`
		DefaultMandatory   Reservation         `json:"default_mandatory"`
		ModelOverrides     json.RawMessage     `json:"model_overrides,omitempty"`
		OnCancel           string              `json:"on_cancel,omitempty"`
		UnknownModelBounds *UnknownModelBounds `json:"unknown_model_bounds,omitempty"`
	}
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	rc.Enabled = w.Enabled
	rc.DefaultMandatory = w.DefaultMandatory
	rc.OnCancel = w.OnCancel
	rc.UnknownModelBounds = w.UnknownModelBounds
	rc.ModelOverrides = nil
	rc.overrideOrder = nil
	if len(w.ModelOverrides) > 0 && string(w.ModelOverrides) != "null" {
		// Decode keys in source-document order.
		dec := json.NewDecoder(strings.NewReader(string(w.ModelOverrides)))
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if delim, ok := tok.(json.Delim); !ok || delim != '{' {
			return fmt.Errorf("model_overrides must be an object")
		}
		rc.ModelOverrides = map[string]Reservation{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyTok.(string)
			if !ok {
				return fmt.Errorf("model_overrides key must be a string")
			}
			var val Reservation
			if err := dec.Decode(&val); err != nil {
				return fmt.Errorf("model_overrides[%q]: %w", key, err)
			}
			if _, dup := rc.ModelOverrides[key]; !dup {
				rc.overrideOrder = append(rc.overrideOrder, key)
			}
			rc.ModelOverrides[key] = val
		}
	}
	return nil
}

// ApplyDefaults promotes nil *bool fields to true and fills DefaultMandatory
// with the documented default of $0.05 when no explicit value was supplied.
//
// An optional logger may be supplied by config-load callers so that a NON-EMPTY
// unrecognized on_cancel value (e.g. a typo like "releasee") is surfaced with a
// WARN before it is silently normalized to the safe default — otherwise an
// operator typo would coerce every cancel into charging the full reserved
// MaxCost with no signal. Empty/unset on_cancel is the legitimate default and is
// normalized quietly.
func (rc *ReservationConfig) ApplyDefaults(logger ...schemas.Logger) {
	if rc.Enabled == nil {
		v := true
		rc.Enabled = &v
	}
	// DefaultMandatory.Amount defaults to 0.05 only if the field was absent
	// (zero-value Reservation). Numeric 0 is invalid wire form anyway —
	// rejected by UnmarshalJSON — so the zero-value case here means "absent".
	if rc.DefaultMandatory.Amount == 0 && !rc.DefaultMandatory.WorstCase {
		rc.DefaultMandatory.Amount = 0.05
	}
	// on_cancel defaults to charge_reserved (parity with today's charge amount, but
	// prompt + logged). Unknown values normalize to the safe default; config.schema.json
	// enforces the enum at load time. A NON-EMPTY unrecognized value (an operator typo)
	// is coerced to charging the full reserved MaxCost per cancel, so surface it with a
	// WARN when a logger is available. Empty/unset is the legitimate default — no warn.
	if rc.OnCancel != "" && rc.OnCancel != OnCancelRelease && rc.OnCancel != OnCancelChargeReserved {
		for _, l := range logger {
			if l != nil {
				l.Warn("governance.reservation.on_cancel: unrecognized value %q; normalizing to %q", rc.OnCancel, OnCancelChargeReserved)
				break
			}
		}
	}
	if rc.OnCancel != OnCancelRelease {
		rc.OnCancel = OnCancelChargeReserved
	}
	if rc.UnknownModelBounds == nil {
		rc.UnknownModelBounds = &UnknownModelBounds{InputTokens: DefaultUnknownModelTokens, OutputTokens: DefaultUnknownModelTokens}
	}
	if rc.overrideOrder == nil && len(rc.ModelOverrides) > 0 {
		// Fall back to sorted key order for in-code construction.
		rc.overrideOrder = make([]string, 0, len(rc.ModelOverrides))
		for k := range rc.ModelOverrides {
			rc.overrideOrder = append(rc.overrideOrder, k)
		}
		sort.Strings(rc.overrideOrder)
	}
}

// Validate checks that every glob pattern in ModelOverrides is well-formed
// according to filepath.Match semantics. It must be called at config load
// time (after ApplyDefaults) so malformed patterns surface as a loud config
// error rather than silently disabling an override at runtime. A typo such
// as an unterminated character class (`claude-[opus`) would otherwise cause
// filepath.Match to return ErrBadPattern, which matchOverride drops on the
// floor — quietly removing the operator's intended worst-case safeguard.
func (rc *ReservationConfig) Validate() error {
	if rc == nil {
		return nil
	}
	if b := rc.UnknownModelBounds; b != nil && (b.InputTokens <= 0 || b.OutputTokens <= 0) {
		return fmt.Errorf("governance.reservation.unknown_model_bounds: input_tokens and output_tokens must be positive, got %d/%d", b.InputTokens, b.OutputTokens)
	}
	for _, pattern := range rc.overrideOrder {
		if _, err := filepath.Match(pattern, ""); err != nil {
			return fmt.Errorf("governance.reservation.model_overrides: invalid glob pattern %q: %w", pattern, err)
		}
	}
	// Patterns set in-code without going through UnmarshalJSON (overrideOrder
	// empty but ModelOverrides populated) also need validation.
	if len(rc.overrideOrder) == 0 {
		for pattern := range rc.ModelOverrides {
			if _, err := filepath.Match(pattern, ""); err != nil {
				return fmt.Errorf("governance.reservation.model_overrides: invalid glob pattern %q: %w", pattern, err)
			}
		}
	}
	return nil
}

func (rc *ReservationConfig) enabled() bool {
	return rc != nil && rc.Enabled != nil && *rc.Enabled
}

// IsEnabled reports whether reservations are switched on (nil-safe).
func (rc *ReservationConfig) IsEnabled() bool { return rc.enabled() }

// IsOverride reports whether model matches a model_overrides key.
func (rc *ReservationConfig) IsOverride(model string) bool {
	if rc == nil {
		return false
	}
	_, ok := rc.matchOverride(model)
	return ok
}

// ResolveReservation returns the amount (in dollars) and a flag indicating
// the caller should fall back to legacy worst-case tier dispatch.
func (rc *ReservationConfig) ResolveReservation(model string) (amount float64, useWorstCase bool) {
	if !rc.enabled() {
		return 1e-9, false
	}
	if ovr, ok := rc.matchOverride(model); ok {
		if ovr.WorstCase {
			return 0, true
		}
		return ovr.Amount, false
	}
	if rc.DefaultMandatory.WorstCase {
		return 0, true
	}
	return rc.DefaultMandatory.Amount, false
}

// TODO(not planned): warn at startup when an override pattern matches no
// known catalog entry.
//
// matchOverride implements most-specific-wins precedence. Each pattern is
// scored by (literalPrefixLen, totalNonGlobLen, -insertionIndex); largest
// tuple wins.
func (rc *ReservationConfig) matchOverride(model string) (Reservation, bool) {
	if len(rc.ModelOverrides) == 0 {
		return Reservation{}, false
	}
	type score struct {
		prefix, nonGlob, negIdx int
	}
	better := func(a, b score) bool {
		if a.prefix != b.prefix {
			return a.prefix > b.prefix
		}
		if a.nonGlob != b.nonGlob {
			return a.nonGlob > b.nonGlob
		}
		return a.negIdx > b.negIdx
	}
	var bestKey string
	var bestScore score
	have := false
	order := rc.overrideOrder
	if len(order) == 0 {
		order = make([]string, 0, len(rc.ModelOverrides))
		for k := range rc.ModelOverrides {
			order = append(order, k)
		}
		sort.Strings(order)
	}
	for idx, pattern := range order {
		matched, err := filepath.Match(pattern, model)
		if err != nil || !matched {
			continue
		}
		sc := score{
			prefix:  literalPrefixLen(pattern),
			nonGlob: nonGlobLen(pattern),
			negIdx:  -idx,
		}
		if !have || better(sc, bestScore) {
			have = true
			bestScore = sc
			bestKey = pattern
		}
	}
	if !have {
		return Reservation{}, false
	}
	return rc.ModelOverrides[bestKey], true
}

func literalPrefixLen(pattern string) int {
	for i := range len(pattern) {
		switch pattern[i] {
		case '*', '?', '[', '\\':
			return i
		}
	}
	return len(pattern)
}

func nonGlobLen(pattern string) int {
	n := 0
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*', '?':
			// skip
		case '\\':
			if i+1 < len(pattern) {
				n++
				i++
			}
		default:
			n++
		}
	}
	return n
}
