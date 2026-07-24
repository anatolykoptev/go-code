package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anatolykoptev/go-kit/env"
	kitmetrics "github.com/anatolykoptev/go-kit/metrics"
	"github.com/anatolykoptev/go-kit/watcher"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/anatolykoptev/vaelor/internal/repofind"
)

// watchEventBufferSize is the capacity of the internal event channel between
// the forwarder goroutine and the single IndexFile worker. When full, new
// events are dropped (counted via events_dropped_total) to bound memory under
// burst load (ADR-15).
const watchEventBufferSize = 100

// defaultWatchDebounce is the per-path debounce window. Rapid bursts of saves
// to the same file are coalesced into one IndexFile call (ADR-11).
const defaultWatchDebounce = 500 * time.Millisecond

// repoEntry maps a discovered repo root to its repoKey for IndexFile dispatch.
type repoEntry struct {
	key  string
	root string
}

// startFileWatcher wires the opt-in file watcher (ADR-9, ADR-13). It discovers
// repos via autoIndexDirs + repofind.Discover, creates a go-kit/watcher with
// ingest ignore patterns, and dispatches debounced save events to a
// single-worker goroutine that calls Pipeline.IndexFile (ADR-4, ADR-6).
//
// In addition to the embedding refresh, each event triggers a debounced +
// single-flighted background AGE-graph refresh via graphRefresher (#642), so
// the graph (call edges, PageRank, communities) stays consistent with the
// embeddings after a watched edit instead of silently going stale.
//
// Graceful degradation (ADR-16): on any watcher failure the function logs a
// warning, sets gocode_watcher_healthy=0, and returns without crashing the
// caller. The MCP server continues to serve with boot-time AutoIndex only.
//
// The watcher discovers repos once at startup; new repos are not watched until
// restart (ADR-17). This limitation is logged at startup.
func startFileWatcher(ctx context.Context, cfg Config, pipeline *embeddings.Pipeline, reg *kitmetrics.Registry, graphStore *codegraph.Store) {
	if pipeline == nil {
		slog.Warn("file watcher: pipeline is nil — watcher disabled")
		return
	}

	dirs := autoIndexDirs(cfg)
	repoRoots := repofind.Discover(dirs)
	if len(repoRoots) == 0 {
		slog.Info("file watcher: no repos discovered — watcher idle")
		reg.Gauge("watcher_healthy").Set(0)
		return
	}

	// Log discovered repos (ADR-17: startup-only discovery).
	slog.Info("file watcher: discovered repos (startup-only; new repos not watched until restart)",
		slog.Int("count", len(repoRoots)),
		slog.String("repos", strings.Join(repoRoots, ", ")))

	debounce := time.Duration(env.Int("WATCH_DEBOUNCE_MS", int(defaultWatchDebounce/time.Millisecond))) * time.Millisecond
	if debounce <= 0 {
		debounce = defaultWatchDebounce
	}

	w, err := watcher.New(repoRoots, buildWatcherOptions(debounce)...)
	if err != nil {
		slog.Warn("file watcher: failed to create watcher — graceful degradation to boot-time AutoIndex",
			slog.Any("error", err))
		reg.Gauge("watcher_healthy").Set(0)
		return
	}

	eventCh, err := w.Watch(ctx)
	if err != nil {
		slog.Warn("file watcher: failed to start watching — graceful degradation to boot-time AutoIndex",
			slog.Any("error", err))
		reg.Gauge("watcher_healthy").Set(0)
		_ = w.Close()
		return
	}

	reg.Gauge("watcher_healthy").Set(1)
	slog.Info("file watcher: started",
		slog.Int("repos", len(repoRoots)),
		slog.Duration("debounce", debounce),
		slog.Int("buffer", watchEventBufferSize))

	// Build repoKey + root lookup: map repoRoot → repoEntry.
	repoMap := make(map[string]repoEntry, len(repoRoots))
	for _, root := range repoRoots {
		repoMap[root] = repoEntry{key: codegraph.GraphNameFor(root), root: root}
	}

	// Background AGE-graph refresh coalescer (#642). Constructed once; each
	// event triggers Trigger(repoKey, root) which debounces + single-flights
	// the rebuild so the graph stays fresh without stampeding the box.
	refreshCfg := codegraph.IndexConfig{
		TTLLocal:            cfg.GraphTTLLocal,
		TTLRemote:           cfg.GraphTTLRemote,
		BatchSize:           cfg.GraphBatchSize,
		EnableSurpriseIndex: cfg.CodegraphSurpriseIndex,
		FlowsMax:            cfg.FlowsMax,
		FlowsDFSDepth:       cfg.FlowsDFSDepth,
	}
	refresher := newGraphRefresher(graphStore, refreshCfg)

	// Internal event channel: forwarder → single IndexFile worker.
	ch := make(chan watcher.Event, watchEventBufferSize)

	// Forwarder goroutine: reads from watcher, pushes to buffered channel.
	go func() {
		defer w.Close()
		defer close(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-eventCh:
				if !ok {
					return
				}
				reg.Incr("events_received_total")
				select {
				case ch <- ev:
				default:
					reg.Incr("events_dropped_total")
					slog.Warn("file watcher: event dropped — buffer full",
						slog.String("path", ev.Path), slog.String("op", ev.Op))
				}
			}
		}
	}()

	// Single-worker goroutine: serializes IndexFile calls (Concurrency=1, ADR-6).
	go func() {
		for ev := range ch {
			processWatchEvent(ctx, ev, repoMap, pipeline, reg, refresher)
		}
		slog.Info("file watcher: worker stopped")
		reg.Gauge("watcher_healthy").Set(0)
	}()
}

