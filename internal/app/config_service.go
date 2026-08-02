package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/mzner/ocis-cli/internal/apperror"
	appconfig "github.com/mzner/ocis-cli/internal/config"
	"github.com/mzner/ocis-cli/internal/credentials"
	appoutput "github.com/mzner/ocis-cli/internal/output"
	"github.com/mzner/ocis-cli/internal/syncjob"
	"github.com/mzner/ocis-cli/internal/syncrecovery"
	"github.com/mzner/ocis-cli/internal/syncstate"
)

type configPathView struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Source string `json:"source"`
}

type configPathsView struct {
	Paths             []configPathView `json:"paths"`
	CredentialBackend string           `json:"credentialBackend"`
}

type configView struct {
	Version  int                 `json:"version"`
	Current  string              `json:"current,omitempty"`
	Profiles []configProfileView `json:"profiles"`
}

type configProfileView struct {
	Name              string `json:"name"`
	Current           bool   `json:"current"`
	Server            string `json:"server"`
	Username          string `json:"username,omitempty"`
	Subject           string `json:"subject,omitempty"`
	AuthType          string `json:"authType,omitempty"`
	Insecure          bool   `json:"insecure"`
	ClientID          string `json:"clientId,omitempty"`
	Issuer            string `json:"issuer,omitempty"`
	TokenURL          string `json:"tokenUrl,omitempty"`
	UserInfoURL       string `json:"userInfoUrl,omitempty"`
	ExpiresAt         int64  `json:"expiresAt,omitempty"`
	DefaultSpace      string `json:"defaultSpace,omitempty"`
	DefaultSpaceOwner string `json:"defaultSpaceOwner,omitempty"`
}

func runConfig(
	_ context.Context,
	request ConfigRequest,
	options RunOptions,
) error {
	options.Logger.Debug("run config operation", "operation", request.Operation)
	switch request.Operation {
	case ConfigPath:
		pathInfo, err := resolveConfigPath()
		if err != nil {
			return err
		}
		return output(
			options, "config-path", pathInfo,
			"%s\n", pathInfo.Path,
		)
	case ConfigPaths:
		paths, err := resolveConfigPaths()
		if err != nil {
			return err
		}
		if options.OutputMode != appoutput.Human {
			return writeOutput(options, "config-paths", paths)
		}
		return writeConfigPaths(options, paths)
	case ConfigShow:
		view, err := loadConfigView(request.Profile, options)
		if err != nil {
			return err
		}
		if options.OutputMode != appoutput.Human {
			return writeOutput(options, "config", view)
		}
		return writeConfigView(options, view)
	default:
		return apperror.Wrap(
			apperror.KindUsage, "config",
			fmt.Errorf("unknown config command %q", request.Operation),
		)
	}
}

func resolveConfigPath() (configPathView, error) {
	configPath, err := appconfig.Path()
	if err != nil {
		return configPathView{}, err
	}
	return configPathView{
		Name: "configuration", Path: configPath,
		Source: environmentSource("OCIS_CONFIG"),
	}, nil
}

func resolveConfigPaths() (configPathsView, error) {
	configPath, err := resolveConfigPath()
	if err != nil {
		return configPathsView{}, err
	}
	jobsPath, err := syncjob.Path()
	if err != nil {
		return configPathsView{}, err
	}
	jobsSource := environmentSource("OCIS_SYNC_JOBS")
	if jobsSource == "default" && configPath.Source == "OCIS_CONFIG" {
		jobsSource = "OCIS_CONFIG"
	}
	stateDirectory, err := syncstate.Directory()
	if err != nil {
		return configPathsView{}, err
	}
	recoveryDirectory, err := syncrecovery.Directory()
	if err != nil {
		return configPathsView{}, err
	}
	return configPathsView{
		Paths: []configPathView{
			configPath,
			{
				Name: "sync jobs", Path: jobsPath,
				Source: jobsSource,
			},
			{
				Name: "sync state", Path: stateDirectory,
				Source: environmentSource("OCIS_STATE_DIR"),
			},
			{
				Name: "sync recovery", Path: recoveryDirectory,
				Source: environmentSource("OCIS_SYNC_RECOVERY_DIR"),
			},
		},
		CredentialBackend: credentials.BackendName(),
	}, nil
}

func environmentSource(name string) string {
	if os.Getenv(name) != "" {
		return name
	}
	return "default"
}

