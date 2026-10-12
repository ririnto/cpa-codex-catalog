package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

// TestResponseInterceptorConcurrentConfigurationSnapshots verifies readers only observe complete valid configurations while writers reconfigure the service.
func TestResponseInterceptorConcurrentConfigurationSnapshots(t *testing.T) {
	service := newPluginService()
	validConfigs := [][]byte{
		[]byte("models:\n  alpha:\n    display_name: Snapshot A\n    description: Description A\n"),
		[]byte("models:\n  alpha:\n    display_name: Snapshot B\n    description: Description B\n"),
	}
	if err := service.configure(validConfigs[0]); err != nil {
		t.Fatalf("configure initial snapshot: %v", err)
	}
	invalidConfig := []byte("models:\n  alpha:\n    display_name: Invalid candidate\n    priority: too-high\n")
	validPairs := map[[2]string]struct{}{
		{"Snapshot A", "Description A"}: {},
		{"Snapshot B", "Description B"}: {},
	}
	const (
		readerCount     = 6
		writerCount     = 3
		readsPerReader  = 100
		writesPerWriter = 40
	)
	start := make(chan struct{})
	var ready sync.WaitGroup
	var workers sync.WaitGroup
	errors := make(chan error, readerCount+writerCount)
	readSnapshot := func() error {
		response, err := service.InterceptResponse(context.Background(), codexListRequest(generatedCodexCatalog))
		if err != nil {
			return fmt.Errorf("InterceptResponse() error: %w", err)
		}
		var catalog struct {
			Models []struct {
				Slug        string `json:"slug"`
				DisplayName string `json:"display_name"`
				Description string `json:"description"`
			} `json:"models"`
		}
		if err := json.Unmarshal(response.Body, &catalog); err != nil {
			return fmt.Errorf("decode intercepted catalog: %w", err)
		}
		if len(catalog.Models) != 1 || catalog.Models[0].Slug != "alpha" {
			return fmt.Errorf("intercepted models = %#v, want the synthetic alpha model", catalog.Models)
		}
		model := catalog.Models[0]
		if _, ok := validPairs[[2]string{model.DisplayName, model.Description}]; !ok {
			return fmt.Errorf("observed model fields (%q, %q) do not match a complete valid snapshot", model.DisplayName, model.Description)
		}
		return nil
	}
	observations := make(chan chan error)
	observerDone := make(chan struct{})
	observationCount := 0
	go func() {
		defer close(observerDone)
		for result := range observations {
			result <- readSnapshot()
			observationCount++
		}
	}()
	observeReload := func() error {
		result := make(chan error)
		observations <- result
		return <-result
	}
	ready.Add(readerCount + writerCount)
	workers.Add(readerCount + writerCount)
	for reader := 0; reader < readerCount; reader++ {
		go func() {
			defer workers.Done()
			ready.Done()
			<-start
			for iteration := 0; iteration < readsPerReader; iteration++ {
				if err := readSnapshot(); err != nil {
					errors <- err
					return
				}
			}
		}()
	}
	for writer := 0; writer < writerCount; writer++ {
		go func(writerIndex int) {
			defer workers.Done()
			ready.Done()
			<-start
			for iteration := 0; iteration < writesPerWriter; iteration++ {
				config := validConfigs[(iteration+writerIndex)%len(validConfigs)]
				if err := service.configure(config); err != nil {
					errors <- fmt.Errorf("configure valid snapshot: %w", err)
					return
				}
				if err := observeReload(); err != nil {
					errors <- err
					return
				}
				if err := service.configure(invalidConfig); err == nil {
					errors <- fmt.Errorf("configure invalid candidate succeeded")
					return
				}
				if err := observeReload(); err != nil {
					errors <- err
					return
				}
			}
		}(writer)
	}
	ready.Wait()
	close(start)
	workers.Wait()
	close(observations)
	<-observerDone
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if observationCount != writerCount*writesPerWriter*2 {
		t.Errorf("observed %d reloads, want %d", observationCount, writerCount*writesPerWriter*2)
	}
	if err := service.configure(invalidConfig); err == nil {
		t.Error("final invalid configuration succeeded")
	}
	if err := readSnapshot(); err != nil {
		t.Error(err)
	}
}
