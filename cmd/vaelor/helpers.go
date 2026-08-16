package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/anatolykoptev/go-kit/llm"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/anatolykoptev/vaelor/internal/policy"
	"github.com/anatolykoptev/vaelor/internal/review"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// xmlCDATA wraps content in a CDATA section to prevent XML escaping of
// source code, tree diagrams, and other content with special characters.
// Use with `xml:",innerxml"` tag on struct fields.
type xmlCDATA struct {
	Inner string `xml:",innerxml"`
}

// wrapCDATA wraps a string in an XML CDATA section, escaping embedded "]]>" sequences.
func wrapCDATA(s string) string {
	s = strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>")
	return "<![CDATA[" + s + "]]>"
}

// maxInlineCharsDefault is the threshold above which output is saved to a file.
const maxInlineCharsDefault = 50_000

// errResult returns a CallToolResult representing a tool-level error.
func errResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}

// textResult returns a CallToolResult with text content.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

// appendMetaFooter returns `body` with an `<!-- meta: <json> -->` footer only
// when `env` carries something actionable (see mcpmeta.Envelope.HasSignal) — a
// bare duration_ms is pure telemetry with zero analytic value to the consumer,
// so it is suppressed to cut response token footprint. Returns body unchanged
// otherwise.
//
// This is the SINGLE render site for the provenance envelope — called once by
// applyBudgetAndTook after budget shaping, from the merged envelope recorded
// by resolveRoot (path signals) and the tool (freshness/hint). No per-tool
// footer-folding helpers remain; the code knows whether provenance was
// recorded, it does not sniff the body for a sentinel.
func appendMetaFooter(body string, env mcpmeta.Envelope) string {
	if !env.HasSignal() {
		return body
	}
	js, err := json.Marshal(env)
	if err != nil {
		return body
	}
	return body + "\n\n<!-- meta: " + string(js) + " -->"
}

// largeTextResult returns a text result, saving to file if content exceeds maxInlineCharsDefault.
// When outputDir is empty or content is small, returns inline text.
// When saved to file, returns a short summary with the file path.
func largeTextResult(text, toolName, outputDir string) *mcp.CallToolResult {
	if outputDir == "" || len(text) <= maxInlineCharsDefault {
		return textResult(text)
	}
	path, ok := saveToFile(text, toolName, outputDir)
	if !ok {
		return textResult(text)
	}
	summary := fmt.Sprintf("%s: output %d chars saved to: %s\n\nUse Read tool to access the file.",
		toolName, len(text), path)
	return textResult(summary)
}

// saveToFile writes content to a timestamped file in outputDir.
// Returns the file path and true on success, or empty string and false on error.
func saveToFile(content, toolName, outputDir string) (string, bool) {
	if err := os.MkdirAll(outputDir, 0o750); err != nil { //nolint:mnd
		return "", false
	}

	filename := fmt.Sprintf("%s_%d.txt", toolName, time.Now().UnixMilli())
	path := filepath.Join(outputDir, filename)

	// File must be world-readable so the consuming agent (running as a different user) can access it.
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:mnd,gosec
		return "", false
	}

	return path, true
}

// xmlMarshalFileResult marshals v as compact XML and always saves to file.
// Falls back to inline when outputDir is empty.
func xmlMarshalFileResult(v any, toolName, outputDir string) *mcp.CallToolResult {
	data, err := xml.Marshal(v)
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err))
	}
	text := xml.Header + string(data)
	if outputDir == "" {
		return textResult(text)
	}
	path, ok := saveToFile(text, toolName, outputDir)
	if !ok {
		return textResult(text)
	}
	summary := fmt.Sprintf("%s: output %d chars saved to: %s\n\nUse Read tool to access the file.",
		toolName, len(text), path)
	return textResult(summary)
}

// xmlMarshalResult marshals v as compact XML and returns it via largeTextResult.
func xmlMarshalResult(v any, toolName, outputDir string) *mcp.CallToolResult {
	data, err := xml.Marshal(v)
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err))
	}
	return largeTextResult(xml.Header+string(data), toolName, outputDir)
}

// xmlMarshalErrorFragment renders the fallback a string-returning XML formatter
// emits when xml.Marshal fails on its response struct: a bare, properly-escaped
// <error> fragment (no xml.Header prolog), matching the marshalSiteAnalyze
// idiom. Marshal of the all-string response structs in this package effectively
// never fails, but errcheck requires the branch, and a hand-rolled raw-%s
// fallback here would reintroduce the exact manual-XML-string-concatenation
// class this migration removes.
func xmlMarshalErrorFragment(err error) string {
	return fmt.Sprintf("<error>%s</error>", escapeXML(err.Error()))
}

// xmlMarshalFragment marshals v as a bare XML fragment (no xml.Header prolog),
// falling back to a well-formed <error> fragment on the effectively-impossible
// marshal error (see xmlMarshalErrorFragment). This is the single wrapper for
// the all-string XML response formatters in this package: it collapses the
// repeated `b, err := xml.Marshal(v); if err != nil { return
// xmlMarshalErrorFragment(err) }; return string(b)` boilerplate into one place.
// The header-prefixed variants stay separate (formatAnalysisXML emits
// xml.Header; xmlMarshalResult / xmlMarshalFileResult return *CallToolResult).
func xmlMarshalFragment(v any) string {
	b, err := xml.Marshal(v)
	if err != nil {
		return xmlMarshalErrorFragment(err)
	}
	return string(b)
}

// jsonMarshalResult marshals v as compact JSON and returns it via textResult.
// Mirrors xmlMarshalResult's error idiom for the (much more common) plain-JSON
// response path: small/medium tool outputs that don't need the file-overflow
// handling largeTextResult provides.
func jsonMarshalResult(v any) *mcp.CallToolResult {
	data, err := json.Marshal(v)
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err))
	}
	return textResult(string(data))
}

// generateNarrative produces an LLM narrative from structured data.
// Returns empty string on any error (non-fatal, including ErrLLMUnavailable from NoOp).
// client must be non-nil; pass llm.NoOp{} when LLM is not configured.
// narrativeTimeout caps the LLM narrative generation in generateNarrative.
// Matches the codegraph package's narrativeTimeout — narrative is best-effort
// and must not eat the tool's remaining timeout budget.
const helpersNarrativeTimeout = 15 * time.Second

func generateNarrative(ctx context.Context, client llm.Completer, systemPrompt string, data any, promptPrefix string) string {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	// Detach from the tool's context: narrative is best-effort. A slow LLM
	// must not consume the remaining tool timeout budget — the caller's
	// primary data is already assembled; narrative is enrichment.
	narrCtx, cancel := context.WithTimeout(context.Background(), helpersNarrativeTimeout)
	defer cancel()
	narrative, err := client.Complete(narrCtx, systemPrompt, promptPrefix+string(dataJSON))
	if err != nil {
		return ""
	}
	return narrative
}

// escapeXML escapes special XML characters in a string.
func escapeXML(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return s // fallback to raw string on error
	}
	return b.String()
}

// capitalizeFirst returns s with the first Unicode letter uppercased.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func applyPolicy(_ context.Context, root string, r *review.DeltaResult) []policy.Finding {
	p, err := policy.LoadWithDefaults(root, os.Getenv("GOCODE_DEFAULT_POLICY"))
	if err != nil || p == nil {
		return nil
	}
	return p.Apply(r, func(path string) string {
		b, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			return ""
		}
		return string(b)
	})
}
