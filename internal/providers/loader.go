package providers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/nullswan/golem/internal/config"
	baseprovider "github.com/nullswan/golem/internal/providers/base"
	"github.com/nullswan/golem/internal/providers/ollamaprovider"
	"github.com/nullswan/golem/internal/providers/openaiprovider"
	openrouterprovider "github.com/nullswan/golem/internal/providers/openrouter"
)

func LoadTextToTextProvider(
	provider AIProvider,
	model string,
) (baseprovider.TextToTextProvider, error) {
	switch provider {
	case OpenAIProvider:
		oaiConfig := openaiprovider.NewOAIProviderConfig(
			os.Getenv("OPENAI_API_KEY"),
			model,
		)
		p, err := openaiprovider.NewTextToTextProvider(
			oaiConfig,
		)
		if err != nil {
			return nil, fmt.Errorf("error creating openai provider: %w", err)
		}

		return p, nil
	case OllamaProvider:
		var cmd *exec.Cmd
		if !ollamaServerIsRunning() {
			var err error
			cmd, err = tryStartOllama()
			if err != nil {
				ollamaOutput := config.GetProgramDirectory() + "/ollama"
				err = backoff.Retry(func() error {
					fmt.Printf(
						"Download ollama to %s\n",
						ollamaOutput,
					)
					return downloadOllama(
						context.TODO(),
						ollamaOutput,
					)
				}, backoff.WithMaxRetries(backoff.NewConstantBackOff(time.Second), 3))
				if err != nil {
					return nil, fmt.Errorf("error installing ollama: %w", err)
				}
			}
		}
		url := getOllamaURL()

		ollamaConfig := ollamaprovider.NewOlamaProviderConfig(
			url,
			model,
		)
		p, err := ollamaprovider.NewTextToTextProvider(
			ollamaConfig,
			cmd,
		)
		if err != nil {
			return nil, fmt.Errorf("error creating ollama provider: %w", err)
		}

		return p, nil
	case OpenRouterProvider:
		orConfig := openrouterprovider.NewORProviderConfig(
			os.Getenv("OPENROUTER_API_KEY"),
			model,
		)
		p, err := openrouterprovider.NewTextToTextProvider(
			orConfig,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"error creating openrouter provider: %w",
				err,
			)
		}

		return p, nil
	case AnthropicProvider:
		return nil, errors.New("anthropic provider not implemented")
	default:
		return nil, fmt.Errorf("unknown provider: %s", provider)
	}
}