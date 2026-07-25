package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/anatolykoptev/vaelor/internal/oxcodes"
	"github.com/anatolykoptev/vaelor/internal/polyglot"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DataflowInput is the input schema for the dataflow_analyze tool.
type DataflowInput struct {
	Repo        string           `json:"repo" jsonschema:"Repository: GitHub slug, URL, or absolute local path"`
	Language    string           `json:"language,omitempty" jsonschema:"Language to analyze (go, python, typescript, javascript, rust). Auto-detected if omitted."`
	Focus       string           `json:"focus,omitempty" jsonschema:"Analysis mode — one of: \"all\" (default), \"quality\" (dead stores, unused vars), \"security\" (taint/injection). NOT a file path; to scope to a file, use file_glob."`
	FileGlob    string           `json:"file_glob,omitempty" jsonschema:"Include only files matching glob"`
	ExcludeGlob string           `json:"exclude_glob,omitempty" jsonschema:"Exclude files matching glob"`
	Rules       []TaintRuleInput `json:"rules,omitempty" jsonschema:"Custom taint-tracking rules (JSON array). When omitted, built-in SQL/command injection rules are used. Each rule: {id, sources:[{pattern,tag}], sinks:[{pattern,arg_index,cwe,description}], sanitizers:[{pattern}], severity}"`
	Limit       int              `json:"limit,omitempty" jsonschema:"Maximum findings per section (quality, security, dead functions). Default 50. Use offset to paginate."`
	Offset      int              `json:"offset,omitempty" jsonschema:"Skip the first N findings in each section (for pagination). Use with limit to page through large result sets."`
	MaxBytes    int              `json:"max_bytes,omitempty" jsonschema:"Response budget in bytes (default 8192). When the response exceeds this, the ranked head is returned with a continuation footer."`
}

// TaintRuleInput is the MCP input schema for a custom taint rule.
type TaintRuleInput struct {
	ID         string             `json:"id" jsonschema:"Rule identifier (e.g. 'log-injection')"`
	Sources    []TaintSourceInput `json:"sources" jsonschema:"Taint sources — where tainted data originates"`
	Sinks      []TaintSinkInput   `json:"sinks" jsonschema:"Taint sinks — where tainted data must not flow"`
	Sanitizers []SanitizerInput   `json:"sanitizers,omitempty" jsonschema:"Functions that neutralize tainted data"`
	Severity   string             `json:"severity" jsonschema:"Severity level: 'high', 'medium', 'low'"`
}

type TaintSourceInput struct {
	Pattern string `json:"pattern" jsonschema:"Function/method pattern that produces tainted data (e.g. 'req.URL.Query')"`
	Tag     string `json:"tag" jsonschema:"Taint tag label (e.g. 'user-input')"`
}

type TaintSinkInput struct {
	Pattern     string `json:"pattern" jsonschema:"Function/method pattern that must not receive tainted data (e.g. 'os.OpenFile')"`
	ArgIndex    int    `json:"arg_index" jsonschema:"Index of the argument to check (0-based)"`
	CWE         string `json:"cwe,omitempty" jsonschema:"CWE identifier (e.g. 'CWE-22')"`
	Description string `json:"description,omitempty" jsonschema:"Human-readable description of the vulnerability"`
}

type SanitizerInput struct {
	Pattern string `json:"pattern" jsonschema:"Function/method that neutralizes taint (e.g. 'filepath.Clean')"`
}

// XML response types.

type xmlDataflowResponse struct {
	XMLName  xml.Name    `xml:"response"`
	Dataflow xmlDataflow `xml:"dataflow"`
}

type xmlDataflow struct {
	Repo          string          `xml:"repo,attr"`
	Language      string          `xml:"language,attr,omitempty"`
	Focus         string          `xml:"focus,attr"`
	FilesAnalyzed int             `xml:"filesAnalyzed,attr"`
	DurationMS    int64           `xml:"durationMs,attr"`
	Quality       *xmlDfQuality   `xml:"quality,omitempty"`
	DeadFunctions *xmlDfDeadFuncs `xml:"deadFunctions,omitempty"`
	Security      *xmlDfSecurity  `xml:"security,omitempty"`
	Partial       bool            `xml:"partial,attr,omitempty"`
	PartialReason string          `xml:"partialReason,attr,omitempty"`
}

type xmlDfQuality struct {
	Count    int                 `xml:"count,attr"`
	Findings []xmlQualityFinding `xml:"finding"`
}

type xmlQualityFinding struct {
	Kind     string `xml:"kind,attr"`
	Severity string `xml:"severity,attr"`
	File     string `xml:"file,attr"`
	Line     int    `xml:"line,attr"`
	Variable string `xml:"variable,attr,omitempty"`
	Message  string `xml:",chardata"`
}

