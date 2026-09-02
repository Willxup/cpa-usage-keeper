package repository

import (
	"testing"
)

func TestModelDimensionKeyPrefersTrimmedAlias(t *testing.T) {
	if got := modelDimensionKey(" model ", " alias "); got != "alias" {
		t.Fatalf("modelDimensionKey = %q, want alias", got)
	}
	if got := modelDimensionGroupKey(" model ", "   "); got != "model" {
		t.Fatalf("modelDimensionGroupKey = %q, want model", got)
	}
}

func TestListModelDimensionOptionsDeduplicatesDisplayKeys(t *testing.T) {
	alias := " alias-a "
	blank := "  "
	values := modelDimensionOptionsFromPairs([]modelDimensionOption{
		{Model: "model", ModelAlias: &alias},
		{Model: "model", ModelAlias: &blank},
		{Model: "model", ModelAlias: nil},
		{Model: "other", ModelAlias: nil},
		{Model: " ", ModelAlias: nil},
	})
	want := []string{"alias-a", "model", "other"}
	if len(values) != len(want) {
		t.Fatalf("values = %v, want %v", values, want)
	}
	for index := range want {
		if values[index] != want[index] {
			t.Fatalf("values = %v, want %v", values, want)
		}
	}
}
