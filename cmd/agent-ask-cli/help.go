package main

import (
	"fmt"

	"github.com/alexgorbatchev/agent-ask-cli/internal/agent"
	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v2"
	"github.com/spf13/cobra"
)

var techCatalog = cobrahelptree.TechCatalog{
	"agent-ask-cli": {
		Summary:     "Run one request through an installed coding harness",
		Description: "Run one request through an installed coding harness - command-line interface.",
	},
	"agent-ask-cli request run": {
		Summary:  "Submit one fresh request",
		Metadata: map[string]string{"stdout": "json|text|ndjson", "authentication": "native harness"},
	},
}

func setupHelp(cmd *cobra.Command) {
	err := cobrahelptree.SetupWithOptions(cmd, cobrahelptree.HelpOptions{
		Catalog: techCatalog,
		Tree:    cobrahelptree.TreeOptions{HideGeneratedCommands: true},
	})
	if err != nil {
		cmd.RunE = func(*cobra.Command, []string) error { return err }
		return
	}
	help := cmd.HelpFunc()
	cmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		if agent.IsAgentMode() {
			if _, err := fmt.Fprintln(c.OutOrStdout(), "ALERT: Agents must read `AGENT=1 agent-ask-cli skill` before using this tool."); err != nil {
				c.PrintErrln(err)
				return
			}
		}
		help(c, args)
	})
}
