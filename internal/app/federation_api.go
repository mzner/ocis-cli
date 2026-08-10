package app

import "context"

// FederationOperation identifies an OCM connection-management use case.
type FederationOperation string

const (
	FederationInviteCreate     FederationOperation = "invite-create"
	FederationInviteList       FederationOperation = "invite-list"
	FederationInviteAccept     FederationOperation = "invite-accept"
	FederationConnectionList   FederationOperation = "connection-list"
	FederationConnectionRemove FederationOperation = "connection-remove"
)

// FederationRequest describes one OCM invitation or connection operation.
type FederationRequest struct {
	Operation   FederationOperation
	Token       string
	Provider    string
	Email       string
	Description string
	Identifier  string
	UserID      bool
	Confirmed   bool
	DryRun      bool
}

// RunFederationWithOptions manages OCM invitations and connections.
func RunFederationWithOptions(
	ctx context.Context,
	request FederationRequest,
	selectedProfile string,
	options RunOptions,
) error {
	return classifyProtocolError(
		"federation "+string(request.Operation),
		runFederation(
			ctx, request, selectedProfile, options.normalized(),
		),
	)
}
