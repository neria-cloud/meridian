package datasheet

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/neria-cloud/meridian/core/schemas"
	configstoreTables "github.com/neria-cloud/meridian/framework/configstore/tables"
)

// EstimateSource says which tier priced a request.
type EstimateSource string

const (
	EstimateNone    EstimateSource = ""          // no pricing row resolved, or a per-unit input is unknown
	EstimateTokens  EstimateSource = "tokens"    // Tier 1: token bound x rates
	EstimatePerUnit EstimateSource = "per_unit"  // Tier 2: image / speech / video per-unit
	EstimateZero    EstimateSource = "zero_rate" // row resolved, every applicable rate is 0
)

// EstimateOptions carries the caller's fallbacks for models without a base
// entry (custom prices only); 0 means the 4096 default.
type EstimateOptions struct {
	FallbackInputTokens  int
	FallbackOutputTokens int
}

const defaultTokenBound = 4096

// Estimate is the worst-case cost of one request before the provider call.
type Estimate struct {
	Cost            float64
	Source          EstimateSource
	MaxInputTokens  int  // Tier-1 bound used (0 for per-unit)
	MaxOutputTokens int  // Tier-1 bound used (0 for per-unit)
	BoundsFromEntry bool // false when the fallbacks priced an override-only model
	Tier            string
	Surcharges      float64
}

// EstimateMaxCost prices the worst case of request through the same
// override-aware resolvePricing that CalculateCost uses, so custom prices
// bound the reservation the way they bill the settlement. It never applies a
// sentinel: a caller decides what an empty Source or a zero cost reserves.
func (s *Store) EstimateMaxCost(request *schemas.BifrostRequest, provider, model string, scopes *LookupScopes, opts EstimateOptions) Estimate {
	if request == nil || model == "" {
		return Estimate{}
	}
	rt := normalizeStreamRequestType(request.RequestType)
	var sc LookupScopes
	if scopes != nil {
		sc = *scopes
	}
	ri := schemas.RoutingInfo{Provider: schemas.ModelProvider(provider), Model: model}
	pricing := s.resolvePricing(ri, rt, sc)
	if pricing == nil {
		return Estimate{}
	}

	switch rt {
	case schemas.ImageGenerationRequest, schemas.ImageEditRequest, schemas.ImageVariationRequest,
		schemas.SpeechRequest, schemas.VideoGenerationRequest, schemas.VideoRemixRequest:
		in, ok := perUnitInput(request, rt)
		if !ok {
			return Estimate{}
		}
		cost := s.computeCostFromInput(in, ri, rt, sc)
		if cost <= 0 {
			return Estimate{Source: EstimateZero}
		}
		return Estimate{Cost: cost, Source: EstimatePerUnit}
	case schemas.TranscriptionRequest:
		// TODO(not planned): STT input is unknown pre-call (the audio is not decoded); Source Zero lets the caller apply its sentinel (N1).
		return Estimate{Source: EstimateZero}
	}

	maxIn, maxOut, fromEntry := tokenBounds(request, rt, pricing, opts)
	usage := &schemas.BifrostLLMUsage{PromptTokens: maxIn, CompletionTokens: maxOut, TotalTokens: maxIn + maxOut}
	est := Estimate{MaxInputTokens: maxIn, MaxOutputTokens: maxOut, BoundsFromEntry: fromEntry, Tier: "base"}
	// Every tier the provider may bill is priced; the highest bounds the reservation.
	for _, c := range tierCandidates(request, rt) {
		cost := s.computeCostFromInput(costInput{usage: usage, tier: c.tier}, ri, rt, sc)
		if cost > est.Cost {
			est.Cost, est.Tier = cost, c.name
		}
	}
	est.Surcharges = estimateSurcharges(request, rt, pricing)
	est.Cost += est.Surcharges
	if est.Cost <= 0 {
		est.Source = EstimateZero
		return est
	}
	est.Source = EstimateTokens
	return est
}

type tierCandidate struct {
	name string
	tier serviceTier
}

