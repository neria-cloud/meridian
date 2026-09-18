package datasheet

import (
	"testing"

	"github.com/neria-cloud/meridian/core/schemas"
	configstoreTables "github.com/neria-cloud/meridian/framework/configstore/tables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func f64(v float64) *float64 { return &v }
func intp(v int) *int        { return &v }
func strp(s string) *string  { return &s }

func imageRow(low, med, high float64) configstoreTables.TableModelPricing {
	return configstoreTables.TableModelPricing{
		Model: "test-img", Provider: "openai", Mode: "image_generation",
		OutputCostPerImageLowQuality: f64(low), OutputCostPerImageMediumQuality: f64(med), OutputCostPerImageHighQuality: f64(high),
	}
}

func chatRow(in, out float64, maxIn, maxOut int) configstoreTables.TableModelPricing {
	row := configstoreTables.TableModelPricing{Model: "gpt-test", Provider: "openai", Mode: "chat", InputCostPerToken: f64(in), OutputCostPerToken: f64(out)}
	if maxIn > 0 {
		row.MaxInputTokens = intp(maxIn)
	}
	if maxOut > 0 {
		row.MaxOutputTokens = intp(maxOut)
	}
	return row
}

func imageReq(model string, n int, size, quality string) *schemas.BifrostRequest {
	p := &schemas.ImageGenerationParameters{}
	if n > 0 {
		p.N = intp(n)
	}
	if size != "" {
		p.Size = strp(size)
	}
	if quality != "" {
		p.Quality = strp(quality)
	}
	return &schemas.BifrostRequest{RequestType: schemas.ImageGenerationRequest,
		ImageGenerationRequest: &schemas.BifrostImageGenerationRequest{Provider: schemas.OpenAI, Model: model, Params: p}}
}

func chatReq(model string, params *schemas.ChatParameters) *schemas.BifrostRequest {
	return &schemas.BifrostRequest{RequestType: schemas.ChatCompletionRequest,
		ChatRequest: &schemas.BifrostChatRequest{Provider: schemas.OpenAI, Model: model, Params: params}}
}

func TestEstimateMaxCost_NilRequest(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	assert.Equal(t, Estimate{}, s.EstimateMaxCost(nil, "openai", "x", nil, EstimateOptions{}))
	assert.Equal(t, 0.0, s.EstimateRequestCost(nil, "openai", "x", nil))
}

func TestEstimateMaxCost_NoPricingEntry(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	est := s.EstimateMaxCost(imageReq("unknown", 1, "", ""), "openai", "unknown", nil, EstimateOptions{})
	assert.Equal(t, EstimateNone, est.Source)
	assert.Equal(t, 0.0, est.Cost)
}

func TestEstimateMaxCost_ImageGen_HighQuality(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	s.SeedPricingForTest("test-img", "openai", schemas.ImageGenerationRequest, imageRow(0.01, 0.04, 0.16))
	est := s.EstimateMaxCost(imageReq("test-img", 2, "1024x1024", "high"), "openai", "test-img", nil, EstimateOptions{})
	assert.Equal(t, EstimatePerUnit, est.Source)
	assert.InDelta(t, 0.32, est.Cost, 1e-9)
	assert.InDelta(t, 0.32, s.EstimateRequestCost(imageReq("test-img", 2, "1024x1024", "high"), "openai", "test-img", nil), 1e-9)
}

func TestEstimateMaxCost_ImageGen_ParamsNil(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	s.SeedPricingForTest("test-img", "openai", schemas.ImageGenerationRequest, imageRow(0.01, 0.04, 0.16))
	req := &schemas.BifrostRequest{RequestType: schemas.ImageGenerationRequest,
		ImageGenerationRequest: &schemas.BifrostImageGenerationRequest{Provider: schemas.OpenAI, Model: "test-img"}}
	// n=1, quality "auto": the sheet has only low/medium/high, so no rate applies.
	est := s.EstimateMaxCost(req, "openai", "test-img", nil, EstimateOptions{})
	assert.Equal(t, EstimateZero, est.Source)
	assert.Equal(t, 0.0, est.Cost)
}

func TestEstimateMaxCost_ImageEdit(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	row := imageRow(0.01, 0.04, 0.16)
	row.Mode = "image_edit"
	s.SeedPricingForTest("test-img", "openai", schemas.ImageEditRequest, row)
	req := &schemas.BifrostRequest{RequestType: schemas.ImageEditRequest,
		ImageEditRequest: &schemas.BifrostImageEditRequest{Provider: schemas.OpenAI, Model: "test-img",
			Params: &schemas.ImageEditParameters{N: intp(3), Quality: strp("medium")}}}
	est := s.EstimateMaxCost(req, "openai", "test-img", nil, EstimateOptions{})
	assert.InDelta(t, 0.12, est.Cost, 1e-9)
}

func TestEstimateMaxCost_Speech_TTS(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	s.SeedPricingForTest("test-tts", "openai", schemas.SpeechRequest, configstoreTables.TableModelPricing{
		Model: "test-tts", Provider: "openai", Mode: "audio_speech", InputCostPerCharacter: f64(0.00001)})
	req := &schemas.BifrostRequest{RequestType: schemas.SpeechStreamRequest,
		SpeechRequest: &schemas.BifrostSpeechRequest{Provider: schemas.OpenAI, Model: "test-tts", Input: &schemas.SpeechInput{Input: "Hello, world!"}}}
	est := s.EstimateMaxCost(req, "openai", "test-tts", nil, EstimateOptions{})
	assert.Equal(t, EstimatePerUnit, est.Source)
	assert.InDelta(t, 0.00013, est.Cost, 1e-12)
	noInput := &schemas.BifrostRequest{RequestType: schemas.SpeechRequest,
		SpeechRequest: &schemas.BifrostSpeechRequest{Provider: schemas.OpenAI, Model: "test-tts"}}
	assert.Equal(t, EstimateNone, s.EstimateMaxCost(noInput, "openai", "test-tts", nil, EstimateOptions{}).Source)
}

func TestEstimateMaxCost_Transcription_ReturnsZero(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	s.SeedPricingForTest("test-stt", "openai", schemas.TranscriptionRequest, configstoreTables.TableModelPricing{
		Model: "test-stt", Provider: "openai", Mode: "audio_transcription", InputCostPerCharacter: f64(0.0001)})
	req := &schemas.BifrostRequest{RequestType: schemas.TranscriptionRequest,
		TranscriptionRequest: &schemas.BifrostTranscriptionRequest{Provider: schemas.OpenAI, Model: "test-stt"}}
	est := s.EstimateMaxCost(req, "openai", "test-stt", nil, EstimateOptions{})
	assert.Equal(t, EstimateZero, est.Source)
	assert.Equal(t, 0.0, est.Cost)
	assert.Equal(t, 0.0, s.EstimateRequestCost(req, "openai", "test-stt", nil))
}

func videoRow(provider string, perSec float64) configstoreTables.TableModelPricing {
	return configstoreTables.TableModelPricing{Model: "test-video", Provider: provider, Mode: "video_generation", OutputCostPerVideoPerSecond: f64(perSec)}
}

func videoReq(provider schemas.ModelProvider, seconds string) *schemas.BifrostRequest {
	r := &schemas.BifrostVideoGenerationRequest{Provider: provider, Model: "test-video"}
	if seconds != "" {
		r.Params = &schemas.VideoGenerationParameters{Seconds: strp(seconds)}
	}
	return &schemas.BifrostRequest{RequestType: schemas.VideoGenerationRequest, VideoGenerationRequest: r}
}

func TestEstimateMaxCost_VideoGen_ExplicitSeconds(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	s.SeedPricingForTest("test-video", "openai", schemas.VideoGenerationRequest, videoRow("openai", 0.10))
	est := s.EstimateMaxCost(videoReq(schemas.OpenAI, "5"), "openai", "test-video", nil, EstimateOptions{})
	assert.InDelta(t, 0.50, est.Cost, 1e-9)
	assert.Equal(t, EstimateNone, s.EstimateMaxCost(videoReq(schemas.OpenAI, "zero"), "openai", "test-video", nil, EstimateOptions{}).Source)
}

func TestEstimateMaxCost_VideoGen_NoSeconds_OpenAI(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	s.SeedPricingForTest("test-video", "openai", schemas.VideoGenerationRequest, videoRow("openai", 0.10))
	// OpenAI's API default clip length is 4 s.
	est := s.EstimateMaxCost(videoReq(schemas.OpenAI, ""), "openai", "test-video", nil, EstimateOptions{})
	assert.Equal(t, EstimatePerUnit, est.Source)
	assert.InDelta(t, 0.40, est.Cost, 1e-9)
}

func TestEstimateMaxCost_VideoGen_NoSeconds_UnknownProvider(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	s.SeedPricingForTest("test-video", "runway", schemas.VideoGenerationRequest, videoRow("runway", 0.10))
	est := s.EstimateMaxCost(videoReq(schemas.ModelProvider("runway"), ""), "runway", "test-video", nil, EstimateOptions{})
	assert.Equal(t, EstimateNone, est.Source)
	assert.Equal(t, 0.0, est.Cost)
}

func TestEstimateMaxCost_VideoGen_NoSeconds_Gemini(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	s.SeedPricingForTest("test-video", "gemini", schemas.VideoGenerationRequest, videoRow("gemini", 0.10))
	est := s.EstimateMaxCost(videoReq(schemas.Gemini, ""), "gemini", "test-video", nil, EstimateOptions{})
	assert.InDelta(t, 0.80, est.Cost, 1e-9)
	remix := &schemas.BifrostRequest{RequestType: schemas.VideoRemixRequest,
		VideoRemixRequest: &schemas.BifrostVideoRemixRequest{Provider: schemas.Gemini}}
	assert.InDelta(t, 0.80, s.EstimateMaxCost(remix, "gemini", "test-video", nil, EstimateOptions{}).Cost, 1e-9)
}

func TestEstimateMaxCost_TextClassReturnsTokens(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	s.SeedPricingForTest("gpt-test", "openai", schemas.ChatCompletionRequest, chatRow(0.0001, 0.0002, 1000, 500))
	est := s.EstimateMaxCost(chatReq("gpt-test", nil), "openai", "gpt-test", nil, EstimateOptions{})
	assert.Equal(t, EstimateTokens, est.Source)
	assert.Equal(t, "base", est.Tier)
	assert.InDelta(t, 1000*0.0001+500*0.0002, est.Cost, 1e-9)
	assert.Equal(t, 0.0, s.EstimateRequestCost(chatReq("gpt-test", nil), "openai", "gpt-test", nil))
	stream := chatReq("gpt-test", nil)
	stream.RequestType = schemas.ChatCompletionStreamRequest
	assert.InDelta(t, est.Cost, s.EstimateMaxCost(stream, "openai", "gpt-test", nil, EstimateOptions{}).Cost, 1e-9)
}

func TestEstimateMaxCost_Tier1_EntryBounds(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	s.SeedPricingForTest("gpt-test", "openai", schemas.ChatCompletionRequest, chatRow(1e-6, 2e-6, 8192, 2048))
	est := s.EstimateMaxCost(chatReq("gpt-test", nil), "openai", "gpt-test", nil, EstimateOptions{})
	assert.True(t, est.BoundsFromEntry)
	assert.Equal(t, 8192, est.MaxInputTokens)
	assert.Equal(t, 2048, est.MaxOutputTokens)
	assert.InDelta(t, 8192*1e-6+2048*2e-6, est.Cost, 1e-12)

	// A row without limits uses the 4096 defaults.
	s.SeedPricingForTest("gpt-nolimit", "openai", schemas.ChatCompletionRequest, chatRow(1e-6, 2e-6, 0, 0))
	est = s.EstimateMaxCost(chatReq("gpt-nolimit", nil), "openai", "gpt-nolimit", nil, EstimateOptions{})
	assert.False(t, est.BoundsFromEntry)
	assert.Equal(t, 4096, est.MaxInputTokens)
	assert.Equal(t, 4096, est.MaxOutputTokens)
}

func TestEstimateMaxCost_Tier1_CallerCaps(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	s.SeedPricingForTest("gpt-test", "openai", schemas.ChatCompletionRequest, chatRow(1e-6, 2e-6, 8192, 2048))
	s.SeedPricingForTest("gpt-test", "openai", schemas.TextCompletionRequest, configstoreTables.TableModelPricing{
		Model: "gpt-test", Provider: "openai", Mode: "completion", InputCostPerToken: f64(1e-6), OutputCostPerToken: f64(2e-6)})
	s.SeedPricingForTest("emb-test", "openai", schemas.EmbeddingRequest, configstoreTables.TableModelPricing{
		Model: "emb-test", Provider: "openai", Mode: "embedding", InputCostPerToken: f64(1e-6), OutputCostPerToken: f64(9)})

	chat := chatReq("gpt-test", &schemas.ChatParameters{MaxCompletionTokens: intp(100)})
	est := s.EstimateMaxCost(chat, "openai", "gpt-test", nil, EstimateOptions{})
	assert.Equal(t, 100, est.MaxOutputTokens)
	assert.InDelta(t, 8192*1e-6+100*2e-6, est.Cost, 1e-12)

	responses := &schemas.BifrostRequest{RequestType: schemas.ResponsesRequest,
		ResponsesRequest: &schemas.BifrostResponsesRequest{Provider: schemas.OpenAI, Model: "gpt-test", Params: &schemas.ResponsesParameters{MaxOutputTokens: intp(50)}}}
	est = s.EstimateMaxCost(responses, "openai", "gpt-test", nil, EstimateOptions{})
	assert.Equal(t, 50, est.MaxOutputTokens)
	assert.Equal(t, EstimateTokens, est.Source)

	text := &schemas.BifrostRequest{RequestType: schemas.TextCompletionRequest,
		TextCompletionRequest: &schemas.BifrostTextCompletionRequest{Provider: schemas.OpenAI, Model: "gpt-test", Params: &schemas.TextCompletionParameters{MaxTokens: intp(7)}}}
	est = s.EstimateMaxCost(text, "openai", "gpt-test", nil, EstimateOptions{})
	assert.Equal(t, 7, est.MaxOutputTokens)

	emb := &schemas.BifrostRequest{RequestType: schemas.EmbeddingRequest,
		EmbeddingRequest: &schemas.BifrostEmbeddingRequest{Provider: schemas.OpenAI, Model: "emb-test"}}
	est = s.EstimateMaxCost(emb, "openai", "emb-test", nil, EstimateOptions{})
	assert.Equal(t, 0, est.MaxOutputTokens)
	assert.InDelta(t, 4096*1e-6, est.Cost, 1e-12)
}

func vkOverride(id, vk, model string, rt schemas.RequestType, patch string) *configstoreTables.TablePricingOverride {
	return &configstoreTables.TablePricingOverride{ID: id, Name: id, ScopeKind: string(ScopeKindVirtualKey), VirtualKeyID: strp(vk),
		MatchType: string(MatchTypeExact), Pattern: model, RequestTypes: []schemas.RequestType{rt}, PricingPatchJSON: patch}
}

func TestEstimateMaxCost_Tier2_OverrideAware(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	s.SeedPricingForTest("test-img", "openai", schemas.ImageGenerationRequest, configstoreTables.TableModelPricing{
		Model: "test-img", Provider: "openai", Mode: "image_generation", OutputCostPerImage: f64(0.16)})
	require.NoError(t, s.SeedOverridesForTest(vkOverride("o1", "vk-a", "test-img", schemas.ImageGenerationRequest, `{"output_cost_per_image": 0.32}`)))
	req := imageReq("test-img", 1, "", "")
	assert.InDelta(t, 0.32, s.EstimateMaxCost(req, "openai", "test-img", &LookupScopes{VirtualKeyID: "vk-a", Provider: "openai"}, EstimateOptions{}).Cost, 1e-9)
	assert.InDelta(t, 0.16, s.EstimateMaxCost(req, "openai", "test-img", &LookupScopes{VirtualKeyID: "vk-b", Provider: "openai"}, EstimateOptions{}).Cost, 1e-9)
	assert.InDelta(t, 0.16, s.EstimateMaxCost(req, "openai", "test-img", nil, EstimateOptions{}).Cost, 1e-9)
}

func TestEstimateMaxCost_BedrockMantleFold(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	s.SeedPricingForTest("claude-x", "bedrock", schemas.ChatCompletionRequest, chatRow(1e-6, 2e-6, 100, 10))
	req := chatReq("claude-x", nil)
	req.ChatRequest.Provider = schemas.BedrockMantle
	est := s.EstimateMaxCost(req, string(schemas.BedrockMantle), "claude-x", nil, EstimateOptions{})
	assert.Equal(t, EstimateTokens, est.Source)
	assert.InDelta(t, 100*1e-6+10*2e-6, est.Cost, 1e-12)
}

func TestEstimateMaxCost_Tier1_FallbackBounds_OverrideOnlyModel(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	// Custom prices for a model the datasheet does not know: no base row, no limits.
	require.NoError(t, s.SeedOverridesForTest(&configstoreTables.TablePricingOverride{ID: "g1", Name: "g1", ScopeKind: string(ScopeKindGlobal),
		MatchType: string(MatchTypeExact), Pattern: "self-hosted", RequestTypes: []schemas.RequestType{schemas.ChatCompletionRequest},
		PricingPatchJSON: `{"input_cost_per_token": 1e-6, "output_cost_per_token": 2e-6}`}))
	req := chatReq("self-hosted", nil)
	req.ChatRequest.Provider = schemas.ModelProvider("custom")
	est := s.EstimateMaxCost(req, "custom", "self-hosted", &LookupScopes{Provider: "custom"}, EstimateOptions{})
	assert.Equal(t, EstimateTokens, est.Source)
	assert.False(t, est.BoundsFromEntry)
	assert.Equal(t, 4096, est.MaxInputTokens)
	assert.InDelta(t, 4096*1e-6+4096*2e-6, est.Cost, 1e-12)

	est = s.EstimateMaxCost(req, "custom", "self-hosted", &LookupScopes{Provider: "custom"}, EstimateOptions{FallbackInputTokens: 8192, FallbackOutputTokens: 2048})
	assert.Equal(t, 8192, est.MaxInputTokens)
	assert.Equal(t, 2048, est.MaxOutputTokens)
	assert.InDelta(t, 8192*1e-6+2048*2e-6, est.Cost, 1e-12)
	assert.Equal(t, EstimateTokens, s.EstimateMaxCost(req, "custom", "self-hosted", nil, EstimateOptions{}).Source, "global custom prices apply without scopes")
}

func tierRow() configstoreTables.TableModelPricing {
	row := chatRow(1e-6, 2e-6, 1000, 100)
	row.InputCostPerTokenPriority, row.OutputCostPerTokenPriority = f64(2e-6), f64(4e-6)
	row.InputCostPerTokenFlex, row.OutputCostPerTokenFlex = f64(5e-7), f64(1e-6)
	row.InputCostPerTokenFast, row.OutputCostPerTokenFast = f64(6e-6), f64(3e-5)
	row.InferenceGeoUSMultiplier = f64(1.1)
	return row
}

func TestEstimateMaxCost_Tier1_TierCandidates(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	s.SeedPricingForTest("gpt-test", "openai", schemas.ChatCompletionRequest, tierRow())
	base := 1000*1e-6 + 100*2e-6
	priority := 1000*2e-6 + 100*4e-6
	fast := 1000*6e-6 + 100*3e-5
	tier := func(v schemas.BifrostServiceTier) *schemas.BifrostServiceTier { return &v }
	cases := []struct {
		name   string
		params *schemas.ChatParameters
		want   float64
		tier   string
	}{
		{"default", nil, base, "base"},
		{"explicit default", &schemas.ChatParameters{ServiceTier: tier(schemas.BifrostServiceTierDefault)}, base, "base"},
		{"priority", &schemas.ChatParameters{ServiceTier: tier(schemas.BifrostServiceTierPriority)}, priority, "priority"},
		{"auto may become priority", &schemas.ChatParameters{ServiceTier: tier(schemas.BifrostServiceTierAuto)}, priority, "priority"},
		{"flex never exceeds base", &schemas.ChatParameters{ServiceTier: tier(schemas.BifrostServiceTierFlex)}, base, "base"},
		{"fast", &schemas.ChatParameters{Speed: strp("fast")}, fast, "fast"},
		{"us geo", &schemas.ChatParameters{InferenceGeo: strp("us")}, base * 1.1, "base+us"},
		{"priority us geo", &schemas.ChatParameters{ServiceTier: tier(schemas.BifrostServiceTierPriority), InferenceGeo: strp("US")}, priority * 1.1, "priority+us"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			est := s.EstimateMaxCost(chatReq("gpt-test", c.params), "openai", "gpt-test", nil, EstimateOptions{})
			assert.InDelta(t, c.want, est.Cost, 1e-12)
			assert.Equal(t, c.tier, est.Tier)
		})
	}
	// Responses requests carry service_tier too.
	responses := &schemas.BifrostRequest{RequestType: schemas.ResponsesRequest,
		ResponsesRequest: &schemas.BifrostResponsesRequest{Provider: schemas.OpenAI, Model: "gpt-test", Params: &schemas.ResponsesParameters{ServiceTier: tier(schemas.BifrostServiceTierPriority)}}}
	assert.InDelta(t, priority, s.EstimateMaxCost(responses, "openai", "gpt-test", nil, EstimateOptions{}).Cost, 1e-12)
}

