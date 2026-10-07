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

type overrideSet struct {
	defaults object
	models   object
}

// ValidateOverrides checks an inline sparse Codex model patch.
func ValidateOverrides(data []byte) error {
	overrides, err := parseOverrides(data)
	if err != nil {
		return err
	}
	if err := validateModelPatch(overrides.defaults, "synthetic-default"); err != nil {
		return fmt.Errorf("catalog: invalid defaults: %w", err)
	}
	for slug, value := range overrides.models {
		patch, ok := value.(object)
		if !ok {
			return errors.New("catalog: each per-model override must be an object")
		}
		if strings.TrimSpace(slug) == "" {
			return errors.New("catalog: model override slugs must be non-empty")
		}
		combined := merge(cloneValue(overrides.defaults).(object), patch)
		if err := validateModelPatch(combined, slug); err != nil {
			return fmt.Errorf("catalog: invalid model override: %w", err)
		}
	}
	return nil
}

// PatchGeneratedResponse applies sparse overrides to a host-generated Codex catalog.
func PatchGeneratedResponse(response, overrideData []byte) ([]byte, bool, error) {
	rootValue, err := decodeJSON(response)
	if err != nil {
		return nil, false, errors.New("catalog: invalid generated catalog JSON")
	}
	root, ok := rootValue.(object)
	if !ok {
		return nil, false, nil
	}
	modelsValue, ok := root["models"]
	if !ok {
		return nil, false, nil
	}
	models, ok := modelsValue.([]any)
	if _, genericList := root["data"]; genericList || !ok || !isCodexModels(models) {
		return nil, false, nil
	}
	overrides, err := parseOverrides(overrideData)
	if err != nil {
		return nil, true, err
	}
	if len(models) == 0 || len(overrides.defaults) == 0 && len(overrides.models) == 0 {
		return append([]byte(nil), response...), true, nil
	}
	changed := false
	for index, value := range models {
		model := value.(object)
		slug := model["slug"].(string)
		patchValue, hasPatch := overrides.models[slug]
		patch, hasPatch := patchValue.(object)
		if len(overrides.defaults) == 0 && (!hasPatch || len(patch) == 0) {
			continue
		}
		merged := merge(model, overrides.defaults)
		if hasPatch {
			merged = merge(merged, patch)
		}
		if err := validateModel(merged); err != nil {
			return nil, true, fmt.Errorf("catalog: invalid override for generated model %d", index)
		}
		models[index] = merged
		changed = true
	}
	if !changed {
		return append([]byte(nil), response...), true, nil
	}
	encoded, err := json.Marshal(root)
	if err != nil {
		return nil, true, errors.New("catalog: unable to encode generated model catalog")
	}
	return encoded, true, nil
}

func isCodexModels(models []any) bool {
	for _, value := range models {
		model, ok := value.(object)
		if !ok {
			return false
		}
		if slug, ok := model["slug"].(string); !ok || strings.TrimSpace(slug) == "" {
			return false
		}
		if _, ok := model["model_messages"].(object); !ok {
			return false
		}
		if _, ok := model["supported_reasoning_levels"].([]any); !ok {
			return false
		}
	}
	return true
}

func parseOverrides(data []byte) (overrideSet, error) {
	value, err := decodeJSON(data)
	if err != nil {
		return overrideSet{}, errors.New("catalog: invalid overrides JSON")
	}
	root, ok := value.(object)
	if !ok {
		return overrideSet{}, errors.New("catalog: overrides must be an object")
	}
	for key := range root {
		if key != "defaults" && key != "models" {
			return overrideSet{}, errors.New("catalog: overrides contain an unsupported top-level field")
		}
	}
	defaults := object{}
	if value, present := root["defaults"]; present {
		defaults, ok = value.(object)
		if !ok {
			return overrideSet{}, errors.New("catalog: defaults must be an object")
		}
		if _, changesSlug := defaults["slug"]; changesSlug {
			return overrideSet{}, errors.New("catalog: defaults cannot change model slugs")
		}
	}
	models := object{}
	if value, present := root["models"]; present {
		models, ok = value.(object)
		if !ok {
			return overrideSet{}, errors.New("catalog: per-model overrides must be an object")
		}
	}
	for slug, value := range models {
		patch, ok := value.(object)
		if !ok {
			return overrideSet{}, errors.New("catalog: each per-model override must be an object")
		}
		if patchSlug, present := patch["slug"]; present && patchSlug != slug {
			return overrideSet{}, errors.New("catalog: per-model override cannot change a model slug")
		}
	}
	return overrideSet{defaults: defaults, models: models}, nil
}