type xmlDfSecurity struct {
	Count    int                  `xml:"count,attr"`
	Findings []xmlSecurityFinding `xml:"finding"`
}

type xmlSecurityFinding struct {
	RuleID   string `xml:"ruleId,attr"`
	Severity string `xml:"severity,attr"`
	File     string `xml:"file,attr"`
	Line     int    `xml:"line,attr,omitempty"`
	CWE      string `xml:"cwe,attr,omitempty"`
	Message  string `xml:",chardata"`
}

// dataflowMaxResults is the DEFAULT number of findings requested from
// ox-codes. Tightened from 100 to 50 so the default call fits the ~8 KB
// response budget (#571) — 100 findings at ~100 chars each exceeded the
// 10149-char client truncation ceiling. The actual fetch grows to cover the
// caller's offset+limit page (see dataflowFetchWindow) so pagination beyond
// the first 50 returns data, not an empty section.
const dataflowMaxResults = 50

// dataflowFetchCap bounds the ox-codes fetch window so deep pagination cannot
// request unbounded result sets from the backend.
const dataflowFetchCap = 500

// dataflowFetchWindow returns how many findings to request from ox-codes so
// the caller's offset+limit page is actually fetchable.
func dataflowFetchWindow(offset, limit int) int {
	if limit <= 0 {
		limit = dataflowDefaultLimit
	}
	if offset < 0 {
		offset = 0
	}
	w := offset + limit
	if w < dataflowMaxResults {
		w = dataflowMaxResults
	}
	if w > dataflowFetchCap {
		w = dataflowFetchCap
	}
	return w
}

// dataflowDefaultLimit is the per-section rendering limit when the caller
// does not specify one. Matches dataflowMaxResults so the default call
// returns at most 50 findings per section.
const dataflowDefaultLimit = 50

// dataflowMaxFileLines is the line count above which a file is flagged as
// oversized in the partial footer. ox-codes analyzes server-side and cannot
// be per-file capped from the client, but a 6000+ line file (#565) dominates
// the analysis time — flagging it gives the agent actionable guidance to
// narrow with exclude_glob.
const dataflowMaxFileLines = 2000

func registerDataflow(server *mcp.Server, cfg Config, deps analyze.Deps) {
	outputDir := cfg.OutputDir

	addTool(server, &mcp.Tool{
		Name: "dataflow_analyze",
		Description: "Unified code quality and security analysis. " +
			"Quality: dead stores, unused variables (data-flow), dead functions (callgraph). " +
			"Security: SQL injection, command injection (taint tracking). " +
			"Combines variable-level (IL/CFG) and function-level (callgraph) dead code detection. " +
			"Use instead of separate dead_code + manual review. Requires ox-codes backend.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DataflowInput) (*mcp.CallToolResult, error) {
		return handleDataflow(ctx, input, deps, outputDir)
	})
}

