# go-code — Code Intelligence MCP Server

**Module**: `github.com/anatolykoptev/go-code` | **Port**: 8897 | **MCP**: `http://127.0.0.1:8897/mcp`
**Languages**: Go, Python, TypeScript/JavaScript, Rust, Java, C, C++, Ruby, C#, PHP, Svelte, Astro, Vue, Kotlin, Swift, HTML (16 total)

## Package Overview

| Package | Role |
|---------|------|
| `cmd/go-code/` | MCP entry point; `tool_*.go` = one file per tool, `register.go` wires them |
| `internal/ingest/` | Repo clone (`--filter=blob:none` partial clone) + walk |
| `internal/parser/` | tree-sitter AST → symbols (16 languages) |
| `internal/analyze/` | Orchestration imported by tool handlers (tools import ONLY this) |
| `internal/callgraph/` | In-memory call tracing + go/types type-aware resolution for Go repos |
| `internal/goanalysis/` | Go package loading (`go/packages`) + type-aware call resolution (`go/types`) |
| `internal/tier/` | 3-tier analysis system (Basic/Enhanced/Full) with degradation warnings |
| `internal/compound/` | Compound tools (`understand`, `prepare_change`) aggregating sub-queries |
| `internal/codegraph/` | Apache AGE persistent graph |
| `internal/embeddings/` | Semantic search: pgvector store, embed pipeline, hybrid RRF, graph expansion |
| `internal/codesearch/` | Grep-like code search with path filter support |
| `internal/freshness/` | Dependency freshness + CVE/vulnerability checking via OSV.dev API |
| `internal/polyglot/` | Multi-language repo structure detection |
| `internal/routes/` | HTTP route extraction (7 languages, used for cross-language edges) |
| `internal/slugparse/` | Canonical slug parser shared by `forge` and `ingest`; stdlib-only leaf package |
| `internal/forge/` | Multi-forge abstraction: `Forge` interface, GitHub + GitLab implementations, URL detection, registry |
| `internal/websearch/` | HTTP client for go-search MCP (smart_search depth=fast), used by repo_search |
| `internal/llm/` | OpenAI-compatible LLM proxy client with retry + fallback |
| `internal/clean/` | Symbol post-processing between parser and analyze |
| `internal/cache/` | Parse/LLM/tool caches (in-mem L1 + optional Redis L2) |
| `internal/compare/` | Repo structural comparison (backs `code_compare`) |
| `internal/deadcode/` | Unused-symbol detection (backs `dead_code`) |
| `internal/explore/` | Quick repo overview (backs `explore`) |
| `internal/impact/` | Change-impact analysis (backs `impact_analysis`) |
| `internal/research/` | Deep repository research (backs `code_research`) |
| `internal/review/` | PR / delta review (backs `review_*`) |
| `internal/designmd/` | Design-doc indexing (backs `design_search`) |
| `internal/learnings/` | pgvector-backed store for prior review findings surfaced in `understand` |
| `internal/oxcodes/` | ox-codes cross-service integration |
| `internal/webanalyze/` | Site technology fingerprinting |

## MCP Tools