func TestEstimateMaxCost_Tier1_Surcharges_WebSearch(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	row := chatRow(1e-6, 2e-6, 1000, 100)
	row.SearchContextCostPerQuery = f64(0.01)
	s.SeedPricingForTest("gpt-test", "openai", schemas.ChatCompletionRequest, row)
	base := 1000*1e-6 + 100*2e-6
	plain := s.EstimateMaxCost(chatReq("gpt-test", nil), "openai", "gpt-test", nil, EstimateOptions{})
	assert.InDelta(t, base, plain.Cost, 1e-12)
	assert.Equal(t, 0.0, plain.Surcharges)

	opts := s.EstimateMaxCost(chatReq("gpt-test", &schemas.ChatParameters{WebSearchOptions: &schemas.ChatWebSearchOptions{}}), "openai", "gpt-test", nil, EstimateOptions{})
	assert.InDelta(t, base+0.01, opts.Cost, 1e-12)
	assert.InDelta(t, 0.01, opts.Surcharges, 1e-12)

	tool := s.EstimateMaxCost(chatReq("gpt-test", &schemas.ChatParameters{Tools: []schemas.ChatTool{{Type: schemas.ChatToolType("web_search_20260209")}}}), "openai", "gpt-test", nil, EstimateOptions{})
	assert.InDelta(t, base+0.01, tool.Cost, 1e-12)

	responses := &schemas.BifrostRequest{RequestType: schemas.ResponsesRequest,
		ResponsesRequest: &schemas.BifrostResponsesRequest{Provider: schemas.OpenAI, Model: "gpt-test", Params: &schemas.ResponsesParameters{Tools: []schemas.ResponsesTool{{Type: schemas.ResponsesToolType("web_search")}}}}}
	assert.InDelta(t, base+0.01, s.EstimateMaxCost(responses, "openai", "gpt-test", nil, EstimateOptions{}).Cost, 1e-12)
}

