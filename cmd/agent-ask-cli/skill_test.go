package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v4"
	"github.com/spf13/cobra"
)

const skillAlert = "ALERT: Agents must read `AGENT=1 agent-ask-cli skill` before using this tool.\n"

func TestSkillCommand(t *testing.T) {
	want, err := os.ReadFile("../../skills/agent-ask-cli/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(want, []byte("name: agent-ask-cli\n")) {
		t.Fatal("skill path or frontmatter does not match advertised skill")
	}
	for _, mode := range []string{"0", "1"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("AGENT", mode)
			t.Chdir(t.TempDir())
			out, err := executeCommand("skill")
			if err != nil {
				t.Fatalf("skill failed: %v", err)
			}
			if out != "Install the agent-ask-cli skill using the Skills CLI:\n\n  npx skills add alexgorbatchev/agent-ask-cli --skill agent-ask-cli\n\nSkill:\n\n  https://github.com/alexgorbatchev/agent-ask-cli/tree/main/skills/agent-ask-cli\n" {
				t.Fatal("skill output must match the fixed installation contract")
			}
		})
	}
}

func TestSkillCommandRejectsArguments(t *testing.T) {
	out, err := executeCommand("skill", "extra")
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("expected Cobra argument rejection, got %v: %s", err, out)
	}
}

func commandTree(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := newRootCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.InitDefaultHelpCmd()
	cmd.InitDefaultCompletionCmd()
	return cmd
}

func walkCommands(cmd *cobra.Command, visit func(*cobra.Command)) {
	if cmd.Hidden {
		return
	}
	visit(cmd)
	for _, child := range cmd.Commands() {
		walkCommands(child, visit)
	}
}

func TestHelpSkillAlert(t *testing.T) {
	for _, mode := range []string{"0", "1", "true", "yes"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("AGENT", mode)
			walkCommands(commandTree(t), func(cmd *cobra.Command) {
				args := strings.Fields(cmd.CommandPath())[1:]
				out, err := executeCommand(append(args, "--help")...)
				if err != nil {
					t.Fatalf("%s help: %v", cmd.CommandPath(), err)
				}
				if mode == "0" {
					if strings.Contains(out, skillAlert) {
						t.Errorf("%s human help contains agent alert", cmd.CommandPath())
					}
					return
				}
				if !strings.HasPrefix(out, skillAlert) || strings.Count(out, skillAlert) != 1 {
					t.Errorf("%s help must start with one skill alert", cmd.CommandPath())
				}
				if !strings.Contains(out, "command: "+cmd.CommandPath()+"\n") {
					t.Errorf("%s lost its agent help content: %s", cmd.CommandPath(), out)
				}
			})
			out, err := executeCommand()
			if err != nil || (mode != "0" && !strings.HasPrefix(out, skillAlert)) {
				t.Fatalf("bare invocation help: %v: %s", err, out)
			}
			out, err = executeCommand("help", "request")
			if err != nil || (mode != "0" && !strings.HasPrefix(out, skillAlert)) {
				t.Fatalf("explicit help command: %v: %s", err, out)
			}
			out, err = executeCommand("--version")
			if err != nil || out != version+"\n" {
				t.Fatalf("version output changed: %v: %q", err, out)
			}
		})
	}
}

func TestSkillDocumentsCommandInterface(t *testing.T) {
	contents, err := os.ReadFile("../../skills/agent-ask-cli/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if omissions := cobrahelptree.SkillOmissions(commandTree(t), techCatalog, string(contents)); len(omissions) > 0 {
		t.Errorf("skill does not document: %s", strings.Join(omissions, ", "))
	}
}
