package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/ririnto/cpa-codex-catalog/internal/catalog"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

const pluginID = "cpa-codex-catalog"

type pluginService struct {
	current atomic.Pointer[serviceSnapshot]
}

type serviceSnapshot struct {
	enabled   bool
	overrides []byte
}

type configFile struct {
	Enabled  *bool                     `yaml:"enabled"`
	Priority int                       `yaml:"priority"`
	Defaults map[string]any            `yaml:"defaults"`
	Models   map[string]map[string]any `yaml:"models"`
}

func newPluginService() *pluginService {
	return &pluginService{}
}

func (s *pluginService) configure(raw []byte) error {
	config, err := parseConfig(raw)
	if err != nil {
		return errors.New("invalid plugin configuration")
	}
	defaults := config.Defaults
	if defaults == nil {
		defaults = map[string]any{}
	}
	models := config.Models
	if models == nil {
		models = map[string]map[string]any{}
	}
	overrides, err := json.Marshal(map[string]any{"defaults": defaults, "models": models})
	if err != nil || catalog.ValidateOverrides(overrides) != nil {
		return errors.New("invalid plugin configuration")
	}
	enabled := true
	if config.Enabled != nil {
		enabled = *config.Enabled
	}
	s.current.Store(&serviceSnapshot{enabled: enabled, overrides: overrides})
	return nil
}

func parseConfig(raw []byte) (configFile, error) {
	var config configFile
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return configFile{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return configFile{}, errors.New("multiple configuration documents")
	}
	return config, nil
}

func currentRegistration() registration {
	result := registration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             pluginID,
			Version:          pluginVersion,
			Author:           "ririnto",
			GitHubRepository: "https://github.com/ririnto/cpa-codex-catalog",
			ConfigFields: []pluginapi.ConfigField{
				{Name: "enabled", Type: pluginapi.ConfigFieldTypeBoolean, Description: "Enables sparse overrides for generated Codex model catalogs."},
				{Name: "defaults", Type: pluginapi.ConfigFieldTypeObject, Description: "Model fields applied to every host-generated Codex model."},
				{Name: "models", Type: pluginapi.ConfigFieldTypeObject, Description: "Sparse model fields keyed by exact Codex model slug."},
			},
		},
	}
	result.Capabilities.ResponseInterceptor = true
	return result
}

func (s *pluginService) InterceptResponse(_ context.Context, request pluginapi.ResponseInterceptRequest) (pluginapi.ResponseInterceptResponse, error) {
	passThrough := pluginapi.ResponseInterceptResponse{Headers: request.ResponseHeaders, Body: request.Body}
	if request.StatusCode != http.StatusOK || request.Stream || request.SourceFormat != "openai" || request.Model != "" || request.RequestedModel != "" || len(request.RequestBody) != 0 || len(request.OriginalRequest) != 0 {
		return passThrough, nil
	}
	snapshot := s.current.Load()
	if snapshot == nil || !snapshot.enabled {
		return passThrough, nil
	}
	patched, isCodexCatalog, err := catalog.PatchGeneratedResponse(request.Body, snapshot.overrides)
	if err != nil || !isCodexCatalog || len(patched) == 0 {
		return passThrough, err
	}
	if bytes.Equal(patched, request.Body) {
		return passThrough, nil
	}
	passThrough.Headers = headersWithout(request.ResponseHeaders, "Content-Length", "ETag")
	passThrough.Body = patched
	passThrough.ClearHeaders = []string{"Content-Length", "ETag"}
	return passThrough, nil
}

func headersWithout(headers http.Header, excluded ...string) http.Header {
	filtered := make(http.Header, len(headers))
	for name, values := range headers {
		remove := false
		for _, excludedName := range excluded {
			if strings.EqualFold(name, excludedName) {
				remove = true
				break
			}
		}
		if !remove {
			filtered[name] = append([]string(nil), values...)
		}
	}
	return filtered
}
