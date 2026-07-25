package main

import (
	"log/slog"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newSemanticDeps constructs the SemanticDeps value used by semantic_search
// (and the CLI search subcommand). It is a pure extraction of the inline
// construction that lived in registerTools (register.go:242-289): the
// returned SemanticDeps is byte-identical to the previous inline construction.
//
// Both the MCP serve path (registerTools) and the CLI search subcommand call
// this function so there is a single wiring site for the semantic dependency
// graph. The function stays in cmd/vaelor (boundaries lens — wiring concern,
// not domain logic; direction cmd → internal/embeddings preserved).
//
// Parameters mirror the variables available at the original call site:
//   - cfg:        loaded Config (embed URL, model, sparse, keyword arm, etc.)
//   - deps:       the analyze.Deps built earlier in registerTools (or a
//     minimal equivalent for the CLI path)
//   - dataPool:   pgvector / relational pool (nil when DATABASE_URL unset)
//   - agePool:    Apache AGE pool (nil when DATABASE_URL unset); used by Expander
//   - graphStore: codegraph.Store (nil when DATABASE_URL unset or preflight failed)
//   - rrfWeights: pre-computed RRF weights (published separately by the caller)
//
// Returns a zero-value SemanticDeps (all nil fields) when EMBED_URL is empty
// or dataPool is nil — matching the original guard semantics.
func newSemanticDeps(
	cfg Config,
	deps analyze.Deps,
	dataPool, agePool *pgxpool.Pool,
	graphStore *codegraph.Store,
	rrfWeights embeddings.RRFWeights,
) SemanticDeps {
	if cfg.EmbedURL == "" {
		slog.Warn("config: semantic_search disabled — EMBED_URL not set; set EMBED_URL to enable semantic search",
			slog.String("env_var", "EMBED_URL"),
		)
		setCapability("semantic_search", false)
		return SemanticDeps{}
	}
	if dataPool == nil {
		setCapability("semantic_search", false)
		return SemanticDeps{}
	}

	ec, err := newCodeEmbedder(cfg)
	if err != nil {
		slog.Warn("embed: code client disabled", slog.Any("error", err))
		setCapability("semantic_search", false)
		return SemanticDeps{}
	}

	es := embeddings.NewStore(dataPool)

	var pipelineOpts []embeddings.PipelineOpt
	if cfg.EmbedPipelineCache {
		pipelineOpts = append(pipelineOpts, embeddings.WithFileCache(embeddings.NewPipelineCache()))
	}
	// INDEX_BUDGET bounds the background index goroutine so a hung embed
	// server cannot keep a goroutine alive indefinitely (Fix 3).
	pipelineOpts = append(pipelineOpts, embeddings.WithIndexBudget(cfg.IndexBudget))

	// #664: symbol-kind expansion (macro/module/type-alias + LOI embed text).
	// Dark-launched — default false; prod is byte-identical until the controller
	// measures recall lift vs index-size cost and enables EXPAND_SYMBOL_KINDS.
	pipelineOpts = append(pipelineOpts, embeddings.WithExpandSymbolKinds(cfg.ExpandSymbolKinds))

	// Sparse embed (P2+P4): optional SPLADE gate. When SPARSE_EMBED_URL is
	// empty the sparseClient is nil — Pipeline stays dense-only AND the P4
	// sparse retrieval arm in handleSemanticHits is skipped entirely
	// (byte-identical to pre-P4 behavior). Token auto-resolved from
	// EMBED_TOKEN env by go-kit/sparse v2 NewHTTPSparseEmbedder.
	//
	// wireSparse also publishes gocode_sparse_embedder_active so a misconfigured
	// SPARSE_EMBED_URL (or vocab mismatch) that silently disables the sparse
	// arm is visible on /metrics (#602), and warns (via warnSparseDisabled) when
	// the arm is off but a positive sparse weight is set.
	sparseClient, sparseOpts := wireSparse(cfg, rrfWeights)
	pipelineOpts = append(pipelineOpts, sparseOpts...)

	// QueryClient wraps ec with the model-correct retrieval prefix.
	// For code-rank-embed: prepends the required query prefix on EmbedQuery.
	// For all other models: returns ec unwrapped (zero overhead, no allocation).
	// Document embedding (Pipeline.Embed) always uses ec directly — never QueryClient.
	qc := embeddings.NewQueryClient(ec, cfg.EmbedModel)

	setCapability("semantic_search", true)
	return SemanticDeps{
		Client:       ec,
		QueryClient:  qc,
		Store:        es,
		Pipeline:     embeddings.NewPipeline(ec, es, cfg.EmbedModel, pipelineOpts...),
		AnalyzeDeps:  deps,
		Expander:     embeddings.NewExpander(agePool),
		GraphStore:   graphStore,
		OxCodes:      buildOxCodesClient(cfg),
		RRFWeights:   rrfWeights,
		SparseClient: sparseClient,
		KeywordArm:   cfg.KeywordArm,
	}
}

// warnSparseDisabled emits a startup WARN when the sparse retrieval arm is
// expected to contribute (RRF_WEIGHT_SPARSE > 0) but SPARSE_EMBED_URL is not
// configured. Without this warning, an operator who increases RRF_WEIGHT_SPARSE
// post-A/B would see ranking quality degrade silently — the sparse arm
// contributes nothing because sparseClient is nil.
//
// Called from newSemanticDeps in the else branch of the sparse embedder
// construction. Extracted as a standalone function for testability (the caller
// path requires a live pgpool that tests cannot easily provide).
func warnSparseDisabled(cfg Config, rrfWeights embeddings.RRFWeights) {
	if cfg.SparseEmbedURL == "" && rrfWeights.Sparse > 0 {
		slog.Warn("config: sparse retrieval disabled — SPARSE_EMBED_URL not set but RRF_WEIGHT_SPARSE > 0; sparse arm will not contribute to ranking",
			slog.String("env_var", "SPARSE_EMBED_URL"),
			slog.Float64("rrf_weight_sparse", rrfWeights.Sparse),
		)
	}
}
