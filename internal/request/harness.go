package request

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type invocation struct {
	executable string
	args, env  []string
	dir        string
}

func buildInvocation(o Options, p prepared, temp string) (invocation, error) {
	v := invocation{executable: o.Executable, env: os.Environ(), dir: o.WorkingDir}
	if v.dir == "" {
		var err error
		v.dir, err = os.Getwd()
		if err != nil {
			return v, failure("invalid_request", "Cannot resolve working directory")
		}
	}
	dir, err := filepath.Abs(v.dir)
	if err != nil {
		return v, failure("invalid_request", "Cannot resolve working directory")
	}
	v.dir = dir
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return v, failure("invalid_request", "Working directory must exist")
	}
	if v.executable == "" {
		v.executable = o.Harness
	}
	v.executable, err = exec.LookPath(v.executable)
	if err != nil {
		return v, failure("harness_unavailable", "Selected harness executable is unavailable")
	}
	v.executable, err = filepath.Abs(v.executable)
	if err != nil {
		return v, failure("harness_unavailable", "Cannot resolve harness executable")
	}
	if o.HomeDir != "" {
		home, err := filepath.Abs(o.HomeDir)
		if err != nil {
			return v, failure("configuration_failed", "Cannot resolve harness home directory")
		}
		info, err := os.Stat(home)
		if err != nil || !info.IsDir() {
			return v, failure("configuration_failed", "Harness home directory must exist")
		}
		key := "CODEX_HOME"
		if o.Harness == "claude" {
			key = "CLAUDE_CONFIG_DIR"
		}
		v.env = replaceEnv(v.env, key, home)
	}
	if o.Harness == "claude" {
		err = claudeArgs(&v, o, p, temp)
	} else {
		err = codexArgs(&v, o, p, temp)
	}
	return v, err
}

func replaceEnv(env []string, key, value string) []string {
	result := make([]string, 0, len(env)+1)
	for _, s := range env {
		if !strings.HasPrefix(s, key+"=") {
			result = append(result, s)
		}
	}
	return append(result, key+"="+value)
}

func nativeConfig(o Options) (map[string]any, error) {
	config := map[string]any{}
	if o.ConfigFile == "" {
		return config, nil
	}
	b, err := os.ReadFile(o.ConfigFile)
	if err != nil {
		return nil, failure("configuration_failed", "Cannot read harness configuration file")
	}
	if o.Harness == "claude" {
		value, err := decodeJSON(b)
		if err != nil {
			return nil, failure("configuration_failed", "Claude configuration must be a JSON object")
		}
		var ok bool
		config, ok = value.(map[string]any)
		if !ok {
			return nil, failure("configuration_failed", "Claude configuration must be a JSON object")
		}
	} else if err := toml.Unmarshal(b, &config); err != nil {
		return nil, failure("configuration_failed", "Codex configuration must be valid TOML")
	}
	return config, nil
}

func claudeArgs(v *invocation, o Options, p prepared, temp string) error {
	config, err := nativeConfig(o)
	if err != nil {
		return err
	}
	v.args = []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages", "--no-session-persistence", "--permission-prompts", "none"}
	if o.ConfigMode == "clean" {
		v.args = append(v.args, "--safe-mode", "--setting-sources", "")
	}
	if o.Model != "" {
		v.args = append(v.args, "--model", o.Model)
	}
	if o.Effort != "" {
		if !oneOf(o.Effort, "low", "medium", "high", "xhigh", "max") {
			return failure("unsupported_option", "Claude effort must be low, medium, high, xhigh, or max")
		}
		v.args = append(v.args, "--effort", o.Effort)
	}
	if o.Thinking != "auto" {
		// Later Claude model families always think. Never claim to disable them.
		if o.Thinking == "off" && !claudeCanDisableThinking(o.Model) {
			return failure("unsupported_option", "Claude thinking off requires an explicit supported Claude 4 or 4.5 model; use effort for newer models")
		}
		config["alwaysThinkingEnabled"] = o.Thinking == "on"
	}
	if len(config) > 0 {
		if err := validateClaudeSettings(config); err != nil {
			return err
		}
		b, err := json.Marshal(config)
		if err != nil {
			return failure("configuration_failed", "Cannot encode Claude settings")
		}
		path := filepath.Join(temp, "claude-settings.json")
		if err := os.WriteFile(path, b, 0600); err != nil {
			return failure("configuration_failed", "Cannot stage Claude settings")
		}
		v.args = append(v.args, "--settings", path)
	}
	if len(p.outputSchema) > 0 {
		v.args = append(v.args, "--json-schema", string(p.outputSchema))
	}
	return nil
}

