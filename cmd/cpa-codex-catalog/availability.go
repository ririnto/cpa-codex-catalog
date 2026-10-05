package main

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/ririnto/cpa-codex-catalog/internal/catalog"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const maxAvailableModelsResponseBytes = catalog.MaxCatalogBytes

type boundedHostHTTPDo func(string, pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error)

func validateAvailableModelsURL(raw string) error {
	if raw == "" || raw != strings.TrimSpace(raw) || strings.Contains(raw, "#") {
		return errors.New("invalid availability URL")
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Opaque != "" || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawFragment != "" {
		return errors.New("invalid availability URL")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return errors.New("invalid availability URL")
	}
	if u.Path != "/v1/models" || u.RawPath != "" {
		return errors.New("invalid availability URL")
	}
	if strings.HasSuffix(u.Host, ":") {
		return errors.New("invalid availability URL")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return errors.New("invalid availability URL")
	}
	for key := range query {
		if strings.EqualFold(key, "client_version") {
			return errors.New("invalid availability URL")
		}
	}
	if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
		return errors.New("invalid availability URL")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func fetchAvailableModels(doBounded boundedHostHTTPDo, callbackID, endpoint, tokenEnv string) ([]byte, error) {
	if doBounded == nil || strings.TrimSpace(callbackID) == "" || validateAvailableModelsURL(endpoint) != nil {
		return nil, errors.New("availability request unavailable")
	}
	request := pluginapi.HTTPRequest{
		Method:           http.MethodGet,
		URL:              endpoint,
		Headers:          http.Header{"Accept": []string{"application/json"}},
		Direct:           true,
		DisableRedirects: true,
		MaxResponseBytes: maxAvailableModelsResponseBytes,
	}
	if tokenEnv != "" {
		if !envNamePattern.MatchString(tokenEnv) {
			return nil, errors.New("availability request unavailable")
		}
		token, exists := os.LookupEnv(tokenEnv)
		if !exists || !bearerTokenPattern.MatchString(token) {
			return nil, errors.New("availability request unavailable")
		}
		request.Headers.Set("Authorization", "Bearer "+token)
	}
	response, err := doBounded(callbackID, request)
	if err != nil {
		return nil, errors.New("availability request unavailable")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || int64(len(response.Body)) > maxAvailableModelsResponseBytes {
		return nil, errors.New("availability request unavailable")
	}
	return append([]byte(nil), response.Body...), nil
}
