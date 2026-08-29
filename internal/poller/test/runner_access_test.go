package poller_test

import (
	"context"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"cpa-usage-keeper/internal/poller"
)

func setRedisProcessRunnerSleep(t *testing.T, runner *poller.RedisProcessRunner, sleep func(context.Context, time.Duration) bool) {
	t.Helper()
	setUnexportedField(t, runner, "sleep", sleep)
}

func setRedisIngestRunnerSleep(t *testing.T, runner *poller.RedisIngestRunner, sleep func(context.Context, time.Duration) bool) {
	t.Helper()
	setUnexportedField(t, runner, "sleep", sleep)
}

func setUnexportedField(t *testing.T, target any, name string, value any) {
	t.Helper()
	field := reflect.ValueOf(target).Elem().FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("field %q not found", name)
	}
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(value))
}

func requireDurations(t *testing.T, got []time.Duration, want []time.Duration) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("unexpected delays: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected delays: got %v want %v", got, want)
		}
	}
}
