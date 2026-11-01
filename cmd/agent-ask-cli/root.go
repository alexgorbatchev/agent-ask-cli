package main

import (
	"github.com/alexgorbatchev/agent-ask-cli/internal/request"
	"github.com/spf13/cobra"
)

const runLong = `Submit one request and return its validated answer.

Exactly one prompt source is required. Each of prompt, input, input schema and output schema accepts at most one of its literal, file and stdin forms. At most one of them may read stdin.`

var version = "dev"

func newRootCommand() *cobra.Command {
	root := &cobra.Command{Use: "agent-ask-cli", Short: "Run one request through an installed coding harness", Version: version, SilenceUsage: true, SilenceErrors: true, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	root.SetVersionTemplate("{{.Version}}\n")
	group := &cobra.Command{Use: "request", Short: "Submit fresh harness requests", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	group.AddCommand(newRunCommand())
	root.AddCommand(group)
	setupHelp(root)
	return root
}

func newRunCommand() *cobra.Command {
	var o request.Options
	cmd := &cobra.Command{Use: "run", Short: "Submit one request and return its validated answer", Long: runLong, Args: cobra.NoArgs}
	sources := []struct {
		name        string
		source      *request.Source
		description string
	}{
		{"prompt", &o.Prompt, "Instructions"}, {"input", &o.Input, "Input data"}, {"input-schema", &o.InputSchema, "Input JSON Schema"}, {"output-schema", &o.OutputSchema, "Output JSON Schema"},
	}
	for _, s := range sources {
		cmd.Flags().StringVar(&s.source.Value, s.name, "", s.description+" as literal content")
		cmd.Flags().StringVar(&s.source.File, s.name+"-file", "", s.description+" from a UTF-8 file")
		cmd.Flags().BoolVar(&s.source.Stdin, s.name+"-stdin", false, s.description+" from stdin")
		cmd.MarkFlagsMutuallyExclusive(s.name, s.name+"-file", s.name+"-stdin")
	}
	f := cmd.Flags()
	f.StringVar(&o.Harness, "harness", "", "Installed harness: claude or codex (required)")
	f.StringVar(&o.InputFormat, "input-format", "text", "Input interpretation: text or json")
	f.StringVar(&o.ConfigMode, "harness-config-mode", "inherit", "Native configuration baseline: inherit or clean")
	f.StringVar(&o.ConfigFile, "harness-config-file", "", "Native configuration overlay file: Claude JSON or Codex TOML")
	f.StringVar(&o.HomeDir, "harness-home-dir", "", "Alternate native configuration/state/authentication directory")
	f.StringVar(&o.Model, "model", "", "Native model name; omitted uses harness default")
	f.StringVar(&o.Effort, "effort", "", "Native reasoning effort; omitted uses harness default")
	f.StringVar(&o.Thinking, "thinking", "auto", "Thinking selection: auto, on, or off")
	f.StringVar(&o.WorkingDir, "working-dir", "", "Harness working directory; omitted uses caller directory")
	f.StringVar(&o.Executable, "harness-executable-file", "", "Harness executable file; omitted resolves harness on PATH")
	f.DurationVar(&o.Timeout, "timeout", defaultTimeout, "Positive request timeout")
	var jsonOutput, plainOutput, streamOutput bool
	f.BoolVar(&jsonOutput, "output-json", false, "Print one validated JSON result envelope (default)")
	f.BoolVar(&plainOutput, "output-plain", false, "Print only the validated final answer")
	f.BoolVar(&streamOutput, "output-stream", false, "Stream sequenced NDJSON events and one terminal result or error")
	cmd.MarkFlagsMutuallyExclusive("output-json", "output-plain", "output-stream")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		for _, s := range sources {
			s.source.Set = f.Changed(s.name) || f.Changed(s.name+"-file") || f.Changed(s.name+"-stdin")
			s.source.FileSet = f.Changed(s.name + "-file")
			s.source.StdinSet = f.Changed(s.name + "-stdin")
		}
		o.OutputMode = "json"
		if plainOutput {
			o.OutputMode = "plain"
		}
		if streamOutput {
			o.OutputMode = "stream"
			mark(cmd.Root(), streamRanMark)
		}
		return request.Run(cmd.Context(), o, cmd.InOrStdin(), cmd.OutOrStdout())
	}
	return cmd
}
