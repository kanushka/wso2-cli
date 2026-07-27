package app

import (
	"fmt"

	"github.com/wso2/wso2-cli/internal/output"
	shellversion "github.com/wso2/wso2-cli/internal/version"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// version reports the shell, protocol, platform, and installed module
// versions from local state.
//
// It reads receipts only. It never launches a module and never opens a network
// connection, so inventory is safe and works offline. A broken installation
// becomes a diagnostic on standard error rather than a failed command, so the
// remaining inventory still reports.
func (s Shell) version(args []string) error {
	if len(args) > 0 {
		return problem.New(problem.CategoryUsage, "shell.unexpected_argument",
			fmt.Sprintf("wso2 version takes no arguments, got %q", args[0])).
			WithRecovery("Run wso2 version.")
	}

	info := shellversion.Current()
	if err := output.Fields(s.Streams.Out, [][2]string{
		{"WSO2 CLI", info.Shell},
		{"Protocol", info.Protocol},
		{"Platform", info.Platform},
	}); err != nil {
		return err
	}

	store, err := s.store()
	if err != nil {
		return err
	}
	installed, problems, err := store.Inventory()
	if err != nil {
		return err
	}

	fmt.Fprintln(s.Streams.Out)
	fmt.Fprintln(s.Streams.Out, "Installed modules")
	if len(installed) == 0 {
		fmt.Fprintln(s.Streams.Out, "No modules are installed.")
	} else {
		table := output.NewTable("name", "version", "platform")
		for _, entry := range installed {
			table.Append(entry.Namespace, "v"+entry.Version, entry.Platform.String())
		}
		if err := table.Render(s.Streams.Out); err != nil {
			return err
		}
	}

	for _, broken := range problems {
		output.Diagnostic(s.Streams.Err, broken.Problem)
	}
	return nil
}
