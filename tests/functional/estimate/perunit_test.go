package estimate

import (
	"testing"

	"github.com/neria-cloud/meridian/core/schemas"
	"github.com/neria-cloud/meridian/framework/configstore/tables"
	"github.com/neria-cloud/meridian/framework/modelcatalog"
	"github.com/neria-cloud/meridian/tests/functional/internal/fixtures"
	"github.com/stretchr/testify/assert"
)

func imageRow() tables.TableModelPricing {
	return tables.TableModelPricing{Mode: "image_generation", OutputCostPerImage: f(0.04),
		OutputCostPerImageHighQuality: f(0.16), OutputCostPerImageAbove512x512Pixels: f(0.08)}
}

func TestPerUnit_Image(t *testing.T) {
	t.Parallel()
	mc := fixtures.NewCatalog(t,
		fixtures.Row{Model: "img", Provider: "openai", RequestType: schemas.ImageGenerationRequest, Pricing: imageRow()},
		fixtures.Row{Model: "img", Provider: "openai", RequestType: schemas.ImageEditRequest, Pricing: func() tables.TableModelPricing { r := imageRow(); r.Mode = "image_edit"; return r }()},
	)
	cases := []struct {
		name string
		req  *schemas.BifrostRequest
		want float64
	}{
		{"single base", fixtures.Image(openai, "img", 1, "256x256", ""), 0.04},
		{"three base", fixtures.Image(openai, "img", 3, "256x256", ""), 0.12},
		{"size tier", fixtures.Image(openai, "img", 1, "1024x1024", ""), 0.08},
		{"quality wins", fixtures.Image(openai, "img", 2, "1024x1024", "high"), 0.32},
		{"params nil defaults to one image", fixtures.Image(openai, "img", 0, "", ""), 0.04},
		{"edit", fixtures.ImageEdit(openai, "img", 2, "high"), 0.32},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			est := mc.EstimateMaxCost(c.req, "openai", "img", nil, none)
			assert.Equal(t, modelcatalog.EstimatePerUnit, est.Source)
			assert.InDelta(t, c.want, est.Cost, 1e-9)
			assert.InDelta(t, c.want, mc.EstimateRequestCost(c.req, "openai", "img", nil), 1e-9)
		})
	}
}

func TestPerUnit_Speech(t *testing.T) {
	t.Parallel()
	mc := fixtures.NewCatalog(t, fixtures.Row{Model: "tts", Provider: "openai", RequestType: schemas.SpeechRequest,
		Pricing: tables.TableModelPricing{Mode: "audio_speech", InputCostPerCharacter: f(1.5e-5)}})
	est := mc.EstimateMaxCost(fixtures.Speech(openai, "tts", "Hello, wörld!"), "openai", "tts", nil, none)
	assert.Equal(t, modelcatalog.EstimatePerUnit, est.Source)
	assert.InDelta(t, 13*1.5e-5, est.Cost, 1e-12)
}

func TestPerUnit_Video(t *testing.T) {
	t.Parallel()
	row := tables.TableModelPricing{Mode: "video_generation", OutputCostPerVideoPerSecond: f(0.10)}
	mc := fixtures.NewCatalog(t,
		fixtures.Row{Model: "vid", Provider: "openai", RequestType: schemas.VideoGenerationRequest, Pricing: row},
		fixtures.Row{Model: "vid", Provider: "gemini", RequestType: schemas.VideoGenerationRequest, Pricing: row},
		fixtures.Row{Model: "vid", Provider: "runway", RequestType: schemas.VideoGenerationRequest, Pricing: row},
	)
	assert.InDelta(t, 0.50, mc.EstimateMaxCost(fixtures.Video(openai, "vid", "5"), "openai", "vid", nil, none).Cost, 1e-9)
	assert.InDelta(t, 0.40, mc.EstimateMaxCost(fixtures.Video(openai, "vid", ""), "openai", "vid", nil, none).Cost, 1e-9, "OpenAI default 4 s")
	assert.InDelta(t, 0.80, mc.EstimateMaxCost(fixtures.Video(schemas.Gemini, "vid", ""), "gemini", "vid", nil, none).Cost, 1e-9, "Gemini default 8 s")
	unknown := mc.EstimateMaxCost(fixtures.Video(schemas.ModelProvider("runway"), "vid", ""), "runway", "vid", nil, none)
	assert.Equal(t, modelcatalog.EstimateNone, unknown.Source)
}

func TestPerUnit_TranscriptionIsZero(t *testing.T) {
	t.Parallel()
	mc := fixtures.NewCatalog(t, fixtures.Row{Model: "stt", Provider: "openai", RequestType: schemas.TranscriptionRequest,
		Pricing: tables.TableModelPricing{Mode: "audio_transcription", InputCostPerAudioPerSecond: f(1e-4)}})
	est := mc.EstimateMaxCost(fixtures.Transcription(openai, "stt"), "openai", "stt", nil, none)
	assert.Equal(t, modelcatalog.EstimateZero, est.Source)
	assert.Equal(t, 0.0, est.Cost)
}

func TestPerUnit_TranscriptionCallerSeconds(t *testing.T) {
	t.Parallel()
	mc := fixtures.NewCatalog(t, fixtures.Row{Model: "stt", Provider: "openai", RequestType: schemas.TranscriptionRequest,
		Pricing: tables.TableModelPricing{Mode: "audio_transcription", InputCostPerAudioPerSecond: f(1e-4)}})
	est := mc.EstimateMaxCost(fixtures.Transcription(openai, "stt"), "openai", "stt", nil, modelcatalog.EstimateOptions{TranscriptionSeconds: 3600})
	assert.Equal(t, modelcatalog.EstimatePerUnit, est.Source)
	assert.InDelta(t, 0.36, est.Cost, 1e-9)
	assert.Equal(t, 3600, est.MediaSeconds)
}

func TestPerUnit_VideoCallerDefault(t *testing.T) {
	t.Parallel()
	row := tables.TableModelPricing{Mode: "video_generation", OutputCostPerVideoPerSecond: f(0.10)}
	mc := fixtures.NewCatalog(t, fixtures.Row{Model: "vid", Provider: "runway", RequestType: schemas.VideoGenerationRequest, Pricing: row})
	est := mc.EstimateMaxCost(fixtures.Video(schemas.ModelProvider("runway"), "vid", ""), "runway", "vid", nil, modelcatalog.EstimateOptions{VideoSeconds: 10})
	assert.Equal(t, modelcatalog.EstimatePerUnit, est.Source)
	assert.InDelta(t, 1.0, est.Cost, 1e-9)
}