// tierCandidates lists the service tiers the request may be billed at:
// base always; priority for service_tier priority or auto; flex for flex
// (a strict discount the provider serves or rejects); fast for speed=fast;
// the US inference geography multiplier applies to every candidate.
func tierCandidates(request *schemas.BifrostRequest, rt schemas.RequestType) []tierCandidate {
	var st *schemas.BifrostServiceTier
	var speed, geo *string
	switch rt {
	case schemas.ChatCompletionRequest:
		if r := request.ChatRequest; r != nil && r.Params != nil {
			st, speed, geo = r.Params.ServiceTier, r.Params.Speed, r.Params.InferenceGeo
		}
	case schemas.ResponsesRequest:
		// TODO(not planned): Responses requests carry no speed/inference_geo in this core version (N15).
		if r := request.ResponsesRequest; r != nil && r.Params != nil {
			st = r.Params.ServiceTier
		}
	}
	base := tierFromResponse(nil, nil, geo)
	suffix := ""
	if base.inferenceGeoUS {
		suffix = "+us"
	}
	out := []tierCandidate{{"base" + suffix, base}}
	if st != nil {
		switch *st {
		case schemas.BifrostServiceTierPriority, schemas.BifrostServiceTierAuto:
			t := base
			t.isPriority = true
			out = append(out, tierCandidate{"priority" + suffix, t})
		case schemas.BifrostServiceTierFlex:
			t := base
			t.isFlex = true
			out = append(out, tierCandidate{"flex" + suffix, t})
		}
	}
	if speed != nil && *speed == "fast" {
		t := base
		t.isFast = true
		out = append(out, tierCandidate{"fast" + suffix, t})
	}
	return out
}

// estimateSurcharges adds the per-request costs Tier 1's token math misses:
// one web-search query when the request enables search, and per-image input
// pricing for every image part of the prompt.
func estimateSurcharges(request *schemas.BifrostRequest, rt schemas.RequestType, pricing *configstoreTables.TableModelPricing) float64 {
	sur := 0.0
	// TODO(not planned): one web-search query per request is assumed; the real count is known only after the call (N13).
	if pricing.SearchContextCostPerQuery != nil && hasWebSearch(request, rt) {
		sur += *pricing.SearchContextCostPerQuery
	}
	// TODO(not planned): token-priced image inputs need the image dimensions, unknown pre-call (N16).
	if pricing.InputCostPerImage != nil {
		sur += float64(countImageParts(request, rt)) * *pricing.InputCostPerImage
	}
	return sur
}

const webSearchToolPrefix = "web_search"

func hasWebSearch(request *schemas.BifrostRequest, rt schemas.RequestType) bool {
	switch rt {
	case schemas.ChatCompletionRequest:
		r := request.ChatRequest
		if r == nil || r.Params == nil {
			return false
		}
		if r.Params.WebSearchOptions != nil {
			return true
		}
		for _, tool := range r.Params.Tools {
			if strings.HasPrefix(string(tool.Type), webSearchToolPrefix) {
				return true
			}
		}
	case schemas.ResponsesRequest:
		r := request.ResponsesRequest
		if r == nil || r.Params == nil {
			return false
		}
		for _, tool := range r.Params.Tools {
			if strings.HasPrefix(string(tool.Type), webSearchToolPrefix) {
				return true
			}
		}
	}
	return false
}

func countImageParts(request *schemas.BifrostRequest, rt schemas.RequestType) int {
	n := 0
	switch rt {
	case schemas.ChatCompletionRequest:
		if request.ChatRequest == nil {
			return 0
		}
		for _, m := range request.ChatRequest.Input {
			if m.Content == nil {
				continue
			}
			for _, b := range m.Content.ContentBlocks {
				if b.Type == schemas.ChatContentBlockTypeImage {
					n++
				}
			}
		}
	case schemas.ResponsesRequest:
		if request.ResponsesRequest == nil {
			return 0
		}
		for _, m := range request.ResponsesRequest.Input {
			if m.Content == nil {
				continue
			}
			for _, b := range m.Content.ContentBlocks {
				if b.Type == schemas.ResponsesInputMessageContentBlockTypeImage {
					n++
				}
			}
		}
	}
	return n
}

// EstimateRequestCost is the Tier-2 (per-unit) estimate alone; 0 for every
// other request type or when the per-unit input is unknown.
func (s *Store) EstimateRequestCost(request *schemas.BifrostRequest, provider, model string, scopes *LookupScopes) float64 {
	est := s.EstimateMaxCost(request, provider, model, scopes, EstimateOptions{})
	if est.Source != EstimatePerUnit {
		return 0
	}
	return est.Cost
}

