package llm

import (
	"fmt"
	"os"
	"time"

	"github.com/ponchione/agent-conductor/internal/config"
)

// NewClientFromProvider creates the appropriate Client implementation based on
// the provider config's explicit Type field.
func NewClientFromProvider(pc config.ProviderConfig, projectPath string) (Client, error) {
	switch pc.Type {
	case "openai":
		return NewProviderClient(Provider{
			Endpoint:         pc.Endpoint,
			Model:            pc.Model,
			APIKey:           os.ExpandEnv(pc.APIKey),
			TimeoutSeconds:   pc.TimeoutSeconds,
			MaxContextTokens: pc.MaxContextTokens,
			Temperature:      pc.Temperature,
		}), nil

	case "claude-cli":
		timeout := time.Duration(pc.TimeoutSeconds) * time.Second
		if pc.TimeoutSeconds == 0 {
			timeout = 120 * time.Second
		}
		return NewClaudeCLIClient(pc.Model, timeout, projectPath), nil

	default:
		return nil, fmt.Errorf("unknown provider type %q", pc.Type)
	}
}