| Tool | Description |
|------|-------------|
| `repo_analyze` | Analyze GitHub/GitLab repo or local path. Modes: `deep` (AST+LLM), `quick` (Code Search), `pr`/`issue` |
| `file_parse` | Parse single file with tree-sitter → symbol table or raw AST |
| `code_compare` | Structural diff of two repos: architecture, API design, dependencies |
| `dep_graph` | Dependency graph as Mermaid/DOT/JSON. `cross_language=true` adds Route edges |
| `symbol_search` | Find functions/types/consts by name pattern or wildcard across a repo |
| `call_trace` | BFS/DFS call chain from a function; forward (callees) or reverse (callers) with LLM narrative |
| `impact_analysis` | Blast radius of changing a function |
| `dead_code` | Find unused exported functions/types |
| `explore` | Quick overview: stats, README, deps, health |
| `code_health` | Quality grade (A-F), 14 sub-scores incl. CVE/vulnerability checking via OSV.dev |
| `code_graph` | Query persistent Apache AGE graph (`gocode` DB). 14 Cypher templates + LLM freeform fallback. Lazy indexing with TTL. Requires `DATABASE_URL` |
| `remember_graph_insights` | Maintenance/internal: persist structural graph findings (surprises, dead_code) to the learnings store so future `understand` calls surface them. Human-invoked only |
| `repo_search` | Discover repos across forges. Parallel web search (go-search) + GitHub/GitLab API, LLM-ranked |
| `github_code_search` | Search code on GitHub via the Code Search API; returns file paths with matching fragments |
| `code_search` | Grep-like search with regex, path filter (`file_glob`), context lines |
| `wp_plugin_search` | Search the WordPress.org plugin directory |
| `semantic_search` | Vector similarity search via pgvector + hybrid RRF + graph expansion |
| `sparse_backfill` | Maintenance/internal: populate `sparse_embedding` for existing `code_embeddings` rows where NULL. Operator-initiated |
| `orphan_sweep` | Maintenance/internal: delete orphaned embedding/state rows. Operator-initiated. DRY-RUN BY DEFAULT (`dry_run` omitted/true ⇒ preview-only count; `dry_run=false` ⇒ real delete). `categories` omitted ⇒ `["embeddings_not_in_state"]` only (original behaviour — deletes embeddings, NEVER state rows). Opt in: `"path_missing"` (state rows whose `source_path` directory is gone — DELETES STATE ROWS, mount-blip guarded), `"pathless"` (state rows with empty `source_path` — DELETES STATE ROWS). Each category reported separately |
| `list_flows` | List precomputed named execution flows for a repo, ordered by priority (highest-PageRank chains first) |
| `find_duplicates` | Find pairs of symbols in one repo that are semantically near-identical |
| `code_research` | Deep repo research — AST traversal + LLM narrative + test linkage |
| `site_analyze` | Web site tech analysis (stack, SEO/OG tags) |
| `site_crawl` | BFS web crawler |
| `understand` | Deep-dive symbol analysis. Aggregates: symbol info + callees + callers + complexity. Type-aware for Go |
| `prepare_change` | Pre-change risk assessment. Aggregates: impact_analysis + dead_code check |
| `review_delta` | Analyze changes between two git refs and compute differential impact |
| `review_pr` | Review a pull request: differential impact analysis on all changes. `dry_run=false` + `event` posts to GitHub and persists learnings. |
| `rewrite` | Structural search-and-replace using AST patterns with `$WILDCARDS` |
| `dataflow_analyze` | Unified code quality + security analysis: dead stores/unused vars (data-flow), dead functions (callgraph), SQL/command injection (taint tracking). Requires ox-codes backend |
| `design_search` | Semantic search across design docs (requires `DESIGN_EMBED_URL`) |
| `debug_investigate` | Correlate Prometheus metrics + Jaeger failed traces + code symbols to suggest the likely buggy file:function for a service+window. Long-running (5min budget) |
| `fleet_versions` | Compare pinned image versions in a repo (Dockerfile, docker-compose*.yml) against images running on one or more hosts |
| `resolve_frame` | Resolve a minified JS stack frame (url, line, column) to its original source location via the companion `.map` file |
| `get_file_health` | Report a 1-10 health score per file using `prior_defect` + `churn_risk` biomarkers; optional paths default to top-20 hotspots |
| `suggest_reviewers` | Rank candidate reviewers for PR file paths using direct authorship, co-change coupling, and recency |
| `federated_cochange` | Find files in different repos that change together (cross-repo co-change), ranked by Wilson lower bound on directional confidence |

## Environment Variables

