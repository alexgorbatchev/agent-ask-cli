package request

import (
	"bytes"
	_ "embed"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Claude print mode can silently skip invalid settings. Validate the native
// document before launch instead of treating a successful run as acceptance.
//
//go:embed schemas/claude-settings.json
var claudeSettingsSchema []byte

var settingsValidator = sync.OnceValues(func() (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(claudeSettingsSchema))
	if err != nil {
		return nil, err
	}
	root := doc.(map[string]any)
	root["additionalProperties"] = false
	c := jsonschema.NewCompiler()
	c.UseLoader(jsonschema.FileLoader{})
	const location = "https://json.schemastore.org/claude-code-settings.json"
	if err := c.AddResource(location, root); err != nil {
		return nil, err
	}
	return c.Compile(location)
})

func validateClaudeSettings(config map[string]any) error {
	schema, err := settingsValidator()
	if err != nil {
		return failure("configuration_failed", "Cannot initialize Claude settings validation")
	}
	if err := schema.Validate(config); err != nil {
		return failure("configuration_failed", "Claude settings do not match the bundled native settings schema")
	}
	return nil
}
