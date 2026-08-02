package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

const maxBatchLineBytes = 1024 * 1024

type parsedBatchOperation struct {
	BatchOperation
	line int
}

type batchRunError struct {
	failed int
	total  int
	first  error
}

func (err *batchRunError) Error() string {
	return fmt.Sprintf(
		"%d of %d operations failed; first failure: %v",
		err.failed, err.total, err.first,
	)
}

func (err *batchRunError) Unwrap() error {
	return err.first
}

func runBatch(
	ctx context.Context, request BatchRequest, selected string,
	options RunOptions,
) error {
	if request.MaxOperations < 1 {
		return apperror.Wrap(
			apperror.KindUsage, "batch",
			errors.New("--max-operations must be at least 1"),
		)
	}
	if !request.DryRun && !request.Confirmed {
		return apperror.Wrap(
			apperror.KindUsage, "batch",
			errors.New("execution requires --yes; use --dry-run to preview"),
		)
	}
	input := request.Input
	if input == nil {
		input = options.In
	}
	operations, err := parseBatchOperations(input, request.MaxOperations)
	if err != nil {
		return err
	}
	client, err := newClientWithOptions(ctx, selected, options)
	if err != nil {
		return err
	}
	if err := client.selectSpace(options.Space); err != nil {
		return err
	}

	summary := BatchSummary{
		Total: len(operations), DryRun: request.DryRun,
		Results: make([]BatchResult, 0, len(operations)),
	}
	var firstFailure error
	stopped := false
	for index, operation := range operations {
		result := newBatchResult(index, operation)
		if stopped {
			result.Status = "skipped"
			summary.Skipped++
			summary.Results = append(summary.Results, result)
			continue
		}
		if request.DryRun {
			result.Status = "planned"
			summary.Planned++
			summary.Results = append(summary.Results, result)
			continue
		}
		filesystemRequest := batchFilesystemRequest(operation.BatchOperation)
		innerOptions := options
		innerOptions.Out = io.Discard
		innerOptions.OutputMode = appoutput.Human
		innerOptions.Quiet = true
		var runErr error
		if operation.Operation == "copy" || operation.Operation == "move" {
			filesystemRequest.Destination, runErr = resolveCopyMoveDestination(
				client, filesystemRequest.Source, filesystemRequest.Destination,
			)
			if runErr == nil {
				result.Destination = filesystemRequest.Destination
				runErr = copyOrMoveFilesystemResolved(
					client, filesystemRequest, innerOptions,
				)
			}
		} else {
			runErr = runFilesystemWithClient(
				ctx, client, filesystemRequest, innerOptions,
			)
		}
		if runErr == nil {
			result.Status = "succeeded"
			summary.Succeeded++
			summary.Results = append(summary.Results, result)
			continue
		}
		classified := classifyProtocolError(operation.Operation, runErr)
		kind, _ := apperror.Details(classified)
		result.Status = "failed"
		result.Error = &BatchOperationError{
			Code: apperror.ExitCode(classified), Kind: string(kind),
			Message: classified.Error(),
		}
		summary.Failed++
		summary.Results = append(summary.Results, result)
		if firstFailure == nil {
			firstFailure = classified
		}
		if !request.ContinueOnError ||
			apperror.IsKind(classified, apperror.KindCanceled) {
			stopped = true
			summary.Stopped = true
		}
	}
	if err := writeBatchSummary(options, summary); err != nil {
		return err
	}
	if firstFailure != nil {
		return &batchRunError{
			failed: summary.Failed, total: summary.Total, first: firstFailure,
		}
	}
	return nil
}