func claudeCanDisableThinking(model string) bool {
	if oneOf(model, "claude-sonnet-4-20250514", "claude-opus-4-20250514", "claude-opus-4-1-20250805") {
		return true
	}
	return model == "claude-sonnet-4-5" || model == "claude-opus-4-5" || strings.HasPrefix(model, "claude-sonnet-4-5-") || strings.HasPrefix(model, "claude-opus-4-5-")
}

func codexArgs(v *invocation, o Options, p prepared, temp string) error {
	config, err := nativeConfig(o)
	if err != nil {
		return err
	}
	v.args = []string{"exec", "--json", "--ephemeral", "--skip-git-repo-check"}
	keys := make([]string, 0, len(config))
	for key := range config {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := codexOverride(v, key, config[key]); err != nil {
			return err
		}
	}
	if o.ConfigMode == "clean" {
		v.args = append(v.args, "--ignore-user-config", "--ignore-rules",
			"-c", "project_doc_max_bytes=0", "-c", "skills.include_instructions=false",
			"-c", "features.skip_host_skill_discovery=true", "-c", "features.plugins=false",
			"-c", "features.hooks=false", "-c", "features.memories=false")
		if err := codexOverride(v, "projects", map[string]any{v.dir: map[string]any{"trust_level": "untrusted"}}); err != nil {
			return err
		}
	}
	if o.Model != "" {
		v.args = append(v.args, "--model", o.Model)
	}
	effort := o.Effort
	if effort != "" && !oneOf(effort, "none", "minimal", "low", "medium", "high", "xhigh") {
		return failure("unsupported_option", "Codex effort must be none, minimal, low, medium, high, or xhigh")
	}
	if o.Thinking == "off" {
		if effort != "" && effort != "none" {
			return failure("invalid_request", "Thinking off conflicts with a reasoning effort")
		}
		effort = "none"
	}
	if o.Thinking == "on" {
		if effort == "none" {
			return failure("invalid_request", "Thinking on conflicts with effort none")
		}
		if effort == "" {
			effort = "medium"
		}
	}
	if effort != "" {
		v.args = append(v.args, "-c", "model_reasoning_effort="+strconv.Quote(effort))
	}
	if len(p.outputSchema) > 0 {
		path := filepath.Join(temp, "output.schema.json")
		if err := os.WriteFile(path, p.outputSchema, 0600); err != nil {
			return failure("configuration_failed", "Cannot stage output schema")
		}
		v.args = append(v.args, "--output-schema", path)
	}
	v.args = append(v.args, "-")
	return nil
}

func codexOverride(v *invocation, key string, value any) error {
	// Native override paths are split on dots, without TOML key unquoting.
	// Nested table keys belong inside the TOML value, where quoting is native.
	if key == "" || strings.ContainsAny(key, ".\"'[]= \t\r\n") {
		return failure("unsupported_option", "Codex configuration contains an unsupported root key")
	}
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).SetTablesInline(true).Encode(map[string]any{"value": value}); err != nil {
		return failure("configuration_failed", "Cannot encode Codex configuration")
	}
	_, encoded, ok := strings.Cut(b.String(), " = ")
	if !ok {
		return failure("configuration_failed", "Cannot encode Codex configuration")
	}
	v.args = append(v.args, "-c", key+"="+strings.TrimSpace(encoded))
	return nil
}

func oneOf(value string, values ...string) bool {
	for _, v := range values {
		if value == v {
			return true
		}
	}
	return false
}
