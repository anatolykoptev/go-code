// cmd/go-code/tool_resolve_frame.go
package main

import (
	"context"
	"net/http"
	"time"

	"github.com/anatolykoptev/go-kit/ratelimit"
	"github.com/anatolykoptev/vaelor/internal/sourcemap"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// resolveFrameResolver is the package-level Resolver shared by the MCP tool
// and the POST /resolve HTTP endpoint. Initialized once in registerResolveFrame.
var resolveFrameResolver *sourcemap.Resolver

// resolveHTTPRateLimiter is the per-IP rate limiter for POST /resolve. Nil when
// SOURCEMAP_RATE_LIMIT is 0 (default). It is also nil when resolve_frame is
// disabled entirely (SourcemapAllowedHosts empty).
var resolveHTTPRateLimiter *ratelimit.KeyLimiter

// resolveFrameCacheSize is the LRU cache size (entries) for parsed source maps.
const resolveFrameCacheSize = 64

// resolveFrameTTL is the per-entry TTL for cached source maps.
const resolveFrameTTL = 10 * time.Minute

// ResolveFrameInput is the MCP tool input schema for resolve_frame.
type ResolveFrameInput struct {
	// URL is the URL of the minified JS bundle (not the .map file).
	URL string `json:"url" jsonschema:"URL of the minified JS bundle (not the .map file). The companion .map file is resolved automatically. Host must be in the SOURCEMAP_ALLOWED_HOSTS allowlist."`
	// Line is the 1-based line number from the browser stack frame.
	Line int `json:"line" jsonschema:"1-based line number from the browser stack frame"`
	// Column is the 1-based column number from the browser stack frame.
	Column int `json:"column" jsonschema:"1-based column number from the browser stack frame"`
}

// registerResolveFrame registers the resolve_frame MCP tool. If
// cfg.SourcemapAllowedHosts is empty, registration is silently skipped (same
// pattern as debug_investigate with PROMETHEUS_URL/JAEGER_URL).
func registerResolveFrame(server *mcp.Server, cfg Config) {
	if len(cfg.SourcemapAllowedHosts) == 0 {
		return
	}

	resolveFrameResolver = sourcemap.NewResolver(
		&http.Client{Timeout: 15 * time.Second},
		resolveFrameCacheSize,
		resolveFrameTTL,
		cfg.SourcemapMaxBodyBytes,
	)

	if cfg.SourcemapRateLimit > 0 {
		resolveHTTPRateLimiter = ratelimit.NewKeyLimiter(cfg.SourcemapRateLimit, cfg.SourcemapRateBurst)
		resolveHTTPRateLimiter.StartCleanup(1*time.Minute, 5*time.Minute)
	}

	addTool(server, &mcp.Tool{
		Name:        "resolve_frame",
		Description: "Resolve a minified JavaScript stack frame (url, line, column) to its original source location using the companion .map file. Returns file, line, column, and function name.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ResolveFrameInput) (*mcp.CallToolResult, error) {
		return handleResolveFrame(ctx, input, cfg.SourcemapAllowedHosts)
	})
}

func handleResolveFrame(ctx context.Context, input ResolveFrameInput, allowedHosts []string) (*mcp.CallToolResult, error) {
	if !sourcemap.IsAllowedURL(input.URL, allowedHosts) {
		return errResult("resolve_frame: URL host not in SOURCEMAP_ALLOWED_HOSTS allowlist"), nil
	}
	if resolveFrameResolver == nil {
		return errResult("resolve_frame: resolver not initialized"), nil
	}

	frame, err := resolveFrameResolver.Resolve(ctx, input.URL, input.Line, input.Column)
	if err != nil {
		return errResult("resolve_frame: " + err.Error()), nil
	}

	// Route through the shared jsonMarshalResult helper (helpers.go) rather than
	// re-implementing json.Marshal -> textResult; the success path is
	// byte-identical (compact JSON), the marshal-error branch is unreachable for
	// a well-formed Frame.
	return jsonMarshalResult(frame), nil
}
