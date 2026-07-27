// Command wso2 is the WSO2 CLI shell.
//
// The shell owns shared policy and dispatches product commands to
// independently released product modules resolved from its managed module
// store.
package main

import (
	"os"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/output"
)

func main() {
	shell := app.Shell{
		Streams: output.Streams{Out: os.Stdout, Err: os.Stderr},
	}
	os.Exit(int(shell.Run(os.Args[1:])))
}
