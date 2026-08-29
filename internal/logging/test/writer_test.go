package logging_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"cpa-usage-keeper/internal/logging"
)

func TestPlainWriterMapsPartialWritesToColoredInput(t *testing.T) {
	colored := []byte("\x1b[32minfo\x1b[0m")

	for _, testCase := range []struct {
		name      string
		writeErr  error
		wantError error
	}{
		{name: "short write", wantError: io.ErrShortWrite},
		{name: "partial write with error", writeErr: io.ErrUnexpectedEOF, wantError: io.ErrUnexpectedEOF},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			target := &partialWriter{limit: 2, writeErr: testCase.writeErr}
			writer := logging.NewPlainWriter(target)

			written, err := writer.Write(colored)

			if !errors.Is(err, testCase.wantError) {
				t.Fatalf("expected %v, got %v", testCase.wantError, err)
			}
			if written != len("\x1b[32min") {
				t.Fatalf("expected colored input prefix length %d, got %d", len("\x1b[32min"), written)
			}
			if target.String() != "in" {
				t.Fatalf("expected plain partial output, got %q", target.String())
			}
		})
	}
}

type partialWriter struct {
	bytes.Buffer
	limit    int
	writeErr error
}

func (w *partialWriter) Write(content []byte) (int, error) {
	if len(content) > w.limit {
		content = content[:w.limit]
	}
	written, _ := w.Buffer.Write(content)
	return written, w.writeErr
}
