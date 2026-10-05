package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/ririnto/cpa-codex-catalog/internal/catalog"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

const pluginID = "cpa-codex-catalog"
const resourcePath = "/v0/resource/plugins/cpa-codex-catalog/models"

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var bearerTokenPattern = regexp.MustCompile(`^[A-Za-z0-9._~+/-]+=*$`)

type pluginService struct {
	current        atomic.Pointer[serviceSnapshot]
	availabilityDo boundedHostHTTPDo
}

type serviceSnapshot struct {
	enabled                 bool
	catalog                 []byte
	bearerHash              [32]byte
	protected               bool
	availableModelsURL      string
	availableModelsTokenEnv string
}

type configFile struct {
	Enabled                 *bool  `yaml:"enabled"`
	Priority                int    `yaml:"priority"`
	CatalogPath             string `yaml:"catalog_path"`
	OverridesPath           string `yaml:"overrides_path"`
	BearerTokenEnv          string `yaml:"bearer_token_env"`
	AvailableModelsURL      string `yaml:"available_models_url"`
	AvailableModelsTokenEnv string `yaml:"available_models_token_env"`
}

func newPluginService(doBounded boundedHostHTTPDo) *pluginService {
	return &pluginService{availabilityDo: doBounded}
}

func (s *pluginService) configure(raw []byte) error {
	config, err := parseConfig(raw)
	if err != nil {
		return errors.New("invalid plugin configuration")
	}
	enabled := true
	if config.Enabled != nil {
		enabled = *config.Enabled
	}
	snapshot := &serviceSnapshot{enabled: enabled}
	if enabled {
		if strings.TrimSpace(config.CatalogPath) == "" {
			return errors.New("invalid plugin configuration")
		}
		if config.AvailableModelsURL != "" && validateAvailableModelsURL(config.AvailableModelsURL) != nil {
			return errors.New("invalid plugin configuration")
		}
		if config.AvailableModelsTokenEnv != "" && !envNamePattern.MatchString(config.AvailableModelsTokenEnv) {
			return errors.New("invalid plugin configuration")
		}
		if config.AvailableModelsURL == "" && config.AvailableModelsTokenEnv != "" {
			return errors.New("invalid plugin configuration")
		}
		catalogBytes, errLoad := catalog.Load(config.CatalogPath, config.OverridesPath)
		if errLoad != nil {
			return errors.New("invalid plugin configuration")
		}
		snapshot.catalog = append([]byte(nil), catalogBytes...)
		snapshot.availableModelsURL = config.AvailableModelsURL
		snapshot.availableModelsTokenEnv = config.AvailableModelsTokenEnv
		if config.BearerTokenEnv != "" {
			if !envNamePattern.MatchString(config.BearerTokenEnv) {
				return errors.New("invalid plugin configuration")
			}
			token, exists := os.LookupEnv(config.BearerTokenEnv)
			if !exists || !bearerTokenPattern.MatchString(token) {
				return errors.New("invalid plugin configuration")
			}
			snapshot.bearerHash = sha256.Sum256([]byte(token))
			snapshot.protected = true
		}
	}
	s.current.Store(snapshot)
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
	if config.BearerTokenEnv != strings.TrimSpace(config.BearerTokenEnv) {
		return configFile{}, errors.New("invalid environment variable name")
	}
	if config.AvailableModelsTokenEnv != strings.TrimSpace(config.AvailableModelsTokenEnv) {
		return configFile{}, errors.New("invalid environment variable name")
	}
	return config, nil
}

func (s *pluginService) registerManagement() pluginapi.ManagementRegistrationResponse {
	snapshot := s.current.Load()
	if snapshot == nil || !snapshot.enabled {
		return pluginapi.ManagementRegistrationResponse{}
	}
	return pluginapi.ManagementRegistrationResponse{Resources: []pluginapi.ResourceRoute{{
		Path:        "/models",
		Menu:        "Codex Model Catalog",
		Description: "Serves the validated Codex model catalog.",
	}}}
}

func (s *pluginService) handleManagement(request pluginapi.ManagementRequest) (pluginapi.ManagementResponse, error) {
	return s.handleManagementWithCallback(request, "")
}

func (s *pluginService) handleManagementWithCallback(request pluginapi.ManagementRequest, callbackID string) (pluginapi.ManagementResponse, error) {
	if request.Path != resourcePath {
		return jsonResponse(http.StatusNotFound, "resource not found", nil), nil
	}
	if request.Method != http.MethodGet {
		return jsonResponse(http.StatusMethodNotAllowed, "method not allowed", http.Header{"Allow": []string{http.MethodGet}}), nil
	}
	snapshot := s.current.Load()
	if snapshot == nil {
		return jsonResponse(http.StatusServiceUnavailable, "resource unavailable", nil), nil
	}
	if !snapshot.enabled {
		return jsonResponse(http.StatusNotFound, "resource not found", nil), nil
	}
	if snapshot.protected && !authorized(request.Headers, snapshot.bearerHash) {
		return jsonResponse(http.StatusUnauthorized, "authorization required", http.Header{"Www-Authenticate": []string{"Bearer"}}), nil
	}
	body := snapshot.catalog
	if snapshot.availableModelsURL != "" {
		availability, err := fetchAvailableModels(s.availabilityDo, callbackID, snapshot.availableModelsURL, snapshot.availableModelsTokenEnv)
		if err != nil {
			return jsonResponse(http.StatusServiceUnavailable, "model availability unavailable", nil), nil
		}
		filtered, err := catalog.IntersectAvailableModels(snapshot.catalog, availability)
		if err != nil {
			return jsonResponse(http.StatusServiceUnavailable, "model availability unavailable", nil), nil
		}
		body = filtered
	}
	return pluginapi.ManagementResponse{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": []string{"application/json; charset=utf-8"}, "Cache-Control": []string{"no-store"}},
		Body:       append([]byte(nil), body...),
	}, nil
}

func authorized(headers http.Header, expectedHash [32]byte) bool {
	values := make([]string, 0, 1)
	for key, headerValues := range headers {
		if strings.EqualFold(key, "Authorization") {
			values = append(values, headerValues...)
		}
	}
	if len(values) != 1 || len(values[0]) < len("Bearer ") || !strings.EqualFold(values[0][:len("Bearer ")], "Bearer ") {
		return false
	}
	token := values[0][len("Bearer "):]
	if !bearerTokenPattern.MatchString(token) {
		return false
	}
	providedHash := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) == 1
}

func jsonResponse(status int, message string, extra http.Header) pluginapi.ManagementResponse {
	headers := http.Header{"Content-Type": []string{"application/json; charset=utf-8"}, "Cache-Control": []string{"no-store"}}
	for key, values := range extra {
		headers[key] = append([]string(nil), values...)
	}
	return pluginapi.ManagementResponse{
		StatusCode: status,
		Headers:    headers,
		Body:       []byte("{\"error\":\"" + message + "\"}"),
	}
}
