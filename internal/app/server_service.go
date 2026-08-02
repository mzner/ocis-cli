package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mzner/ocis-cli/internal/apperror"
	appoutput "github.com/mzner/ocis-cli/internal/output"
)

func runServer(_ context.Context, request ServerRequest, options RunOptions) error {
	options.Logger.Debug("run server operation", "operation", request.Operation)
	s, err := loadStore(options.Dependencies)
	if err != nil {
		return err
	}
	switch request.Operation {
	case ServerAdd:
		name, server := request.Name, strings.TrimRight(request.Server, "/")
		if err := validateServerURL(server); err != nil {
			return apperror.Wrap(apperror.KindUsage, "add server", err)
		}
		clientID := request.ClientID
		if clientID == "" {
			clientID = defaultClientID
		}
		secret := os.Getenv("OCIS_CLIENT_SECRET")
		s.Profiles[name] = profile{
			Server: server, Insecure: request.Insecure,
			ClientID: clientID, ClientSecret: secret,
		}
		if s.Current == "" {
			s.Current = name
		}
		if err := saveStore(options.Dependencies, s); err != nil {
			return err
		}
		return output(
			options, "server",
			map[string]any{
				"name": name, "server": server, "current": s.Current == name,
				"insecure": request.Insecure,
			},
			"Added %s (%s)\n", name, server,
		)
	case ServerList:
		names := make([]string, 0, len(s.Profiles))
		for name := range s.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)
		if options.OutputMode != appoutput.Human {
			rows := make([]map[string]any, 0, len(names))
			for _, name := range names {
				p := s.Profiles[name]
				rows = append(rows, map[string]any{
					"name": name, "server": p.Server, "current": name == s.Current,
					"authenticated": p.Password != "" || p.RefreshToken != "" || p.AccessToken != "",
					"username":      p.Username,
				})
			}
			return writeOutput(options, "server", rows)
		}
		for _, name := range names {
			prefix := " "
			if name == s.Current {
				prefix = "*"
			}
			p := s.Profiles[name]
			_, _ = fmt.Fprintf(options.Out, "%s %-16s %s", prefix, name, p.Server)
			if p.Username != "" {
				_, _ = fmt.Fprintf(options.Out, "  %s", p.Username)
			}
			_, _ = fmt.Fprintln(options.Out)
		}
		return nil
	case ServerUse:
		if _, ok := s.Profiles[request.Name]; !ok {
			return apperror.Wrap(
				apperror.KindUsage, "use server",
				fmt.Errorf("unknown server profile %q", request.Name),
			)
		}
		s.Current = request.Name
		if err := saveStore(options.Dependencies, s); err != nil {
			return err
		}
		return output(
			options, "server", map[string]string{"current": s.Current},
			"Using %s\n", s.Current,
		)
	case ServerRemove:
		if _, ok := s.Profiles[request.Name]; !ok {
			return apperror.Wrap(
				apperror.KindUsage, "remove server",
				fmt.Errorf("unknown server profile %q", request.Name),
			)
		}
		delete(s.Profiles, request.Name)
		if err := options.Dependencies.Credentials.Delete(request.Name); err != nil {
			return err
		}
		if s.Current == request.Name {
			s.Current = ""
			for name := range s.Profiles {
				s.Current = name
				break
			}
		}
		if err := saveStore(options.Dependencies, s); err != nil {
			return err
		}
		return output(
			options, "server", map[string]string{"removed": request.Name},
			"Removed %s\n", request.Name,
		)
	default:
		return apperror.Wrap(
			apperror.KindUsage, "server",
			fmt.Errorf("unknown server command %q", request.Operation),
		)
	}
}