| Variable | Default | Notes |
|----------|---------|-------|
| `MCP_PORT` | `8897` | |
| `LLM_API_BASE` | `http://127.0.0.1:8317/v1` | Local OpenAI-compatible proxy (e.g. LiteLLM, CLIProxyAPI) |
| `LLM_API_KEY` | required | |
| `LLM_API_KEY_FALLBACK` | optional | Comma-separated API keys, same-model key rotation on 429/5xx. Disabled when `LLM_MODEL_FALLBACK` is set. |
| `LLM_MODEL_FALLBACK` | optional | CSV cross-provider model chain (e.g. `gemini-3.1-flash-lite-preview,cerebras-qwen-3-235b`). When set, overrides key-rotation; cliproxyapi routes each model id to its upstream provider. |
| `LLM_MODEL` | `cerebras-gpt-oss-120b` | Fresh-deploy fallback only; prod always overrides via `LLM_MODEL` (fleet llm.env) |
| `GITHUB_TOKEN` | optional | Higher rate limits + private repos |
| `GITHUB_SEARCH_REPOS` | optional | Default repos for quick-mode code search |
| `WORKSPACE_DIR` | `/tmp/go-code-workspace` | Temp clone location |
| `MAX_FILE_KB` | `512` | |
| `INDEX_SKIP_DIRS` | `` | Comma-separated dir names appended to the built-in skip set (`vendor`, `node_modules`, `target`, etc.). E.g. `INDEX_SKIP_DIRS=mygenerated,legacy`. Empty = default list only. |
| `MAX_REPO_MB` | `250` | Total ingested-source cap for deep repo_analyze. Per-request override via the `max_repo_mb` tool arg (>0 wins; 0 uses this default). Truncation emits a WARN. |
| `REDIS_URL` | optional | L2 cache, DB 6 |
| `DATABASE_URL` | optional | PostgreSQL DSN for Apache AGE (`gocode` database) |
| `LEARNINGS_DATABASE_URL` | optional | PostgreSQL DSN (pgvector) for the review learnings store; falls back to `DATABASE_URL` when unset. When both are empty, the learnings loop is silently disabled |
| `GRAPH_TTL_LOCAL` | `3600` | Seconds |
| `GRAPH_TTL_REMOTE` | `86400` | Seconds |
| `GRAPH_BATCH_SIZE` | `5` | Keep small — AGE limitation |
| `GITLAB_TOKEN` | optional | GitLab API token (`PRIVATE-TOKEN` header) |
| `GITLAB_URL` | optional | Self-hosted GitLab base URL (default: `https://gitlab.com`) |
| `GO_SEARCH_URL` | optional | go-search MCP endpoint for web search (e.g. `http://go-search:8890/mcp`) |
| `EMBED_URL` | optional | Embedding server (e.g. `http://embed-server:8082`) — enables semantic_search |
| `EMBED_MODEL` | `code-rank-embed` | Model name for OpenAI-compatible embed API |
| `AUTO_INDEX_DIRS` | optional | Comma-separated dirs eligible for auto-indexing (e.g. `/host/src`). Indexing is lazy per-repo on first semantic query, not eager at boot. Runtime compose sets `/host/src` |
| `PATH_MAPPINGS` | optional | Host-to-container path mapping (e.g. `/path/to/repos:/host`) |
| `OUTPUT_DIR` | optional | Output dir for generated files (e.g. `/tmp/go-code-output`) |
| `GITHUB_WEBHOOK_SECRET` | optional | When set, enables `/webhook/github` PR-review receiver |
| `REVIEW_POST_ENABLED` | `false` | When `true`, webhook posts reviews; otherwise dry-log |
| `REVIEW_BOT_USER` | optional | GitHub login to skip (avoid self-triggered loops) |

## Learnings loop

`review_pr` (with `dry_run=false`) persists one `learnings.Record` per changed symbol on successful PostReview — `review_outcome` mapped from the GitHub review event (`APPROVE`→`good`, `REQUEST_CHANGES`→`bad`, else `neutral`), with flag/note derived from the first policy finding whose line intersects the symbol. `review_pr` (dry-run path) writes `risk_level` (`low`/`medium`/`high`) on the same table, in an orthogonal column. `understand` then calls `Store.Nearest(repo, symbol, 3)` and emits matches as `prior_learnings` on its JSON result (omitted when empty). `Store.NearestVector(ctx, query, k)` provides semantic lookup when an embedder is configured at `learnings.New` time. Storage is gated on `LEARNINGS_DATABASE_URL` (or `DATABASE_URL`); with neither, the loop silently no-ops.

