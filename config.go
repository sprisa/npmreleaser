package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/template"
)

type configVariables struct {
	Env map[string]string
}

func loadConfig(path string) (*ConfigSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		env[key] = value
	}
	value, err = renderConfigValues(value, configVariables{Env: env}, path)
	if err != nil {
		return nil, err
	}
	// Render parsed strings, preserving JSON types and escaping environment-
	// provided quotes and newlines when the configuration is encoded again.
	data, err = json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var config ConfigSpec
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return &config, nil
}

func renderConfigValues(value any, variables configVariables, path string) (any, error) {
	switch value := value.(type) {
	case string:
		if !strings.Contains(value, "{{") {
			return value, nil
		}
		tmpl, err := template.New(path).Option("missingkey=error").Parse(value)
		if err != nil {
			return nil, fmt.Errorf("template %s: %w", path, err)
		}
		var output strings.Builder
		if err := tmpl.Execute(&output, variables); err != nil {
			return nil, fmt.Errorf("template %s: %w", path, err)
		}
		return output.String(), nil
	case map[string]any:
		for key, entry := range value {
			rendered, err := renderConfigValues(entry, variables, path+"."+key)
			if err != nil {
				return nil, err
			}
			value[key] = rendered
		}
	case []any:
		for i, entry := range value {
			rendered, err := renderConfigValues(entry, variables, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			value[i] = rendered
		}
	}
	return value, nil
}
