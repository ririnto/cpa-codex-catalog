package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBuildOverlaysFieldsAndReturnsOnlyModels(t *testing.T) {
	base := []byte(`{"client_version":"synthetic","fetched_at":"2030-01-01T00:00:00Z","identity":"synthetic-provider","models":[{"slug":"alpha","display_name":"Alpha","supported_reasoning_levels":[{"effort":"low","description":"Light"},{"effort":"high","description":"Deep"}],"shell_type":"shell_command","visibility":"list","supported_in_api":true,"priority":1,"support_verbosity":false,"truncation_policy":{"mode":"bytes","limit":10000},"experimental_supported_tools":[],"default_reasoning_level":"high","context_window":65536,"max_context_window":131072,"model_messages":{"instructions_template":"Base prompt","tools":{"multi_agent":{"spawn_agent":{"description":"Base tool"},"send_message":{"description":"Keep me"}}}},"extension":{"nested":{"kept":true,"array":["base"]}},"supports_parallel_tool_calls":true},{"slug":"beta","display_name":"Beta","supported_reasoning_levels":[{"effort":"low","description":"Light"}],"shell_type":"unified_exec","visibility":"list","supported_in_api":true,"priority":2,"support_verbosity":false,"truncation_policy":{"mode":"tokens","limit":8000},"experimental_supported_tools":[],"model_messages":{"instructions_template":"Beta prompt"},"extension":{"other":true}}]}`)
	overrides := []byte(`{"defaults":{"display_name":"Custom","context_window":98304,"extension":{"nested":{"defaulted":true,"array":["default"]}},"shared":{"value":"default"}},"models":{"alpha":{"default_reasoning_level":"low","model_messages":{"tools":{"multi_agent":{"spawn_agent":{"description":"Custom tool"}}}},"extension":{"nested":{"array":["override"]}},"shared":{"value":"alpha"}}}}`)
	got, err := Build(base, overrides)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("output keys = %v, want only models", keys(result))
	}
	models, ok := result["models"].([]any)
	if !ok || len(models) != 2 {
		t.Fatalf("models = %#v, want two models", result["models"])
	}
	alpha := models[0].(map[string]any)
	beta := models[1].(map[string]any)
	if alpha["display_name"] != "Custom" || beta["display_name"] != "Custom" {
		t.Fatal("defaults were not applied to each model")
	}
	if alpha["default_reasoning_level"] != "low" {
		t.Fatalf("per-model reasoning override = %v", alpha["default_reasoning_level"])
	}
	if alpha["context_window"] != float64(98304) || beta["context_window"] != float64(98304) {
		t.Fatal("default context window was not applied")
	}
	alphaExtension := alpha["extension"].(map[string]any)["nested"].(map[string]any)
	betaExtension := beta["extension"].(map[string]any)["nested"].(map[string]any)
	if !reflect.DeepEqual(alphaExtension["array"], []any{"override"}) {
		t.Fatalf("array override = %#v", alphaExtension["array"])
	}
	if alphaExtension["kept"] != true || alphaExtension["defaulted"] != true {
		t.Fatalf("recursive merge dropped base or default fields: %#v", alphaExtension)
	}
	if _, present := betaExtension["array"]; !present {
		t.Fatal("default nested array was not applied to beta")
	}
	sharedBeta := beta["shared"].(map[string]any)
	if sharedBeta["value"] != "default" {
		t.Fatalf("one model's patch mutated another model: %#v", sharedBeta)
	}
	modelMessages := alpha["model_messages"].(map[string]any)
	if modelMessages["instructions_template"] != "Base prompt" {
		t.Fatal("base prompt was not preserved")
	}
	tools := modelMessages["tools"].(map[string]any)
	multiAgent := tools["multi_agent"].(map[string]any)
	spawnAgent := multiAgent["spawn_agent"].(map[string]any)
	if spawnAgent["description"] != "Custom tool" || multiAgent["send_message"].(map[string]any)["description"] != "Keep me" {
		t.Fatalf("nested prompt metadata was not merged: %#v", multiAgent)
	}
	if alpha["supports_parallel_tool_calls"] != true {
		t.Fatal("unknown base metadata was not preserved")
	}
}

