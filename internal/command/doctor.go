package command

import "github.com/spf13/cobra"

func newDoctorCommand(options *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use: "doctor [PROFILE]", Short: "Validate configuration and connectivity",
		Args: maximumArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			profile := options.profile
			if len(args) == 1 {
				profile = args[0]
			}
			return runDoctor(command, options, profile)
		},
	}
}
