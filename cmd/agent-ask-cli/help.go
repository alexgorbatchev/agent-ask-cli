package main

import (
	"errors"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v4"
	"github.com/spf13/cobra"
)

const helpRejectedMark = "help-rejected"

var errUnknownHelpTarget = errors.New("unknown help target")

var techCatalog = cobrahelptree.TechCatalog{
	"agent-ask-cli request run": {
		Metadata: map[string]string{"stdout": "json|plain|ndjson", "authentication": "native harness"},
	},
}

func setupHelp(cmd *cobra.Command) {
	err := cobrahelptree.SetupWithOptions(cmd, cobrahelptree.HelpOptions{
		Catalog: techCatalog,
		Skill:   cobrahelptree.SkillSource{Repository: "alexgorbatchev/agent-ask-cli", Branch: "main"},
		// outputTracker reports the failure as JSON, so the library stays quiet.
		OnHelpWriteError: func(*cobra.Command, error) {},
	})
	if err != nil {
		cmd.RunE = func(*cobra.Command, []string) error { return err }
		return
	}
	rejectUnknownHelpTargets(cmd)
}

// rejectUnknownHelpTargets closes both routes by which cobra answers help for a
// command that does not exist with its parent's screen. The --help flag skips
// argument validation, and the help command drops the words Find could not match.
func rejectUnknownHelpTargets(root *cobra.Command) {
	root.InitDefaultHelpCmd()
	helpCommand, _, err := root.Find([]string{"help"})
	if err != nil {
		root.RunE = func(*cobra.Command, []string) error { return err }
		return
	}

	printHelp := root.HelpFunc()
	root.SetHelpFunc(func(c *cobra.Command, args []string) {
		// The help command's positional arguments are its target, not a typo.
		if c != helpCommand && c.Flags().NArg() > 0 {
			mark(c.Root(), helpRejectedMark)
			return
		}
		printHelp(c, args)
	})

	runHelp := helpCommand.Run
	helpCommand.RunE = func(c *cobra.Command, args []string) error {
		if _, unmatched, err := c.Root().Find(args); err != nil || len(unmatched) > 0 {
			return errUnknownHelpTarget
		}
		runHelp(c, args)
		return nil
	}
}
