package fixtures

import "github.com/neria-cloud/meridian/core/schemas"

// Chat builds a chat request; params may be nil.
func Chat(provider schemas.ModelProvider, model string, params *schemas.ChatParameters) *schemas.BifrostRequest {
	return &schemas.BifrostRequest{RequestType: schemas.ChatCompletionRequest,
		ChatRequest: &schemas.BifrostChatRequest{Provider: provider, Model: model, Params: params}}
}

// ChatWithImages builds a chat request whose prompt carries n image parts.
func ChatWithImages(provider schemas.ModelProvider, model string, n int) *schemas.BifrostRequest {
	blocks := []schemas.ChatContentBlock{{Type: schemas.ChatContentBlockTypeText, Text: Ptr("describe")}}
	for i := 0; i < n; i++ {
		blocks = append(blocks, schemas.ChatContentBlock{Type: schemas.ChatContentBlockTypeImage})
	}
	r := Chat(provider, model, nil)
	r.ChatRequest.Input = []schemas.ChatMessage{{Content: &schemas.ChatMessageContent{ContentBlocks: blocks}}}
	return r
}

// Responses builds a responses request; params may be nil.
func Responses(provider schemas.ModelProvider, model string, params *schemas.ResponsesParameters) *schemas.BifrostRequest {
	return &schemas.BifrostRequest{RequestType: schemas.ResponsesRequest,
		ResponsesRequest: &schemas.BifrostResponsesRequest{Provider: provider, Model: model, Params: params}}
}

// TextCompletion builds a text completion request; params may be nil.
func TextCompletion(provider schemas.ModelProvider, model string, params *schemas.TextCompletionParameters) *schemas.BifrostRequest {
	return &schemas.BifrostRequest{RequestType: schemas.TextCompletionRequest,
		TextCompletionRequest: &schemas.BifrostTextCompletionRequest{Provider: provider, Model: model, Params: params}}
}

// Embedding builds an embedding request.
func Embedding(provider schemas.ModelProvider, model string) *schemas.BifrostRequest {
	return &schemas.BifrostRequest{RequestType: schemas.EmbeddingRequest,
		EmbeddingRequest: &schemas.BifrostEmbeddingRequest{Provider: provider, Model: model}}
}

// Image builds an image generation request; n <= 0, size "" and quality "" are omitted.
func Image(provider schemas.ModelProvider, model string, n int, size, quality string) *schemas.BifrostRequest {
	p := &schemas.ImageGenerationParameters{}
	if n > 0 {
		p.N = Ptr(n)
	}
	if size != "" {
		p.Size = Ptr(size)
	}
	if quality != "" {
		p.Quality = Ptr(quality)
	}
	return &schemas.BifrostRequest{RequestType: schemas.ImageGenerationRequest,
		ImageGenerationRequest: &schemas.BifrostImageGenerationRequest{Provider: provider, Model: model, Params: p}}
}

// ImageEdit builds an image edit request.
func ImageEdit(provider schemas.ModelProvider, model string, n int, quality string) *schemas.BifrostRequest {
	p := &schemas.ImageEditParameters{N: Ptr(n)}
	if quality != "" {
		p.Quality = Ptr(quality)
	}
	return &schemas.BifrostRequest{RequestType: schemas.ImageEditRequest,
		ImageEditRequest: &schemas.BifrostImageEditRequest{Provider: provider, Model: model, Params: p}}
}

// Speech builds a TTS request for text.
func Speech(provider schemas.ModelProvider, model, text string) *schemas.BifrostRequest {
	return &schemas.BifrostRequest{RequestType: schemas.SpeechRequest,
		SpeechRequest: &schemas.BifrostSpeechRequest{Provider: provider, Model: model, Input: &schemas.SpeechInput{Input: text}}}
}

// Video builds a video generation request; seconds "" leaves the duration unset.
func Video(provider schemas.ModelProvider, model, seconds string) *schemas.BifrostRequest {
	r := &schemas.BifrostVideoGenerationRequest{Provider: provider, Model: model}
	if seconds != "" {
		r.Params = &schemas.VideoGenerationParameters{Seconds: Ptr(seconds)}
	}
	return &schemas.BifrostRequest{RequestType: schemas.VideoGenerationRequest, VideoGenerationRequest: r}
}

// Transcription builds an STT request.
func Transcription(provider schemas.ModelProvider, model string) *schemas.BifrostRequest {
	return &schemas.BifrostRequest{RequestType: schemas.TranscriptionRequest,
		TranscriptionRequest: &schemas.BifrostTranscriptionRequest{Provider: provider, Model: model}}
}
