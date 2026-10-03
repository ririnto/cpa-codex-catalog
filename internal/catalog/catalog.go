package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const MaxCatalogBytes = 1 << 20

type object = map[string]any

func Build(base []byte, overrides []byte) ([]byte, error) {
	baseValue, err := decodeJSON(base)
	if err != nil {
		return nil, errors.New("catalog: invalid base JSON")
	}
	baseRoot, ok := baseValue.(object)
	if !ok {
		return nil, errors.New("catalog: base catalog must be an object")
	}
	modelsValue, ok := baseRoot["models"]
	if !ok {
		return nil, errors.New("catalog: base catalog must contain models")
	}
	models, ok := modelsValue.([]any)
	if !ok {
		return nil, errors.New("catalog: base models must be an array")
	}
	if len(models) == 0 {
		return nil, errors.New("catalog: base catalog must contain at least one model")
	}
	overrideValue, err := decodeJSON(overrides)
	if err != nil {
		return nil, errors.New("catalog: invalid overrides JSON")
	}
	overrideRoot, ok := overrideValue.(object)
	if !ok {
		return nil, errors.New("catalog: overrides must be an object")
	}
	for key := range overrideRoot {
		if key != "defaults" && key != "models" {
			return nil, errors.New("catalog: overrides contain an unsupported top-level field")
		}
	}
	defaults := object{}
	if value, present := overrideRoot["defaults"]; present {
		defaults, ok = value.(object)
		if !ok {
			return nil, errors.New("catalog: defaults must be an object")
		}
		if _, changesSlug := defaults["slug"]; changesSlug {
			return nil, errors.New("catalog: defaults cannot change model slugs")
		}
	}
	modelOverrides := object{}
	if value, present := overrideRoot["models"]; present {
		modelOverrides, ok = value.(object)
		if !ok {
			return nil, errors.New("catalog: per-model overrides must be an object")
		}
	}
	seen := make(map[string]struct{}, len(models))
	modelsBySlug := make(map[string]object, len(models))
	for index, value := range models {
		model, ok := value.(object)
		if !ok {
			return nil, fmt.Errorf("catalog: model at index %d must be an object", index)
		}
		slug, ok := model["slug"].(string)
		if !ok || strings.TrimSpace(slug) == "" {
			return nil, fmt.Errorf("catalog: model at index %d must have a non-empty slug", index)
		}
		if _, duplicate := seen[slug]; duplicate {
			return nil, errors.New("catalog: model slugs must be unique")
		}
		seen[slug] = struct{}{}
		modelsBySlug[slug] = model
	}
	for slug := range modelOverrides {
		if _, exists := modelsBySlug[slug]; !exists {
			return nil, errors.New("catalog: per-model override does not match a base model")
		}
	}
	for index, value := range models {
		model := value.(object)
		slug := model["slug"].(string)
		merged := merge(model, defaults)
		if patchValue, present := modelOverrides[slug]; present {
			patch, ok := patchValue.(object)
			if !ok {
				return nil, errors.New("catalog: each per-model override must be an object")
			}
			if patchSlug, present := patch["slug"]; present {
				if patchSlug != slug {
					return nil, errors.New("catalog: per-model override cannot change a model slug")
				}
			}
			merged = merge(merged, patch)
		}
		if err := validateModel(merged); err != nil {
			return nil, fmt.Errorf("catalog: invalid model at index %d: %w", index, err)
		}
	}
	out, err := json.Marshal(object{"models": models})
	if err != nil {
		return nil, errors.New("catalog: unable to encode merged models")
	}
	if len(out) > MaxCatalogBytes {
		return nil, errors.New("catalog: merged model catalog exceeds the 1 MiB limit")
	}
	return out, nil
}

func Load(basePath, overridesPath string) ([]byte, error) {
	base, err := os.ReadFile(basePath)
	if err != nil {
		return nil, errors.New("catalog: unable to read base catalog")
	}
	overrides := []byte(`{}`)
	if overridesPath != "" {
		overrides, err = os.ReadFile(overridesPath)
		if err != nil {
			return nil, errors.New("catalog: unable to read overrides")
		}
	}
	return Build(base, overrides)
}

func decodeJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := decodeValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("trailing JSON data")
	}
	return value, nil
}

func decodeValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := token.(json.Delim); ok {
		switch delimiter {
		case '{':
			value := object{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, errors.New("invalid JSON object key")
				}
				if _, exists := value[key]; exists {
					return nil, errors.New("duplicate JSON object key")
				}
				child, err := decodeValue(decoder)
				if err != nil {
					return nil, err
				}
				value[key] = child
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return nil, errors.New("invalid JSON object")
			}
			return value, nil
		case '[':
			value := []any{}
			for decoder.More() {
				child, err := decodeValue(decoder)
				if err != nil {
					return nil, err
				}
				value = append(value, child)
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return nil, errors.New("invalid JSON array")
			}
			return value, nil
		default:
			return nil, errors.New("invalid JSON delimiter")
		}
	}
	return token, nil
}

func merge(base object, patch object) object {
	for key, patchValue := range patch {
		if baseValue, exists := base[key]; exists {
			baseObject, baseIsObject := baseValue.(object)
			patchObject, patchIsObject := patchValue.(object)
			if baseIsObject && patchIsObject {
				base[key] = merge(baseObject, patchObject)
				continue
			}
		}
		base[key] = cloneValue(patchValue)
	}
	return base
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case object:
		cloned := make(object, len(typed))
		for key, child := range typed {
			cloned[key] = cloneValue(child)
		}
		return cloned
	case []any:
		cloned := make([]any, len(typed))
		for index, child := range typed {
			cloned[index] = cloneValue(child)
		}
		return cloned
	default:
		return value
	}
}

func validateModel(model object) error {
	if err := requireString(model, "slug"); err != nil {
		return err
	}
	if slug, _ := model["slug"].(string); strings.TrimSpace(slug) == "" {
		return errors.New("slug must be non-empty")
	}
	if err := requireString(model, "display_name"); err != nil {
		return err
	}
	if err := requireBool(model, "supported_in_api"); err != nil {
		return err
	}
	if err := requireBool(model, "support_verbosity"); err != nil {
		return err
	}
	if err := requireInt(model, "priority", 32); err != nil {
		return err
	}
	if err := requireArray(model, "supported_reasoning_levels"); err != nil {
		return err
	}
	if err := requireArray(model, "experimental_supported_tools"); err != nil {
		return err
	}
	if err := validateStringArray("experimental_supported_tools", model["experimental_supported_tools"]); err != nil {
		return err
	}
	if err := requireObject(model, "truncation_policy"); err != nil {
		return err
	}
	if err := validateEnum(model, "shell_type", true, false, "unified_exec", "shell_command", "default", "local", "disabled"); err != nil {
		return err
	}
	if err := validateEnum(model, "visibility", true, false, "list", "hide", "none"); err != nil {
		return err
	}
	if err := validateTruncationPolicy(model["truncation_policy"]); err != nil {
		return err
	}
	if err := validateReasoningLevels(model); err != nil {
		return err
	}
	for field, rule := range map[string]struct {
		nullable bool
		allowed  []string
	}{
		"default_reasoning_summary": {allowed: []string{"none", "auto", "concise", "detailed"}},
		"default_verbosity":         {nullable: true, allowed: []string{"low", "medium", "high"}},
		"apply_patch_tool_type":     {nullable: true, allowed: []string{"freeform"}},
		"web_search_tool_type":      {allowed: []string{"text", "text_and_image"}},
		"tool_mode":                 {nullable: true, allowed: []string{"direct", "code_mode", "code_mode_only"}},
		"multi_agent_version":       {nullable: true, allowed: []string{"disabled", "v1", "v2"}},
	} {
		if err := validateEnum(model, field, false, rule.nullable, rule.allowed...); err != nil {
			return err
		}
	}
	if err := validateOptionalString(model, "description"); err != nil {
		return err
	}
	for _, field := range []string{"default_reasoning_level", "default_service_tier", "comp_hash", "model_specialty", "auto_review_model_override", "base_instructions", "multi_agent_reasoning_effort"} {
		if err := validateOptionalString(model, field); err != nil {
			return err
		}
	}
	for _, field := range []string{"context_window", "max_context_window", "auto_compact_token_limit"} {
		if err := validateOptionalPositiveInt(model, field); err != nil {
			return err
		}
	}
	if contextWindow, ok := intField(model, "context_window"); ok {
		if maxContextWindow, maxOK := intField(model, "max_context_window"); maxOK && contextWindow > maxContextWindow {
			return errors.New("context_window must not exceed max_context_window")
		}
	}
	if value, present := model["effective_context_window_percent"]; present {
		percent, ok := intValue(value)
		if !ok || percent < 1 || percent > 100 {
			return errors.New("effective_context_window_percent must be between 1 and 100")
		}
	}
	for _, field := range []string{"additional_speed_tiers", "input_modalities"} {
		if value, present := model[field]; present {
			if err := validateStringArray(field, value); err != nil {
				return err
			}
		}
	}
	if modalities, present := model["input_modalities"]; present {
		for _, modalityValue := range modalities.([]any) {
			modality := modalityValue.(string)
			if modality != "text" && modality != "image" && modality != "audio" {
				return errors.New("input_modalities contains an unsupported value")
			}
		}
	}
	for _, field := range []string{"include_skills_usage_instructions", "include_plugin_usage_instructions", "include_apps_usage_instructions", "supports_reasoning_summary_parameter", "supports_image_detail_original", "supports_search_tool", "supports_experimental_context", "use_responses_lite", "supports_reasoning_effort_updates", "node_repl_auto_review_required", "node_repl_disabled"} {
		if err := validateOptionalBool(model, field); err != nil {
			return err
		}
	}
	for _, field := range []string{"guardian", "available_access_programs", "upgrade", "model_messages"} {
		if err := validateOptionalObject(model, field); err != nil {
			return err
		}
	}
	if value, present := model["availability_nux"]; present && value != nil {
		if err := validateMessageObject(value, "availability_nux"); err != nil {
			return err
		}
	}
	if value, present := model["service_tiers"]; present {
		if err := validateServiceTiers(value); err != nil {
			return err
		}
	}
	if value, present := model["model_messages"]; present && value != nil {
		if err := validateModelMessages(value); err != nil {
			return err
		}
	}
	if !hasInstructionTemplate(model) {
		return errors.New("model requires model_messages.instructions_template or legacy base_instructions")
	}
	return nil
}