func TestBuildRejectsMalformedInputsAndPatches(t *testing.T) {
	base := baseWithModels(syntheticModel("alpha"))
	modelWithoutInstructions := syntheticModel("alpha")
	delete(modelWithoutInstructions["model_messages"].(map[string]any), "instructions_template")
	cases := []struct {
		name      string
		base      []byte
		overrides []byte
	}{
		{name: "invalid base JSON", base: []byte(`{"models":[}`), overrides: []byte(`{}`)},
		{name: "duplicate JSON key", base: []byte(`{"models":[],"models":[]}`), overrides: []byte(`{}`)},
		{name: "wrong models type", base: []byte(`{"models":null}`), overrides: []byte(`{}`)},
		{name: "empty catalog", base: []byte(`{"models":[]}`), overrides: []byte(`{}`)},
		{name: "unknown model slug", base: base, overrides: []byte(`{"models":{"missing":{"display_name":"Other"}}}`)},
		{name: "slug change", base: base, overrides: []byte(`{"models":{"alpha":{"slug":"other"}}}`)},
		{name: "default slug", base: base, overrides: []byte(`{"defaults":{"slug":"other"}}`)},
		{name: "duplicate override key", base: base, overrides: []byte(`{"defaults":{},"defaults":{}}`)},
		{name: "invalid default reasoning", base: base, overrides: []byte(`{"defaults":{"default_reasoning_level":"high"}}`)},
		{name: "invalid enum", base: base, overrides: []byte(`{"defaults":{"shell_type":"remote_shell"}}`)},
		{name: "invalid context bounds", base: base, overrides: []byte(`{"defaults":{"context_window":200000,"max_context_window":100000}}`)},
		{name: "invalid nested prompt type", base: base, overrides: []byte(`{"defaults":{"model_messages":{"instructions_template":7}}}`)},
		{name: "invalid nested tool message", base: base, overrides: []byte(`{"defaults":{"model_messages":{"tools":{"multi_agent":{"spawn_agent":7}}}}}`)},
		{name: "invalid nested instructions variable", base: base, overrides: []byte(`{"defaults":{"model_messages":{"instructions_variables":{"personality_default":7}}}}`)},
		{name: "incomplete token budget", base: base, overrides: []byte(`{"defaults":{"model_messages":{"token_budget":{}}}}`)},
		{name: "invalid guardian policy mode type", base: base, overrides: []byte(`{"defaults":{"guardian":{"shell":7}}}`)},
		{name: "invalid access program entry type", base: base, overrides: []byte(`{"defaults":{"available_access_programs":{"cyber":[7]}}}`)},
		{name: "invalid upgrade model type", base: base, overrides: []byte(`{"defaults":{"upgrade":{"model":7,"migration_markdown":""}}}`)},
		{name: "invalid guardian transcript sources", base: base, overrides: []byte(`{"defaults":{"model_messages":{"guardian_v2":{"transcript":{"sources":[7]}}}}}`)},
		{name: "missing instruction template", base: baseWithModels(modelWithoutInstructions), overrides: []byte(`{}`)},
		{name: "duplicate model slugs", base: baseWithModels(syntheticModel("alpha"), syntheticModel("alpha")), overrides: []byte(`{}`)},
		{name: "trailing JSON value", base: base, overrides: []byte(`{} {}`)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := Build(testCase.base, testCase.overrides); err == nil {
				t.Fatal("Build() succeeded, want validation error")
			}
		})
	}
}