## Graph signal ecosystem

Two graph representations cooperate through `internal/graphx/` interfaces (`Analytics`, `CrossRefs`) without either package knowing about the other. `codegraph.Store` implements both via adapters (`NewAnalyticsAdapter`, `NewCrossRefsAdapter`); `analyze.Deps.Graph`/`Refs` defaults to `graphx.Noop{}` when `DATABASE_URL` is empty.

| Signal (AGE-computed) | Exposed via | Consumed by |
|---|---|---|
| PageRank, Community | `Analytics.Symbol` | `understand` (`<graph_analytics>`), `prepare_change` (`communities_crossed`, `high_pagerank_callers`) |
| Top-K PageRank | `Analytics.TopPageRank` | `prepare_change` (decile threshold for high-PR flag) |
| Community membership | `Analytics.Symbol` | `review_pr` (`community_move` flag) |
| Surprise score | `Analytics.Symbol` | `review_pr` (`high_surprise` flag when ≥0.5) |
| HANDLES / FETCHES | `CrossRefs.HandlesRoute`, `CrossRefs.FetchedBy` | `call_trace` (cross-language fetch nodes at +1 depth) |
| TESTED_BY | `CrossRefs.TestedBy` | `impact_analysis` (`tests_covering`) |

**Cold-path guarantee**: every consumer skips enrichment when `Signals.Found == false` or `CrossRefs` returns empty, preserving byte-identical output when the graph has no snapshot.

## Webhook

### GitHub webhook
- Endpoint: `POST /webhook/github` on MCP port (:8897)
- Headers required: `X-GitHub-Event`, `X-Hub-Signature-256`
- Events handled: `pull_request` (opened/synchronize/reopened)
- Setup: expose via your existing tunnel (e.g. dozor/Cloudflare) and register in GitHub repo settings

## Build

```bash
# All go commands need GOWORK=off — the user-wide go.work interferes.
make build   # CGO_ENABLED=1 required (tree-sitter grammars are C)
make lint
make test
make deploy  # docker compose build --no-cache + up -d
```

## Conventions

- Dependency direction: `ingest → parser → clean → analyze → llm`
- `compare`, `analyze`, `callgraph` are peers — none imports the others (except `callgraph` imports `goanalysis` for type-aware resolution)
- `forge` package depends only on `slugparse` (no other internal packages)
- `slugparse` is a leaf — depends only on stdlib
- Tool handlers (`cmd/go-code/tool_*.go`) import `internal/analyze` only — no direct internal package access
- Error messages: lowercase, `fmt.Errorf("context: %w", err)`
- Context always first param; never stored in structs
- HTTP clients always use `http.NewRequestWithContext`

## Gotchas

- **Apache AGE**: no `ON CREATE SET` / `ON MATCH SET` — use separate `CREATE` then `SET` statements
- **AGE batch size**: `GRAPH_BATCH_SIZE=5` — larger batches cause parse errors in AGE Cypher
- **code_graph DB name**: always `gocode` (not the service name, not configurable at runtime)
- **Local repo paths**: Docker mounts `/path/to/repos:/host:ro`; `PATH_MAPPINGS=/path/to/repos:/host` translates paths automatically
- **Partial clone**: `--filter=blob:none` reduces memory for large repos (no blob download during clone)
- **Semantic search stack**: embed-server (code-rank-embed, 768 dim via `EMBED_MODELS` multi-model) → pgvector (HNSW cosine) → hybrid RRF (semantic + keyword) → graph expansion (1-hop AGE)
- **MCP registration**: `claude mcp add -s user -t http go-code http://127.0.0.1:8897/mcp`
- **`GOWORK=off` mandatory**: without it, `go test`/`go build` resolve against the user-wide `go.work` and break imports
- **Extension → language is single source of truth via `handler.Extensions()`** — don't duplicate in a separate map (historical bug: `.cjs` was in handler but missing from the lang map, making `ParseFile("foo.cjs")` fail silently)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for: adding a new tool, adding a new language, CGO details.
