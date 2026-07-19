package client

type researcherToolDefinition struct {
	name        string
	description string
	properties  map[string]any
	required    []string
}

func (d researcherToolDefinition) parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           d.properties,
		"required":             d.required,
		"additionalProperties": false,
	}
}

// researcherToolDefinitions is the provider-neutral researcher tool contract.
// The small description differences preserve the prompts used before the harness migration.
func researcherToolDefinitions(provider string) []researcherToolDefinition {
	findQueryDescription := "Natural language description of what files to find. Examples: 'CFR trainer tests', 'all zig files', 'error handling code', 'main entry point', 'configuration files'"
	findPaths := map[string]any{
		"type":        "array",
		"description": "Optional directories to search within. Defaults to entire project.",
		"items":       map[string]any{"type": "string"},
		"default":     []string{},
	}
	summarizePaths := map[string]any{
		"type":        "array",
		"description": "List of file paths to summarize",
		"items":       map[string]any{"type": "string"},
		"minItems":    1,
	}
	focus := map[string]any{
		"type":        "string",
		"description": "Optional focus for the summaries. Examples: 'error handling patterns', 'public API', 'test coverage', 'dependencies'",
		"default":     "",
	}
	readPaths := map[string]any{
		"type":        "array",
		"description": "List of file paths to read in full",
		"items":       map[string]any{"type": "string"},
		"minItems":    1,
	}
	if provider == AnthropicProvider {
		findQueryDescription = "Natural language description of what files to find."
		findPaths = map[string]any{
			"type":        "array",
			"description": "Directories to search within. Use an empty array for the entire project.",
			"items":       map[string]any{"type": "string"},
		}
		summarizePaths = map[string]any{
			"type":     "array",
			"items":    map[string]any{"type": "string"},
			"minItems": 1,
		}
		focus = map[string]any{
			"type":        "string",
			"description": "What the summaries should focus on. Use an empty string for a general summary.",
		}
		readPaths = map[string]any{
			"type":     "array",
			"items":    map[string]any{"type": "string"},
			"minItems": 1,
		}
	}

	return []researcherToolDefinition{
		{
			name:        "find_files",
			description: "Discover files matching a natural-language query.",
			properties: map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": findQueryDescription,
					"minLength":   1,
				},
				"paths": findPaths,
			},
			required: []string{"query", "paths"},
		},
		{
			name:        "summarize_files",
			description: "Generate concise summaries of source files before reading them in full.",
			properties: map[string]any{
				"paths": summarizePaths,
				"focus": focus,
			},
			required: []string{"paths", "focus"},
		},
		{
			name:        "read_files",
			description: "Read selected files in full after they have been triaged.",
			properties: map[string]any{
				"paths": readPaths,
			},
			required: []string{"paths"},
		},
	}
}
