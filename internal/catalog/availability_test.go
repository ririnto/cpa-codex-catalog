package catalog

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestIntersectAvailableModelsPreservesMetadataAndCatalogOrder(t *testing.T) {
	alpha := syntheticModel("alpha")
	alpha["custom_metadata"] = map[string]any{"nested": map[string]any{"values": []any{"keep", 3}}}
	blocked := syntheticModel("blocked")
	blocked["supported_in_api"] = false
	disabled := syntheticModel("disabled")
	conflict := syntheticModel("conflict")
	beta := syntheticModel("beta")
	base := baseWithModels(alpha, blocked, disabled, conflict, beta)
	merged, err := Build(base, []byte(`{"defaults":{"display_name":"Configured"}}`))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	availability := []byte(`{"object":"list","data":[{"id":"beta","supported_in_api":true},{"id":"unknown","supported_in_api":true},{"id":"blocked","supported_in_api":true},{"id":"disabled","supported_in_api":false},{"id":"conflict","supported_in_api":true},{"id":"conflict","supported_in_api":false},{"id":"Alpha","supported_in_api":true},{"id":"alpha","object":"model","owned_by":"fixture"},{"id":"alpha","supported_in_api":true}]}`)
	got, err := IntersectAvailableModels(merged, availability)
	if err != nil {
		t.Fatalf("IntersectAvailableModels() error = %v", err)
	}
	var result struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatalf("decode filtered catalog: %v", err)
	}
	if len(result.Models) != 2 || result.Models[0]["slug"] != "alpha" || result.Models[1]["slug"] != "beta" {
		t.Fatalf("filtered models = %#v, want alpha and beta in catalog order", result.Models)
	}
	var mergedRoot struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(merged, &mergedRoot); err != nil {
		t.Fatalf("decode merged catalog: %v", err)
	}
	for _, model := range result.Models {
		var source map[string]any
		for _, candidate := range mergedRoot.Models {
			if candidate["slug"] == model["slug"] {
				source = candidate
				break
			}
		}
		if !reflect.DeepEqual(model, source) {
			t.Fatalf("model %q metadata changed during intersection", model["slug"])
		}
	}
}

func TestIntersectAvailableModelsAcceptsEmptyAvailability(t *testing.T) {
	merged, err := Build(baseWithModels(syntheticModel("alpha")), []byte(`{}`))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	got, err := IntersectAvailableModels(merged, []byte(`{"data":[]}`))
	if err != nil {
		t.Fatalf("IntersectAvailableModels() error = %v", err)
	}
	if string(got) != `{"models":[]}` {
		t.Fatalf("empty intersection = %s, want {\"models\":[]}", got)
	}
}

func TestIntersectAvailableModelsRejectsMalformedResponses(t *testing.T) {
	merged, err := Build(baseWithModels(syntheticModel("alpha")), []byte(`{}`))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	for _, availability := range [][]byte{
		[]byte(`{`),
		[]byte(`[]`),
		[]byte(`{}`),
		[]byte(`{"data":null}`),
		[]byte(`{"data":[null]}`),
		[]byte(`{"data":[{"supported_in_api":true}]}`),
		[]byte(`{"data":[{"id":"alpha","supported_in_api":"true"}]}`),
		[]byte(`{"data":[]} {}`),
		[]byte(`{"data":[],"data":[]}`),
	} {
		if _, err := IntersectAvailableModels(merged, availability); err == nil {
			t.Fatalf("IntersectAvailableModels(%s) succeeded", availability)
		}
	}
}
