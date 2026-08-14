package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	archiveapp "github.com/mzner/ocis-cli/internal/app/archive"
	"github.com/mzner/ocis-cli/internal/apperror"
	"github.com/mzner/ocis-cli/internal/credentials"
	"github.com/mzner/ocis-cli/internal/sharing"
	"github.com/mzner/ocis-cli/internal/webdav"
)

// DoctorCheck is one diagnostic check in a doctor result.
type DoctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// DoctorResult describes the validated profile and advertised DAV features.
type DoctorResult struct {
	Profile      string               `json:"profile"`
	Server       string               `json:"server"`
	Checks       []DoctorCheck        `json:"checks"`
	Capabilities webdav.Capabilities  `json:"capabilities"`
	Features     sharing.Capabilities `json:"features"`
}

// RunDoctorWithOptions validates configuration, credential storage,
// authentication, and the remote DAV endpoint.
func RunDoctorWithOptions(
	ctx context.Context, selectedProfile string, options RunOptions,
) error {
	options = options.normalized()
	persisted, err := options.Dependencies.Config.Load()
	if err != nil {
		return fmt.Errorf("check configuration: %w", err)
	}
	name, selected, err := selectProfile(persisted, selectedProfile)
	if err != nil {
		return err
	}
	checks := []DoctorCheck{{
		Name: "configuration", Status: "ok",
		Detail: fmt.Sprintf("schema %d", persisted.Version),
	}}
	if _, err := options.Dependencies.Credentials.Get(name); err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return apperror.Wrap(
				apperror.KindAuthentication, "check credential service",
				fmt.Errorf("profile %q has no stored credentials; run ocis login %s", name, name),
			)
		}
		return fmt.Errorf("check credential service: %w", err)
	}
	checks = append(checks, DoctorCheck{Name: "credential service", Status: "ok"})
	client, err := newClientWithOptions(ctx, name, options)
	if err != nil {
		return classifyProtocolError("check authentication", err)
	}
	capabilities, err := client.capabilities()
	if err != nil {
		return classifyProtocolError("check DAV capabilities", err)
	}
	checks = append(checks, DoctorCheck{
		Name: "DAV capabilities", Status: "ok",
		Detail: strings.Join(capabilities.DAV, ", "),
	})
	features, err := client.sharingClient().Capabilities(ctx)
	if err != nil {
		return classifyProtocolError("check server capabilities", err)
	}
	spaceStatus := "unsupported"
	if features.Spaces.Enabled {
		spaceStatus = "ok"
	}
	checks = append(checks, DoctorCheck{
		Name: "spaces capability", Status: spaceStatus,
		Detail: spaceCapabilityDetail(features),
	})
	linkStatus := "unsupported"
	if features.Sharing.APIEnabled && features.Sharing.Public.Enabled {
		linkStatus = "ok"
	}
	checks = append(checks, DoctorCheck{
		Name: "public links", Status: linkStatus,
		Detail: publicLinkCapabilityDetail(features),
	})
	tusStatus := "unsupported"
	if features.Files.TUS.Version == "1.0.0" &&
		features.Files.TUS.Resumable == "1.0.0" {
		tusStatus = "ok"
	}
	checks = append(checks, DoctorCheck{
		Name: "resumable uploads", Status: tusStatus,
		Detail: resumableUploadCapabilityDetail(features),
	})
	archiveStatus := "unsupported"
	if _, err := archiveapp.SelectCapabilities(features.Files.Archivers); err == nil {
		archiveStatus = "ok"
	}
	checks = append(checks, DoctorCheck{
		Name: "archive downloads", Status: archiveStatus,
		Detail: archiveapp.CapabilityDetail(features),
	})
	eventStatus := "unsupported"
	if features.Core.SupportSSE {
		eventStatus = "ok"
	}
	checks = append(checks, DoctorCheck{
		Name: "real-time events", Status: eventStatus,
		Detail: "core.support-sse",
	})
	if _, err := client.stat("/"); err != nil {
		return classifyProtocolError("check DAV authentication", err)
	}
	checks = append(checks, DoctorCheck{Name: "authentication", Status: "ok"})
	result := DoctorResult{
		Profile: name, Server: selected.Server,
		Checks: checks, Capabilities: capabilities, Features: features,
	}
	var human strings.Builder
	_, _ = fmt.Fprintf(&human, "%s (%s)\n", name, selected.Server)
	for _, check := range checks {
		_, _ = fmt.Fprintf(&human, "  [%s] %s", check.Status, check.Name)
		if check.Detail != "" {
			_, _ = fmt.Fprintf(&human, ": %s", check.Detail)
		}
		_ = human.WriteByte('\n')
	}
	return output(options, "diagnostic", result, "%s", human.String())
}

func capabilityVersion(version string) string {
	if version == "" {
		return "not advertised"
	}
	return "version " + version
}

func spaceCapabilityDetail(capabilities sharing.Capabilities) string {
	detail := capabilityVersion(capabilities.Spaces.Version)
	if capabilities.Spaces.Enabled {
		return detail + "; permissions checked per operation"
	}
	return detail
}

func publicLinkCapabilityDetail(capabilities sharing.Capabilities) string {
	if !capabilities.Sharing.APIEnabled ||
		!capabilities.Sharing.Public.Enabled {
		return "disabled"
	}
	values := []string{"enabled"}
	if capabilities.Sharing.Public.Password.Enforced {
		values = append(values, "password required")
	}
	if capabilities.Sharing.Public.ExpireDate.Enabled {
		values = append(values, "expiration supported")
	}
	return strings.Join(values, ", ")
}

func resumableUploadCapabilityDetail(
	capabilities sharing.Capabilities,
) string {
	tus := capabilities.Files.TUS
	if tus.Version == "" || tus.Resumable == "" {
		return "not advertised; WebDAV PUT fallback"
	}
	return fmt.Sprintf(
		"TUS %s; chunk size %d bytes; extensions %s",
		tus.Version, tus.MaxChunkSize, strings.Join(tus.Extensions, ", "),
	)
}