func TestEstimateMaxCost_Tier1_Surcharges_ImageParts(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	row := chatRow(1e-6, 2e-6, 1000, 100)
	row.InputCostPerImage = f64(0.002)
	s.SeedPricingForTest("gpt-test", "openai", schemas.ChatCompletionRequest, row)
	req := chatReq("gpt-test", nil)
	req.ChatRequest.Input = []schemas.ChatMessage{{Content: &schemas.ChatMessageContent{ContentBlocks: []schemas.ChatContentBlock{
		{Type: schemas.ChatContentBlockTypeText, Text: strp("describe")},
		{Type: schemas.ChatContentBlockTypeImage},
		{Type: schemas.ChatContentBlockTypeImage},
	}}}}
	est := s.EstimateMaxCost(req, "openai", "gpt-test", nil, EstimateOptions{})
	assert.InDelta(t, 1000*1e-6+100*2e-6+2*0.002, est.Cost, 1e-12)
	assert.InDelta(t, 0.004, est.Surcharges, 1e-12)
}

func TestEstimateMaxCost_Tier1_ZeroRates(t *testing.T) {
	t.Parallel()
	s := NewTestStore(nil)
	s.SeedPricingForTest("gpt-free", "openai", schemas.ChatCompletionRequest, configstoreTables.TableModelPricing{Model: "gpt-free", Provider: "openai", Mode: "chat"})
	est := s.EstimateMaxCost(chatReq("gpt-free", nil), "openai", "gpt-free", nil, EstimateOptions{})
	assert.Equal(t, EstimateZero, est.Source)
	assert.Equal(t, 0.0, est.Cost)
	assert.Equal(t, 4096, est.MaxInputTokens)
}
