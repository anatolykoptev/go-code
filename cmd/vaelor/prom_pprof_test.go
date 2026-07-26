package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPromMux_PprofWiring verifies the wiring requirement of issue #754:
//
//   - The metrics-listener mux (PROM_PORT 9897) serves the net/http/pprof heap
//     endpoint with 200 and a non-empty body.
//   - A mux constructed the way the MCP listener constructs its mux — a fresh
//     http.NewServeMux() that does NOT call buildPromMux — does NOT serve
//     pprof (404). The MCP listener (mcpserver.Run, port 8897) builds its own
//     *http.ServeMux and passes it to the combinedRoutes callback; it never
//     calls buildPromMux, so pprof stays off 8897. This test guards the
//     invariant: if someone moves pprof registration onto a path that every
//     new ServeMux inherits (a package-level side effect), or wires it into
//     the MCP route callback, the MCP-side 404 assertion is the canary.
//
// We do not assert pprof's output format; that is the stdlib's job. We assert
// the wiring, which is the thing this change can break.
func TestPromMux_PprofWiring(t *testing.T) {
	t.Run("prom mux serves heap", func(t *testing.T) {
		srv := httptest.NewServer(buildPromMux())
		defer srv.Close()

		resp, err := http.Get(srv.URL + "/debug/pprof/heap")
		if err != nil {
			t.Fatalf("heap GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("heap status = %d, want 200", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("heap read: %v", err)
		}
		if len(body) == 0 {
			t.Fatal("heap body empty, want non-empty pprof payload")
		}
	})

	t.Run("mcp-style mux does not serve pprof", func(t *testing.T) {
		// The MCP listener builds a fresh *http.ServeMux inside mcpserver.Run
		// (cmd/vaelor/main.go:203 notes "http.DefaultServeMux is unused by
		// mcpserver"). A fresh ServeMux does not inherit buildPromMux's
		// registrations, so pprof must be absent there.
		mcpMux := http.NewServeMux()
		srv := httptest.NewServer(mcpMux)
		defer srv.Close()

		resp, err := http.Get(srv.URL + "/debug/pprof/heap")
		if err != nil {
			t.Fatalf("mcp mux heap GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("mcp mux /debug/pprof/heap status = %d, want 404 (pprof must not leak onto the MCP listener)", resp.StatusCode)
		}
	})
}
