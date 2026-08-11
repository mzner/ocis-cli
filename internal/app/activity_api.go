package app

import "context"

// ActivityRequest describes one activity-history query.
type ActivityRequest struct {
	Path     string
	Depth    int
	DepthSet bool
	Limit    int
	Sort     string
}

// RunActivityWithOptions lists activity history visible to the authenticated
// user.
func RunActivityWithOptions(
	ctx context.Context,
	request ActivityRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"activity list",
		runActivity(ctx, request, selectedProfile, options.normalized()),
	)
}