func hasInstructionTemplate(model object) bool {
	if _, ok := model["base_instructions"].(string); ok {
		return true
	}
	messages, ok := model["model_messages"].(object)
	if !ok {
		return false
	}
	_, ok = messages["instructions_template"].(string)
	return ok
}

func validateReasoningLevels(model object) error {
	levels := model["supported_reasoning_levels"].([]any)
	seen := make(map[string]struct{}, len(levels))
	for _, value := range levels {
		level, ok := value.(object)
		if !ok {
			return errors.New("supported_reasoning_levels entries must be objects")
		}
		effort, ok := level["effort"].(string)
		if !ok || strings.TrimSpace(effort) == "" {
			return errors.New("reasoning effort must be a non-empty string")
		}
		if _, duplicate := seen[effort]; duplicate {
			return errors.New("supported_reasoning_levels efforts must be unique")
		}
		seen[effort] = struct{}{}
		if _, ok := level["description"].(string); !ok {
			return errors.New("reasoning effort description must be a string")
		}
	}
	if value, present := model["default_reasoning_level"]; present && value != nil {
		defaultEffort, ok := value.(string)
		if !ok || strings.TrimSpace(defaultEffort) == "" {
			return errors.New("default_reasoning_level must be a non-empty string or null")
		}
		if _, supported := seen[defaultEffort]; !supported {
			return errors.New("default_reasoning_level must be supported by the model")
		}
	}
	return nil
}

func validateTruncationPolicy(value any) error {
	policy, ok := value.(object)
	if !ok {
		return errors.New("truncation_policy must be an object")
	}
	mode, ok := policy["mode"].(string)
	if !ok || (mode != "bytes" && mode != "tokens") {
		return errors.New("truncation_policy mode must be bytes or tokens")
	}
	limit, ok := intValue(policy["limit"])
	if !ok || limit <= 0 {
		return errors.New("truncation_policy limit must be a positive integer")
	}
	return nil
}

func validateServiceTiers(value any) error {
	tiers, ok := value.([]any)
	if !ok {
		return errors.New("service_tiers must be an array")
	}
	for _, value := range tiers {
		tier, ok := value.(object)
		if !ok {
			return errors.New("service_tiers entries must be objects")
		}
		for _, field := range []string{"id", "name", "description"} {
			if _, ok := tier[field].(string); !ok {
				return fmt.Errorf("service_tiers entries require string %s", field)
			}
		}
	}
	return nil
}

