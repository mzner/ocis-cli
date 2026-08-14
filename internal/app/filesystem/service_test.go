package filesystem

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	"github.com/mzner/ocis-cli/internal/transfer"
)

func TestProgressReporterWritesAggregateProgress(t *testing.T) {
	var output bytes.Buffer
	report := progressReporter(Options{Err: &output, OutputMode: appoutput.Human})
	if report == nil {
		t.Fatal("progress reporter is nil")
	}
	report(transfer.Progress{Operation: "upload", Destination: "/report.txt", CompletedBytes: 50, TotalBytes: 100, CompletedFiles: 1, TotalFiles: 2, StartedAt: time.Now().Add(-time.Second)})
	for _, expected := range []string{"upload", "1/2 files", "50/100 bytes", "50%", "/report.txt"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("progress missing %q: %s", expected, output.String())
		}
	}
	if progressReporter(Options{Quiet: true}) != nil {
		t.Fatal("quiet mode returned a progress reporter")
	}
}

func TestStreamHelperErrors(t *testing.T) {
	if _, cleanup, err := spoolInput(failingReader{}); err == nil {
		cleanup()
		t.Fatal("spooling a failing reader succeeded")
	}
	if err := writeFileTo(io.Discard, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("writing a missing file succeeded")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestBatchInputGuards(t *testing.T) {
	for _, input := range []string{`{"operation":"upload","source":"-","destination":"/x"}`, `{"operation":"download","source":"/x","destination":"-"}`, `{"operation":"touch","path":"/x","parents":true}`, `{"operation":"mkdir","path":"/x","unknown":true}`} {
		if _, err := parseBatchOperations(strings.NewReader(input), 10); !apperror.IsKind(err, apperror.KindUsage) {
			t.Errorf("input %s: %v", input, err)
		}
	}
	_, err := parseBatchOperations(strings.NewReader("{\"operation\":\"mkdir\",\"path\":\"/a\"}\n{\"operation\":\"mkdir\",\"path\":\"/b\"}\n"), 1)
	if !apperror.IsKind(err, apperror.KindUsage) || !strings.Contains(err.Error(), "--max-operations 1") {
		t.Fatalf("limit error: %v", err)
	}
	for _, input := range []string{"", `{"operation":"mkdir","path":"/x"} {}`} {
		if _, err := parseBatchOperations(strings.NewReader(input), 10); !apperror.IsKind(err, apperror.KindUsage) {
			t.Errorf("input %q: %v", input, err)
		}
	}
}
