package request

import (
	"encoding/json"
	"github.com/pelletier/go-toml/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeArguments(t *testing.T) {
	for _, harness := range []string{"codex", "claude"} {
		t.Run(harness, func(t *testing.T) {
			o := options(t, harness)
			o.HomeDir = t.TempDir()
			o.Model = "model"
			o.Effort = "low"
			o.ConfigMode = "clean"
			o.Thinking = "on"
			o.ConfigFile = filepath.Join(t.TempDir(), "config")
			config := `[features]
custom = true
[model_providers.custom]
name = "native"
`
			if harness == "claude" {
				config = `{"permissions":{"allow":["Read"]},"model":"from-file"}`
			}
			if err := os.WriteFile(o.ConfigFile, []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			v, err := buildInvocation(o, prepared{outputSchema: []byte(titleSchema)}, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Join(v.args, " ")
			if !strings.Contains(args, "--model model") || !strings.Contains(args, "low") {
				t.Fatalf("args %q", args)
			}
			if harness == "codex" {
				for _, want := range []string{"--ignore-user-config", "project_doc_max_bytes=0", "skip_host_skill_discovery=true", `features={custom = true}`, "--output-schema"} {
					if !strings.Contains(args, want) {
						t.Errorf("missing %q from %s", want, args)
					}
				}
				if !strings.Contains(strings.Join(v.env, "\n"), "CODEX_HOME="+o.HomeDir) {
					t.Fatal("wrong home")
				}
			} else {
				if !strings.Contains(args, "--safe-mode") || !strings.Contains(args, "--json-schema") {
					t.Fatalf("args %q", args)
				}
				var settings map[string]any
				for i, arg := range v.args {
					if arg == "--settings" {
						data, err := os.ReadFile(v.args[i+1])
						if err != nil {
							t.Fatal(err)
						}
						if err := json.Unmarshal(data, &settings); err != nil {
							t.Fatal(err)
						}
					}
				}
				if settings["alwaysThinkingEnabled"] != true || settings["model"] != "from-file" {
					t.Fatalf("settings %+v", settings)
				}
			}
		})
	}
}

func TestInvocationValidation(t *testing.T) {
	cases := []struct {
		name, harness, code string
		change              func(*Options)
	}{
		{"path lookup", "codex", "harness_unavailable", func(o *Options) { o.Executable = "ask-llm-missing-harness" }},
		{"directory", "codex", "invalid_request", func(o *Options) { o.WorkingDir = "missing" }},
		{"home", "codex", "configuration_failed", func(o *Options) { o.HomeDir = "missing" }},
		{"config", "codex", "configuration_failed", func(o *Options) { o.ConfigFile = "missing" }},
		{"codex effort", "codex", "unsupported_option", func(o *Options) { o.Effort = "max" }},
		{"claude effort", "claude", "unsupported_option", func(o *Options) { o.Effort = "none" }},
		{"claude thinking off", "claude", "unsupported_option", func(o *Options) { o.Thinking = "off" }},
		{"thinking conflict off", "codex", "invalid_request", func(o *Options) { o.Thinking = "off"; o.Effort = "high" }},
		{"thinking conflict on", "codex", "invalid_request", func(o *Options) { o.Thinking = "on"; o.Effort = "none" }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			o := options(t, tt.harness)
			tt.change(&o)
			_, err := buildInvocation(o, prepared{}, t.TempDir())
			if err == nil || ErrorValue(err).Code != tt.code {
				t.Fatalf("err=%v", err)
			}
		})
	}
	for _, harness := range []string{"codex", "claude"} {
		for _, body := range []string{"invalid [", `[]`, "true"} {
			t.Run(harness+body, func(t *testing.T) {
				o := options(t, harness)
				o.ConfigFile = filepath.Join(t.TempDir(), "config")
				if err := os.WriteFile(o.ConfigFile, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := nativeConfig(o); err == nil {
					t.Fatal("accepted bad config")
				}
			})
		}
	}
}

func TestThinkingMappingAndDefaultPaths(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, tt := range []struct{ harness, model, thinking, want string }{{"codex", "", "on", "model_reasoning_effort=\"medium\""}, {"codex", "", "off", "model_reasoning_effort=\"none\""}, {"claude", "claude-sonnet-4-5-20250929", "off", `"alwaysThinkingEnabled":false`}} {
		o := options(t, tt.harness)
		o.Model = tt.model
		o.Thinking = tt.thinking
		o.WorkingDir = ""
		v, err := buildInvocation(o, prepared{}, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		args := strings.Join(v.args, " ")
		if tt.harness == "claude" {
			for i, arg := range v.args {
				if arg == "--settings" {
					data, err := os.ReadFile(v.args[i+1])
					if err != nil {
						t.Fatal(err)
					}
					args = string(data)
				}
			}
		}
		if !strings.Contains(args, tt.want) {
			t.Fatalf("args %q", v.args)
		}
	}
	env := replaceEnv([]string{"A=old", "AB=keep", "A=duplicate"}, "A", "new")
	if strings.Join(env, ",") != "AB=keep,A=new" {
		t.Fatal(env)
	}
}

func TestClaudeSettingsRejections(t *testing.T) {
	for _, config := range []map[string]any{{"alwaysThinkingEnabled": "yes"}, {"unknownSetting": true}, {"permissions": map[string]any{"allow": []any{42}}}} {
		if err := validateClaudeSettings(config); err == nil || ErrorValue(err).Code != "configuration_failed" {
			t.Fatalf("config=%v err=%v", config, err)
		}
	}
}

func TestCodexOverridesUseNativePaths(t *testing.T) {
	var v invocation
	if err := codexOverride(&v, "model", "selected"); err != nil {
		t.Fatal(err)
	}
	key, _, ok := strings.Cut(v.args[1], "=")
	var decoded map[string]any
	if err := toml.Unmarshal([]byte(v.args[1]), &decoded); err != nil {
		t.Fatal(err)
	}
	if !ok || key != "model" || decoded["model"] != "selected" {
		t.Fatalf("native key %q", v.args)
	}
	if err := codexOverride(&v, "projects", map[string]any{"/path.with.dots": map[string]any{"trust_level": "untrusted"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v.args[3], "projects={") {
		t.Fatalf("projects override %q", v.args)
	}
	if err := codexOverride(&v, "bad.root", true); err == nil {
		t.Fatal("unrepresentable root key accepted")
	}
	if err := codexOverride(&v, "value", make(chan int)); err == nil {
		t.Fatal("unrepresentable TOML accepted")
	}
}

func TestThinkingRejectsLaterClaudeFamilies(t *testing.T) {
	for _, model := range []string{"claude-sonnet-4-6", "claude-opus-5-5", "sonnet", ""} {
		o := options(t, "claude")
		o.Thinking = "off"
		o.Model = model
		if _, err := buildInvocation(o, prepared{}, t.TempDir()); err == nil || ErrorValue(err).Code != "unsupported_option" {
			t.Fatalf("model=%s err=%v", model, err)
		}
	}
	o := options(t, "claude")
	o.Thinking = "off"
	o.Model = "claude-sonnet-4-20250514"
	if _, err := buildInvocation(o, prepared{}, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}
