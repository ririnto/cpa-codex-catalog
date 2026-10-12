package integration

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newNativeHTTPClient(t *testing.T) *http.Client {
	t.Helper()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	return client
}

// TestNativeHTTPClientBoundsStalledResponses verifies timeouts cover both headers and body reads and fixture handlers are joined.
func TestNativeHTTPClientBoundsStalledResponses(t *testing.T) {
	for _, stage := range []string{"headers", "body"} {
		t.Run(stage, func(t *testing.T) {
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if stage == "body" {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				<-release
			}))
			defer server.Close()
			defer close(release)
			client := newNativeHTTPClient(t)
			response, err := client.Get(server.URL)
			if stage == "body" {
				if err != nil {
					t.Fatalf("receive fixture headers: %v", err)
				}
				_, err = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
			}
			if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
				t.Fatalf("stalled %s error = %v, want a request timeout", stage, err)
			}
		})
	}
}
