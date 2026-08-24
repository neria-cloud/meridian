package oauth2

import "github.com/neria-cloud/meridian/core/schemas"

var logger schemas.Logger

func SetLogger(l schemas.Logger) {
	logger = l
}