func handleDataflow(ctx context.Context, input DataflowInput, deps analyze.Deps, outputDir string) (*mcp.CallToolResult, error) {
	if deps.OxCodes == nil {
		return errResult("dataflow_analyze requires ox-codes backend (OX_CODES_URL not configured)"), nil
	}
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}

	focus := input.Focus
	if focus == "" {
		focus = "all"
	}
	// #565: agents pass a FILE PATH as `focus` expecting it to scope the
	// analysis to that file. `focus` is an analysis-mode enum, not a path —
	// detect the misuse early and point to file_glob (the real scoping knob).
	if focus != "all" && focus != "quality" && focus != "security" {
		if strings.Contains(focus, "/") || strings.Contains(focus, ".") {
			return errResult(fmt.Sprintf(
				"focus must be 'all', 'quality', or 'security' (got %q — looks like a file path; use file_glob to scope to a file)",
				focus)), nil
		}
		return errResult("focus must be 'all', 'quality', or 'security'"), nil
	}

	limit := input.Limit
	if limit <= 0 {
		limit = dataflowDefaultLimit
	}
	offset := input.Offset
	if offset < 0 {
		offset = 0
	}

	t0 := time.Now()

	// Soft deadline: 25s default, strictly below the ~100s external MCP proxy
	// timeout. Without it the three sequential ox-codes/callgraph analyses run
	// unbounded — 2× 30s HTTP + dead-function callgraph = ~90s on a large file
	// (#565), past the proxy kill. Applied before resolveRoot so clone is
	// bounded too.
	softCtx, softCancel := mcpmeta.SoftDeadlineWith(ctx, mcpmeta.SlowToolSoftDeadline)
	defer softCancel()

	root, cleanup, err := resolveRoot(softCtx, input.Repo, "", deps)
	if err != nil {
		if softCtx.Err() != nil {
			return softDeadlineResult(
				fmt.Sprintf("dataflow_analyze: timed out resolving repo after %s — retry with a local path or narrower file_glob.", time.Since(t0).Round(time.Second)),
				"repo resolution (soft deadline)",
				time.Since(t0),
			), nil
		}
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	// Auto-detect language from repo when not specified.
	if input.Language == "" {
		input.Language = detectDominantLanguage(root)
		if input.Language != "" {
			slog.Debug("dataflow: auto-detected language", "lang", input.Language)
		}
	}

	resp := xmlDataflowResponse{
		Dataflow: xmlDataflow{
			Repo:     input.Repo,
			Language: input.Language,
			Focus:    focus,
		},
	}

	var totalDuration int64
	var skipped []string

	// Quality analysis (dead stores, unused vars).
	if focus == "all" || focus == "quality" {
		if softCtx.Err() != nil {
			skipped = append(skipped, "quality analysis")
		} else {
			qResp, err := runQualityAnalysis(softCtx, deps.OxCodes, root, input)
			if err != nil {
				if softCtx.Err() != nil {
					skipped = append(skipped, "quality analysis")
				} else {
					slog.Warn("dataflow quality analysis failed", "err", err)
				}
			} else {
				resp.Dataflow.Quality = qResp.xmlDfQuality
				resp.Dataflow.FilesAnalyzed = qResp.filesAnalyzed
				totalDuration += qResp.durationMS
			}
		}
	}

	// Dead function analysis (callgraph-based).
	if (focus == "all" || focus == "quality") && softCtx.Err() == nil {
		if dfResult := runDeadFunctionAnalysis(softCtx, root, input.Language, deps); dfResult != nil {
			resp.Dataflow.DeadFunctions = dfResult.xmlDfDeadFuncs
			totalDuration += dfResult.durationMS
		}
	} else if focus == "all" || focus == "quality" {
		skipped = append(skipped, "dead function analysis")
	}

	// Security analysis (taint tracking).
	if focus == "all" || focus == "security" {
		if softCtx.Err() != nil {
			skipped = append(skipped, "security/taint analysis")
		} else {
			sResp, err := runSecurityAnalysis(softCtx, deps.OxCodes, root, input)
			if err != nil {
				if softCtx.Err() != nil {
					skipped = append(skipped, "security/taint analysis")
				} else {
					slog.Warn("dataflow taint analysis failed", "err", err)
				}
			} else {
				resp.Dataflow.Security = sResp.xmlDfSecurity
				if sResp.filesAnalyzed > resp.Dataflow.FilesAnalyzed {
					resp.Dataflow.FilesAnalyzed = sResp.filesAnalyzed
				}
				totalDuration += sResp.durationMS
			}
		}
	}

	resp.Dataflow.DurationMS = totalDuration

	partial := softCtx.Err() != nil && len(skipped) > 0
	if partial {
		resp.Dataflow.Partial = true
		reason := strings.Join(skipped, ", ") + " — soft deadline"
		// Pre-scan for oversized files (#565, the dominant timeout cause) ONLY on
		// the partial path — the scan is a full tree-walk + large-file reads, dead
		// work on the fast path this PR aims to speed up (review MAJOR).
		oversized := findOversizedFiles(root, input.Language, dataflowMaxFileLines)
		if len(oversized) > 0 {
			reason += fmt.Sprintf("; oversized files (>%d lines): %s — narrow with exclude_glob", dataflowMaxFileLines, strings.Join(oversized, ", "))
		}
		resp.Dataflow.PartialReason = reason
	}

	if resp.Dataflow.Quality == nil && resp.Dataflow.DeadFunctions == nil && resp.Dataflow.Security == nil {
		if partial {
			return softDeadlineResult(
				fmt.Sprintf("dataflow_analyze: timed out after %s — no analysis section completed. %s", time.Since(t0).Round(time.Second), resp.Dataflow.PartialReason),
				strings.Join(skipped, ", ")+" (soft deadline)",
				time.Since(t0),
			), nil
		}
		return errResult("dataflow analysis failed: no results from ox-codes"), nil
	}

	// Apply offset/limit pagination to each section's findings.
	applyDataflowPagination(&resp.Dataflow, offset, limit)

	data, err := xml.Marshal(resp)
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil
	}
	text := xml.Header + string(data)

	// Partial path: append a partial footer to the XML text so the agent sees
	// both the partial data and what was truncated. Fast path (no deadline)
	// is byte-identical (Partial/PartialReason are omitempty).
	if partial {
		text += mcpmeta.PartialFooter(resp.Dataflow.PartialReason)
	}

	// Apply per-call budget override when max_bytes is set.
	// Use ShapeWithHint (not Shape) so the tool-specific pagination hint is
	// preserved even when the text fits within the override budget but
	// exceeds the default — without ShapeWithHint, the addTool wrapper would
	// re-shape with the default budget and replace this hint with a generic
	// one (#582).
	if input.MaxBytes > 0 {
		text = mcpmeta.ShapeWithHint(text, budgetOverride(input.MaxBytes),
			fmt.Sprintf("pass offset=%d for the next page", offset+limit))
	}
	return largeTextResult(text, "dataflow_analyze", outputDir), nil
}

