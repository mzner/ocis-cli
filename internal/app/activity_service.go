package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mzner/ocis-cli/internal/activities"
	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func runActivity(
	ctx context.Context,
	request ActivityRequest,
	selectedProfile string,
	options RunOptions,
) error {
	if err := validateActivityRequest(request); err != nil {
		return apperror.Wrap(apperror.KindUsage, "activity list", err)
	}
	client, err := newClientWithOptions(ctx, selectedProfile, options)
	if err != nil {
		return err
	}

	remote := strings.TrimSpace(request.Path)
	itemID := ""
	if remote != "" || options.Space != "" {
		if err := client.selectSpace(options.Space); err != nil {
			return err
		}
		if remote == "" {
			remote = "/"
		}
		metadata, err := client.stat(remote)
		if err != nil {
			return err
		}
		if metadata.ResourceID == "" {
			return errors.New(
				"server did not return a stable resource ID; scoped activity history is unsupported",
			)
		}
		itemID = metadata.ResourceID
	}

	var depth *int
	if request.DepthSet {
		depth = &request.Depth
	}
	activityRequest := activities.ListRequest{
		ItemID: itemID, Depth: depth, Limit: request.Limit, Sort: request.Sort,
	}
	var values []activities.Activity
	if itemID == "" {
		values, err = listAccountActivities(
			ctx, client, activityRequest, options,
		)
	} else {
		values, err = client.activitiesClient().List(ctx, activityRequest)
	}
	if err != nil {
		return activityListError(remote, err)
	}
	return writeActivities(values, options)
}

func listAccountActivities(
	ctx context.Context,
	client *client,
	request activities.ListRequest,
	options RunOptions,
) ([]activities.Activity, error) {
	drives, err := client.graphClient().ListMyDrives(ctx)
	if err != nil {
		return nil, err
	}
	values := make([]activities.Activity, 0)
	seen := make(map[string]struct{})
	permissionFailures := 0
	successfulDrives := 0
	var permissionErr error
	for _, drive := range drives {
		driveType := strings.ToLower(strings.TrimSpace(drive.DriveType))
		if drive.ID == "" || drive.Root.Deleted != nil ||
			(driveType != "personal" && driveType != "project") {
			continue
		}
		request.ItemID = drive.ID
		driveActivities, err := client.activitiesClient().List(ctx, request)
		if protocolStatus(err) == http.StatusForbidden {
			permissionFailures++
			permissionErr = err
			continue
		}
		if err != nil {
			return nil, fmt.Errorf(
				"list activity history for Space %q: %w", drive.Name, err,
			)
		}
		successfulDrives++
		for _, value := range driveActivities {
			if value.ID != "" {
				if _, exists := seen[value.ID]; exists {
					continue
				}
				seen[value.ID] = struct{}{}
			}
			values = append(values, value)
		}
	}
	if successfulDrives == 0 && permissionFailures > 0 {
		return nil, permissionErr
	}
	if permissionFailures > 0 {
		_, _ = fmt.Fprintf(
			options.Err,
			"Warning: activity history is unavailable for %d Space(s) due to server permissions\n",
			permissionFailures,
		)
	}
	sortActivities(values, request.Sort)
	if request.Limit > 0 && len(values) > request.Limit {
		values = values[:request.Limit]
	}
	return values, nil
}

func sortActivities(values []activities.Activity, order string) {
	descending := !strings.EqualFold(strings.TrimSpace(order), "asc")
	sort.SliceStable(values, func(left, right int) bool {
		leftTime, leftErr := time.Parse(
			time.RFC3339Nano, values[left].Times.RecordedTime,
		)
		rightTime, rightErr := time.Parse(
			time.RFC3339Nano, values[right].Times.RecordedTime,
		)
		if leftErr == nil && rightErr == nil && !leftTime.Equal(rightTime) {
			if descending {
				return leftTime.After(rightTime)
			}
			return leftTime.Before(rightTime)
		}
		if values[left].Times.RecordedTime != values[right].Times.RecordedTime {
			if descending {
				return values[left].Times.RecordedTime > values[right].Times.RecordedTime
			}
			return values[left].Times.RecordedTime < values[right].Times.RecordedTime
		}
		return values[left].ID < values[right].ID
	})
}

func validateActivityRequest(request ActivityRequest) error {
	if request.DepthSet && request.Depth < -1 {
		return errors.New("--depth must be -1 or greater")
	}
	if request.Limit != -1 &&
		(request.Limit < 1 || request.Limit > 1000) {
		return errors.New("--limit must be -1 or between 1 and 1000")
	}
	switch strings.ToLower(strings.TrimSpace(request.Sort)) {
	case "asc", "desc":
		return nil
	default:
		return errors.New("--sort must be asc or desc")
	}
}

func activityListError(remote string, err error) error {
	scope := "account-wide activity history"
	if remote != "" {
		scope = fmt.Sprintf("activity history for %s", cleanRemote(remote))
	}
	switch protocolStatus(err) {
	case http.StatusForbidden:
		return fmt.Errorf(
			"the current user is not allowed to list %s: %w", scope, err,
		)
	case http.StatusNotFound, http.StatusMethodNotAllowed,
		http.StatusNotImplemented:
		return fmt.Errorf(
			"server does not expose the oCIS activity-history service: %w", err,
		)
	default:
		return err
	}
}

func writeActivities(values []activities.Activity, options RunOptions) error {
	if options.OutputMode != appoutput.Human {
		return writeOutput(options, "activity", values)
	}
	if len(values) == 0 {
		_, err := fmt.Fprintln(options.Out, "No activities found")
		return err
	}
	writer := tabwriter.NewWriter(options.Out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "DATE\tMESSAGE\tID"); err != nil {
		return err
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(
			writer, "%s\t%s\t%s\n", value.Times.RecordedTime,
			activityLine(renderActivityMessage(value)), value.ID,
		); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func renderActivityMessage(value activities.Activity) string {
	message := value.Template.Message
	for name, variable := range value.Template.Variables {
		replacement := activityVariableText(variable)
		if replacement == "" {
			continue
		}
		message = strings.ReplaceAll(message, "{"+name+"}", replacement)
	}
	return message
}

func activityVariableText(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case map[string]any:
		for _, key := range []string{"displayName", "name", "value", "id"} {
			if text, ok := value[key].(string); ok && strings.TrimSpace(text) != "" {
				return text
			}
		}
	}
	return ""
}

func activityLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
