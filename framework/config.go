package framework

import "github.com/neria-cloud/meridian/framework/modelcatalog"

// FrameworkConfig represents the configuration for the framework.
type FrameworkConfig struct {
	Pricing *modelcatalog.Config `json:"pricing,omitempty"`
}