// tokenBounds resolves (maxInput, maxOutput) for Tier 1: the caller's
// fallbacks (4096 when zero), then the resolved row's limits (an override-only
// row carries none), then the request's own output cap. fromEntry reports
// whether a row limit was used.
func tokenBounds(request *schemas.BifrostRequest, rt schemas.RequestType, pricing *configstoreTables.TableModelPricing, opts EstimateOptions) (maxIn, maxOut int, fromEntry bool) {
	maxIn, maxOut = orDefault(opts.FallbackInputTokens), orDefault(opts.FallbackOutputTokens)
	if pricing != nil {
		if pricing.MaxInputTokens != nil && *pricing.MaxInputTokens > 0 {
			maxIn, fromEntry = *pricing.MaxInputTokens, true
		}
		if pricing.MaxOutputTokens != nil && *pricing.MaxOutputTokens > 0 {
			maxOut, fromEntry = *pricing.MaxOutputTokens, true
		}
	}
	switch rt {
	case schemas.ChatCompletionRequest:
		if r := request.ChatRequest; r != nil && r.Params != nil && r.Params.MaxCompletionTokens != nil {
			maxOut = *r.Params.MaxCompletionTokens
		}
	case schemas.ResponsesRequest:
		if r := request.ResponsesRequest; r != nil && r.Params != nil && r.Params.MaxOutputTokens != nil {
			maxOut = *r.Params.MaxOutputTokens
		}
	case schemas.TextCompletionRequest:
		if r := request.TextCompletionRequest; r != nil && r.Params != nil && r.Params.MaxTokens != nil {
			maxOut = *r.Params.MaxTokens
		}
	case schemas.EmbeddingRequest:
		maxOut = 0
	}
	return maxIn, maxOut, fromEntry
}

func orDefault(n int) int {
	if n > 0 {
		return n
	}
	return defaultTokenBound
}

// perUnitInput builds the costInput of a per-unit request from its params;
// ok is false when the billable unit is unknown before the call.
func perUnitInput(request *schemas.BifrostRequest, rt schemas.RequestType) (costInput, bool) {
	switch rt {
	case schemas.ImageGenerationRequest, schemas.ImageEditRequest, schemas.ImageVariationRequest:
		n, size, quality := estimateImageParams(request)
		return costInput{
			imageUsage:   &schemas.ImageUsage{OutputTokensDetails: &schemas.ImageTokenDetails{NImages: n}},
			imageSize:    size,
			imageQuality: quality,
		}, true
	case schemas.SpeechRequest:
		if request.SpeechRequest == nil || request.SpeechRequest.Input == nil {
			return costInput{}, false
		}
		return costInput{audioTextInputChars: utf8.RuneCountInString(request.SpeechRequest.Input.Input)}, true
	case schemas.VideoGenerationRequest, schemas.VideoRemixRequest:
		seconds, ok := videoSecondsForEstimate(request)
		if !ok {
			return costInput{}, false
		}
		return costInput{videoSeconds: &seconds}, true
	}
	return costInput{}, false
}

// estimateImageParams extracts (n, size, quality) from the three image request
// shapes; n defaults to 1. Variation requests have no quality.
func estimateImageParams(request *schemas.BifrostRequest) (n int, size, quality string) {
	n = 1
	switch {
	case request.ImageGenerationRequest != nil && request.ImageGenerationRequest.Params != nil:
		p := request.ImageGenerationRequest.Params
		if p.N != nil {
			n = *p.N
		}
		if p.Size != nil {
			size = *p.Size
		}
		if p.Quality != nil {
			quality = *p.Quality
		}
	case request.ImageEditRequest != nil && request.ImageEditRequest.Params != nil:
		p := request.ImageEditRequest.Params
		if p.N != nil {
			n = *p.N
		}
		if p.Size != nil {
			size = *p.Size
		}
		if p.Quality != nil {
			quality = *p.Quality
		}
	case request.ImageVariationRequest != nil && request.ImageVariationRequest.Params != nil:
		p := request.ImageVariationRequest.Params
		if p.N != nil {
			n = *p.N
		}
		if p.Size != nil {
			size = *p.Size
		}
	}
	if n < 1 {
		n = 1
	}
	return n, size, quality
}

// videoSecondsForEstimate returns the clip length to price: the request's
// seconds, else the provider's API default (Gemini/Vertex 8, OpenAI 4).
func videoSecondsForEstimate(request *schemas.BifrostRequest) (int, bool) {
	var provider schemas.ModelProvider
	var explicit *string
	switch {
	case request.VideoGenerationRequest != nil:
		provider = request.VideoGenerationRequest.Provider
		if request.VideoGenerationRequest.Params != nil {
			explicit = request.VideoGenerationRequest.Params.Seconds
		}
	case request.VideoRemixRequest != nil:
		provider = request.VideoRemixRequest.Provider
	default:
		return 0, false
	}
	if explicit != nil {
		n, err := strconv.Atoi(*explicit)
		return n, err == nil && n > 0
	}
	switch provider {
	case schemas.Gemini, schemas.Vertex:
		n, _ := strconv.Atoi(schemas.DefaultVideoDuration)
		return n, n > 0
	case schemas.OpenAI:
		return 4, true
	}
	return 0, false
}
