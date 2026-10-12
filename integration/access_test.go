package integration

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestNativeHostRejectsUnauthorizedGeneratedCodexCatalogRequests verifies the generated Codex model route requires the configured synthetic API key.
func TestNativeHostRejectsUnauthorizedGeneratedCodexCatalogRequests(t *testing.T) {
	binary, err := filepath.Abs(filepath.Join("..", "build", "native", "cliproxyapi-v8.0.20"))
	if err != nil {
		t.Fatalf("resolve prepared host path: %v", err)
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("native integration requires prepared host artifacts; run `go tool task prepare-native` and `go tool task integration`: %v", err)
	}
	var requests map[string]modelListRequest
	loadSeed(t, "model-list-requests.json", &requests)
	codexRequest := requests["codex"]
	base := startProxy(t, binary, codexRequest, loadSeedBytes(t, "plugin-config.json"))
	for _, testCase := range []struct {
		name          string
		authorization string
	}{
		{name: "missing authorization"},
		{name: "wrong authorization", authorization: "Bearer wrong-fixture-key"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request, err := http.NewRequest(codexRequest.Method, base+codexRequest.Path, nil)
			if err != nil {
				t.Fatalf("construct generated catalog request: %v", err)
			}
			for name, value := range codexRequest.Headers {
				request.Header.Set(name, value)
			}
			request.Header.Del("Authorization")
			if testCase.authorization != "" {
				request.Header.Set("Authorization", testCase.authorization)
			}
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatalf("request generated catalog: %v", err)
			}
			_, readErr := io.Copy(io.Discard, response.Body)
			closeErr := response.Body.Close()
			if readErr != nil {
				t.Errorf("read unauthorized response body: %v", readErr)
			}
			if closeErr != nil {
				t.Errorf("close unauthorized response body: %v", closeErr)
			}
			if response.StatusCode != http.StatusUnauthorized {
				t.Errorf("GET generated catalog status = %d, want %d", response.StatusCode, http.StatusUnauthorized)
			}
		})
	}
}