func parseBatchOperations(
	input io.Reader, maxOperations int,
) ([]parsedBatchOperation, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), maxBatchLineBytes)
	operations := make([]parsedBatchOperation, 0)
	for line := 1; scanner.Scan(); line++ {
		data := strings.TrimSpace(scanner.Text())
		if data == "" {
			continue
		}
		if len(operations) >= maxOperations {
			return nil, apperror.Wrap(
				apperror.KindUsage, "batch",
				fmt.Errorf(
					"input exceeds --max-operations %d at line %d",
					maxOperations, line,
				),
			)
		}
		var operation BatchOperation
		decoder := json.NewDecoder(strings.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&operation); err != nil {
			return nil, batchLineError(line, err)
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			if err == nil {
				err = errors.New("multiple JSON values are not allowed")
			}
			return nil, batchLineError(line, err)
		}
		if err := validateBatchOperation(&operation); err != nil {
			return nil, batchLineError(line, err)
		}
		operations = append(operations, parsedBatchOperation{
			BatchOperation: operation, line: line,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, apperror.Wrap(
			apperror.KindUsage, "batch",
			fmt.Errorf("read JSONL input: %w", err),
		)
	}
	if len(operations) == 0 {
		return nil, apperror.Wrap(
			apperror.KindUsage, "batch",
			errors.New("JSONL input contains no operations"),
		)
	}
	return operations, nil
}

func batchLineError(line int, err error) error {
	return apperror.Wrap(
		apperror.KindUsage, "batch",
		fmt.Errorf("line %d: %w", line, err),
	)
}

func validateBatchOperation(operation *BatchOperation) error {
	operation.Operation = normalizeBatchOperation(operation.Operation)
	switch operation.Operation {
	case "mkdir":
		if strings.TrimSpace(operation.Path) == "" {
			return errors.New("mkdir requires path")
		}
		if operation.Source != "" || operation.Destination != "" ||
			operation.Recursive || operation.Overwrite || operation.NoClobber ||
			operation.Verify != nil {
			return errors.New("mkdir accepts only operation, path, and parents")
		}
	case "touch":
		if strings.TrimSpace(operation.Path) == "" {
			return errors.New("touch requires path")
		}
		if operation.Source != "" || operation.Destination != "" ||
			operation.Recursive || operation.Overwrite || operation.NoClobber ||
			operation.Parents || operation.Verify != nil {
			return errors.New("touch accepts only operation and path")
		}
	case "remove":
		if strings.TrimSpace(operation.Path) == "" {
			return errors.New("remove requires path")
		}
		if operation.Source != "" || operation.Destination != "" ||
			operation.Overwrite || operation.NoClobber || operation.Parents ||
			operation.Verify != nil {
			return errors.New(
				"remove accepts only operation, path, and recursive",
			)
		}
	case "copy", "move":
		if strings.TrimSpace(operation.Source) == "" ||
			strings.TrimSpace(operation.Destination) == "" {
			return fmt.Errorf(
				"%s requires source and destination", operation.Operation,
			)
		}
		if operation.Path != "" || operation.Recursive || operation.NoClobber ||
			operation.Parents || operation.Verify != nil {
			return fmt.Errorf(
				"%s accepts only operation, source, destination, and overwrite",
				operation.Operation,
			)
		}
	case "upload", "download":
		if strings.TrimSpace(operation.Source) == "" ||
			strings.TrimSpace(operation.Destination) == "" {
			return fmt.Errorf(
				"%s requires source and destination", operation.Operation,
			)
		}
		if operation.Path != "" || operation.Parents {
			return fmt.Errorf(
				"%s does not accept path or parents", operation.Operation,
			)
		}
		if operation.NoClobber && operation.Overwrite {
			return fmt.Errorf(
				"%s noClobber and overwrite are mutually exclusive",
				operation.Operation,
			)
		}
		if operation.Operation == "upload" && operation.Source == "-" {
			return errors.New("batch upload cannot read content from stdin")
		}
		if operation.Operation == "download" && operation.Destination == "-" {
			return errors.New("batch download cannot write content to stdout")
		}
	default:
		return fmt.Errorf(
			"unsupported operation %q; use mkdir, touch, upload, download, copy, move, or remove",
			operation.Operation,
		)
	}
	return nil
}

func normalizeBatchOperation(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "cp", "copy":
		return "copy"
	case "mv", "move":
		return "move"
	case "rm", "remove", "delete":
		return "remove"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func batchFilesystemRequest(operation BatchOperation) FilesystemRequest {
	verify := true
	if operation.Verify != nil {
		verify = *operation.Verify
	}
	request := FilesystemRequest{
		Recursive: operation.Recursive, Overwrite: operation.Overwrite,
		NoClobber: operation.NoClobber, Verify: verify, Parents: operation.Parents,
	}
	switch operation.Operation {
	case "mkdir":
		request.Operation = FilesystemMkdir
		request.Source = operation.Path
	case "touch":
		request.Operation = FilesystemTouch
		request.Source = operation.Path
	case "remove":
		request.Operation = FilesystemRemove
		request.Source = operation.Path
	case "copy":
		request.Operation = FilesystemCopy
		request.Source = operation.Source
		request.Destination = operation.Destination
	case "move":
		request.Operation = FilesystemMove
		request.Source = operation.Source
		request.Destination = operation.Destination
	case "upload":
		request.Operation = FilesystemUpload
		request.Source = operation.Source
		request.Destination = operation.Destination
	case "download":
		request.Operation = FilesystemDownload
		request.Source = operation.Source
		request.Destination = operation.Destination
	}
	return request
}

func newBatchResult(index int, operation parsedBatchOperation) BatchResult {
	result := BatchResult{
		Index: index + 1, Line: operation.line,
		Operation: operation.Operation,
	}
	switch operation.Operation {
	case "mkdir":
		result.Path = cleanRemote(operation.Path)
		result.Parents = operation.Parents
	case "touch", "remove":
		result.Path = cleanRemote(operation.Path)
	case "copy", "move":
		result.Source = cleanRemote(operation.Source)
		result.Destination = cleanRemote(operation.Destination)
	case "upload":
		result.Source = operation.Source
		result.Destination = cleanRemote(operation.Destination)
	case "download":
		result.Source = cleanRemote(operation.Source)
		result.Destination = operation.Destination
	}
	return result
}

func writeBatchSummary(options RunOptions, summary BatchSummary) error {
	if options.OutputMode == appoutput.JSONL {
		return writeOutput(options, "batch-result", summary.Results)
	}
	if options.OutputMode == appoutput.JSON {
		return writeOutput(options, "batch", summary)
	}
	writer := tabwriter.NewWriter(options.Out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(
		writer, "INDEX\tLINE\tOPERATION\tSTATUS\tTARGET",
	); err != nil {
		return err
	}
	for _, result := range summary.Results {
		target := result.Path
		if target == "" {
			target = result.Source + " -> " + result.Destination
		}
		if _, err := fmt.Fprintf(
			writer, "%d\t%d\t%s\t%s\t%s\n",
			result.Index, result.Line, result.Operation, result.Status, target,
		); err != nil {
			return err
		}
		if result.Error != nil {
			if _, err := fmt.Fprintf(
				writer, "\t\t\terror\t%s\n", result.Error.Message,
			); err != nil {
				return err
			}
		}
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(
		options.Out,
		"Summary: %d succeeded, %d failed, %d planned, %d skipped\n",
		summary.Succeeded, summary.Failed, summary.Planned, summary.Skipped,
	)
	return err
}
