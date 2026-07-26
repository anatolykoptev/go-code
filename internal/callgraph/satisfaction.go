package callgraph

import (
	"context"
	"log/slog"
	"time"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// ExtractGoImplements computes structural interface-satisfaction relationships
// for a Go module via go/types and returns them as parser.TypeRelationship values
// of kind RelImplements, ready to flow through buildRelationshipEdges (code_graph)
// or be appended to cg.TypeRels (call_trace). One edge is produced per
// (concrete type T, interface I) where T or *T implements I.
//
// The caller owns the go/packages load and passes the *LoadResult through the
// seam (issue #747): ExtractGoImplements no longer loads go/packages itself.
// The load was previously shared with tryGoTypesResolution (CALLS) via a
// process-global TTL+LRU cache (goanalysis.CachedLoadPackages), which pinned
// the full go/types arena (NeedDeps: .Types/.TypesInfo/.Syntax for every
// package) for the cache TTL — ~2 GB per entry, size 8, OOM-killing the 3 GiB
// indexer ten times in two days. A value shared between two sequential steps
// of one request is a parameter, not process-global state; passing it makes
// the arena's lifetime the request's, so it becomes collectable the moment
// the request ends. EnrichWithTypedResolution (the shared composition seam)
// loads once and hands the same *LoadResult to both tryGoTypesResolution
// (CALLS) and ExtractGoImplements (IMPLEMENTS); warmGoTypesCache does the
// same for the background warm path.
//
// Go-only and best-effort: returns nil (not an error) when no satisfaction
// exists. The returned relationships' File field is the concrete type's
// ABSOLUTE declaration path, so buildRelationshipEdges keys the IMPLEMENTS
// edge's subject endpoint onto the same Symbol vertex (name + repo-relative
// file) that buildSymbolGraph created.
//
// Called from EnrichWithTypedResolution (the shared composition seam) so both
// BuildFromRepo (call_trace/impact_analysis) and buildAGECallGraph (code_graph)
// get Go IMPLEMENTS edges. See issue #467.
func ExtractGoImplements(ctx context.Context, root string, lr *goanalysis.LoadResult) []parser.TypeRelationship {
	if lr == nil {
		return nil
	}

	t0 := time.Now()

	sats := goanalysis.ComputeSatisfactions(lr.Packages)
	rels := make([]parser.TypeRelationship, 0, len(sats))
	for _, s := range sats {
		// A self-edge (type whose interface name resolves to itself) is impossible
		// here because ComputeSatisfactions only pairs non-interface types with
		// interfaces. Skip empty endpoints defensively.
		if s.Type == "" || s.Interface == "" || s.TypeFile == "" {
			continue
		}
		rels = append(rels, parser.TypeRelationship{
			Subject: s.Type,
			Target:  s.Interface,
			Kind:    parser.RelImplements,
			File:    s.TypeFile,
		})
	}

	implementsLoadTotal.WithLabelValues("ok").Inc()
	implementsEdgesTotal.WithLabelValues(implementsRepoKey(root)).Add(float64(len(rels)))
	slog.Info("callgraph: IMPLEMENTS go/types satisfaction done",
		slog.String("repo", root), slog.Int("edges", len(rels)),
		slog.Duration("elapsed", time.Since(t0)))
	return rels
}
