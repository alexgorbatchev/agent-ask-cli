package main

import (
	"github.com/spf13/cobra"
	"io"
)

const skillText = "Install the ask-llm-cli skill using the Skills CLI:\n\n  npx skills add alexgorbatchev/ask-llm-cli --skill ask-llm-cli\n\nSkill:\n\n  https://github.com/alexgorbatchev/ask-llm-cli/tree/main/skills/ask-llm-cli\n"

func newSkillCommand() *cobra.Command {
	return &cobra.Command{Use: "skill", Short: "Print operational skill installation instructions", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		_, err := io.WriteString(cmd.OutOrStdout(), skillText)
		return err
	}}
}
