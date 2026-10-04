package catalog

import (
	"encoding/json"
	"errors"
)

func IntersectAvailableModels(mergedCatalog, availability []byte) ([]byte, error) {
	availableValue, err := decodeJSON(availability)
	if err != nil {
		return nil, errors.New("catalog: invalid availability response")
	}
	availableRoot, ok := availableValue.(object)
	if !ok {
		return nil, errors.New("catalog: availability response must be an object")
	}
	dataValue, ok := availableRoot["data"]
	if !ok {
		return nil, errors.New("catalog: availability response must contain data")
	}
	data, ok := dataValue.([]any)
	if !ok {
		return nil, errors.New("catalog: availability data must be an array")
	}
	availableIDs := make(map[string]struct{}, len(data))
	excludedIDs := make(map[string]struct{})
	for _, value := range data {
		model, ok := value.(object)
		if !ok {
			return nil, errors.New("catalog: availability model must be an object")
		}
		id, ok := model["id"].(string)
		if !ok || id == "" {
			return nil, errors.New("catalog: availability model must have an id")
		}
		if supportValue, present := model["supported_in_api"]; present {
			supported, ok := supportValue.(bool)
			if !ok {
				return nil, errors.New("catalog: availability model has invalid API support")
			}
			if !supported {
				delete(availableIDs, id)
				excludedIDs[id] = struct{}{}
				continue
			}
		}
		if _, excluded := excludedIDs[id]; excluded {
			continue
		}
		availableIDs[id] = struct{}{}
	}
	mergedValue, err := decodeJSON(mergedCatalog)
	if err != nil {
		return nil, errors.New("catalog: invalid merged catalog")
	}
	mergedRoot, ok := mergedValue.(object)
	if !ok {
		return nil, errors.New("catalog: merged catalog must be an object")
	}
	modelsValue, ok := mergedRoot["models"]
	if !ok {
		return nil, errors.New("catalog: merged catalog must contain models")
	}
	models, ok := modelsValue.([]any)
	if !ok {
		return nil, errors.New("catalog: merged models must be an array")
	}
	filtered := make([]any, 0, len(models))
	seenSlugs := make(map[string]struct{}, len(models))
	for _, value := range models {
		model, ok := value.(object)
		if !ok {
			return nil, errors.New("catalog: merged model must be an object")
		}
		slug, ok := model["slug"].(string)
		if !ok || slug == "" {
			return nil, errors.New("catalog: merged model must have a slug")
		}
		if _, duplicate := seenSlugs[slug]; duplicate {
			return nil, errors.New("catalog: merged model slugs must be unique")
		}
		seenSlugs[slug] = struct{}{}
		supported, ok := model["supported_in_api"].(bool)
		if !ok {
			return nil, errors.New("catalog: merged model must declare API support")
		}
		if !supported {
			continue
		}
		if _, available := availableIDs[slug]; available {
			filtered = append(filtered, model)
		}
	}
	result, err := json.Marshal(object{"models": filtered})
	if err != nil {
		return nil, errors.New("catalog: unable to encode available models")
	}
	if len(result) > MaxCatalogBytes {
		return nil, errors.New("catalog: available models exceed the 1 MiB limit")
	}
	return result, nil
}
