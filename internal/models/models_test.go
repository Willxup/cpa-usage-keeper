package models

import "testing"

func TestAllIncludesCoreModels(t *testing.T) {
	items := All()
	if len(items) != 5 {
		t.Fatalf("expected 5 models, got %d", len(items))
	}
}
