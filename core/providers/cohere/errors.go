package cohere

import (
	providerUtils "github.com/neria-cloud/meridian/core/providers/utils"
	"github.com/neria-cloud/meridian/core/schemas"
	"github.com/valyala/fasthttp"
)

func parseCohereError(resp *fasthttp.Response) *schemas.BifrostError {
	var errorResp CohereError
	bifrostErr := providerUtils.HandleProviderAPIError(resp, &errorResp)
	bifrostErr.Type = &errorResp.Type
	if bifrostErr.Error == nil {
		bifrostErr.Error = &schemas.ErrorField{}
	}
	bifrostErr.Error.Message = errorResp.Message
	if errorResp.Code != nil {
		bifrostErr.Error.Code = errorResp.Code
	}
	return bifrostErr
}
