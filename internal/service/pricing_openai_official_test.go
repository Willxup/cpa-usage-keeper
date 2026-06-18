package service

import "testing"

func TestNormalizePricingSyncSourceDefaultsToOpenAIOfficial(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty defaults to official", input: "", want: pricingSyncSourceOpenAIOfficialID},
		{name: "official alias", input: "openai_official", want: pricingSyncSourceOpenAIOfficialID},
		{name: "official spaced alias", input: "OpenAI Official", want: pricingSyncSourceOpenAIOfficialID},
		{name: "models dev alias", input: "models_dev", want: pricingSyncSourceModelsDevID},
		{name: "models dev dotted alias", input: "models.dev", want: pricingSyncSourceModelsDevID},
		{name: "unknown falls back to official", input: "unknown", want: pricingSyncSourceOpenAIOfficialID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizePricingSyncSource(tt.input); got != tt.want {
				t.Fatalf("normalizePricingSyncSource(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