func validateModelPatch(patch object, slug string) error {
	model := object{
		"slug":                         slug,
		"display_name":                 "Synthetic model",
		"supported_in_api":             true,
		"support_verbosity":            false,
		"priority":                     json.Number("1"),
		"supported_reasoning_levels":   []any{object{"effort": "low", "description": "Synthetic"}},
		"experimental_supported_tools": []any{},
		"truncation_policy":            object{"mode": "tokens", "limit": json.Number("1")},
		"shell_type":                   "shell_command",
		"visibility":                   "list",
		"model_messages":               object{"instructions_template": "Synthetic instructions"},
	}
	if _, hasUpgradePatch := patch["upgrade"].(object); hasUpgradePatch {
		model["upgrade"] = object{
			"model":              "Synthetic upgrade model",
			"migration_markdown": "Synthetic migration notes",
		}
	}
	if _, hasAvailabilityNuxPatch := patch["availability_nux"].(object); hasAvailabilityNuxPatch {
		model["availability_nux"] = object{"message": "Synthetic availability message"}
	}
	if _, hasAccessProgramsPatch := patch["available_access_programs"].(object); hasAccessProgramsPatch {
		model["available_access_programs"] = object{"cyber": []any{}}
	}
	if messages, ok := patch["model_messages"].(object); ok {
		if _, hasTokenBudgetPatch := messages["token_budget"].(object); hasTokenBudgetPatch {
			modelMessages := model["model_messages"].(object)
			modelMessages["token_budget"] = object{
				"reminder_threshold_tokens":           json.Number("1"),
				"auto_compact_fallback_buffer_tokens": json.Number("1"),
				"reminder_message_template":           "Synthetic reminder",
				"guidance_message":                    "Synthetic guidance",
				"auto_compact_fallback_prompt":        "Synthetic fallback prompt",
			}
		}
	}
	if _, hasLevels := patch["supported_reasoning_levels"]; !hasLevels {
		if defaultEffort, ok := patch["default_reasoning_level"].(string); ok && defaultEffort != "" {
			model["supported_reasoning_levels"] = []any{object{"effort": defaultEffort, "description": "Synthetic"}}
		}
	}
	merged := merge(model, patch)
	return validateModel(merged)
}

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
	overridesSet, err := parseOverrides(overrides)
	if err != nil {
		return nil, err
	}
	defaults := overridesSet.defaults
	modelOverrides := overridesSet.models
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
			merged = merge(merged, patchValue.(object))
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
	for _, field := range []string{"default_reasoning_level", "default_service_tier", "comp_hash", "model_specialty", "auto_review_model_override", "base_instructions"} {
		if err := validateOptionalString(model, field); err != nil {
			return err
		}
	}
	if err := validateOptionalReasoningEffort(model, "multi_agent_reasoning_effort"); err != nil {
		return err
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
	if err := validateOptionalObjectValue(model, "guardian", validateGuardianPolicy); err != nil {
		return err
	}
	if err := validateOptionalObjectValue(model, "available_access_programs", validateAccessPrograms); err != nil {
		return err
	}
	if err := validateOptionalObjectValue(model, "upgrade", validateUpgrade); err != nil {
		return err
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
	if err := validateOptionalObjectValue(model, "model_messages", validateModelMessages); err != nil {
		return err
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
		if !ok || effort == "" {
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
		if !ok || defaultEffort == "" {
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

func validateModelMessages(messages object) error {
	for _, field := range []string{"content_filter_guidance", "persistent_instructions", "instructions_template"} {
		if err := validateOptionalString(messages, field); err != nil {
			return fmt.Errorf("model_messages.%w", err)
		}
	}
	validators := []struct {
		field    string
		validate func(object) error
	}{
		{field: "tools", validate: validateToolMessages},
		{field: "instructions_variables", validate: validateInstructionsVariables},
		{field: "approvals", validate: validateApprovalMessages},
		{field: "collaboration_modes", validate: validateCollaborationModeMessages},
		{field: "auto_review", validate: validateAutoReviewMessages},
		{field: "permissions", validate: validatePermissionMessages},
		{field: "multi_agent", validate: validateMultiAgentMessages},
		{field: "token_budget", validate: validateTokenBudget},
		{field: "guardian_v2", validate: validateGuardianV2},
		{field: "confirmation_policies", validate: validateConfirmationPolicies},
	}
	for _, validator := range validators {
		if err := validateOptionalObjectValue(messages, validator.field, validator.validate); err != nil {
			return fmt.Errorf("model_messages.%w", err)
		}
	}
	return nil
}

func validateGuardianPolicy(policy object) error {
	for _, field := range []string{"computer_use", "shell", "file_changes", "mcp", "network", "permissions"} {
		if err := validateOptionalString(policy, field); err != nil {
			return fmt.Errorf("guardian.%w", err)
		}
	}
	for _, field := range []string{"other_tools", "unscored_action"} {
		if raw, present := policy[field]; present {
			if _, ok := raw.(string); !ok {
				return fmt.Errorf("guardian.%s must be a string", field)
			}
		}
	}
	if err := validateNullableBoolFields(policy, "initial_cua_call", "sandboxed_exec_commands"); err != nil {
		return fmt.Errorf("guardian.%w", err)
	}
	return nil
}

func validateAccessPrograms(programs object) error {
	if err := requireArray(programs, "cyber"); err != nil {
		return fmt.Errorf("available_access_programs.%w", err)
	}
	if err := validateStringArray("cyber", programs["cyber"]); err != nil {
		return fmt.Errorf("available_access_programs.%w", err)
	}
	return nil
}

func validateUpgrade(upgrade object) error {
	for _, field := range []string{"model", "migration_markdown"} {
		if err := requireString(upgrade, field); err != nil {
			return fmt.Errorf("upgrade.%w", err)
		}
	}
	return nil
}

func validateToolMessages(tools object) error {
	if err := validateOptionalObjectValue(tools, "indirect_description_prefixes", validateIndirectDescriptionPrefixes); err != nil {
		return fmt.Errorf("tools.%w", err)
	}
	if err := validateOptionalObjectValue(tools, "send_user_message_async", validateToolMessage); err != nil {
		return fmt.Errorf("tools.%w", err)
	}
	if err := validateOptionalObjectValue(tools, "multi_agent", validateMultiAgentToolMessages); err != nil {
		return fmt.Errorf("tools.%w", err)
	}
	if err := validateOptionalObjectValue(tools, "code_mode", validateCodeModeToolMessages); err != nil {
		return fmt.Errorf("tools.%w", err)
	}
	if err := validateOptionalObjectValue(tools, "mcp_resources", validateMcpResourceToolMessages); err != nil {
		return fmt.Errorf("tools.%w", err)
	}
	return nil
}

func validateIndirectDescriptionPrefixes(prefixes object) error {
	for _, field := range []string{"namespaces", "mcp_servers"} {
		raw, present := prefixes[field]
		if !present || raw == nil {
			continue
		}
		values, ok := raw.(object)
		if !ok {
			return fmt.Errorf("indirect_description_prefixes.%s must be an object or null", field)
		}
		for _, value := range values {
			if _, ok := value.(string); !ok {
				return fmt.Errorf("indirect_description_prefixes.%s values must be strings", field)
			}
		}
	}
	return nil
}

func validateToolMessage(message object) error {
	if err := validateOptionalStringFields(message, "description", "parameters"); err != nil {
		return fmt.Errorf("tool message.%w", err)
	}
	return nil
}

func validateMultiAgentToolMessages(messages object) error {
	for _, field := range []string{"spawn_agent", "send_message", "followup_task", "wait_agent", "interrupt_agent", "list_agents", "create_channel", "get_channels", "list_threads", "search_posts", "read_thread", "read_post", "subscribe", "unsubscribe", "post"} {
		if err := validateOptionalObjectValue(messages, field, validateToolMessage); err != nil {
			return fmt.Errorf("multi_agent.%w", err)
		}
	}
	return nil
}

func validateCodeModeToolMessages(messages object) error {
	for _, field := range []string{"exec", "wait"} {
		if err := validateOptionalObjectValue(messages, field, validateToolMessage); err != nil {
			return fmt.Errorf("code_mode.%w", err)
		}
	}
	return validateOptionalStringFields(messages, "deferred_nested_tools_guidance", "mcp_typescript_preamble")
}

func validateMcpResourceToolMessages(messages object) error {
	for _, field := range []string{"list_mcp_resources", "list_mcp_resource_templates", "read_mcp_resource"} {
		if err := validateOptionalObjectValue(messages, field, validateToolMessage); err != nil {
			return fmt.Errorf("mcp_resources.%w", err)
		}
	}
	return nil
}

func validateInstructionsVariables(variables object) error {
	return validateOptionalStringFields(variables, "personality_default", "personality_friendly", "personality_pragmatic")
}

func validateApprovalMessages(messages object) error {
	return validateOptionalStringFields(messages, "on_request", "on_request_auto_review", "never", "unless_trusted")
}

func validateCollaborationModeMessages(messages object) error {
	return validateOptionalStringFields(messages, "default", "plan")
}

func validateAutoReviewMessages(messages object) error {
	return validateOptionalStringFields(messages, "policy", "policy_template", "node_repl_policy", "rejection_instructions", "timeout_instructions")
}

func validatePermissionMessages(messages object) error {
	return validateOptionalStringFields(messages, "danger_full_access", "workspace_write", "read_only")
}

func validateMultiAgentMessages(messages object) error {
	if err := validateOptionalObjectValue(messages, "role", validateMultiAgentRoleMessages); err != nil {
		return fmt.Errorf("multi_agent.%w", err)
	}
	if err := validateOptionalObjectValue(messages, "mode", validateMultiAgentModeMessages); err != nil {
		return fmt.Errorf("multi_agent.%w", err)
	}
	return nil
}

func validateMultiAgentRoleMessages(messages object) error {
	return validateOptionalStringFields(messages, "root", "subagent")
}

func validateMultiAgentModeMessages(messages object) error {
	return validateOptionalStringFields(messages, "explicit", "proactive", "hint_text")
}

func validateTokenBudget(budget object) error {
	if err := validateOptionalBool(budget, "enabled"); err != nil {
		return fmt.Errorf("token_budget.%w", err)
	}
	if err := validateOptionalBool(budget, "use_history_notes_extension"); err != nil {
		return fmt.Errorf("token_budget.%w", err)
	}
	for _, field := range []string{"reminder_threshold_tokens", "auto_compact_fallback_buffer_tokens"} {
		if err := requireInt(budget, field, 64); err != nil {
			return fmt.Errorf("token_budget.%w", err)
		}
	}
	for _, field := range []string{"reminder_message_template", "guidance_message", "auto_compact_fallback_prompt"} {
		if err := requireString(budget, field); err != nil {
			return fmt.Errorf("token_budget.%w", err)
		}
	}
	return nil
}

func validateGuardianV2(config object) error {
	if err := validateEnum(config, "async_classifier_mode", false, true, "snapshot", "conversation"); err != nil {
		return fmt.Errorf("guardian_v2.%w", err)
	}
	if err := validateOptionalString(config, "classifier_instructions"); err != nil {
		return fmt.Errorf("guardian_v2.%w", err)
	}
	if err := validateOptionalReasoningEffort(config, "reasoning_effort"); err != nil {
		return fmt.Errorf("guardian_v2.%w", err)
	}
	for _, field := range []string{"async_classifier_conversation_token_limit", "max_tool_call_lag", "max_action_tokens", "max_classifier_instruction_tokens", "max_parent_compaction_tokens"} {
		if err := validateOptionalUnsignedInt(config, field, 64); err != nil {
			return fmt.Errorf("guardian_v2.%w", err)
		}
	}
	if err := validateOptionalUnsignedInt(config, "review_threshold_basis_points", 16); err != nil {
		return fmt.Errorf("guardian_v2.%w", err)
	}
	if err := validateNullableBoolFields(config, "reuse_parent_compaction"); err != nil {
		return fmt.Errorf("guardian_v2.%w", err)
	}
	if err := validateOptionalObjectValue(config, "transcript", validateGuardianV2Transcript); err != nil {
		return fmt.Errorf("guardian_v2.%w", err)
	}
	return nil
}

func validateGuardianV2Transcript(transcript object) error {
	if sources, present := transcript["sources"]; present && sources != nil {
		if err := validateStringArray("sources", sources); err != nil {
			return fmt.Errorf("transcript.%w", err)
		}
	}
	if err := validateNullableBoolFields(transcript, "include_images"); err != nil {
		return fmt.Errorf("transcript.%w", err)
	}
	for _, field := range []string{"max_message_entry_tokens", "max_tool_entry_tokens", "max_message_transcript_tokens", "max_tool_transcript_tokens", "max_recent_non_user_entries"} {
		if err := validateOptionalUnsignedInt(transcript, field, 64); err != nil {
			return fmt.Errorf("transcript.%w", err)
		}
	}
	return nil
}

func validateConfirmationPolicies(policies object) error {
	return validateOptionalStringFields(policies, "browser_use", "computer_use")
}

func validateOptionalObjectValue(value object, field string, validate func(object) error) error {
	raw, present := value[field]
	if !present || raw == nil {
		return nil
	}
	nested, ok := raw.(object)
	if !ok {
		return fmt.Errorf("%s must be an object or null", field)
	}
	if err := validate(nested); err != nil {
		return fmt.Errorf("%s.%w", field, err)
	}
	return nil
}

func validateOptionalStringFields(value object, fields ...string) error {
	for _, field := range fields {
		if err := validateOptionalString(value, field); err != nil {
			return err
		}
	}
	return nil
}

func validateOptionalReasoningEffort(value object, field string) error {
	raw, present := value[field]
	if !present || raw == nil {
		return nil
	}
	effort, ok := raw.(string)
	if !ok || effort == "" {
		return fmt.Errorf("%s must be a non-empty string or null", field)
	}
	return nil
}

func validateNullableBoolFields(value object, fields ...string) error {
	for _, field := range fields {
		raw, present := value[field]
		if !present || raw == nil {
			continue
		}
		if _, ok := raw.(bool); !ok {
			return fmt.Errorf("%s must be a boolean or null", field)
		}
	}
	return nil
}

func validateOptionalUnsignedInt(value object, field string, bits int) error {
	raw, present := value[field]
	if !present || raw == nil {
		return nil
	}
	number, ok := raw.(json.Number)
	if !ok {
		return fmt.Errorf("%s must be an unsigned integer or null", field)
	}
	if _, err := strconv.ParseUint(string(number), 10, bits); err != nil {
		return fmt.Errorf("%s must be an unsigned integer or null", field)
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
