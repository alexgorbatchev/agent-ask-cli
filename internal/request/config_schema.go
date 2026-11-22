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

//go:embed schemas/agy-settings.json
var agySettingsSchema []byte

func compileClosedSettingsSchema(raw []byte, location string) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	root := doc.(map[string]any)
	root["additionalProperties"] = false
	c := jsonschema.NewCompiler()
	c.UseLoader(jsonschema.FileLoader{})
	if err := c.AddResource(location, root); err != nil {
		return nil, err
	}
	return c.Compile(location)
}

var settingsValidator = sync.OnceValues(func() (*jsonschema.Schema, error) {
	return compileClosedSettingsSchema(claudeSettingsSchema, "https://json.schemastore.org/claude-code-settings.json")
})

var agySettingsValidator = sync.OnceValues(func() (*jsonschema.Schema, error) {
	return compileClosedSettingsSchema(agySettingsSchema, "https://antigravity.google/schemas/v1/settings.json")
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

func validateAgySettings(config map[string]any) error {
	schema, err := agySettingsValidator()
	if err != nil {
		return failure("configuration_failed", "Cannot initialize Antigravity settings validation")
	}
	if err := schema.Validate(config); err != nil {
		return failure("configuration_failed", "Antigravity settings do not match the bundled native settings schema")
	}
	return nil
}
