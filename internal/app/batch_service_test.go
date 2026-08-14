package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func TestBatchDryRunValidatesAllOperationsWithoutMutation(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		requests++
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	input := strings.NewReader(strings.Join([]string{
		`{"operation":"mkdir","path":"/batch/nested","parents":true}`,
		`{"operation":"touch","path":"/batch/empty.txt"}`,
		`{"operation":"cp","source":"/a","destination":"/b"}`,
		`{"operation":"mv","source":"/b","destination":"/c"}`,
		`{"operation":"rm","path":"/c"}`,
	}, "\n"))
	var output bytes.Buffer
	err := RunBatchWithOptions(
		context.Background(), BatchRequest{
			Input: input, DryRun: true, MaxOperations: 10,
		}, "", RunOptions{Out: &output, OutputMode: appoutput.JSON},
	)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 0 || !strings.Contains(output.String(), `"planned": 5`) ||
		!strings.Contains(output.String(), `"operation": "copy"`) ||
		!strings.Contains(output.String(), `"operation": "remove"`) ||
		!strings.Contains(output.String(), `"parents": true`) {
		t.Fatalf("requests=%d output=%s", requests, output.String())
	}
}

func TestBatchRejectsCompleteInvalidDocumentBeforeMutation(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		requests++
		writer.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	err := RunBatchWithOptions(
		context.Background(), BatchRequest{
			Input: strings.NewReader(
				"{\"operation\":\"mkdir\",\"path\":\"/would-mutate\"}\n" +
					"{\"operation\":\"remove\",\"path\":\"/x\",\"overwrite\":true}\n",
			),
			Confirmed: true, MaxOperations: 10,
		}, "", RunOptions{Out: io.Discard},
	)
	if !apperror.IsKind(err, apperror.KindUsage) || requests != 0 ||
		!strings.Contains(err.Error(), "line 2") {
		t.Fatalf("error=%v requests=%d", err, requests)
	}
}

