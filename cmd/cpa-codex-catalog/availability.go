package main

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/ririnto/cpa-codex-catalog/internal/catalog"
)

const maxAvailableModelsResponseBytes = catalog.MaxCatalogBytes

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

func fetchAvailableModels(client *http.Client, endpoint, tokenEnv string) ([]byte, error) {
	if client == nil || validateAvailableModelsURL(endpoint) != nil {
		return nil, errors.New("availability request unavailable")
	}
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("availability request unavailable")
	}
	request.Header.Set("Accept", "application/json")
	if tokenEnv != "" {
		if !envNamePattern.MatchString(tokenEnv) {
			return nil, errors.New("availability request unavailable")
		}
		token, exists := os.LookupEnv(tokenEnv)
		if !exists || !bearerTokenPattern.MatchString(token) {
			return nil, errors.New("availability request unavailable")
		}
		request.Header.Set("Authorization", "Bearer "+token)
	}
	// ManagementRequest has no context, so the client uses no active-request deadline.
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("availability request unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || response.ContentLength > maxAvailableModelsResponseBytes {
		return nil, errors.New("availability request unavailable")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAvailableModelsResponseBytes+1))
	if err != nil || len(body) > maxAvailableModelsResponseBytes {
		return nil, errors.New("availability request unavailable")
	}
	return body, nil
}
