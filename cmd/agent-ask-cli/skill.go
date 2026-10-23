package main

import (
	"github.com/spf13/cobra"
	"io"
)

const skillText = "Install the agent-ask-cli skill using the Skills CLI:\n\n  npx skills add alexgorbatchev/agent-ask-cli --skill agent-ask-cli\n\nSkill:\n\n  https://github.com/alexgorbatchev/agent-ask-cli/tree/main/skills/agent-ask-cli\n"

func newSkillCommand() *cobra.Command {
	return &cobra.Command{Use: "skill", Short: "Print operational skill installation instructions", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		_, err := io.WriteString(cmd.OutOrStdout(), skillText)
		return err
	}}
}