func TestBatchStopsAndReportsSkippedOperations(t *testing.T) {
	var deletes int
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.Method {
		case "MKCOL":
			writer.WriteHeader(http.StatusCreated)
		case "PROPFIND":
			writer.WriteHeader(http.StatusNotFound)
		case "COPY":
			writer.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(writer, "missing")
		case http.MethodDelete:
			deletes++
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	input := strings.Join([]string{
		`{"operation":"mkdir","path":"/one"}`,
		`{"operation":"copy","source":"/missing","destination":"/copy"}`,
		`{"operation":"remove","path":"/one"}`,
	}, "\n")
	var output bytes.Buffer
	err := RunBatchWithOptions(
		context.Background(), BatchRequest{
			Input: strings.NewReader(input), Confirmed: true, MaxOperations: 10,
		}, "", RunOptions{Out: &output, OutputMode: appoutput.JSON},
	)
	if apperror.ExitCode(err) != 4 || deletes != 0 ||
		!strings.Contains(output.String(), `"succeeded": 1`) ||
		!strings.Contains(output.String(), `"failed": 1`) ||
		!strings.Contains(output.String(), `"skipped": 1`) ||
		!strings.Contains(output.String(), `"stopped": true`) {
		t.Fatalf("error=%v deletes=%d output=%s", err, deletes, output.String())
	}
}

func TestBatchContinueOnErrorExecutesRemainingOperations(t *testing.T) {
	var deletes int
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.Method {
		case "COPY":
			writer.WriteHeader(http.StatusNotFound)
		case "PROPFIND":
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(writer, appDAVFile)
		case http.MethodDelete:
			deletes++
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	var output bytes.Buffer
	err := RunBatchWithOptions(
		context.Background(), BatchRequest{
			Input: strings.NewReader(
				"{\"operation\":\"copy\",\"source\":\"/missing\",\"destination\":\"/copy\"}\n" +
					"{\"operation\":\"remove\",\"path\":\"/old\"}\n",
			),
			Confirmed: true, ContinueOnError: true, MaxOperations: 10,
		}, "", RunOptions{Out: &output, OutputMode: appoutput.JSONL},
	)
	if apperror.ExitCode(err) != 4 || deletes != 1 ||
		strings.Count(strings.TrimSpace(output.String()), "\n") != 1 ||
		!strings.Contains(output.String(), `"status":"failed"`) ||
		!strings.Contains(output.String(), `"status":"succeeded"`) {
		t.Fatalf("error=%v deletes=%d output=%s", err, deletes, output.String())
	}
}

func TestBatchClassifiesPermissionDenial(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		if request.Method != "MKCOL" {
			t.Fatalf("unexpected method: %s", request.Method)
		}
		writer.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	var output bytes.Buffer
	err := RunBatchWithOptions(
		context.Background(), BatchRequest{
			Input: strings.NewReader(
				`{"operation":"mkdir","path":"/forbidden"}`,
			),
			Confirmed: true, MaxOperations: 10,
		}, "", RunOptions{Out: &output, OutputMode: appoutput.JSON},
	)
	if apperror.ExitCode(err) != 3 ||
		!strings.Contains(output.String(), `"kind": "authentication"`) {
		t.Fatalf("error=%v output=%s", err, output.String())
	}
}

func TestBatchCancellationStopsEvenWithContinueOnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, _ *http.Request,
	) {
		writer.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	err := RunBatchWithOptions(
		ctx, BatchRequest{
			Input: strings.NewReader(
				"{\"operation\":\"mkdir\",\"path\":\"/one\"}\n" +
					"{\"operation\":\"mkdir\",\"path\":\"/two\"}\n",
			),
			Confirmed: true, ContinueOnError: true, MaxOperations: 10,
		}, "", RunOptions{Out: &output, OutputMode: appoutput.JSON},
	)
	if apperror.ExitCode(err) != 130 ||
		!strings.Contains(output.String(), `"skipped": 1`) ||
		!strings.Contains(output.String(), `"stopped": true`) {
		t.Fatalf("error=%v output=%s", err, output.String())
	}
}

func TestBatchExecutesEverySupportedOperation(t *testing.T) {
	methods := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		methods[request.Method]++
		switch {
		case request.Method == http.MethodGet &&
			strings.Contains(request.URL.Path, "/ocs/"):
			writer.WriteHeader(http.StatusNotFound)
		case request.Method == "PROPFIND":
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(writer, appDAVFile)
		case request.Method == http.MethodGet:
			writer.Header().Set("Content-Length", "5")
			_, _ = io.WriteString(writer, "hello")
		case request.Method == http.MethodPut:
			body, err := io.ReadAll(request.Body)
			if err != nil || string(body) != "hello" {
				t.Errorf("upload body=%q err=%v", body, err)
			}
			writer.WriteHeader(http.StatusCreated)
		case request.Method == "MKCOL", request.Method == "COPY",
			request.Method == "MOVE":
			writer.WriteHeader(http.StatusCreated)
		case request.Method == http.MethodDelete:
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	local := t.TempDir()
	upload := filepath.Join(local, "upload.txt")
	download := filepath.Join(local, "download.txt")
	if err := os.WriteFile(upload, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	input := strings.NewReader(strings.Join([]string{
		`{"operation":"mkdir","path":"/batch"}`,
		`{"operation":"touch","path":"/batch/existing.txt"}`,
		`{"operation":"upload","source":` + fmt.Sprintf("%q", upload) + `,"destination":"/batch/upload.txt","verify":false}`,
		`{"operation":"copy","source":"/batch/upload.txt","destination":"/batch/copy.txt"}`,
		`{"operation":"move","source":"/batch/copy.txt","destination":"/batch/moved.txt"}`,
		`{"operation":"download","source":"/batch/moved.txt","destination":` + fmt.Sprintf("%q", download) + `,"verify":false}`,
		`{"operation":"remove","path":"/batch/moved.txt"}`,
	}, "\n"))
	var output bytes.Buffer
	if err := RunBatchWithOptions(
		context.Background(), BatchRequest{
			Input: input, Confirmed: true, MaxOperations: 10,
		}, "", RunOptions{Out: &output, Quiet: true},
	); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(download)
	if err != nil || string(data) != "hello" {
		t.Fatalf("download=%q err=%v", data, err)
	}
	for _, method := range []string{
		"MKCOL", http.MethodPut, "COPY", "MOVE", http.MethodGet,
		"PROPFIND", http.MethodDelete,
	} {
		if methods[method] == 0 {
			t.Errorf("method %s was not used: %#v", method, methods)
		}
	}
	if !strings.Contains(output.String(), "7 succeeded") ||
		!strings.Contains(output.String(), "INDEX") {
		t.Fatalf("human output: %s", output.String())
	}
}

func TestBatchReportsResolvedDirectoryDestination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter, request *http.Request,
	) {
		switch request.Method {
		case "PROPFIND":
			writer.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(
				writer, davDirectoryXML("/remote.php/dav/files/alice/Documents/", "Documents"),
			)
		case "COPY":
			writer.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected method: %s", request.Method)
		}
	}))
	defer server.Close()
	configureFilesystemTestProfile(t, server.URL)
	var output bytes.Buffer
	if err := RunBatchWithOptions(
		context.Background(), BatchRequest{
			Input: strings.NewReader(
				`{"operation":"copy","source":"/report.txt","destination":"/Documents"}`,
			),
			Confirmed: true, MaxOperations: 10,
		}, "", RunOptions{Out: &output, OutputMode: appoutput.JSON},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(
		output.String(), `"destination": "/Documents/report.txt"`,
	) {
		t.Fatalf("output: %s", output.String())
	}
}

func TestBatchInputGuards(t *testing.T) {
	confirmed := BatchRequest{
		Input:         strings.NewReader(`{"operation":"mkdir","path":"/x"}`),
		MaxOperations: 1,
	}
	if err := RunBatchWithOptions(
		context.Background(), confirmed, "", RunOptions{Out: io.Discard},
	); !apperror.IsKind(err, apperror.KindUsage) ||
		!strings.Contains(err.Error(), "requires --yes") {
		t.Fatalf("confirmation error: %v", err)
	}

}