// sourceWatchExtensions returns the parser-registered source file extensions
// that the file watcher subscribes to. Sourced from parser.RegisteredExtensions
// (the single source of truth — handler.Extensions() aggregated by the
// registry) so a newly registered language is automatically watched without a
// duplicate extension map. The historical bug a duplicated ext map caused:
// `.cjs` was registered in the handler but missing from a separate lang map,
// making ParseFile("foo.cjs") fail silently. Hardcoding a second list here
// would reintroduce that class of drift (#644).
func sourceWatchExtensions() []string {
	return parser.RegisteredExtensions()
}

// buildWatcherOptions assembles the functional options for the file watcher.
// Extracted from startFileWatcher so the extension filter is unit-testable in
// isolation (a behavioral test that a non-source file produces no event while a
// source file does). The extension list is the parser registry, NOT a
// hardcoded duplicate — see sourceWatchExtensions.
func buildWatcherOptions(debounce time.Duration) []watcher.Option {
	return []watcher.Option{
		watcher.WithRecursive(true),
		watcher.WithDebounce(debounce),
		watcher.WithIgnoreDirs(ingest.IgnoredDirNames()...),
		// B5 (#644): restrict events to files vaelor can actually index.
		// Without this every file change (README, go.sum, .git internals, .env)
		// fires an event into the cap-100 channel; under burst (git checkout /
		// pull) non-source events fill the buffer and real source-file events
		// are dropped (events_dropped_total) and not reindexed until a full
		// rebuild.
		watcher.WithExtensions(sourceWatchExtensions()...),
	}
}

// processWatchEvent maps a watcher.Event to (repoKey, root, relPath) and calls
// Pipeline.IndexFile, then triggers a debounced background AGE-graph refresh
// (#642) so the graph stays consistent with the embeddings. Errors are logged
// but do not crash the worker.
func processWatchEvent(ctx context.Context, ev watcher.Event, repoMap map[string]repoEntry, pipeline *embeddings.Pipeline, reg *kitmetrics.Registry, refresher *graphRefresher) {
	// Determine which repo the path belongs to (prefix match on repo root).
	var root, repoKey, relPath string
	for r, info := range repoMap {
		if ev.Path == r || strings.HasPrefix(ev.Path, r+string(os.PathSeparator)) {
			root, repoKey = info.root, info.key
			relPath = strings.TrimPrefix(ev.Path, root+string(os.PathSeparator))
			break
		}
	}
	if root == "" {
		slog.Debug("file watcher: path not in any watched repo, skipping", slog.String("path", ev.Path))
		return
	}
	relPath = filepath.ToSlash(relPath)

	slog.Info("file watcher: event",
		slog.String("path", relPath), slog.String("op", ev.Op), slog.String("repo", repoKey))

	reg.Gauge("debounce_active_gauge").Set(1)
	start := time.Now()
	_, err := pipeline.IndexFile(ctx, repoKey, root, relPath)
	reg.ObserveSeconds("indexfile_duration_seconds", time.Since(start))
	reg.Gauge("debounce_active_gauge").Set(0)

	if err != nil {
		slog.Warn("file watcher: IndexFile error",
			slog.String("repo", repoKey), slog.String("file", relPath), slog.Any("error", err))
	}

	// Trigger a debounced background AGE-graph refresh (#642). The graph reads
	// source files directly (independent of embeddings), so it must refresh on
	// any source-file write the watcher saw — even when IndexFile short-
	// circuited (test file, oversized) or errored. Trigger is non-blocking and
	// coalesces bursts, so this never stalls the worker or stamps the box.
	refresher.Trigger(repoKey, root)
}
