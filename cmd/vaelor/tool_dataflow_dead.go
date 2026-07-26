package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/deadcode"
)

// xmlDfDeadFuncs holds dead function detection results (callgraph-based).
type xmlDfDeadFuncs struct {
	Total   int                `xml:"total,attr"`
	Dead    int                `xml:"dead,attr"`
	Ratio   float64            `xml:"ratio,attr"`
	Warming string             `xml:"warming,attr,omitempty"`
	Symbols []xmlDfDeadFuncSym `xml:"symbol"`
}

type xmlDfDeadFuncSym struct {
	Name       string `xml:"name,attr"`
	Kind       string `xml:"kind,attr"`
	File       string `xml:"file,attr"`
	Package    string `xml:"package,attr,omitempty"`
	Line       int    `xml:"line,attr"`
	Lines      int    `xml:"lines,attr"`
	Confidence string `xml:"confidence,attr"`
}

type deadFuncsResult struct {
	*xmlDfDeadFuncs
	durationMS int64
}

// dataflowDeadBuildFromRepo is the production seam for callgraph.BuildFromRepo;
// handler-level tests can override it to avoid heavy parsing.
var dataflowDeadBuildFromRepo = callgraph.BuildFromRepo

// runDeadFunctionAnalysis runs callgraph-based dead code detection.
// Returns nil (not error) if callgraph building fails — non-fatal for dataflow.
func runDeadFunctionAnalysis(ctx context.Context, root, language string, deps analyze.Deps) *deadFuncsResult {
	start := time.Now()

	cg, err := dataflowDeadBuildFromRepo(ctx, callgraph.TraceRepoInput{
		Root:     root,
		Language: language,
	})
	if err != nil {
		slog.Warn("dataflow: callgraph build failed, skipping dead functions", "err", err)
		return nil
	}

	result := deadcode.Analyze(cg, deadcode.Options{
		IncludeExported: false,
		HookCallbacks:   cg.HookCallbacks,
		Relationships:   cg.TypeRels,
		OxCodes:         deps.OxCodes,
		Root:            root,
		Language:        language,
		Ctx:             ctx,
	})

	symbols := make([]xmlDfDeadFuncSym, len(result.DeadSymbols))
	for i, s := range result.DeadSymbols {
		symbols[i] = xmlDfDeadFuncSym{
			Name:       s.Name,
			Kind:       s.Kind,
			File:       s.File,
			Package:    s.Package,
			Line:       s.StartLine,
			Lines:      s.Lines,
			Confidence: s.Confidence,
		}
	}

	return &deadFuncsResult{
		xmlDfDeadFuncs: &xmlDfDeadFuncs{
			Total:   result.TotalFunctions,
			Dead:    result.DeadCount,
			Ratio:   result.DeadRatio,
			Warming: warmingAttr(cg.Warming),
			Symbols: symbols,
		},
		durationMS: time.Since(start).Milliseconds(),
	}
}