func loadConfigView(
	selectedProfile string,
	options RunOptions,
) (configView, error) {
	store, err := options.Dependencies.Config.Load()
	if err != nil {
		return configView{}, err
	}
	names := make([]string, 0, len(store.Profiles))
	if selectedProfile != "" {
		if _, ok := store.Profiles[selectedProfile]; !ok {
			return configView{}, apperror.Wrap(
				apperror.KindUsage, "config show",
				fmt.Errorf("unknown server profile %q", selectedProfile),
			)
		}
		names = append(names, selectedProfile)
	} else {
		for name := range store.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	profiles := make([]configProfileView, 0, len(names))
	for _, name := range names {
		selected := store.Profiles[name]
		profiles = append(profiles, configProfileView{
			Name: name, Current: name == store.Current,
			Server: selected.Server, Username: selected.Username,
			Subject: selected.Subject, AuthType: selected.AuthType,
			Insecure: selected.Insecure, ClientID: selected.ClientID,
			Issuer: selected.Issuer, TokenURL: selected.TokenURL,
			UserInfoURL: selected.UserInfoURL, ExpiresAt: selected.ExpiresAt,
			DefaultSpace:      selected.DefaultSpace,
			DefaultSpaceOwner: selected.DefaultSpaceOwner,
		})
	}
	return configView{
		Version: store.Version, Current: store.Current, Profiles: profiles,
	}, nil
}

func writeConfigPaths(options RunOptions, view configPathsView) error {
	table := tabwriter.NewWriter(options.Out, 0, 4, 2, ' ', 0)
	for _, item := range view.Paths {
		source := "default"
		if item.Source != "default" {
			source = "from " + item.Source
		}
		if _, err := fmt.Fprintf(
			table, "%s\t%s\t(%s)\n",
			displayPathName(item.Name), item.Path, source,
		); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(
		table, "Credentials\t%s\t(operating system)\n",
		view.CredentialBackend,
	); err != nil {
		return err
	}
	return table.Flush()
}

func displayPathName(name string) string {
	switch name {
	case "configuration":
		return "Configuration"
	case "sync jobs":
		return "Sync jobs"
	case "sync state":
		return "Sync state"
	case "sync recovery":
		return "Sync recovery"
	default:
		return name
	}
}

func writeConfigView(options RunOptions, view configView) error {
	if _, err := fmt.Fprintf(
		options.Out, "Schema version: %d\nCurrent profile: %s\n",
		view.Version, emptyDisplay(view.Current),
	); err != nil {
		return err
	}
	if len(view.Profiles) == 0 {
		_, err := fmt.Fprintln(options.Out, "Profiles: (none)")
		return err
	}
	if _, err := fmt.Fprintln(options.Out, "Profiles:"); err != nil {
		return err
	}
	for _, selected := range view.Profiles {
		marker := " "
		if selected.Current {
			marker = "*"
		}
		if _, err := fmt.Fprintf(
			options.Out, "  %s %s\n", marker, selected.Name,
		); err != nil {
			return err
		}
		table := tabwriter.NewWriter(options.Out, 0, 4, 2, ' ', 0)
		fields := [][2]string{
			{"Server", selected.Server},
			{"Username", selected.Username},
			{"OIDC subject", selected.Subject},
			{"Authentication", selected.AuthType},
			{"Insecure TLS", fmt.Sprintf("%t", selected.Insecure)},
			{"OIDC client ID", selected.ClientID},
			{"OIDC issuer", selected.Issuer},
			{"Token endpoint", selected.TokenURL},
			{"UserInfo endpoint", selected.UserInfoURL},
			{"Token expiry", formatExpiry(selected.ExpiresAt)},
			{"Default Space", selected.DefaultSpace},
			{"Space owner", selected.DefaultSpaceOwner},
		}
		for _, field := range fields {
			if field[1] == "" {
				continue
			}
			if _, err := fmt.Fprintf(
				table, "      %s:\t%s\n", field[0], field[1],
			); err != nil {
				return err
			}
		}
		if err := table.Flush(); err != nil {
			return err
		}
	}
	return nil
}

func emptyDisplay(value string) string {
	if value == "" {
		return "(none)"
	}
	return value
}

func formatExpiry(value int64) string {
	if value <= 0 {
		return ""
	}
	return time.Unix(value, 0).Format(time.RFC3339)
}
