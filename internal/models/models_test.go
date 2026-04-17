package models

import "testing"

func TestAllIncludesCoreModels(t *testing.T) {
	items := All()
	if len(items) != 3 {
		t.Fatalf("expected 3 models, got %d", len(items))
	}
}