func TestBuildAcceptsPartialTypedMetadataAndPreservesUnknownFields(t *testing.T) {
	model := syntheticModel("alpha")
	model["guardian"] = map[string]any{}
	model["available_access_programs"] = map[string]any{"cyber": []any{}}
	model["upgrade"] = map[string]any{"model": "upgrade-target", "migration_markdown": ""}
	modelMessages := model["model_messages"].(map[string]any)
	modelMessages["tools"] = map[string]any{}
	modelMessages["permissions"] = map[string]any{}
	modelMessages["guardian_v2"] = map[string]any{}
	modelMessages["future_message_group"] = map[string]any{"future_value": []any{1, true}}
	base, err := json.Marshal(map[string]any{"cache_revision": "synthetic", "models": []map[string]any{model}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Build(base, []byte(`{"defaults":{"model_messages":{"tools":{"multi_agent":{"spawn_agent":{"description":"Partial override"}}}}}}`))
	if err != nil {
		t.Fatalf("Build() rejected valid partial metadata: %v", err)
	}
	var response map[string]any
	if err := json.Unmarshal(got, &response); err != nil {
		t.Fatal(err)
	}
	if len(response) != 1 {
		t.Fatalf("response wrapper fields = %v, want only models", keys(response))
	}
	resultModel := response["models"].([]any)[0].(map[string]any)
	message := resultModel["model_messages"].(map[string]any)
	tool := message["tools"].(map[string]any)["multi_agent"].(map[string]any)["spawn_agent"].(map[string]any)
	if tool["description"] != "Partial override" {
		t.Fatalf("partial ToolMessage = %#v", tool)
	}
	if _, ok := message["future_message_group"]; !ok {
		t.Fatal("unknown nested metadata was not preserved")
	}
}

func TestBuildValidatesModelTypesAndSize(t *testing.T) {
	invalid := syntheticModel("alpha")
	invalid["input_modalities"] = []any{"text", "video"}
	if _, err := Build(baseWithModels(invalid), []byte(`{}`)); err == nil {
		t.Fatal("unsupported input modality was accepted")
	}
	invalid = syntheticModel("alpha")
	invalid["supported_reasoning_levels"] = []any{"low"}
	if _, err := Build(baseWithModels(invalid), []byte(`{}`)); err == nil {
		t.Fatal("invalid reasoning level type was accepted")
	}
	invalid = syntheticModel("alpha")
	invalid["extension"] = strings.Repeat("x", MaxCatalogBytes)
	if _, err := Build(baseWithModels(invalid), []byte(`{}`)); err == nil {
		t.Fatal("oversized catalog was accepted")
	}
}

func TestBuildAcceptsLegacyBaseInstructions(t *testing.T) {
	model := syntheticModel("alpha")
	delete(model["model_messages"].(map[string]any), "instructions_template")
	model["base_instructions"] = "Legacy instructions"
	if _, err := Build(baseWithModels(model), []byte(`{}`)); err != nil {
		t.Fatalf("Build() rejected a legacy instruction template: %v", err)
	}
}

func TestLoadReadsFilesAndRedactsPathsOnErrors(t *testing.T) {
	directory := t.TempDir()
	basePath := filepath.Join(directory, "models.json")
	overridesPath := filepath.Join(directory, "overrides.json")
	if err := os.WriteFile(basePath, baseWithModels(syntheticModel("alpha")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overridesPath, []byte(`{"defaults":{"display_name":"Configured"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(basePath, overridesPath); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	missingPath := filepath.Join(directory, "private-models.json")
	if _, err := Load(missingPath, ""); err == nil || strings.Contains(err.Error(), directory) || strings.Contains(err.Error(), "private-models.json") {
		t.Fatalf("Load() error exposed a path or did not fail: %v", err)
	}
}

func baseWithModels(models ...map[string]any) []byte {
	encoded, err := json.Marshal(map[string]any{"models": models})
	if err != nil {
		panic(err)
	}
	return encoded
}

func syntheticModel(slug string) map[string]any {
	return map[string]any{
		"slug":                         slug,
		"display_name":                 slug,
		"supported_reasoning_levels":   []any{map[string]any{"effort": "low", "description": "Light"}},
		"shell_type":                   "shell_command",
		"visibility":                   "list",
		"supported_in_api":             true,
		"priority":                     1,
		"support_verbosity":            false,
		"truncation_policy":            map[string]any{"mode": "bytes", "limit": 10000},
		"experimental_supported_tools": []any{},
		"model_messages":               map[string]any{"instructions_template": "Synthetic instructions"},
	}
}

func keys(value map[string]any) []string {
	result := make([]string, 0, len(value))
	for key := range value {
		result = append(result, key)
	}
	return result
}
