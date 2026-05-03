package main

import (
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/jinto/kittypaw/core"
)

func TestSetupLLMProviderChoicesOrder(t *testing.T) {
	got := setupLLMProviderChoices()
	want := []string{"Anthropic (Claude)", "OpenAI", "Gemini", "OpenRouter", "Local (Ollama)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("setupLLMProviderChoices() = %#v, want %#v", got, want)
	}
}

func TestSetupLLMDefaultIndex(t *testing.T) {
	cases := []struct {
		name string
		cfg  core.LLMConfig
		want int
	}{
		{"anthropic", core.LLMConfig{Provider: "anthropic"}, 1},
		{"openai", core.LLMConfig{Provider: "openai"}, 2},
		{"gemini", core.LLMConfig{Provider: "gemini"}, 3},
		{"openrouter", core.LLMConfig{Provider: "openai", BaseURL: core.OpenRouterBaseURL}, 4},
		{"local", core.LLMConfig{Provider: "openai", BaseURL: "http://localhost:11434/v1/chat/completions"}, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := setupLLMDefaultIndex(&core.Config{LLM: tc.cfg})
			if got != tc.want {
				t.Fatalf("setupLLMDefaultIndex(%+v) = %d, want %d", tc.cfg, got, tc.want)
			}
		})
	}
}

func TestSetupLLMModelChoices(t *testing.T) {
	if got := setupLLMModelChoices("anthropic")[0]; got != core.ClaudeDefaultModel {
		t.Fatalf("anthropic default model = %q, want %q", got, core.ClaudeDefaultModel)
	}
	if got := setupLLMModelChoices("openai")[0]; got != core.OpenAIDefaultModel {
		t.Fatalf("openai default model = %q, want %q", got, core.OpenAIDefaultModel)
	}
	if got := setupLLMModelChoices("gemini")[0]; got != core.GeminiDefaultModel {
		t.Fatalf("gemini default model = %q, want %q", got, core.GeminiDefaultModel)
	}
}

func TestReadPasswordMaskedLoopCtrlCAborts(t *testing.T) {
	input := []byte{3}
	pos := 0

	got, err := readPasswordMaskedLoop(func(p []byte) (int, error) {
		if pos >= len(input) {
			return 0, io.EOF
		}
		p[0] = input[pos]
		pos++
		return 1, nil
	}, io.Discard)

	if !errors.Is(err, errPromptCanceled) {
		t.Fatalf("err = %v, want errPromptCanceled", err)
	}
	if got != "" {
		t.Fatalf("password = %q, want empty", got)
	}
}

func TestReadPasswordMaskedLoopEmptyEnterAllowsRetry(t *testing.T) {
	input := []byte{'\n'}
	pos := 0

	got, err := readPasswordMaskedLoop(func(p []byte) (int, error) {
		if pos >= len(input) {
			return 0, io.EOF
		}
		p[0] = input[pos]
		pos++
		return 1, nil
	}, io.Discard)

	if err != nil {
		t.Fatalf("err = %v, want nil for empty Enter", err)
	}
	if got != "" {
		t.Fatalf("password = %q, want empty", got)
	}
}