func validateModelMessages(value any) error {
	messages, ok := value.(object)
	if !ok {
		return errors.New("model_messages must be an object")
	}
	for _, field := range []string{"content_filter_guidance", "persistent_instructions", "instructions_template"} {
		if err := validateOptionalString(messages, field); err != nil {
			return fmt.Errorf("model_messages.%s must be a string or null", field)
		}
	}
	for _, field := range []string{"tools", "instructions_variables", "approvals", "collaboration_modes", "auto_review", "permissions", "multi_agent", "token_budget", "guardian_v2", "confirmation_policies"} {
		if err := validateOptionalObject(messages, field); err != nil {
			return fmt.Errorf("model_messages.%s must be an object or null", field)
		}
	}
	return nil
}

func validateMessageObject(value any, field string) error {
	message, ok := value.(object)
	if !ok {
		return fmt.Errorf("%s must be an object or null", field)
	}
	if _, ok := message["message"].(string); !ok {
		return fmt.Errorf("%s.message must be a string", field)
	}
	return nil
}

func validateEnum(value object, field string, required bool, nullable bool, allowed ...string) error {
	raw, present := value[field]
	if !present {
		if !required {
			return nil
		}
		return fmt.Errorf("%s is required", field)
	}
	if raw == nil {
		if nullable {
			return nil
		}
		return fmt.Errorf("%s must be a string", field)
	}
	text, ok := raw.(string)
	if !ok {
		return fmt.Errorf("%s must be a string", field)
	}
	for _, candidate := range allowed {
		if text == candidate {
			return nil
		}
	}
	return fmt.Errorf("%s has an unsupported value", field)
}

func requireString(value object, field string) error {
	if _, ok := value[field].(string); !ok {
		return fmt.Errorf("%s is required and must be a string", field)
	}
	return nil
}

func requireBool(value object, field string) error {
	if _, ok := value[field].(bool); !ok {
		return fmt.Errorf("%s is required and must be a boolean", field)
	}
	return nil
}

func requireInt(value object, field string, bits int) error {
	if _, ok := intValue(value[field]); !ok {
		return fmt.Errorf("%s is required and must be an integer", field)
	}
	parsed, _ := intValue(value[field])
	if bits == 32 && (parsed < -1<<31 || parsed > 1<<31-1) {
		return fmt.Errorf("%s is outside the supported range", field)
	}
	return nil
}

func requireArray(value object, field string) error {
	if _, ok := value[field].([]any); !ok {
		return fmt.Errorf("%s is required and must be an array", field)
	}
	return nil
}

func requireObject(value object, field string) error {
	if _, ok := value[field].(object); !ok {
		return fmt.Errorf("%s is required and must be an object", field)
	}
	return nil
}

func validateOptionalString(value object, field string) error {
	raw, present := value[field]
	if !present || raw == nil {
		return nil
	}
	if _, ok := raw.(string); !ok {
		return fmt.Errorf("%s must be a string or null", field)
	}
	return nil
}

func validateOptionalBool(value object, field string) error {
	raw, present := value[field]
	if !present {
		return nil
	}
	if _, ok := raw.(bool); !ok {
		return fmt.Errorf("%s must be a boolean", field)
	}
	return nil
}

func validateOptionalObject(value object, field string) error {
	raw, present := value[field]
	if !present || raw == nil {
		return nil
	}
	if _, ok := raw.(object); !ok {
		return fmt.Errorf("%s must be an object or null", field)
	}
	return nil
}

func validateOptionalPositiveInt(value object, field string) error {
	raw, present := value[field]
	if !present || raw == nil {
		return nil
	}
	integer, ok := intValue(raw)
	if !ok || integer <= 0 {
		return fmt.Errorf("%s must be a positive integer or null", field)
	}
	return nil
}

func validateStringArray(field string, value any) error {
	items, ok := value.([]any)
	if !ok {
		return fmt.Errorf("%s must be an array", field)
	}
	for _, item := range items {
		if _, ok := item.(string); !ok {
			return fmt.Errorf("%s entries must be strings", field)
		}
	}
	return nil
}

func intField(value object, field string) (int64, bool) {
	raw, present := value[field]
	if !present || raw == nil {
		return 0, false
	}
	integer, ok := intValue(raw)
	return integer, ok
}

func intValue(value any) (int64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	integer, err := strconv.ParseInt(string(number), 10, 64)
	return integer, err == nil
}