type qualityResult struct {
	*xmlDfQuality
	filesAnalyzed int
	durationMS    int64
}

func runQualityAnalysis(ctx context.Context, client *oxcodes.Client, root string, input DataflowInput) (*qualityResult, error) {
	result, err := client.DataflowAnalyze(ctx, oxcodes.DataflowInput{
		Root:        root,
		Language:    input.Language,
		MaxResults:  dataflowFetchWindow(input.Offset, input.Limit),
		FileGlob:    input.FileGlob,
		ExcludeGlob: input.ExcludeGlob,
	})
	if err != nil {
		return nil, fmt.Errorf("quality: %w", err)
	}

	findings := make([]xmlQualityFinding, len(result.Findings))
	for i, f := range result.Findings {
		findings[i] = xmlQualityFinding{
			Kind:     f.Kind,
			Severity: f.Severity,
			File:     f.File,
			Line:     f.Span.StartLine,
			Variable: f.Variable,
			Message:  f.Message,
		}
	}

	return &qualityResult{
		xmlDfQuality:  &xmlDfQuality{Count: result.TotalFindings, Findings: findings},
		filesAnalyzed: result.FilesAnalyzed,
		durationMS:    result.DurationMS,
	}, nil
}

// detectDominantLanguage walks root and returns the most common programming language
// detected from file extensions. Returns "" if the root cannot be walked or no
// recognised source files are found.
//
// The walk itself is bespoke (filesystem paths, not []*ingest.File), but the
// argmax-over-counts half reuses the canonical primitive so there is still
// only one "most frequent" tie-break implementation in the repo.
func detectDominantLanguage(root string) string {
	counts := make(map[string]int)
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if lang := ingest.DetectLanguage(path); lang != "" {
			counts[lang]++
		}
		return nil
	})
	return polyglot.DominantLanguageFromCounts(counts)
}

// findOversizedFiles walks root and returns the relative paths of source files
// exceeding maxLines, capped at 5 entries. Used to flag the dominant cause of
// dataflow timeouts (#565: a 6130-line file) so the agent can narrow with
// exclude_glob. Returns nil when no file exceeds the threshold.
func findOversizedFiles(root, language string, maxLines int) []string {
	var oversized []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if lang := ingest.DetectLanguage(path); lang == "" || (language != "" && lang != language) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		// Cheap byte-based estimate: ~3 bytes/line for typical source. Only
		// read+count lines for files above the byte threshold to avoid
		// reading every file.
		if info.Size() < int64(maxLines)*2 {
			return nil
		}
		data, rErr := os.ReadFile(path)
		if rErr != nil {
			return nil
		}
		lines := strings.Count(string(data), "\n") + 1
		if lines > maxLines {
			rel, _ := filepath.Rel(root, path)
			oversized = append(oversized, rel)
		}
		return nil
	})
	if len(oversized) > 5 {
		oversized = oversized[:5]
	}
	return oversized
}

// applyDataflowPagination applies offset/limit to each section's findings
// slice in place. The Count/Total/Dead/Ratio attributes are preserved
// (they reflect the full result set, not just the page) so the agent
// knows how many total findings exist and can paginate with offset.
func applyDataflowPagination(df *xmlDataflow, offset, limit int) {
	if offset <= 0 && limit <= 0 {
		return
	}
	if df.Quality != nil {
		df.Quality.Findings = paginateFindings(df.Quality.Findings, offset, limit)
	}
	if df.DeadFunctions != nil {
		df.DeadFunctions.Symbols = paginateDeadSyms(df.DeadFunctions.Symbols, offset, limit)
	}
	if df.Security != nil {
		df.Security.Findings = paginateFindings(df.Security.Findings, offset, limit)
	}
}

func paginateFindings[T any](findings []T, offset, limit int) []T {
	if len(findings) == 0 {
		return findings
	}
	if offset > 0 {
		if offset >= len(findings) {
			return findings[:0]
		}
		findings = findings[offset:]
	}
	if limit > 0 && len(findings) > limit {
		findings = findings[:limit]
	}
	return findings
}

func paginateDeadSyms(syms []xmlDfDeadFuncSym, offset, limit int) []xmlDfDeadFuncSym {
	if len(syms) == 0 {
		return syms
	}
	if offset > 0 {
		if offset >= len(syms) {
			return syms[:0]
		}
		syms = syms[offset:]
	}
	if limit > 0 && len(syms) > limit {
		syms = syms[:limit]
	}
	return syms
}
