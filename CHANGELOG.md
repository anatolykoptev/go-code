# Changelog

**2026-07-17:** The project was renamed from **go-code** to **Vaelor**. Older entries refer to the project under its former name and are left intact.

## [1.61.1](https://github.com/anatolykoptev/vaelor/compare/v1.61.0...v1.61.1) (2026-07-26)


### Fixed

* **build-info:** bake dozor-injected git SHA into gocode_build_info ([#749](https://github.com/anatolykoptev/vaelor/issues/749)) ([b7f37c5](https://github.com/anatolykoptev/vaelor/commit/b7f37c5997ebd7cf633e513adc63bf92b9a6a465))
* **callgraph:** answer with the tree-sitter tier instead of waiting out the deadline on a cold Go repo ([#737](https://github.com/anatolykoptev/vaelor/issues/737)) ([d78a294](https://github.com/anatolykoptev/vaelor/commit/d78a2942f62b230fb174413a4d7eaef7eaedbad8))

## [1.61.0](https://github.com/anatolykoptev/vaelor/compare/v1.60.0...v1.61.0) (2026-07-26)


### Added

* **argnorm:** make find_bugs a real alias, honest hints for the rest ([#744](https://github.com/anatolykoptev/vaelor/issues/744)) ([3a672e7](https://github.com/anatolykoptev/vaelor/commit/3a672e76add878024d6e4533e8569e1540bbc013))

## [1.60.0](https://github.com/anatolykoptev/vaelor/compare/v1.59.13...v1.60.0) (2026-07-25)


### Added

* **mcpmeta:** adopt the result-shortening ladder in understand and impact_analysis ([#730](https://github.com/anatolykoptev/vaelor/issues/730)) ([1dbf9d5](https://github.com/anatolykoptev/vaelor/commit/1dbf9d57a02db684a84858ccc2b38229d36a222e))

## [1.59.13](https://github.com/anatolykoptev/vaelor/compare/v1.59.12...v1.59.13) (2026-07-25)


### Fixed

* **embeddings:** route IncrementalSync same-SHA fast path through shared reconcile seam ([#720](https://github.com/anatolykoptev/vaelor/issues/720)) ([#725](https://github.com/anatolykoptev/vaelor/issues/725)) ([e33195a](https://github.com/anatolykoptev/vaelor/commit/e33195a907411e921f1b04157f4aeee0122f6d91))
* **semantic_search:** return no-match, not "indexing", when an indexed repo has zero hits ([#722](https://github.com/anatolykoptev/vaelor/issues/722)) ([7620567](https://github.com/anatolykoptev/vaelor/commit/76205676608e0af3680ab68a3f73d5f13b9a0437))

## [1.59.12](https://github.com/anatolykoptev/vaelor/compare/v1.59.11...v1.59.12) (2026-07-25)


### Fixed

* **embeddings:** dry-run reconcile on fast path, 3-way root resolution, pathless enumeration ([#711](https://github.com/anatolykoptev/vaelor/issues/711), [#714](https://github.com/anatolykoptev/vaelor/issues/714)) ([#719](https://github.com/anatolykoptev/vaelor/issues/719)) ([5bb470a](https://github.com/anatolykoptev/vaelor/commit/5bb470a7fa6ed9bc7760565ed03c750ea7a7e5db))
* **mcpmeta:** wire outputDir into semantic_search and restore ladder laziness ([#685](https://github.com/anatolykoptev/vaelor/issues/685) part 2) ([#716](https://github.com/anatolykoptev/vaelor/issues/716)) ([ed510d3](https://github.com/anatolykoptev/vaelor/commit/ed510d325a05a074985a68b0e99bee1021e663a5))

## [1.59.11](https://github.com/anatolykoptev/vaelor/compare/v1.59.10...v1.59.11) (2026-07-25)


### Fixed

* **parser:** thread ParseOpts.Parser through Svelte/Astro parse paths ([#713](https://github.com/anatolykoptev/vaelor/issues/713)) ([ea79c21](https://github.com/anatolykoptev/vaelor/commit/ea79c21ed6adc87b7c44dbc93ba70e93bee65cd6))

## [1.59.10](https://github.com/anatolykoptev/go-code/compare/v1.59.9...v1.59.10) (2026-07-25)


### Fixed

* **embeddings:** reconcile stale file_path rows under live repo_keys ([#708](https://github.com/anatolykoptev/go-code/issues/708)) ([#710](https://github.com/anatolykoptev/go-code/issues/710)) ([3e0fb2c](https://github.com/anatolykoptev/go-code/commit/3e0fb2cbbaca8f517d0bec547d15aa028f2ab5c6))

## [1.59.9](https://github.com/anatolykoptev/vaelor/compare/v1.59.8...v1.59.9) (2026-07-25)


### Fixed

* **mcpmeta:** progressive result-shortening ladder for code_search ([#704](https://github.com/anatolykoptev/vaelor/issues/704)) ([de740f4](https://github.com/anatolykoptev/vaelor/commit/de740f4ed428ab14052f1d98cac99a268ee14985))

## [1.59.8](https://github.com/anatolykoptev/vaelor/compare/v1.59.7...v1.59.8) (2026-07-25)


### Fixed

* **retrieval:** gate stale AGE graph in RRF path — self-heal + observability + dark drop ([#691](https://github.com/anatolykoptev/vaelor/issues/691)) ([#700](https://github.com/anatolykoptev/vaelor/issues/700)) ([46c72cf](https://github.com/anatolykoptev/vaelor/commit/46c72cfa2035391338c7d04cc10badbe095a3b31))

## [1.59.7](https://github.com/anatolykoptev/vaelor/compare/v1.59.6...v1.59.7) (2026-07-25)


### Fixed

* **mcp:** revive remaining dead param descriptions + guard the tag name (Closes [#684](https://github.com/anatolykoptev/vaelor/issues/684)) ([#697](https://github.com/anatolykoptev/vaelor/issues/697)) ([06d07e6](https://github.com/anatolykoptev/vaelor/commit/06d07e63b8d7c50faee7e175d863509dc97214b4))

## [1.59.6](https://github.com/anatolykoptev/vaelor/compare/v1.59.5...v1.59.6) (2026-07-25)


### Documentation

* **mcp:** describe every param of the 10 highest-arity tools (refs [#684](https://github.com/anatolykoptev/vaelor/issues/684)) ([#695](https://github.com/anatolykoptev/vaelor/issues/695)) ([cef70b1](https://github.com/anatolykoptev/vaelor/commit/cef70b1ed3f59df692798fbddcda0b760691f8e5))

## [1.59.5](https://github.com/anatolykoptev/vaelor/compare/v1.59.4...v1.59.5) (2026-07-25)


### Fixed

* **config:** make silently-disabled capabilities loud and observable (Closes [#600](https://github.com/anatolykoptev/vaelor/issues/600), [#601](https://github.com/anatolykoptev/vaelor/issues/601), [#602](https://github.com/anatolykoptev/vaelor/issues/602), [#603](https://github.com/anatolykoptev/vaelor/issues/603)) ([#692](https://github.com/anatolykoptev/vaelor/issues/692)) ([d90e4e9](https://github.com/anatolykoptev/vaelor/commit/d90e4e93fcd8a9a80c8bf48336f6d9ecdcc59d2c))

## [1.59.4](https://github.com/anatolykoptev/go-code/compare/v1.59.3...v1.59.4) (2026-07-24)


### Fixed

* **index:** debounced background AGE-graph refresh on watched edits (Closes [#642](https://github.com/anatolykoptev/go-code/issues/642)) ([#689](https://github.com/anatolykoptev/go-code/issues/689)) ([64e7719](https://github.com/anatolykoptev/go-code/commit/64e771951b7c26744d7efb3c7e35732cd377c7ed))

## [1.59.3](https://github.com/anatolykoptev/vaelor/compare/v1.59.2...v1.59.3) (2026-07-24)


### Fixed

* **ingest:** single-flight per repo_key on cold external-repo clone (Closes [#678](https://github.com/anatolykoptev/vaelor/issues/678)) ([#681](https://github.com/anatolykoptev/vaelor/issues/681)) ([70306f1](https://github.com/anatolykoptev/vaelor/commit/70306f1cce3285b14855bfbfe08b4e886a5af2e7))

## [1.59.2](https://github.com/anatolykoptev/vaelor/compare/v1.59.1...v1.59.2) (2026-07-24)


### Fixed

* **ingest:** plain-rename fallback for first-clone / RENAME_EXCHANGE-unsupported (Closes [#676](https://github.com/anatolykoptev/vaelor/issues/676)) ([#677](https://github.com/anatolykoptev/vaelor/issues/677)) ([a74d820](https://github.com/anatolykoptev/vaelor/commit/a74d82063a373532975a25b9838ff3596d0675fc))

## [1.59.1](https://github.com/anatolykoptev/vaelor/compare/v1.59.0...v1.59.1) (2026-07-24)


### Fixed

* stale keyword-arm log note + eval repo-map slug matching ([#673](https://github.com/anatolykoptev/vaelor/issues/673)) ([10b28c2](https://github.com/anatolykoptev/vaelor/commit/10b28c2657271cd95b41284182bd3b6946453ed5))

## [1.59.0](https://github.com/anatolykoptev/vaelor/compare/v1.58.0...v1.59.0) (2026-07-24)


### Added

* **parser:** macro/module/type-alias kinds + [@doc](https://github.com/doc) LOI embed text behind EXPAND_SYMBOL_KINDS (dark) ([#671](https://github.com/anatolykoptev/vaelor/issues/671)) ([412bf9f](https://github.com/anatolykoptev/vaelor/commit/412bf9f4086bda08063a13bdef2ffd7a9f447e62))

## [1.58.0](https://github.com/anatolykoptev/vaelor/compare/v1.57.0...v1.58.0) (2026-07-24)


### Added

* **rrf:** configurable rank_window_size cap on MergeRRF (default unbounded) ([#669](https://github.com/anatolykoptev/vaelor/issues/669)) ([5cc9fa2](https://github.com/anatolykoptev/vaelor/commit/5cc9fa2cb2a992ab36c0a89788a861ed128c77ac))

## [1.57.0](https://github.com/anatolykoptev/vaelor/compare/v1.56.0...v1.57.0) (2026-07-24)


### Added

* **retrieval:** default keyword arm to bm25f at RRF weight 0.5 (empirical tuning) ([#667](https://github.com/anatolykoptev/vaelor/issues/667)) ([a83534c](https://github.com/anatolykoptev/vaelor/commit/a83534c540d2616b1d6c4bb8798bf4c49a96cb56))

## [1.56.0](https://github.com/anatolykoptev/vaelor/compare/v1.55.2...v1.56.0) (2026-07-24)


### Added

* **ranking:** tokenized BM25F term matching + Zoekt field weights/test penalty ([#665](https://github.com/anatolykoptev/vaelor/issues/665)) ([b7880ab](https://github.com/anatolykoptev/vaelor/commit/b7880aba6c9095fed7244212095de0e89a0eff87))

## [1.55.2](https://github.com/anatolykoptev/go-code/compare/v1.55.1...v1.55.2) (2026-07-24)


### Fixed

* **embeddings:** index type-level symbols (class/interface/trait/struct/enum), not just func/method ([#658](https://github.com/anatolykoptev/go-code/issues/658)) ([df2275c](https://github.com/anatolykoptev/go-code/commit/df2275c63e3e5081e718127eb6c3f582d0247164))

## [1.55.1](https://github.com/anatolykoptev/vaelor/compare/v1.55.0...v1.55.1) (2026-07-24)


### Fixed

* **eval:** retry transient tool signals, warm indexes before measuring, distinct python golden slug ([#656](https://github.com/anatolykoptev/vaelor/issues/656)) ([ea4a614](https://github.com/anatolykoptev/vaelor/commit/ea4a61431e53a1ff3207567e00762a60e88d2a7f))

## [1.55.0](https://github.com/anatolykoptev/go-code/compare/v1.54.0...v1.55.0) (2026-07-24)


### Added

* **eval:** repo_analyze mode + real fusion-mode gate ([#645](https://github.com/anatolykoptev/go-code/issues/645)) ([#651](https://github.com/anatolykoptev/go-code/issues/651)) ([84b044c](https://github.com/anatolykoptev/go-code/commit/84b044c69f1dddb66ff3a4d0d6495bdd360680c5))

## [1.54.0](https://github.com/anatolykoptev/go-code/compare/v1.53.2...v1.54.0) (2026-07-24)


### Added

* **eval:** latency, per-language, repo-map, keyword-arm/fusion gate flags ([#645](https://github.com/anatolykoptev/go-code/issues/645)) ([#647](https://github.com/anatolykoptev/go-code/issues/647)) ([f840c69](https://github.com/anatolykoptev/go-code/commit/f840c69e87d9ea5a56fcc4f40346f5a21314c482))

## [1.53.2](https://github.com/anatolykoptev/vaelor/compare/v1.53.1...v1.53.2) (2026-07-24)


### Fixed

* **ranking:** score BM25F candidates against their own document (B1, [#640](https://github.com/anatolykoptev/vaelor/issues/640)) ([#646](https://github.com/anatolykoptev/vaelor/issues/646)) ([b5180c8](https://github.com/anatolykoptev/vaelor/commit/b5180c884c6210b7a87b09d70a8d112221bbb3b7))

## [1.53.1](https://github.com/anatolykoptev/vaelor/compare/v1.53.0...v1.53.1) (2026-07-23)


### Fixed

* **codegraph:** create metadata tables in app schema + idempotent guarded ownership self-heal ([#520](https://github.com/anatolykoptev/vaelor/issues/520)) ([#638](https://github.com/anatolykoptev/vaelor/issues/638)) ([624db9f](https://github.com/anatolykoptev/vaelor/commit/624db9f8e774fe73f9ffe73de5acfa755a15801b))

## [1.53.0](https://github.com/anatolykoptev/vaelor/compare/v1.52.5...v1.53.0) (2026-07-23)


### Added

* **metrics:** instrument 7 silent failure classes ([#610](https://github.com/anatolykoptev/vaelor/issues/610)) ([#634](https://github.com/anatolykoptev/vaelor/issues/634)) ([edc143c](https://github.com/anatolykoptev/vaelor/commit/edc143cffb919390cd4c333ef16a7bcdf7ef8a13))

## [1.52.5](https://github.com/anatolykoptev/vaelor/compare/v1.52.4...v1.52.5) (2026-07-23)


### Fixed

* **config:** wire MaxRepoBytes/GithubSearchRepos, warn inert rank-weights, drop dead wphooks ([#604](https://github.com/anatolykoptev/vaelor/issues/604)-606) ([#633](https://github.com/anatolykoptev/vaelor/issues/633)) ([7df04e9](https://github.com/anatolykoptev/vaelor/commit/7df04e9ccf5c4ee6348209327b1d8cbe55b141ac))

## [1.52.4](https://github.com/anatolykoptev/vaelor/compare/v1.52.3...v1.52.4) (2026-07-23)


### Fixed

* **config:** warn on silent feature-disable ([#599](https://github.com/anatolykoptev/vaelor/issues/599)-603) ([#630](https://github.com/anatolykoptev/vaelor/issues/630)) ([f79d4a9](https://github.com/anatolykoptev/vaelor/commit/f79d4a9ea65c9f430a39a99dd2fc43448caf611b))

## [1.52.3](https://github.com/anatolykoptev/vaelor/compare/v1.52.2...v1.52.3) (2026-07-23)


### Fixed

* **suggest_reviewers:** surface real co-change coupling across renames ([#355](https://github.com/anatolykoptev/vaelor/issues/355)) ([#626](https://github.com/anatolykoptev/vaelor/issues/626)) ([bf1fe58](https://github.com/anatolykoptev/vaelor/commit/bf1fe588617b2f05c91596509c053e4e997adad5))

## [1.52.2](https://github.com/anatolykoptev/vaelor/compare/v1.52.1...v1.52.2) (2026-07-23)


### Fixed

* cancel metric-gauge tickers + bound federated co-change cache ([#624](https://github.com/anatolykoptev/vaelor/issues/624)) ([7f78bd1](https://github.com/anatolykoptev/vaelor/commit/7f78bd17d42536f9e7902d2f4c5544b3ae4fff9d))
* **mcp:** bounded partial results for explore/code_compare/dataflow under deadline ([#534](https://github.com/anatolykoptev/vaelor/issues/534)/[#566](https://github.com/anatolykoptev/vaelor/issues/566)/[#565](https://github.com/anatolykoptev/vaelor/issues/565)) ([#623](https://github.com/anatolykoptev/vaelor/issues/623)) ([085c641](https://github.com/anatolykoptev/vaelor/commit/085c6412ca06f4edc04712f225097fa1598e9f68))

## [1.52.1](https://github.com/anatolykoptev/vaelor/compare/v1.52.0...v1.52.1) (2026-07-23)


### Fixed

* **mcp:** surface required repo, limit alias, 408 retry, focus enum guidance ([#621](https://github.com/anatolykoptev/vaelor/issues/621)) ([6e75892](https://github.com/anatolykoptev/vaelor/commit/6e75892e9ef3cff45a9da9e0489001df1e33b00b))

## [1.52.0](https://github.com/anatolykoptev/vaelor/compare/v1.51.4...v1.52.0) (2026-07-23)


### Added

* **review_delta:** analyze impact against the head ref's worktree ([#583](https://github.com/anatolykoptev/vaelor/issues/583)) ([#619](https://github.com/anatolykoptev/vaelor/issues/619)) ([b146c81](https://github.com/anatolykoptev/vaelor/commit/b146c81056c82021462517962cb638e553c381c4))


### Fixed

* **codegraph:** validate content hash on TTL cache hit ([#613](https://github.com/anatolykoptev/vaelor/issues/613)) ([3c99a1b](https://github.com/anatolykoptev/vaelor/commit/3c99a1b56a7f887dd46674608166e549af94f200))

## [1.51.4](https://github.com/anatolykoptev/vaelor/compare/v1.51.3...v1.51.4) (2026-07-23)


### Fixed

* **codegraph:** invalidate existsCache on DropGraph ([#593](https://github.com/anatolykoptev/vaelor/issues/593)) ([#615](https://github.com/anatolykoptev/vaelor/issues/615)) ([99a04e9](https://github.com/anatolykoptev/vaelor/commit/99a04e92ad070f72699b0fc3ed7c62dd45272741))

## [1.51.3](https://github.com/anatolykoptev/vaelor/compare/v1.51.2...v1.51.3) (2026-07-23)


### Fixed

* **parser:** synchronize handler registry data race + test-hygiene flakes ([#573](https://github.com/anatolykoptev/vaelor/issues/573)) ([#614](https://github.com/anatolykoptev/vaelor/issues/614)) ([40156e7](https://github.com/anatolykoptev/vaelor/commit/40156e79847bdd2b56af2bfb5e087ec363b48397))

## [1.51.2](https://github.com/anatolykoptev/vaelor/compare/v1.51.1...v1.51.2) (2026-07-23)


### Fixed

* **embeddings:** harden orphan class — single-flight sync index ([#589](https://github.com/anatolykoptev/vaelor/issues/589)) + cascade trigger ([#588](https://github.com/anatolykoptev/vaelor/issues/588)) ([#612](https://github.com/anatolykoptev/vaelor/issues/612)) ([3e17042](https://github.com/anatolykoptev/vaelor/commit/3e17042c052930497e2d2d9e7a8910401d1b4cb0))

## [1.51.1](https://github.com/anatolykoptev/vaelor/compare/v1.51.0...v1.51.1) (2026-07-22)


### Fixed

* **embeddings:** stop producing first-index orphans (retry + fail-closed compensate) ([#590](https://github.com/anatolykoptev/vaelor/issues/590)) ([e492dff](https://github.com/anatolykoptev/vaelor/commit/e492dffac580f35e07bc4d1ca40e748e97165408))

## [1.51.0](https://github.com/anatolykoptev/vaelor/compare/v1.50.2...v1.51.0) (2026-07-22)


### Added

* **orphan_sweep:** dry-run by default, explicit dry_run=false to delete ([#586](https://github.com/anatolykoptev/vaelor/issues/586)) ([efa9620](https://github.com/anatolykoptev/vaelor/commit/efa96209c6a244f3a282807a08cb28e275368eca))

## [1.50.2](https://github.com/anatolykoptev/vaelor/compare/v1.50.1...v1.50.2) (2026-07-22)


### Fixed

* **mcp-ux:** 7 live-probe residuals — compare deadline, hint-on-error, budget wiring ([#584](https://github.com/anatolykoptev/vaelor/issues/584)) ([d519960](https://github.com/anatolykoptev/vaelor/commit/d519960f9325149b7c147a111c24e11d33d00deb))

## [1.50.1](https://github.com/anatolykoptev/vaelor/compare/v1.50.0...v1.50.1) (2026-07-22)


### Fixed

* **mcp-ux:** make repo schema-optional so [#569](https://github.com/anatolykoptev/vaelor/issues/569) inference actually runs ([#578](https://github.com/anatolykoptev/vaelor/issues/578)) ([a8f57c1](https://github.com/anatolykoptev/vaelor/commit/a8f57c14db23939e5fa294e980a3bdc57afeaba6))

## [1.50.0](https://github.com/anatolykoptev/vaelor/compare/v1.49.0...v1.50.0) (2026-07-22)


### Added

* **mcp-ux:** response budgets, pagination, soft deadlines, took_ms ([#576](https://github.com/anatolykoptev/vaelor/issues/576)) ([0f2c9ac](https://github.com/anatolykoptev/vaelor/commit/0f2c9ac76d6651b4cd90b96bee0267ab6d72fd7e))

## [1.49.0](https://github.com/anatolykoptev/vaelor/compare/v1.48.1...v1.49.0) (2026-07-22)


### Added

* **mcp-ux:** tolerant arg normalization, repo inference, did-you-mean ([#574](https://github.com/anatolykoptev/vaelor/issues/574)) ([f013286](https://github.com/anatolykoptev/vaelor/commit/f013286b1532abeb3355e339b30bd5cca87d1cb3))

## [1.48.1](https://github.com/anatolykoptev/vaelor/compare/v1.48.0...v1.48.1) (2026-07-20)


### Fixed

* **cli:** restore search subcommand + fix version embedding ([#560](https://github.com/anatolykoptev/vaelor/issues/560)) ([cb9eadb](https://github.com/anatolykoptev/vaelor/commit/cb9eadbd5620d73704e400191da32caa0d60eebe))
* **deps:** bump go-kit to v0.97.3, re-vendor to restore cli/watcher ([#562](https://github.com/anatolykoptev/vaelor/issues/562)) [no-deploy] ([ab2c17a](https://github.com/anatolykoptev/vaelor/commit/ab2c17a5edb5da3e9b6dda397fd5305b1c2fcb1d))

## [1.48.0](https://github.com/anatolykoptev/vaelor/compare/v1.47.0...v1.48.0) (2026-07-19)


### Added

* **research:** add phase-timer logging to code_research pipeline ([#558](https://github.com/anatolykoptev/vaelor/issues/558)) ([7f3521c](https://github.com/anatolykoptev/vaelor/commit/7f3521c29ea89d9504565f6ad769e3559dadaf3d))

## [1.47.0](https://github.com/anatolykoptev/vaelor/compare/v1.46.4...v1.47.0) (2026-07-19)


### Added

* **ci:** prebuilt vaelor-test-pg GHCR image + switch preflight to pull ([#556](https://github.com/anatolykoptev/vaelor/issues/556)) ([345ecd8](https://github.com/anatolykoptev/vaelor/commit/345ecd8b2e2611cecf30a7ba2fafde6f6b3a91d2))

## [1.46.4](https://github.com/anatolykoptev/vaelor/compare/v1.46.3...v1.46.4) (2026-07-19)


### Fixed

* **dockerignore:** include vendor/ in Docker build context for -mod=vendor ([ffcb553](https://github.com/anatolykoptev/vaelor/commit/ffcb553f436fc70d372734f71970b7bb7cc4318a))

## [1.46.3](https://github.com/anatolykoptev/vaelor/compare/v1.46.2...v1.46.3) (2026-07-19)


### Fixed

* **dockerfile:** remove go mod download step (incompatible with -mod=vendor) ([c4c4200](https://github.com/anatolykoptev/vaelor/commit/c4c420080bf6071d61117195bb6b9491125dfc29))

## [1.46.2](https://github.com/anatolykoptev/vaelor/compare/v1.46.1...v1.46.2) (2026-07-19)


### Fixed

* re-vendor to sync modules.txt with go.mod (restore go-kit/cli + watcher) ([affa7c1](https://github.com/anatolykoptev/vaelor/commit/affa7c1b9535edf024b7f287acb55ca9741c677f))
* remove accidental backup dirs from vendor ([2e4dab2](https://github.com/anatolykoptev/vaelor/commit/2e4dab2b75047fd73f2e81df95c47de9d7d0f671))

## [1.46.1](https://github.com/anatolykoptev/vaelor/compare/v1.46.0...v1.46.1) (2026-07-19)


### Fixed

* **dockerfile:** add -mod=vendor for vendor-only go-kit packages ([4888d50](https://github.com/anatolykoptev/vaelor/commit/4888d5042c0bcc785361b7b77e0010556e2c7d26))

## [1.46.0](https://github.com/anatolykoptev/vaelor/compare/v1.45.1...v1.46.0) (2026-07-19)


### Added

* **analyze:** rank.go fusion via WeightedRRF (opt-in via ANALYZE_RANK_FUSION_MODE=rrf) ([b92c1c0](https://github.com/anatolykoptev/vaelor/commit/b92c1c0f276aa643ceade112d2861be1fb3a73cf))
* **analyze:** rank.go fusion via WeightedRRF (opt-in) ([7acc8ba](https://github.com/anatolykoptev/vaelor/commit/7acc8ba662cfc84ef477ee5c791e17fb8a47d667))
* annotate understand/call_trace callers with production/test kind ([#491](https://github.com/anatolykoptev/vaelor/issues/491)) ([#508](https://github.com/anatolykoptev/vaelor/issues/508)) ([eeea22c](https://github.com/anatolykoptev/vaelor/commit/eeea22cdce6e94db093a818f3905f348efec19af))
* **autoindex:** skip repos whose main branch hasn't moved ([#10](https://github.com/anatolykoptev/vaelor/issues/10)) ([54e76ca](https://github.com/anatolykoptev/vaelor/commit/54e76ca38de3a4d33a45f0def61d5c2ab4d5efca))
* **b3:** expand body window to +50 lines when EndLine unknown ([#86](https://github.com/anatolykoptev/vaelor/issues/86)) ([4b5a36d](https://github.com/anatolykoptev/vaelor/commit/4b5a36daaeae03ae4f12b97a8951af10392ad835))
* **bootstrap:** self-grant ownership + create perms; fail-fast on missing ag_catalog access ([#112](https://github.com/anatolykoptev/vaelor/issues/112)) ([cb22465](https://github.com/anatolykoptev/vaelor/commit/cb224655ff9b958a3ed8de1c4fa7311b88b6d5d8))
* **call_trace:** add refresh parameter to bypass in-memory cache ([#457](https://github.com/anatolykoptev/vaelor/issues/457)) ([09ba0c8](https://github.com/anatolykoptev/vaelor/commit/09ba0c867f69584bbf8d629c986f93163b9944a3))
* **call_trace:** fast path from AGE graph — avoid 2-60s repo reparse ([#434](https://github.com/anatolykoptev/vaelor/issues/434)) ([cfe8e3e](https://github.com/anatolykoptev/vaelor/commit/cfe8e3e670d6c888d4bf6b8d02d07a84d66d1f10))
* **callgraph:** eager GOCACHE warm at startup for AUTO_INDEX_DIRS Go repos ([#35](https://github.com/anatolykoptev/vaelor/issues/35)) ([6270d1b](https://github.com/anatolykoptev/vaelor/commit/6270d1bb298cca12412156fd171d785ea7d01cd7))
* **codegraph:** build FETCHES FromKey as Handler:File composite (Wave 5) ([#154](https://github.com/anatolykoptev/vaelor/issues/154)) ([b97aeff](https://github.com/anatolykoptev/vaelor/commit/b97aeff5e62459beb43df7d47a744cd584613f12))
* **codegraph:** build HANDLES FromKey as Handler:File composite (Wave 6) ([#155](https://github.com/anatolykoptev/vaelor/issues/155)) ([6da02bb](https://github.com/anatolykoptev/vaelor/commit/6da02bb1ea7d4efe14868f08d4d3b7dc4961da85))
* **codegraph:** populate Go IMPLEMENTS edges via go/types satisfaction ([#220](https://github.com/anatolykoptev/vaelor/issues/220)) ([ba11db7](https://github.com/anatolykoptev/vaelor/commit/ba11db70053325bc2be367b2b4adf48e23c9b87a))
* **codegraph:** preflight graph-existence check on read-path ([#43](https://github.com/anatolykoptev/vaelor/issues/43)) ([4772d38](https://github.com/anatolykoptev/vaelor/commit/4772d38e458ef1998acddc5f2a43a75c2189548e))
* **compare:** wire ParseCache through BuildSnapshot/CompareInput ([eee4715](https://github.com/anatolykoptev/vaelor/commit/eee47151596cb2ba5f77a615e866187912c6dcbf))
* debug_investigate MCP tool — Prometheus + Jaeger + symbol correlation ([#56](https://github.com/anatolykoptev/vaelor/issues/56)) ([28ae34e](https://github.com/anatolykoptev/vaelor/commit/28ae34ec42567403845e1a9a027ccba3ce2de496))
* **debug_investigate:** latency + saturation spike detection (Phase β.4) ([#63](https://github.com/anatolykoptev/vaelor/issues/63)) ([4cbf8c3](https://github.com/anatolykoptev/vaelor/commit/4cbf8c3ee1aa2ed22beaf60acfbb56f4fdb77baa))
* **debug_investigate:** Phase 3 — direct symbol resolution via OTEL code.* tags (closes [#74](https://github.com/anatolykoptev/vaelor/issues/74)) ([#77](https://github.com/anatolykoptev/vaelor/issues/77)) ([36bc2e5](https://github.com/anatolykoptev/vaelor/commit/36bc2e59002331ed07b8bff6eab773ab6a27eefe))
* **debug_investigate:** Phase 6 — log excerpts via dozor side-car (β.3b) ([#66](https://github.com/anatolykoptev/vaelor/issues/66)) ([6807b86](https://github.com/anatolykoptev/vaelor/commit/6807b86cfc7aa351feaa8a00252bf8dd4528d826))
* **debug_investigate:** Phase α — auto-discovery, sourcemap resolver, hint_kind, SRP split ([#61](https://github.com/anatolykoptev/vaelor/issues/61)) ([bbbe261](https://github.com/anatolykoptev/vaelor/commit/bbbe2614b9fe90c41d56f516d66a0fe61f63221e))
* **debug_investigate:** Phase γ.B — dead-code filter + impact + symbol body ([#69](https://github.com/anatolykoptev/vaelor/issues/69)) ([457f6ce](https://github.com/anatolykoptev/vaelor/commit/457f6ce94053e663ddbe5932353538f5f743b036))
* **debug_investigate:** Phase γ.C — historical incidents + hint-driven candidate hypotheses ([#70](https://github.com/anatolykoptev/vaelor/issues/70)) ([838c520](https://github.com/anatolykoptev/vaelor/commit/838c52073357e7525cc890ac21b0f8e260bcda3f))
* **debug_investigate:** Phase γ.D — multi-signal fusion + recent diff embedding ([#71](https://github.com/anatolykoptev/vaelor/issues/71)) ([4f6aac6](https://github.com/anatolykoptev/vaelor/commit/4f6aac64e903c23701faa9916ceb7a55f9029ef1))
* **debug_investigate:** Phase γ.E — LLM cache + structured next_check (machine-readable) ([#72](https://github.com/anatolykoptev/vaelor/issues/72)) ([0bcec13](https://github.com/anatolykoptev/vaelor/commit/0bcec13812f7826e64e3b1a47460a1dc37ab4300))
* **debug_investigate:** Prometheus alerts ingestion (Phase β.5) — captures constant-state invariant violations ([#64](https://github.com/anatolykoptev/vaelor/issues/64)) ([21c333b](https://github.com/anatolykoptev/vaelor/commit/21c333b9c83e08c3cca4f8003f8d1a6983f5ebd2))
* **debug_investigate:** Sprint B1 — function body in LLM context (deep code reasoning) ([#79](https://github.com/anatolykoptev/vaelor/issues/79)) ([397c286](https://github.com/anatolykoptev/vaelor/commit/397c286a946dad05dbe2b145aacf73abca2f9a19))
* **debug_investigate:** Sprint B2 — upstream callgraph walk for root-cause discovery ([#80](https://github.com/anatolykoptev/vaelor/issues/80)) ([3e5b113](https://github.com/anatolykoptev/vaelor/commit/3e5b113af14b3501e2344666703ef856751edab7))
* **debug_investigate:** Sprint B4/B5 — downstream callees walk + body excerpts top-5 ([#88](https://github.com/anatolykoptev/vaelor/issues/88)) ([87f1cbd](https://github.com/anatolykoptev/vaelor/commit/87f1cbd1d1680b346423497d8e5217f8a78ba47d))
* drop-in httpmw.NewServeMux + slogh trace correlation ([#95](https://github.com/anatolykoptev/vaelor/issues/95)) ([68d98c3](https://github.com/anatolykoptev/vaelor/commit/68d98c371982efe00479638896c6d01509a30db1))
* dual-read VAELOR_/GO_CODE_ env vars (rebrand) [no-deploy] ([386fc78](https://github.com/anatolykoptev/vaelor/commit/386fc78bf5e611b43f1eb10f95b59313b09a40e1))
* **embeddings:** autoindex concurrency cap + retry-with-backoff (28min→14min cold-start) ([#4](https://github.com/anatolykoptev/vaelor/issues/4)) ([f01941e](https://github.com/anatolykoptev/vaelor/commit/f01941ef67e2a53be16b4ea0ae16eea3ccdd0bbb))
* **embeddings:** cache symbol entries via go-kit cache.GetIfValid (-80% embed-server traffic) ([#5](https://github.com/anatolykoptev/vaelor/issues/5)) ([cd58aa3](https://github.com/anatolykoptev/vaelor/commit/cd58aa35aa53f1c3f0eb8528e513b472eaf267a3))
* **embeddings:** cut model from jina-code-v2 to code-rank-embed ([#231](https://github.com/anatolykoptev/vaelor/issues/231)) ([a4ebf24](https://github.com/anatolykoptev/vaelor/commit/a4ebf24d06d9e8a18e94da41419a5242b5ea4214))
* **embeddings:** enable graph, hotspot, and recency arms in semantic_search RRF ([d3c50ea](https://github.com/anatolykoptev/vaelor/commit/d3c50eae98bde3c04a29c592407426927d19abb3))
* **embeddings:** file-level IndexFile primitive for incremental indexing ([2264d0a](https://github.com/anatolykoptev/vaelor/commit/2264d0ab76b69f07ee1e7f07e15466580e27865f))
* **embeddings:** file-level IndexFile primitive for incremental indexing ([769860f](https://github.com/anatolykoptev/vaelor/commit/769860f31c82cff249e84db32494b54631480c02))
* **embeddings:** gocode_repo_info gauge — resolve opaque repo hash to path ([#227](https://github.com/anatolykoptev/vaelor/issues/227)) ([bfb3247](https://github.com/anatolykoptev/vaelor/commit/bfb3247d779000aa82899992d4bc825128d82db5))
* **embeddings:** IncrementalSync orchestrator using git-diff reconciliation ([0546414](https://github.com/anatolykoptev/vaelor/commit/0546414bc35a00301e197fad693f55ff078d1b0b))
* **embeddings:** IncrementalSync orchestrator using git-diff reconciliation ([0ebf6b4](https://github.com/anatolykoptev/vaelor/commit/0ebf6b4b13e1e049bd8d8456e037f8730c300ba9))
* **embeddings:** WeightedRRF static weights via RRF_WEIGHT_SEMANTIC/KEYWORD env ([#7](https://github.com/anatolykoptev/vaelor/issues/7)) ([e8f7f01](https://github.com/anatolykoptev/vaelor/commit/e8f7f014e768852de4d36e873824f0a1e7a73df3))
* **envdetect:** ADR 0002 Phase 0 — static build/test/install command detection ([#296](https://github.com/anatolykoptev/vaelor/issues/296)) ([eaff91b](https://github.com/anatolykoptev/vaelor/commit/eaff91b2b6422d2f0ee0ffffbdb9cdd86a81fffc))
* **eval:** offline retrieval-quality harness for go-code ([#6](https://github.com/anatolykoptev/vaelor/issues/6)) ([7d53d71](https://github.com/anatolykoptev/vaelor/commit/7d53d71dc41abaab6360c6266d593cb363b732fd))
* **federate:** deadline-bounded federated_cochange with partial results + background prep ([#171](https://github.com/anatolykoptev/vaelor/issues/171)) ([4023320](https://github.com/anatolykoptev/vaelor/commit/40233203ba117223cf4a606ff8ecc5ed1206eb40))
* find_duplicates — intra-repo semantic clone detector (5 phases) ([#215](https://github.com/anatolykoptev/vaelor/issues/215)) ([382805c](https://github.com/anatolykoptev/vaelor/commit/382805c34744b62140ad7d9c8fb9d91a103ad1e3))
* **fleet/ssh:** shadow-copy ~/.ssh to writable dir to bypass strict-mode check ([#130](https://github.com/anatolykoptev/vaelor/issues/130)) ([b6f8d8e](https://github.com/anatolykoptev/vaelor/commit/b6f8d8e0347c4f6f31a7e1cf97aef779759bd390))
* **fleet:** multi-host hosts[] input + cross-host SiblingDrift ([#132](https://github.com/anatolykoptev/vaelor/issues/132)) ([6714adf](https://github.com/anatolykoptev/vaelor/commit/6714adff17d6891f6e515ef74d661f6e1b774ed1))
* **fleet:** runtime binary version awareness — fleet_versions tool + debug_investigate Phase 7 ([#124](https://github.com/anatolykoptev/vaelor/issues/124)) ([5dcdfdc](https://github.com/anatolykoptev/vaelor/commit/5dcdfdcb99a357d652cc20c16c41e55d2619696d))
* **fleet:** upstream changelog correlation for TagDrift rows ([#133](https://github.com/anatolykoptev/vaelor/issues/133)) ([28bc6bb](https://github.com/anatolykoptev/vaelor/commit/28bc6bb606abf6067ab4da2cfd7df6d05dc31c31))
* **forge:** GitHub App authentication for separate rate-limit pool ([#39](https://github.com/anatolykoptev/vaelor/issues/39)) ([45d1e74](https://github.com/anatolykoptev/vaelor/commit/45d1e742d483507f7f63b3344299bccdaafe2cde))
* **github_code_search:** add max_fragment_chars and max_total_chars ([#383](https://github.com/anatolykoptev/vaelor/issues/383)) ([a2113ce](https://github.com/anatolykoptev/vaelor/commit/a2113ce8038fc2243c3733c1f182585c5e485df7))
* **go-code:** add nullable sparse_embedding sparsevec column (SPLADE P1) ([#194](https://github.com/anatolykoptev/vaelor/issues/194)) ([8787cca](https://github.com/anatolykoptev/vaelor/commit/8787ccad466161d7a893f86088615ba1c84728a9))
* **go-code:** binary stale-demote safety-net for missed orphans (defense-in-depth) ([#210](https://github.com/anatolykoptev/vaelor/issues/210)) ([01a43ec](https://github.com/anatolykoptev/vaelor/commit/01a43ec0e84d58cfd6d915ba87aace909b518c93))
* **go-code:** BM25F lexical search arm over trigram candidates (BM25F P3) ([#206](https://github.com/anatolykoptev/vaelor/issues/206)) ([4224932](https://github.com/anatolykoptev/vaelor/commit/4224932512c0e88d85dc6a5c5535202f286b9002))
* **go-code:** enable graph, hotspot, and recency RRF arms in semantic_search ([96eafed](https://github.com/anatolykoptev/vaelor/commit/96eafed43046561d65719a81eb3a383e1139582d))
* **go-code:** flag-gated BM25F keyword arm with grep fallback (BM25F P4) ([#207](https://github.com/anatolykoptev/vaelor/issues/207)) ([a6c31ac](https://github.com/anatolykoptev/vaelor/commit/a6c31ac29fc9c02a792fa3189fe64210cf72461f))
* **go-code:** gated SPLADE sparse-vector indexing, batched by server cap (SPLADE P2) ([#195](https://github.com/anatolykoptev/vaelor/issues/195)) ([cf35ba0](https://github.com/anatolykoptev/vaelor/commit/cf35ba059d0df931cd105fbac39cecf9c9721cd5))
* **go-code:** graph-candidate generator as dark-launched 4th RRF arm (graph-first P1) ([#212](https://github.com/anatolykoptev/vaelor/issues/212)) ([351b77b](https://github.com/anatolykoptev/vaelor/commit/351b77b7dcd21b7657f7e85982d48fdc3f6b068f))
* **go-code:** index-time named execution flows (graph-first Phase 2 CORE) ([#213](https://github.com/anatolykoptev/vaelor/issues/213)) ([a9c8713](https://github.com/anatolykoptev/vaelor/commit/a9c871326f509afc993569e22760ee44d12355c7))
* **go-code:** offline A/B harness for SPLADE arm (nDCG@10 + paired t-test gate, SPLADE P6) ([#199](https://github.com/anatolykoptev/vaelor/issues/199)) ([ee9df7d](https://github.com/anatolykoptev/vaelor/commit/ee9df7df7af9dd79f2ff92ddf79fc945f6be91eb))
* **go-code:** operator-triggered sparse_backfill MCP tool (SPLADE P5) ([#198](https://github.com/anatolykoptev/vaelor/issues/198)) ([6d0f695](https://github.com/anatolykoptev/vaelor/commit/6d0f6955393fd350578cdc805a961b347bc83f17))
* **go-code:** Phase 3a federated MCP foundation — repo resolver + cross-repo co-change ([#160](https://github.com/anatolykoptev/vaelor/issues/160)) ([ed9323d](https://github.com/anatolykoptev/vaelor/commit/ed9323d5ae7cce8b7bd73c313c9499c30eb38426))
* **go-code:** Phase 3a.1 — federated co-change signal quality (origin-dedup + lift + sw.js filter) ([#161](https://github.com/anatolykoptev/vaelor/issues/161)) ([c0246e5](https://github.com/anatolykoptev/vaelor/commit/c0246e5f92d8c9d9d94d3e6b85dbd28bd4c4d325))
* **go-code:** Phase 3a.2 — Dunning G² significance ranking (two-tier, support-first) ([#162](https://github.com/anatolykoptev/vaelor/issues/162)) ([ffb4358](https://github.com/anatolykoptev/vaelor/commit/ffb43587ac63aeac308d62a29bc77b288a664ff8))
* **go-code:** Phase 3a.3 — Wilson-LB ranking + ubiquitous-file filter (CodeScene/Evan-Miller port) ([#163](https://github.com/anatolykoptev/vaelor/issues/163)) ([9cb05f1](https://github.com/anatolykoptev/vaelor/commit/9cb05f18e4e52235582169bbe30387179124fda4))
* **go-code:** Phase B — semantic route-match verification (verified-first cross-repo coupling) ([#164](https://github.com/anatolykoptev/vaelor/issues/164)) ([fe0a037](https://github.com/anatolykoptev/vaelor/commit/fe0a037338b550818b05de3a428a59992d0f4b43))
* **go-code:** port repowise patterns — Phase 1 (_meta envelope + biomarkers + 2 new tools) ([#156](https://github.com/anatolykoptev/vaelor/issues/156)) ([784a026](https://github.com/anatolykoptev/vaelor/commit/784a02695677c72191f3554c287d78ac3529d06d))
* **go-code:** resolve relative TS/JS imports to their package container ([#187](https://github.com/anatolykoptev/vaelor/issues/187)) ([4958c53](https://github.com/anatolykoptev/vaelor/commit/4958c53a36e07476b784d2601fb08b39e72d2992))
* **go-code:** resolve TS $lib and @scope/workspace imports ([#189](https://github.com/anatolykoptev/vaelor/issues/189)) ([17a7679](https://github.com/anatolykoptev/vaelor/commit/17a76792984ec825a805aeb5e537296b7bd89f9e))
* **go-code:** sparse as dark-launched 3rd weighted-RRF arm (SPLADE P4) ([#197](https://github.com/anatolykoptev/vaelor/issues/197)) ([3d95701](https://github.com/anatolykoptev/vaelor/commit/3d957018c621aa5d4ddef00abb7ff315ef520f22))
* **go-code:** sparse retrieval + sparsevec HNSW index (SPLADE P3) ([#196](https://github.com/anatolykoptev/vaelor/issues/196)) ([a6cf22c](https://github.com/anatolykoptev/vaelor/commit/a6cf22c0c6b88c3223192d409fd904877e0f11a7))
* **go-kit/cli:** add generic cobra scaffold + MCP config-snippet printer ([#537](https://github.com/anatolykoptev/vaelor/issues/537)) ([190bdfb](https://github.com/anatolykoptev/vaelor/commit/190bdfb83c64c66959afbe81b48102050f418670))
* **go-kit/watcher:** add thin go-filewatcher/v2 adapter with debounce + ignore-dir ([#538](https://github.com/anatolykoptev/vaelor/issues/538)) ([8f2ab6c](https://github.com/anatolykoptev/vaelor/commit/8f2ab6cfb3040561ada4063562c681e5455acc09))
* **html:** Wave 3 — applicable cross-cuts + docs 15→16 + MAJOR-2 fix ([#152](https://github.com/anatolykoptev/vaelor/issues/152)) ([10a4a98](https://github.com/anatolykoptev/vaelor/commit/10a4a9887cd8244babaefdb1778796e0ab3066a2))
* **html:** Wave 4 — enclosing-template scope tracking → Route.Handler ([#153](https://github.com/anatolykoptev/vaelor/issues/153)) ([49be4a1](https://github.com/anatolykoptev/vaelor/commit/49be4a11d3df8dd7d5c7108ed71856fdd4c3c98d))
* **image:** add openssh-client to runtime so fleet_versions ssh-probe works ([#129](https://github.com/anatolykoptev/vaelor/issues/129)) ([8d18624](https://github.com/anatolykoptev/vaelor/commit/8d186247037be9c2f7345bc0b45c0da1908b089a))
* **importresolve:** stopgap virtual:* module resolution to defining package ([#423](https://github.com/anatolykoptev/vaelor/issues/423)) ([#425](https://github.com/anatolykoptev/vaelor/issues/425)) ([7044d7d](https://github.com/anatolykoptev/vaelor/commit/7044d7dd2b0598f8a446c889b00fd166b38c2c2a))
* **ingest:** add MaxFiles cap to SnapshotOpts and IngestOpts ([43d7ffc](https://github.com/anatolykoptev/vaelor/commit/43d7ffc4cda3b9e8d9de8f8208977d148ef14334))
* **ingest:** INDEX_SKIP_DIRS override + gocode_ingest_skipped_dirs_total counter ([#211](https://github.com/anatolykoptev/vaelor/issues/211)) ([0fc9100](https://github.com/anatolykoptev/vaelor/commit/0fc9100730fa60a0c5a1c186f6ebc27f3447d471))
* **ingest:** surface skip reasons in IngestResult + index.go log ([#113](https://github.com/anatolykoptev/vaelor/issues/113)) ([56541f3](https://github.com/anatolykoptev/vaelor/commit/56541f3a980d07a7735e5df45e1fb01b8f40d640))
* **investigate:** Tasks 5+6 — OperationToFuncName + Hypothesis/RankHypotheses ([#51](https://github.com/anatolykoptev/vaelor/issues/51)) ([1a1e4aa](https://github.com/anatolykoptev/vaelor/commit/1a1e4aaf9ae1179db5f187db4264f005ad3ba428))
* **investigate:** Tasks 7+8 — InvestigationStore + BuildSystemPrompt ([#52](https://github.com/anatolykoptev/vaelor/issues/52)) ([3c58b86](https://github.com/anatolykoptev/vaelor/commit/3c58b868b75661fc42bb327ad134fc65579c3cf1))
* **jaegerclient:** bootstrap Jaeger HTTP client + ListServices + FindTraces + GetTrace ([#47](https://github.com/anatolykoptev/vaelor/issues/47)) ([d6bc36a](https://github.com/anatolykoptev/vaelor/commit/d6bc36af2f4ead43053acec31d1cc586a72d52d6))
* **kotlin:** Wave 3 — cross-cutting integration (tested_by, speculative, astdiff, importcat, apisurf, delta) ([#146](https://github.com/anatolykoptev/vaelor/issues/146)) ([4a59643](https://github.com/anatolykoptev/vaelor/commit/4a5964377d35c1a1d12d8abd6a48584f6e93b97c))
* **llm:** circuit breaker + observability middleware for LLM client ([#120](https://github.com/anatolykoptev/vaelor/issues/120)) ([5407fc8](https://github.com/anatolykoptev/vaelor/commit/5407fc8f585fff10276d2e6999591548b9020c0c))
* **llm:** configurable cooldown TTL via LLM_COOLDOWN_SECONDS (default 15m) ([#234](https://github.com/anatolykoptev/vaelor/issues/234)) ([74fb6d5](https://github.com/anatolykoptev/vaelor/commit/74fb6d52105342306e771b91233a9d9ec7e1800c))
* **llm:** expose LLM_PER_ATTEMPT_TIMEOUT for model chains ([14ca39a](https://github.com/anatolykoptev/vaelor/commit/14ca39a4f2f3e0f7e16593e8b94d5f04f53100c5))
* **llm:** make LLM optional (Completer iface + per-tool degrade policy) ([#118](https://github.com/anatolykoptev/vaelor/issues/118)) ([0ada4c8](https://github.com/anatolykoptev/vaelor/commit/0ada4c8522538261c128a7731a7c5583f731e388))
* **llm:** wire LLM_MODEL_FALLBACK chain (Phase 2) ([e0c02df](https://github.com/anatolykoptev/vaelor/commit/e0c02dfd1e3de4f11ed7048bfcba9b8b8127f67f))
* **llm:** wire LLM_MODEL_FALLBACK chain (Phase 2) ([4a3815a](https://github.com/anatolykoptev/vaelor/commit/4a3815ac4fee82a4413996457b29a6b64b79ed62))
* **llm:** wire per-model cooldown + bump go-kit v0.83.0 ([#233](https://github.com/anatolykoptev/vaelor/issues/233)) ([07b291a](https://github.com/anatolykoptev/vaelor/commit/07b291a335da42da2eb07c52a7c44c482e6894bb))
* **mcp:** adopt NewServer + KeepAlive + SchemaCache + DisableLocalhostProtection ([#529](https://github.com/anatolykoptev/vaelor/issues/529)) ([a1b963c](https://github.com/anatolykoptev/vaelor/commit/a1b963c71e0d9ea2ee97f71b7c19e531b24a1de4))
* **metrics:** code_health/code_graph build-failure counters + AGE staleness gauge ([6fac47e](https://github.com/anatolykoptev/vaelor/commit/6fac47e3ca6bf59a92109f06eefc6ff6eebb1d33))
* **metrics:** observability counters for slug-normalize, files-changed, forge-resolve ([#30](https://github.com/anatolykoptev/vaelor/issues/30)) ([7d80669](https://github.com/anatolykoptev/vaelor/commit/7d80669ef87bb6970b4e1ce637e1d585aa9d982e))
* **metrics:** wire ModelFilterObserver to Prometheus counters ([#230](https://github.com/anatolykoptev/vaelor/issues/230)) ([2f4b395](https://github.com/anatolykoptev/vaelor/commit/2f4b39557635da43c5b86ebcdd3c6f172a0ac799))
* **otel:** instrument go-code with go-kit/tracing — Jaeger integration ([#87](https://github.com/anatolykoptev/vaelor/issues/87)) ([5cad582](https://github.com/anatolykoptev/vaelor/commit/5cad5828e18df8b479420a5bff510f4e9969177b))
* **oxcodes:** custom taint rules, anti-patterns, rewrite rejections, cache metrics ([#438](https://github.com/anatolykoptev/vaelor/issues/438)) ([59942f3](https://github.com/anatolykoptev/vaelor/commit/59942f33a15f2939165fd93db3886d75f7c30bc1))
* **parser, routes:** HTML/htmx Wave 2 — attribute extraction + routes/match_html ([#151](https://github.com/anatolykoptev/vaelor/issues/151)) ([5d9013d](https://github.com/anatolykoptev/vaelor/commit/5d9013d11561dd124d6c7feb309b1ab8f8f1e613))
* **parser:** astro alias resolution + vue SFC handler ([#241](https://github.com/anatolykoptev/vaelor/issues/241)) ([09fc9fc](https://github.com/anatolykoptev/vaelor/commit/09fc9fc2a15b92eba2454dfd1131f2ce70f28dfc))
* **parser:** Astro markup {expr} calls + refs via shared tsxLang reparse ([#269](https://github.com/anatolykoptev/vaelor/issues/269)) ([d687f70](https://github.com/anatolykoptev/vaelor/commit/d687f70a245c8d13fd653ba6d232750ada1b98e9))
* **parser:** HTML/htmx Wave 1 — handler + Go template preproc ([#150](https://github.com/anatolykoptev/vaelor/issues/150)) ([f93c209](https://github.com/anatolykoptev/vaelor/commit/f93c2090187b56f3fdfe0d90e36bd80c28cfefea))
* **parser:** Kotlin Wave 1 — handler + tag query ([#144](https://github.com/anatolykoptev/vaelor/issues/144)) ([45aace6](https://github.com/anatolykoptev/vaelor/commit/45aace66399856aad6d12b3f5a478261185ded45))
* **parser:** Kotlin Wave 2 — calls + rels + interface + sealed/enum ([#145](https://github.com/anatolykoptev/vaelor/issues/145)) ([5f517bb](https://github.com/anatolykoptev/vaelor/commit/5f517bb20c047f2611bd8b7903d6e600a49b7aa1))
* **parser:** Svelte component composition — TemplateRefs, USES edges, destructured $props() ([#270](https://github.com/anatolykoptev/vaelor/issues/270)) ([5fc3b26](https://github.com/anatolykoptev/vaelor/commit/5fc3b2688fbd4ff6d054be029aaf7460a91634b4))
* **parser:** Svelte template-expressions + control-flow-effective calls/refs ([#271](https://github.com/anatolykoptev/vaelor/issues/271)) ([e3b83e7](https://github.com/anatolykoptev/vaelor/commit/e3b83e7171319c5919018b02dd5df2f20ab15fba))
* **parser:** Swift Wave 1 — handler + tag query ([#147](https://github.com/anatolykoptev/vaelor/issues/147)) ([48af911](https://github.com/anatolykoptev/vaelor/commit/48af911823f81aa22fbec2a0d054a955d5fc941f))
* **parser:** Swift Wave 2 — calls + rels + protocol body + nits ([#148](https://github.com/anatolykoptev/vaelor/issues/148)) ([5b783be](https://github.com/anatolykoptev/vaelor/commit/5b783be24b67eac191bd4db935bbb9f53c421e51))
* port github_code_search from go-search to go-code ([#377](https://github.com/anatolykoptev/vaelor/issues/377)) ([daf5011](https://github.com/anatolykoptev/vaelor/commit/daf50115f83eaced609a8c5545c414615fa95e9f))
* **promclient:** bootstrap Prometheus HTTP client + QueryRange ([#46](https://github.com/anatolykoptev/vaelor/issues/46)) ([78f50f9](https://github.com/anatolykoptev/vaelor/commit/78f50f97287491489d54dc0bf344b46f613c1d0b))
* **repo_analyze:** surface ox-codes dataflow signals at deep mode ([#23](https://github.com/anatolykoptev/vaelor/issues/23)) ([5f12157](https://github.com/anatolykoptev/vaelor/commit/5f121577e579d42ed8a796e844bb1386a39974dd))
* **rerank:** env-tunable rerank timeouts (GOCODE_RERANK_TIMEOUT_S, GOCODE_SEMANTIC_RERANK_TIMEOUT_S) ([#110](https://github.com/anatolykoptev/vaelor/issues/110)) ([a345e8f](https://github.com/anatolykoptev/vaelor/commit/a345e8f6c4c079968f58e9d5fb2112e21e773a53))
* **resolve:** per-IP rate limit for POST /resolve ([#326](https://github.com/anatolykoptev/vaelor/issues/326)) ([a79e71a](https://github.com/anatolykoptev/vaelor/commit/a79e71ab0146bd7f25622cfde597c5cf825f6b26))
* **routes:** consolidate lineAt helper and add Line capture to 5 matchers (FU-CG.7) ([#331](https://github.com/anatolykoptev/vaelor/issues/331)) ([e5e598d](https://github.com/anatolykoptev/vaelor/commit/e5e598d11dc753a1458f01a2ad147b8760827d12))
* **scip:** extract IMPLEMENTS edges from Rust SCIP index — trait impl discovery ([#445](https://github.com/anatolykoptev/vaelor/issues/445)) ([12c99d0](https://github.com/anatolykoptev/vaelor/commit/12c99d0e0ffafa02f8e5e4b60f32f161b6462234))
* **scip:** filter stdlib method calls from SCIP edges to reduce call_trace noise ([#456](https://github.com/anatolykoptev/vaelor/issues/456)) ([31c011c](https://github.com/anatolykoptev/vaelor/commit/31c011c652de1aa996e6d5470f082cebf972c86f))
* **scip:** install scip-java for multi-language type-aware analysis ([#37](https://github.com/anatolykoptev/vaelor/issues/37)) ([3307894](https://github.com/anatolykoptev/vaelor/commit/3307894419866e43bf2f31709dd8c21602e85870))
* **scip:** run SCIP indexers for ALL detected languages, not just dominant ([#459](https://github.com/anatolykoptev/vaelor/issues/459)) ([b8a1c96](https://github.com/anatolykoptev/vaelor/commit/b8a1c96bac952483a13936ec6fbebd8796e12e1d))
* **semantic_search:** add code_graph hint to indexing status ([#359](https://github.com/anatolykoptev/vaelor/issues/359)) ([c64c7d8](https://github.com/anatolykoptev/vaelor/commit/c64c7d87f5a9c300f869ba313cd9a4ccee059661))
* **sourcemap:** make sourcemap max body size configurable ([#324](https://github.com/anatolykoptev/vaelor/issues/324)) ([970b9c4](https://github.com/anatolykoptev/vaelor/commit/970b9c404d7651d30e058a2ce301b11c2d732df6))
* **suggestions:** replace embedding fallback with pg_trgm trigram search ([720ab8b](https://github.com/anatolykoptev/vaelor/commit/720ab8b7ac504639d33ca1da5f130ee75b72991b))
* **suggestions:** replace embedding fallback with pg_trgm trigram search ([bea82f3](https://github.com/anatolykoptev/vaelor/commit/bea82f358e9f1ae17c335aa8060115bf74dd1822))
* **swift:** Wave 3 — cross-cutting integration (tested_by, speculative, astdiff, importcat, apisurf, delta) ([#149](https://github.com/anatolykoptev/vaelor/issues/149)) ([555f80b](https://github.com/anatolykoptev/vaelor/commit/555f80be409d607e1bf4749352980aa90922b7ec))
* **symbol_search:** add ast-grep structural pattern mode ([#22](https://github.com/anatolykoptev/vaelor/issues/22)) ([ec98b17](https://github.com/anatolykoptev/vaelor/commit/ec98b173cea8f4b2ab1d2d4147cc7024f3653d58))
* **tracing:** wire httpmw.RegisterRoute for OTEL code.* attrs ([5cc1592](https://github.com/anatolykoptev/vaelor/commit/5cc15926251abd6a68b1feb342f2a1ca133d40a2))
* **tracing:** wire httpmw.RegisterRoute for OTEL code.* attrs on webhook route ([3d6c241](https://github.com/anatolykoptev/vaelor/commit/3d6c2411ad6f329d885fb3e731c59ffd258b01b5))
* **vaelor/cli:** add cobra root + status/init subcommands, migrate index-designs ([#540](https://github.com/anatolykoptev/vaelor/issues/540)) ([7c03f36](https://github.com/anatolykoptev/vaelor/commit/7c03f3614b55432d83ff70082c6d5f0573c5f1f2))
* **vaelor:** wire opt-in file watcher with graceful degradation + metrics ([#544](https://github.com/anatolykoptev/vaelor/issues/544)) ([ced45d4](https://github.com/anatolykoptev/vaelor/commit/ced45d4113af2229b4e217ee3521a9a10c496b63))


### Fixed

* **age:** use $libdir/plugins/age path so non-superuser roles can LOAD ([#109](https://github.com/anatolykoptev/vaelor/issues/109)) ([6d9c2f9](https://github.com/anatolykoptev/vaelor/commit/6d9c2f99736313c397694d49c90ea9150f651acc))
* **astro:** narrow alias-counter emit-gate to broken declared aliases ([#243](https://github.com/anatolykoptev/vaelor/issues/243)) ([ae14c69](https://github.com/anatolykoptev/vaelor/commit/ae14c6919af83cfed05d00db6eced8500cc709d2))
* async lazy-build for understand/call_trace cold-start ([#490](https://github.com/anatolykoptev/vaelor/issues/490)) ([#501](https://github.com/anatolykoptev/vaelor/issues/501)) ([1d8ff01](https://github.com/anatolykoptev/vaelor/commit/1d8ff01f426d34c4f700f88e449b0b2ececfe89c))
* **autoindex:** emit skipped_no_vendor outcome + assert no-WARN contract ([#180](https://github.com/anatolykoptev/vaelor/issues/180)) ([fa41fee](https://github.com/anatolykoptev/vaelor/commit/fa41fee6a6af62711c5a6ed75ff62bd3dbfbb1da))
* **autoindex:** skip eager-warm for repos without vendor/ (etsy-forge, dozor) ([#104](https://github.com/anatolykoptev/vaelor/issues/104)) ([a809cbd](https://github.com/anatolykoptev/vaelor/commit/a809cbd84559047c86b217cecadbe8cb0c4aa394))
* B1 relative path candidates + B2 cycle node skip + Source=Span seed test (closes [#81](https://github.com/anatolykoptev/vaelor/issues/81)) ([#82](https://github.com/anatolykoptev/vaelor/issues/82)) ([74dfb30](https://github.com/anatolykoptev/vaelor/commit/74dfb30a2ab7df82d00e99e73f8a26a231068a25))
* **b1:** service-aware path candidate /host/src/&lt;service&gt;/&lt;rel&gt; ([#83](https://github.com/anatolykoptev/vaelor/issues/83)) ([36dbe69](https://github.com/anatolykoptev/vaelor/commit/36dbe6935c481a4599de91bbbf4ad85a7ac137a5))
* **call_trace:** normalize direction values to callers/callees ([#320](https://github.com/anatolykoptev/vaelor/issues/320)) ([196360a](https://github.com/anatolykoptev/vaelor/commit/196360ab2f66774cc3f321438919bccfc296d2d1))
* **call_trace:** rewrite TraceFromAGE with iterative BFS (AGE lacks list comprehension) ([#436](https://github.com/anatolykoptev/vaelor/issues/436)) ([48fdb6f](https://github.com/anatolykoptev/vaelor/commit/48fdb6fcbb1a66329233f2b26039590cb9adac6a))
* caller_kind accuracy — IsTestFile gate + unresolved bucket ([#507](https://github.com/anatolykoptev/vaelor/issues/507)) ([#510](https://github.com/anatolykoptev/vaelor/issues/510)) ([593cb3a](https://github.com/anatolykoptev/vaelor/commit/593cb3a25dbdbba5fcb9454733afd4d2c6ac4783))
* **callgraph:** apply stdlib filter to tree-sitter path, not just SCIP ([#466](https://github.com/anatolykoptev/vaelor/issues/466)) ([#470](https://github.com/anatolykoptev/vaelor/issues/470)) ([042d156](https://github.com/anatolykoptev/vaelor/commit/042d1564f90ce04307fd26aa562b8761be991168))
* **callgraph:** filter callees to call_expression only, exclude member access and vars ([#28](https://github.com/anatolykoptev/vaelor/issues/28)) ([a02e013](https://github.com/anatolykoptev/vaelor/commit/a02e0130eb66e8e2a8e4b0a5f53a4bb013f804f1))
* **callgraph:** resolve generic-function callers in package-level var initializers ([#280](https://github.com/anatolykoptev/vaelor/issues/280)) ([134b7b7](https://github.com/anatolykoptev/vaelor/commit/134b7b73516064381b9789143c0cfc1546932a07))
* **callgraph:** unblock cold-cache prewarm with CGO_ENABLED=0 + log packages.Load failure ([#29](https://github.com/anatolykoptev/vaelor/issues/29)) ([208b22c](https://github.com/anatolykoptev/vaelor/commit/208b22c0f895f07c0fe2614ea5e7c3eaed3ce37f))
* **callgraph:** wire typed call-edge resolution into the AGE-graph path for dead-code accuracy (BUG A, gated default-off) ([3051854](https://github.com/anatolykoptev/vaelor/commit/3051854738f0902d26e625da66c1d37c2238243d))
* **clients:** stop allocating httputil.Client on every call ([#316](https://github.com/anatolykoptev/vaelor/issues/316)) ([6ab7016](https://github.com/anatolykoptev/vaelor/commit/6ab70163b6e341c39446b8d5ed19bd5bf572c2e2))
* **code_graph:** return building status instead of tool error ([#361](https://github.com/anatolykoptev/vaelor/issues/361)) ([dcf8bf8](https://github.com/anatolykoptev/vaelor/commit/dcf8bf8387ee463d93dc18cfb5031846cd50b54d))
* **code_health:** stop deleting a remote clone while the background snapshot is still reading it ([#246](https://github.com/anatolykoptev/vaelor/issues/246)) ([30d0486](https://github.com/anatolykoptev/vaelor/commit/30d0486cb73c004a5a3acf30a59a7f9f6be78b17))
* **codegraph:** add side to side-blind Route MATCH queries (FU-CG.8) ([#333](https://github.com/anatolykoptev/vaelor/issues/333)) ([2275e8f](https://github.com/anatolykoptev/vaelor/commit/2275e8f010aeb044b288a5933c0a85f627d4b5ad))
* **codegraph:** apply ageSetup search_path in bookkeeping-table accessors ([ceee1fc](https://github.com/anatolykoptev/vaelor/commit/ceee1fc1ec6861bff11b8d710f6d38b5f7ceadaa))
* **codegraph:** apply ageSetup search_path in bookkeeping-table accessors ([e4132bf](https://github.com/anatolykoptev/vaelor/commit/e4132bf2a7acdd60d53f356c9efa81a4a2a7207a))
* **codegraph:** emit IMPLEMENTS edge label for IsInterface call edges ([#447](https://github.com/anatolykoptev/vaelor/issues/447)) ([8573e03](https://github.com/anatolykoptev/vaelor/commit/8573e037a431d4714d204b037c5b9524785da055))
* **codegraph:** enable typed call enrichment by default ([#314](https://github.com/anatolykoptev/vaelor/issues/314)) ([7369296](https://github.com/anatolykoptev/vaelor/commit/7369296c4d6ff2f9e08fa1045e3db6ff134ab70d))
* **codegraph:** FU-CG.9 — make route edge counters truthful (built vs unmatched) ([#335](https://github.com/anatolykoptev/vaelor/issues/335)) ([59f7127](https://github.com/anatolykoptev/vaelor/commit/59f7127f6472699d112a2a7f875be254a6a40b68))
* **codegraph:** memory guard + chunked COPY to prevent OOM kernel panic ([#428](https://github.com/anatolykoptev/vaelor/issues/428)) ([#429](https://github.com/anatolykoptev/vaelor/issues/429)) ([fad1c41](https://github.com/anatolykoptev/vaelor/commit/fad1c4176c57a3bce38ba35017709d7d346506c1))
* **codegraph:** preflight guard for graph-missing on read-path ([#42](https://github.com/anatolykoptev/vaelor/issues/42)) ([c31b8ca](https://github.com/anatolykoptev/vaelor/commit/c31b8ca9ac4ff3ed34b394b27579089b8366e127))
* **codegraph:** prune stale dead-code scores when a function stops being an orphan ([#295](https://github.com/anatolykoptev/vaelor/issues/295)) ([d154853](https://github.com/anatolykoptev/vaelor/commit/d15485314dc9f7fc082e0985459208ed6c283662))
* **codegraph:** remove HasGoModule gate from buildAGECallGraph — enable SCIP for Rust ([#449](https://github.com/anatolykoptev/vaelor/issues/449)) ([e6228d8](https://github.com/anatolykoptev/vaelor/commit/e6228d829ac8278436a7b4fe72dccf9831da7f9f))
* **codegraph:** repair fleet-wide HANDLES/FETCHES=0 — route→graph edge builder ([#167](https://github.com/anatolykoptev/vaelor/issues/167)) ([878aa40](https://github.com/anatolykoptev/vaelor/commit/878aa404528848ef080174ad2b3c7c32ed777033))
* **codegraph:** write-path guards + replace fragile template count test ([#44](https://github.com/anatolykoptev/vaelor/issues/44)) ([dec2270](https://github.com/anatolykoptev/vaelor/commit/dec227083e73f45baa8d54c548669b1451947822))
* **compare,codegraph:** code_compare grade reflects freshness + language-aware isExported; [#253](https://github.com/anatolykoptev/vaelor/issues/253) cleanup ([ded3103](https://github.com/anatolykoptev/vaelor/commit/ded310373f1727c947593d8f1475c14f57fe8db5))
* **compare:** avoid duplicate BuildSnapshot when comparing a repo to itself ([f5a5db8](https://github.com/anatolykoptev/vaelor/commit/f5a5db8012554e945a7172b9bf24454e15d7b1d1))
* **compare:** avoid re-parsing files for type relationships ([b02687c](https://github.com/anatolykoptev/vaelor/commit/b02687c3987ee60dedf029359cf8b81fa5092f61))
* **compare:** cap code_compare deadlines to fit 100s proxy timeout ([9b6fdf5](https://github.com/anatolykoptev/vaelor/commit/9b6fdf5b3e786bba044e40a840fc11ad318ce846))
* **compare:** dedupe self-compare snapshots + ParseCache integration ([5963214](https://github.com/anatolykoptev/vaelor/commit/5963214588d593661fbe23e1d602543e527b0817))
* **compare:** deterministic cycle-pair order in find2Cycles (flaky test) ([#272](https://github.com/anatolykoptev/vaelor/issues/272)) ([32a92f7](https://github.com/anatolykoptev/vaelor/commit/32a92f7dbcb17e4734476c822de2dacb00ef7cf7))
* **compare:** raise code_compare deadline from 90s to 3m ([#309](https://github.com/anatolykoptev/vaelor/issues/309)) ([0eb598b](https://github.com/anatolykoptev/vaelor/commit/0eb598bd4710a2bb8ad631d7c718cc86119e43c1))
* **compare:** reuse tree-sitter parser per worker in BuildSnapshot ([#384](https://github.com/anatolykoptev/vaelor/issues/384)) ([3b54e23](https://github.com/anatolykoptev/vaelor/commit/3b54e233fd3d6386bfebc04958657ccf684e8627))
* **compare:** treat zero-dependency repos as N/A for freshness+vuln scoring ([e9f41e8](https://github.com/anatolykoptev/vaelor/commit/e9f41e8f5c4066de7c9e89be42176a607e5e0310))
* **compare:** treat zero-dependency repos as N/A in code_compare grade (match code_health/[#250](https://github.com/anatolykoptev/vaelor/issues/250)) ([25dd2bb](https://github.com/anatolykoptev/vaelor/commit/25dd2bb0b8292a6e66d4789853c86d866f789983))
* **complexity:** unify cyclomatic complexity on parser as single owner ([816b275](https://github.com/anatolykoptev/vaelor/commit/816b275e81fab975ead496d53a7f17e591adf708))
* content-hash staleness guard for cgCache L2 ([#497](https://github.com/anatolykoptev/vaelor/issues/497)) ([#504](https://github.com/anatolykoptev/vaelor/issues/504)) ([752e12a](https://github.com/anatolykoptev/vaelor/commit/752e12a95a63dda4e94a28e2d2692473fd8bccc7))
* **db:** reset pooled-conn search_path on release — bare code_* resolves public, not ag_catalog ([#173](https://github.com/anatolykoptev/vaelor/issues/173)) ([428e4ad](https://github.com/anatolykoptev/vaelor/commit/428e4ada8421798ba14aba3e91770746a541ee9f))
* **deadcode:** language-aware exported check for non-IsPublic languages ([#281](https://github.com/anatolykoptev/vaelor/issues/281)) ([4546b71](https://github.com/anatolykoptev/vaelor/commit/4546b71b59b524a87093ba040017c5c47738e117))
* **debug_investigate:** dedup historical incidents by (Repo, Symbol) ([#85](https://github.com/anatolykoptev/vaelor/issues/85)) ([9616c43](https://github.com/anatolykoptev/vaelor/commit/9616c439121d5b2a5a3b0126cd5057d7362d3b29)), closes [#84](https://github.com/anatolykoptev/vaelor/issues/84)
* **debug_investigate:** drop t.Skip and document %q/%s label choice ([#318](https://github.com/anatolykoptev/vaelor/issues/318)) ([3d44a00](https://github.com/anatolykoptev/vaelor/commit/3d44a000fc39d2236e6b67ccf4dcb25fb9fb3bc2))
* **debug_investigate:** faster polling + LLM timeout bump + service-&gt;repo body mapping ([#99](https://github.com/anatolykoptev/vaelor/issues/99)) ([f296435](https://github.com/anatolykoptev/vaelor/commit/f29643539f7c3348258ff31db0b3d1014b3460c6))
* **debug_investigate:** include code.function in subject + map /build/ paths to host ([49343d4](https://github.com/anatolykoptev/vaelor/commit/49343d4e8e17e93c29c7b46ae5c9d58ad13ea595))
* **debug_investigate:** include code.function in subject + map /build/ paths to host ([49343d4](https://github.com/anatolykoptev/vaelor/commit/49343d4e8e17e93c29c7b46ae5c9d58ad13ea595))
* **debug_investigate:** include code.function in subject + map /build/ paths to host ([e460ab5](https://github.com/anatolykoptev/vaelor/commit/e460ab5b14ecbda37f2ddabe2a8f53ce4b902a4c))
* **debug_investigate:** include repo in cache key + honor explicit repo arg ([#90](https://github.com/anatolykoptev/vaelor/issues/90)) ([b7723f3](https://github.com/anatolykoptev/vaelor/commit/b7723f3861f550f34ac5947f7114b698a4ce7d5f))
* **debug_investigate:** MetricsQueried in legacy path is += not = (closes [#75](https://github.com/anatolykoptev/vaelor/issues/75)) ([#76](https://github.com/anatolykoptev/vaelor/issues/76)) ([d858755](https://github.com/anatolykoptev/vaelor/commit/d858755e319ede2b235e8ba5b17b0a8d2f61b59b))
* **debug_investigate:** Phase 2 — baseline trace fetch (was error-only, starved symbol correlation) ([#73](https://github.com/anatolykoptev/vaelor/issues/73)) ([831c549](https://github.com/anatolykoptev/vaelor/commit/831c549b6c9bb39e01fd8e8d22bd4518817517c9))
* **embeddings:** delete only true orphans (positive IN-list), not per-chunk anti-join ([b6ddc6a](https://github.com/anatolykoptev/vaelor/commit/b6ddc6a48d97211c0a53c7f13ff2e6c302cf688f))
* **embeddings:** incremental sync froze indexed_sha on first unsupported file in diff ([#170](https://github.com/anatolykoptev/vaelor/issues/170)) ([5583fe9](https://github.com/anatolykoptev/vaelor/commit/5583fe9639715b91f4382a5a97ff5129d7b69b11))
* **embeddings:** NUL-separate in-memory symbol keys (colon-in-path safe) + document dedup lossiness ([fd9f7b3](https://github.com/anatolykoptev/vaelor/commit/fd9f7b34c4e5ff31cfe12ecbbd98af17e2213e88))
* **embeddings:** rate-gate autoindex concurrency to 1 for single-worker embed backend ([#217](https://github.com/anatolykoptev/vaelor/issues/217)) ([06b2e09](https://github.com/anatolykoptev/vaelor/commit/06b2e099eab1f24784d309f7c7b648829a6a24e1))
* **embeddings:** replace misleading freshness gauge with commits-behind + count SetRepoState write-failures ([#172](https://github.com/anatolykoptev/vaelor/issues/172)) ([458c9ff](https://github.com/anatolykoptev/vaelor/commit/458c9ff3e00bf094dd4463d8abe9983437b79cb7))
* **embeddings:** treat all 5xx as retryable; add embed_model per-row; continuous orphan gauge ([#232](https://github.com/anatolykoptev/vaelor/issues/232)) ([f599da6](https://github.com/anatolykoptev/vaelor/commit/f599da6840299064f3dd1dccc52c6f31e0dcc36f))
* **explore:** files_changed reflects single commit diff, not cumulative range ([#26](https://github.com/anatolykoptev/vaelor/issues/26)) ([7871abe](https://github.com/anatolykoptev/vaelor/commit/7871abefb5c887dfca3f4c9a51855c7effaa62f6))
* **explore:** label health score as approximate with hint ([#249](https://github.com/anatolykoptev/vaelor/issues/249)) ([3bb3e90](https://github.com/anatolykoptev/vaelor/commit/3bb3e90bfacafee052092e4479ab2ed0a8f31a77))
* **federate:** FU-1.1 — thread request ctx into ResolveRepos for cancellable origin dedup ([#337](https://github.com/anatolykoptev/vaelor/issues/337)) ([16d22f2](https://github.com/anatolykoptev/vaelor/commit/16d22f21ec2ea4b7dc9a0a9e4067ae9e69832804))
* **federate:** pass asOf time.Time to CrossRepoCoChange to avoid wall-clock git log --since ([1da47a2](https://github.com/anatolykoptev/vaelor/commit/1da47a209dc2e4a93b9759938cf9cf00d34999d6))
* **fleet/ssh:** pass -F flag explicitly and rewrite ~ paths in shadow config ([#131](https://github.com/anatolykoptev/vaelor/issues/131)) ([ee9f263](https://github.com/anatolykoptev/vaelor/commit/ee9f263731af23362d29b9fbb04e688861fa304c))
* **forge:** deflake metrics_test.go counter delta assertions ([#308](https://github.com/anatolykoptev/vaelor/issues/308)) ([13e2481](https://github.com/anatolykoptev/vaelor/commit/13e2481b193831f0dd7b48ea7dec11f679a66660))
* **forge:** ExtractSlug + DetectForge accept URL/SSH forms ([#27](https://github.com/anatolykoptev/vaelor/issues/27)) ([82f2286](https://github.com/anatolykoptev/vaelor/commit/82f228605d81f30384fc984630f263138eea6d13))
* **gitutil:** accept .git file form in worktree detection ([#36](https://github.com/anatolykoptev/vaelor/issues/36)) ([96ea0e1](https://github.com/anatolykoptev/vaelor/commit/96ea0e192e7790c7e9357def6b9a48f229380611))
* **go-code:** accept owner/repo form in github_code_search tool ([#381](https://github.com/anatolykoptev/vaelor/issues/381)) ([fcd71df](https://github.com/anatolykoptev/vaelor/commit/fcd71df03c66db9586cf7cc96761b24976374463))
* **go-code:** batch build-time dead_code rerank to the server's per-request cap ([#191](https://github.com/anatolykoptev/vaelor/issues/191)) ([e24fbb4](https://github.com/anatolykoptev/vaelor/commit/e24fbb48dbe8cf100c561a9248b3619cc28f5821))
* **go-code:** embed HTTP timeout + bounded async index ctx + attributable cancel ([#216](https://github.com/anatolykoptev/vaelor/issues/216)) ([2c32684](https://github.com/anatolykoptev/vaelor/commit/2c326843b2b3059904636c5a23b9d0efaeee6da2))
* **go-code:** exclude *_test.go imports from circular-dep detection ([#184](https://github.com/anatolykoptev/vaelor/issues/184)) ([f24d75d](https://github.com/anatolykoptev/vaelor/commit/f24d75d1a344dec9abfc37d6618ec0d3ea28f42c))
* **go-code:** group archgraph queries by package path, not base name ([#186](https://github.com/anatolykoptev/vaelor/issues/186)) ([8d7ffa8](https://github.com/anatolykoptev/vaelor/commit/8d7ffa87f8904b68426f25bce93cc55919eee831))
* **go-code:** Phase 2a cleanup — 17 items (BUG-FH-1/2 closed, error encoding unified, +13 cosmetic) ([#157](https://github.com/anatolykoptev/vaelor/issues/157)) ([9d80b71](https://github.com/anatolykoptev/vaelor/commit/9d80b71473e102e829416fbfd5e94a005f4ea199))
* **go-code:** Phase 2b infra — Commits count, churn growth, since window, --follow, WithFreshness wiring ([#158](https://github.com/anatolykoptev/vaelor/issues/158)) ([f239728](https://github.com/anatolykoptev/vaelor/commit/f23972884ef4c9bbecad9150c06d59448e0a3ee6))
* **go-code:** pool AfterRelease RESET ALL, not DISCARD ALL (26000 regression) ([#176](https://github.com/anatolykoptev/vaelor/issues/176)) ([e24c92e](https://github.com/anatolykoptev/vaelor/commit/e24c92e63d756e8283f0fee0e7e69d784e3a891c))
* **go-code:** reconcile orphan embedding rows on full index + operator sweep (Bug B — phantom symbols) ([#209](https://github.com/anatolykoptev/vaelor/issues/209)) ([c255c4d](https://github.com/anatolykoptev/vaelor/commit/c255c4dd0a7d1b2a4533802c1f6ad116d80c43cc))
* **go-code:** rerank via go-kit/rerank.Client, drop hardcoded embed-server URL ([#190](https://github.com/anatolykoptev/vaelor/issues/190)) ([82fc468](https://github.com/anatolykoptev/vaelor/commit/82fc468c1ab784253fed3699bcdea7d274d3c3c5))
* **go-code:** self-index desync (SHA-gate data-aware) + HTTP-index-cancel observability ([#214](https://github.com/anatolykoptev/vaelor/issues/214)) ([3ce4f94](https://github.com/anatolykoptev/vaelor/commit/3ce4f941f4d40359b49ff4bc6871d79f8265fa0a))
* **go-code:** sparsevec batch size 500→100 (data-bound statement_timeout) + accurate write_failed counter ([#201](https://github.com/anatolykoptev/vaelor/issues/201)) ([cc7e29c](https://github.com/anatolykoptev/vaelor/commit/cc7e29c60f228fc2eabffd582b48b269818ddcd7))
* **go-code:** unify local package nodes (stop duplicate dir/import-path vertices) ([#185](https://github.com/anatolykoptev/vaelor/issues/185)) ([bdee2c6](https://github.com/anatolykoptev/vaelor/commit/bdee2c6f70e574afbaa12fe9aa29b09dd17a1635))
* **graph-arm:** invert pagerank sub-generator — keyword-relevant ranked by pagerank ([#219](https://github.com/anatolykoptev/vaelor/issues/219)) ([2654fbd](https://github.com/anatolykoptev/vaelor/commit/2654fbd1bb3226ea1b5bd214d380446ea523949b))
* **importresolve:** honor package.json exports map for workspace subpath imports ([#422](https://github.com/anatolykoptev/vaelor/issues/422)) ([#424](https://github.com/anatolykoptev/vaelor/issues/424)) ([45dc05a](https://github.com/anatolykoptev/vaelor/commit/45dc05ac604662e453df741b1d86fa28c871cdf9))
* **ingest,explore:** shallow clone depth=2 + shallow-boundary guard in countDiffTreeFiles ([#31](https://github.com/anatolykoptev/vaelor/issues/31)) ([06f542b](https://github.com/anatolykoptev/vaelor/commit/06f542ba37b4276121626433d1a60d944bfc76fb))
* **ingest:** accept comma-separated focus keywords ([#305](https://github.com/anatolykoptev/vaelor/issues/305)) ([f67814c](https://github.com/anatolykoptev/vaelor/commit/f67814c14f6dc6129f430c4d0de06cbfd8138cb8))
* **ingest:** atomic clone via renameat2 RENAME_EXCHANGE; errno breakdown for read_error ([#116](https://github.com/anatolykoptev/vaelor/issues/116)) ([b9d27eb](https://github.com/anatolykoptev/vaelor/commit/b9d27ebf731f8dbb53047e5d93ae9ca36eefff94))
* **ingest:** defensive copy in IngestRepo cache to prevent aliasing ([#477](https://github.com/anatolykoptev/vaelor/issues/477)) ([d7369ed](https://github.com/anatolykoptev/vaelor/commit/d7369eddf4347346f2124c9855c429a8d177ada7))
* **ingest:** NormalizeSlug accepts URL and SSH forms ([#24](https://github.com/anatolykoptev/vaelor/issues/24)) ([89e5280](https://github.com/anatolykoptev/vaelor/commit/89e528092c9135f4bb78edca6339b0a642d73093))
* **ingest:** refresh credentials via GIT_CONFIG before git fetch ([#107](https://github.com/anatolykoptev/vaelor/issues/107)) ([e6e8221](https://github.com/anatolykoptev/vaelor/commit/e6e82212d30b6fa8fdef6f8db98a3fcc9bd0a1fd))
* **ingest:** refresh on cache-hit to remote HEAD instead of trusting on-disk state ([#21](https://github.com/anatolykoptev/vaelor/issues/21)) ([a9ea497](https://github.com/anatolykoptev/vaelor/commit/a9ea497be23c174308bbde71a5f4e1d97d642ddc))
* **ingest:** use App installation token for clone when configured ([#105](https://github.com/anatolykoptev/vaelor/issues/105)) ([a9ce6ae](https://github.com/anatolykoptev/vaelor/commit/a9ce6ae5a7c7c131e7be9d8b02afd87df3facf1b))
* **llm-obs:** register metrics against go-code's registry, not default ([#121](https://github.com/anatolykoptev/vaelor/issues/121)) ([40a15ac](https://github.com/anatolykoptev/vaelor/commit/40a15ac789c35dfa9a5f93ab07cf48fd8b6b4ee4))
* **llm:** default per-attempt timeout for chain rotation + review_delta 120s ([#391](https://github.com/anatolykoptev/vaelor/issues/391)) ([#395](https://github.com/anatolykoptev/vaelor/issues/395)) ([0d6ebb5](https://github.com/anatolykoptev/vaelor/commit/0d6ebb53d83219f2d7631bb00a7e358401659dd8))
* **mcpmeta:** correct misleading stale-index remediation advice ([#169](https://github.com/anatolykoptev/vaelor/issues/169)) ([05e4ceb](https://github.com/anatolykoptev/vaelor/commit/05e4ceb568c4257ef914b36db5b5941bb57b10e0))
* **mcp:** raise code_graph timeout + non-blocking narrative + branch cleanup ([#433](https://github.com/anatolykoptev/vaelor/issues/433)) ([bd72573](https://github.com/anatolykoptev/vaelor/commit/bd7257319ef6f537e538eee938ceea8497894048))
* **mcp:** return tool results as application/json, not single-line SSE ([#245](https://github.com/anatolykoptev/vaelor/issues/245)) ([47cd6c6](https://github.com/anatolykoptev/vaelor/commit/47cd6c6e228392f9199aa2b5d5bb0d0fdf23c167))
* **mcp:** reverse-map container paths in outputs + zero-result hint ([#45](https://github.com/anatolykoptev/vaelor/issues/45)) ([f30d6b5](https://github.com/anatolykoptev/vaelor/commit/f30d6b56fd87bf364ae50bbccdb2fc8e12adba9f))
* **metrics:** add per-symbol cognitive complexity and fix JS docRatio ([#247](https://github.com/anatolykoptev/vaelor/issues/247)) ([eef6282](https://github.com/anatolykoptev/vaelor/commit/eef62824bc2d45ef76eb74cf7df5d09f1b196265))
* **metrics:** pre-register alert-facing series at boot (graph age, zero-embeddings) ([#287](https://github.com/anatolykoptev/vaelor/issues/287)) ([e7c605e](https://github.com/anatolykoptev/vaelor/commit/e7c605e7f5af51d7c9b0ab3af06c2c222cdda247))
* **metrics:** record outcome=error on resolve failure + drop unemitted skipped label ([0810d34](https://github.com/anatolykoptev/vaelor/commit/0810d3451e2b9701dc826dcb5ffd478e68f5c821))
* **metrics:** scope code-graph age gauge to AUTO_INDEX_DIRS repos ([#291](https://github.com/anatolykoptev/vaelor/issues/291)) ([61c7bd6](https://github.com/anatolykoptev/vaelor/commit/61c7bd6dae46629da4fda44fdfeaf71b1884b4d7))
* **metrics:** unify health score and add arch fallback for unindexed repos ([#248](https://github.com/anatolykoptev/vaelor/issues/248)) ([78c7e55](https://github.com/anatolykoptev/vaelor/commit/78c7e55706a014565b294aefeec527601be6db21))
* **oxcodes:** bump structural-search HTTP timeout 10s-&gt;30s ([#168](https://github.com/anatolykoptev/vaelor/issues/168)) ([639cb1b](https://github.com/anatolykoptev/vaelor/commit/639cb1bddcdfcb9d33ecdbef9e68b76a085a6be8))
* ParseCache drops call sites and ignores includeBody on hit ([#286](https://github.com/anatolykoptev/vaelor/issues/286)) ([f6dc7eb](https://github.com/anatolykoptev/vaelor/commit/f6dc7ebf87c0dfd4cf70ebbed7411de09d5b9e31))
* **parser:** dual-emit rune symbols so $state query finds all bound declarations ([#108](https://github.com/anatolykoptev/vaelor/issues/108)) ([f69810e](https://github.com/anatolykoptev/vaelor/commit/f69810ed07d3f7e831d415a46df51fc13b23582c))
* **parser:** JS/TS-family Symbol.Language parity — .jsx/.js/.mjs/.cjs emit javascript ([#268](https://github.com/anatolykoptev/vaelor/issues/268)) ([eb64edf](https://github.com/anatolykoptev/vaelor/commit/eb64edfe523a08e7b7f5221de09a2f4a06c8ffaa))
* **parser:** route Vue call extraction through the two-region ScriptCalls/MarkupCalls split ([#409](https://github.com/anatolykoptev/vaelor/issues/409)) ([#414](https://github.com/anatolykoptev/vaelor/issues/414)) ([1b6d8b1](https://github.com/anatolykoptev/vaelor/commit/1b6d8b18f67204a88e1042d1a05e46a3622db4a5))
* pass Logger to mcpserver.Run to preserve slogh wrapper ([5baf8e9](https://github.com/anatolykoptev/vaelor/commit/5baf8e93e2b04870b0c9ce70f9227e08bc71910d))
* **pgutil:** alertable metric for frozen index marker ([#520](https://github.com/anatolykoptev/vaelor/issues/520)) ([9bc4935](https://github.com/anatolykoptev/vaelor/commit/9bc49350ffcaf28b41be9264d13181618a6f1b64))
* **pipeline-file:** mirror indexRepo filters (isTestFile + maxIndexFileBytes) ([9247295](https://github.com/anatolykoptev/vaelor/commit/92472953e07bad4a93e6d10ad8616febe09b3fc9))
* **pipeline-incremental:** bind ctx to git diff exec + surface stderr ([f6a8589](https://github.com/anatolykoptev/vaelor/commit/f6a85894abd2e0cf3206d34096440770c89a6f0e))
* **polyglot/pinned:** don't abort Collect walk on permission errors ([#126](https://github.com/anatolykoptev/vaelor/issues/126)) ([f15aa1f](https://github.com/anatolykoptev/vaelor/commit/f15aa1f7226b211deab42b4e15a594a6b4ec2324))
* **polyglot/pinned:** resolve compose include: directive recursively ([#125](https://github.com/anatolykoptev/vaelor/issues/125)) ([ff0e593](https://github.com/anatolykoptev/vaelor/commit/ff0e593f31fccd63cf708c2974951947f9cfba44))
* **polyglot/pinned:** skip nested git repos, submodules, and .claude worktrees ([#127](https://github.com/anatolykoptev/vaelor/issues/127)) ([e485e59](https://github.com/anatolykoptev/vaelor/commit/e485e5904c1fec3527676f7d233d562ccb935cdd))
* put tracemcpmw first so hooks receive span context ([5fc68fb](https://github.com/anatolykoptev/vaelor/commit/5fc68fb56c6f49a30f9cbd308d8695af37ac672c))
* **release-please:** guard auto-merge step when no release PR ([#311](https://github.com/anatolykoptev/vaelor/issues/311)) ([355a08c](https://github.com/anatolykoptev/vaelor/commit/355a08c8b482962fb68031d90d07ef7d28f4266c))
* **release:** amd64 CGO CC override + consolidate to one goreleaser config ([#277](https://github.com/anatolykoptev/vaelor/issues/277)) ([a46412e](https://github.com/anatolykoptev/vaelor/commit/a46412e50e7d2353a6d669efb2448e1296cd5294))
* **release:** goreleaser main path cmd/go-code -&gt; cmd/vaelor ([5b4124f](https://github.com/anatolykoptev/vaelor/commit/5b4124f16148255ca997bfedcf69a7a381410326))
* **repo_analyze:** omit empty &lt;signature&gt; tag entirely (not just content) ([#19](https://github.com/anatolykoptev/vaelor/issues/19)) ([b913f22](https://github.com/anatolykoptev/vaelor/commit/b913f22504bfa09b1386ac8bd02976e75360a72c))
* **resolve:** prefer local /host/src checkout over clone for matching slugs ([c07c970](https://github.com/anatolykoptev/vaelor/commit/c07c97052cae7bfa727097df4254ff6808c184e7))
* **resolve:** prefer local /host/src checkout over clone for matching slugs ([be97d8e](https://github.com/anatolykoptev/vaelor/commit/be97d8e79d806cd83328190f21db5f20ce779b58))
* **resolve:** resolve bare repo names against LocalRepoDirs registry ([#226](https://github.com/anatolykoptev/vaelor/issues/226)) ([d3e4589](https://github.com/anatolykoptev/vaelor/commit/d3e45890c2ea8b3d2ed68d1eec5c94842329d403))
* retry-safe, lock-safe embeddings & designmd schema init ([#495](https://github.com/anatolykoptev/vaelor/issues/495), [#496](https://github.com/anatolykoptev/vaelor/issues/496)) ([#499](https://github.com/anatolykoptev/vaelor/issues/499)) ([d2c3064](https://github.com/anatolykoptev/vaelor/commit/d2c306408d4d324a9aefb5fe8cc3bd2e0ef73561))
* **review_pr:** pass FETCH_HEAD to diff, not warm-clone HEAD ([#12](https://github.com/anatolykoptev/vaelor/issues/12)) ([76e3081](https://github.com/anatolykoptev/vaelor/commit/76e30819c089a38e9c67d3e97c0918930c7cfdbe))
* **review_pr:** worktree-isolated checkout for call graph analysis ([#13](https://github.com/anatolykoptev/vaelor/issues/13)) ([ef084e9](https://github.com/anatolykoptev/vaelor/commit/ef084e9ed24d7d6488f4a730cea8ad6d5eeda8ee))
* **review:** correct untested-symbol false positives in review_delta ([#392](https://github.com/anatolykoptev/vaelor/issues/392)) ([5f23f64](https://github.com/anatolykoptev/vaelor/commit/5f23f6401d2965365652457b2924004817498645))
* **review:** route PR-post write path through the multi-forge registry ([#284](https://github.com/anatolykoptev/vaelor/issues/284)) ([ae86487](https://github.com/anatolykoptev/vaelor/commit/ae8648726f94eddcf11e6552d66886e407ac35d9))
* **review:** use valid ox-codes scope "function_bodies" in review_delta ([#420](https://github.com/anatolykoptev/vaelor/issues/420)) ([3d9f499](https://github.com/anatolykoptev/vaelor/commit/3d9f49906f661abf3e432ce5e1fde82c2d754ec3)), closes [#419](https://github.com/anatolykoptev/vaelor/issues/419)
* **review:** worktree-aware git invocation via --git-dir + PathRewrite ([#38](https://github.com/anatolykoptev/vaelor/issues/38)) ([677b10c](https://github.com/anatolykoptev/vaelor/commit/677b10c39d351b32557ad74422a363ec9a0978b0))
* sane fresh-deploy defaults for LLM model and /resolve rate limit ([#412](https://github.com/anatolykoptev/vaelor/issues/412)) ([2dc7076](https://github.com/anatolykoptev/vaelor/commit/2dc7076cad4b1dd453d6c075b69a3f7dab836da5))
* **scip:** use content hash instead of mtimes for CacheKey — no false misses on git checkout ([#458](https://github.com/anatolykoptev/vaelor/issues/458)) ([cb2a0a1](https://github.com/anatolykoptev/vaelor/commit/cb2a0a1ff5b3415b585e30615c5463fd4ff2f6dd))
* **scip:** wire Cache into trySCIPResolution — skip re-indexing on cache hit ([#443](https://github.com/anatolykoptev/vaelor/issues/443)) ([0a71e0b](https://github.com/anatolykoptev/vaelor/commit/0a71e0b5e537eab8043dcc14efaaa4d4c60a7726))
* **semantic_search:** strip AGE agtype quotes from complexity values ([7f54db3](https://github.com/anatolykoptev/vaelor/commit/7f54db3a8043d1b15607d0da5e3dcd3da01b884c))
* **semantic-fallback:** cap embed query at 5s sub-context ([166ef7e](https://github.com/anatolykoptev/vaelor/commit/166ef7e9eb3cca5ea06b617a1f3c4eb3d8c8f56a))
* **semantic-search:** dedup semantic-only + CE-rerank results by file:symbol (Bug A) ([#208](https://github.com/anatolykoptev/vaelor/issues/208)) ([c7ae2d0](https://github.com/anatolykoptev/vaelor/commit/c7ae2d0007e94e3c327904ce1423506915327a89))
* **semhealth:** eliminate two find_duplicates false-positive classes ([#218](https://github.com/anatolykoptev/vaelor/issues/218)) ([1880548](https://github.com/anatolykoptev/vaelor/commit/18805484fc75c9404017909495a55c7239874552))
* **semhealth:** guard self-join by repo size + statement_timeout ([77a5460](https://github.com/anatolykoptev/vaelor/commit/77a546081bd805ae045d4b103cd781604fad6a5d))
* serialize EnsureGraph provisioning to fix pg_type 23505 race ([#417](https://github.com/anatolykoptev/vaelor/issues/417)) ([48c1aae](https://github.com/anatolykoptev/vaelor/commit/48c1aaedb0b71a42035f892d7803ad6f3ef63da8))
* shrink code_compare LLM prompt to fit 8k-token fleet models ([#398](https://github.com/anatolykoptev/vaelor/issues/398)) ([429438d](https://github.com/anatolykoptev/vaelor/commit/429438d3576c5258a0f8260057bb8e74b16ac3d5))
* SSE + tool keepalive via go-mcpserver v0.17.0 ([#539](https://github.com/anatolykoptev/vaelor/issues/539)) ([daa5905](https://github.com/anatolykoptev/vaelor/commit/daa59059a67aa8b4c51cf86b8a60148d73aa8c08))
* **test:** make TestSignalHitsLiveIntegration self-contained (nightly green) ([#389](https://github.com/anatolykoptev/vaelor/issues/389)) ([4ff1205](https://github.com/anatolykoptev/vaelor/commit/4ff1205b5607598b231c60061a9de45c5c44ed2a))
* three go-code anomalies from 2026-06-12 investigation ([#228](https://github.com/anatolykoptev/vaelor/issues/228)) ([2877d8d](https://github.com/anatolykoptev/vaelor/commit/2877d8d0a8614b75fe1225afd3c61bc40e37fdb5))
* **toolserver:** add understand to ToolTimeouts (30s) ([6047f7b](https://github.com/anatolykoptev/vaelor/commit/6047f7bb00ca5b7105063d359ccf6e1feada320a))
* **tracing:** cast webhook handler to concrete type for correct code.* attrs ([4b3ed8e](https://github.com/anatolykoptev/vaelor/commit/4b3ed8eb0e4001fc216b497469cc24d674a1e867))
* **tracing:** cast webhook handler to concrete type for correct code.* attrs ([8e68b82](https://github.com/anatolykoptev/vaelor/commit/8e68b822eb981603adb98fd952f61f3efa1fba4e))
* **tracing:** use method expression for real code.* source location ([905aa24](https://github.com/anatolykoptev/vaelor/commit/905aa24dd8f07212e961af390cf51dc9abc06dcb))
* **tracing:** use method expression for real source location in code.* attrs ([67f1104](https://github.com/anatolykoptev/vaelor/commit/67f11045a09fd5386c8cad71b64588e27e6352bb))
* transfer table ownership on learnings + designmd store init ([#265](https://github.com/anatolykoptev/vaelor/issues/265)) ([6d726b4](https://github.com/anatolykoptev/vaelor/commit/6d726b4d2d9b650a31d32b45a63b087d1e205659))
* **understand:** bound semantic fallback + add tool timeout + guard semhealth self-join ([1d02992](https://github.com/anatolykoptev/vaelor/commit/1d0299262d4a424e46bb7187183f7c8078a5bf6e))
* use concrete slog.TextHandler as slogh base to avoid log bridge deadlock ([#96](https://github.com/anatolykoptev/vaelor/issues/96)) ([d600307](https://github.com/anatolykoptev/vaelor/commit/d600307f0dba544e82c547b7c73d19083a80c27c))
* use slog.InfoContext in hooks for trace_id correlation ([#97](https://github.com/anatolykoptev/vaelor/issues/97)) ([f6e31aa](https://github.com/anatolykoptev/vaelor/commit/f6e31aafc51f57d14077ef37d1d5cc64a6d399e8))
* **vendor:** commit tree-sitter PHP cgo headers stripped by go mod vendor ([#17](https://github.com/anatolykoptev/vaelor/issues/17)) ([209811c](https://github.com/anatolykoptev/vaelor/commit/209811c4399b329a3d26a8c16c816c8b2654fbed))


### Performance

* **ci:** -short merge gate + nightly full suite (26m -&gt; ~min) ([#301](https://github.com/anatolykoptev/vaelor/issues/301)) ([b04c379](https://github.com/anatolykoptev/vaelor/commit/b04c379a136491e5397e996240f77f5225e7348f))
* compact hand-built XML formatters + code_compare metrics json ([#261](https://github.com/anatolykoptev/vaelor/issues/261)) ([e5c3f22](https://github.com/anatolykoptev/vaelor/commit/e5c3f22d05b60e79ed8432575964bf78e42c2de4))
* **debug_investigate:** Sprint A — parallel Prom queries + skip LLM on quiet signal (6× speedup) ([#78](https://github.com/anatolykoptev/vaelor/issues/78)) ([e7e9ae9](https://github.com/anatolykoptev/vaelor/commit/e7e9ae9b90488a23cd97e8fc28b39c07e8523c6e))
* drop MCP response indentation + duration-only meta footer ([#260](https://github.com/anatolykoptev/vaelor/issues/260)) ([3ee5282](https://github.com/anatolykoptev/vaelor/commit/3ee5282769063fffa3daaf4600473df010f6692e))
* **go-code:** batch sparse-embedding writes + raise backfill deadline ([#200](https://github.com/anatolykoptev/vaelor/issues/200)) ([bdb72ab](https://github.com/anatolykoptev/vaelor/commit/bdb72ab88bf34876aa7b9aa73e62432f8576de15))
* **go-code:** Phase 2c — batch initialCreationLines (BUG-FH-2b cold latency 34s→~3s) ([#159](https://github.com/anatolykoptev/vaelor/issues/159)) ([cb8bf39](https://github.com/anatolykoptev/vaelor/commit/cb8bf39fdc7eb4f4630b4a4ce199a5b953aaa6a7))
* **ingest:** process-level IngestRepo cache to eliminate redundant walks ([#464](https://github.com/anatolykoptev/vaelor/issues/464)) ([#474](https://github.com/anatolykoptev/vaelor/issues/474)) ([7238702](https://github.com/anatolykoptev/vaelor/commit/7238702d1bf6d48d299fde71313234c2102ce683))
* optional go-kit/cache Redis L2 for ingestRepoCache and cgCache ([#493](https://github.com/anatolykoptev/vaelor/issues/493), [#494](https://github.com/anatolykoptev/vaelor/issues/494)) ([#498](https://github.com/anatolykoptev/vaelor/issues/498)) ([0afd748](https://github.com/anatolykoptev/vaelor/commit/0afd7488a29a153aaedbb8eae356ec7c6535a452))
* **parser:** add BenchmarkParseFile and BenchmarkBuildSnapshot ([#404](https://github.com/anatolykoptev/vaelor/issues/404)) ([6461bf6](https://github.com/anatolykoptev/vaelor/commit/6461bf629a32ccfae76dd4f8f568614706351ed2))
* **parser:** share one tree between ParseFile and ExtractCalls ([#400](https://github.com/anatolykoptev/vaelor/issues/400)) ([#408](https://github.com/anatolykoptev/vaelor/issues/408)) ([eafcae6](https://github.com/anatolykoptev/vaelor/commit/eafcae6038adbf1bcb15864af64a1eb20c4739ce))
* **parser:** single-parse Svelte runes instead of double parse ([#406](https://github.com/anatolykoptev/vaelor/issues/406)) ([41970bd](https://github.com/anatolykoptev/vaelor/commit/41970bd58dc9c56e4b45c438dbb73e3d67d06911)), closes [#401](https://github.com/anatolykoptev/vaelor/issues/401)
* **review:** cap review_delta impacted_symbols by default ([#391](https://github.com/anatolykoptev/vaelor/issues/391)) ([#415](https://github.com/anatolykoptev/vaelor/issues/415)) ([29b28ca](https://github.com/anatolykoptev/vaelor/commit/29b28cac1c32db0349759269cb59134ffac52723))
* **scip:** parallelize multi-language SCIP indexing ([#465](https://github.com/anatolykoptev/vaelor/issues/465)) ([#471](https://github.com/anatolykoptev/vaelor/issues/471)) ([378f607](https://github.com/anatolykoptev/vaelor/commit/378f607aaf35de06effae9d70c489b4f994ecd9a))
* **test:** parallelize DB-free test packages (gate ~8m -&gt; ~3.2m) ([#302](https://github.com/anatolykoptev/vaelor/issues/302)) ([c5636f6](https://github.com/anatolykoptev/vaelor/commit/c5636f62a2b3b3d77cc18ad1d8c23bc7058ececc))


### Changed

* **age:** drop per-connection LOAD; rely on shared_preload_libraries with startup check ([#111](https://github.com/anatolykoptev/vaelor/issues/111)) ([e17b4fa](https://github.com/anatolykoptev/vaelor/commit/e17b4fa6eae5b88caf038d9055773c6a9f8b1874))
* **cache:** migrate ParseCache onto generic cache.LRU + per-cache tests + semhealth fixture ([cac9d1c](https://github.com/anatolykoptev/vaelor/commit/cac9d1c8a8d4d12d82b4e3bc7a87df97e9dee09a))
* **callgraph:** move extractGoImplements into EnrichWithTypedResolution ([#467](https://github.com/anatolykoptev/vaelor/issues/467)) ([#472](https://github.com/anatolykoptev/vaelor/issues/472)) ([902890b](https://github.com/anatolykoptev/vaelor/commit/902890b236d2f1ca4acc3da56969d98f45200d6c))
* **callgraph:** unified ingest→parse→build→enrich pipeline ([#463](https://github.com/anatolykoptev/vaelor/issues/463)) ([#475](https://github.com/anatolykoptev/vaelor/issues/475)) ([#478](https://github.com/anatolykoptev/vaelor/issues/478)) ([ae8e74e](https://github.com/anatolykoptev/vaelor/commit/ae8e74e23198e57f6d76f2eaecb20840767895cf))
* **clients:** migrate websearch/oxcodes onto httputil.Client ([#283](https://github.com/anatolykoptev/vaelor/issues/283)) ([b2e62e4](https://github.com/anatolykoptev/vaelor/commit/b2e62e4907ed172df8c916bd7c8cb737a81b7b93))
* **codegraph:** unify IMPLEMENTS edge paths — single construction via buildRelationshipEdges ([#461](https://github.com/anatolykoptev/vaelor/issues/461)) ([e48fa4e](https://github.com/anatolykoptev/vaelor/commit/e48fa4e93e7bd2e18c4b43709b8cb800871e8940))
* consolidate dominant-language argmax into one canonical helper ([#285](https://github.com/anatolykoptev/vaelor/issues/285)) ([22d9db3](https://github.com/anatolykoptev/vaelor/commit/22d9db31b65b5cd2df51e6c56445af04c2e792ed))
* **embeddings:** extract Store.WipeRepo + add wipe CLI subcommand ([#543](https://github.com/anatolykoptev/vaelor/issues/543)) ([6f5c593](https://github.com/anatolykoptev/vaelor/commit/6f5c59333d4a125bf7b4baa0d73842a2c0f70705))
* **embeddings:** use go-kit cache.WithMetrics (v0.33.0 bump) ([#8](https://github.com/anatolykoptev/vaelor/issues/8)) ([3c55d28](https://github.com/anatolykoptev/vaelor/commit/3c55d28f7d119cf2b780f4381ed95a670c4f5350))
* generic cache.LRU for 4 caches + dedup 3 helpers ([5be753e](https://github.com/anatolykoptev/vaelor/commit/5be753e0a3a710cebac7481109f35ed6392b97b5))
* **go-code:** decompose computeHealth into per-subscore helpers ([#183](https://github.com/anatolykoptev/vaelor/issues/183)) ([18227a9](https://github.com/anatolykoptev/vaelor/commit/18227a99308a8baafa1ca41089096ee7c4826bc3))
* **go-code:** decompose formatInvestigationResult into per-section writers ([#182](https://github.com/anatolykoptev/vaelor/issues/182)) ([d8c6834](https://github.com/anatolykoptev/vaelor/commit/d8c68345a097d254cbe6da12360513d01abe4153))
* **go-code:** decompose ScanHtmxRefs (cyclomatic 57→3) ([#204](https://github.com/anatolykoptev/vaelor/issues/204)) ([09606fd](https://github.com/anatolykoptev/vaelor/commit/09606fdc79d0a18c4d69d7909cd84315c329341a))
* **go-code:** dedup 3 copy-paste blocks (dupl → 0 repo-wide) ([#181](https://github.com/anatolykoptev/vaelor/issues/181)) ([111a5a4](https://github.com/anatolykoptev/vaelor/commit/111a5a42162d3699834f7084e295c8d51c9b4446))
* **go-code:** migrate error/no-match + design_search/semantic XML onto typed structs + xml.Marshal ([#263](https://github.com/anatolykoptev/vaelor/issues/263)) ([f1924c8](https://github.com/anatolykoptev/vaelor/commit/f1924c8d7f238bff7b2e8097c7effeb7fffdbe3d))
* **go-code:** migrate final 3 hand-rolled XML formatters onto xml.Marshal + collapse error/json clones ([#266](https://github.com/anatolykoptev/vaelor/issues/266)) ([7d805d5](https://github.com/anatolykoptev/vaelor/commit/7d805d591c20bd143e0378eabe33d7f47732eae5))
* **go-code:** migrate site_analyze/site_crawl/debug_investigate XML onto typed structs + xml.Marshal ([#262](https://github.com/anatolykoptev/vaelor/issues/262)) ([792d3bd](https://github.com/anatolykoptev/vaelor/commit/792d3bd9fededc7661b75b1e238cc4be11dc192a))
* **go-code:** split AGE/data connection pools + schema-qualification guards ([#178](https://github.com/anatolykoptev/vaelor/issues/178)) ([bdb55fd](https://github.com/anatolykoptev/vaelor/commit/bdb55fd8f9563cee66e34b5cd875b219b6a4d69a))
* **go-code:** unify 3 import resolvers into internal/importresolve ([#188](https://github.com/anatolykoptev/vaelor/issues/188)) ([437653a](https://github.com/anatolykoptev/vaelor/commit/437653af5b9e5364c879527e720e0d6a04783716))
* **go-code:** unify tokenization + stopwords into internal/lextoken leaf (BM25F P2) ([#205](https://github.com/anatolykoptev/vaelor/issues/205)) ([c7378da](https://github.com/anatolykoptev/vaelor/commit/c7378da07481a570bc85edc0254f7fd69c77be2e))
* **ingest:** unify parseFilesParallel into shared ingest.ParseFilesParallel ([#469](https://github.com/anatolykoptev/vaelor/issues/469)) ([#473](https://github.com/anatolykoptev/vaelor/issues/473)) ([b95d7e0](https://github.com/anatolykoptev/vaelor/commit/b95d7e00611c57382142847f3ec8a0a31824ec9c))
* **llm-obs:** swap direct-prom histogram for kit Registry.ObserveSeconds ([#122](https://github.com/anatolykoptev/vaelor/issues/122)) ([c4af302](https://github.com/anatolykoptev/vaelor/commit/c4af302d68e2904d242363e8c1045b21e5849c4d))
* migrate to go-kit/rerank.RRF (v0.32.0 bump) ([#3](https://github.com/anatolykoptev/vaelor/issues/3)) ([9f2b0f0](https://github.com/anatolykoptev/vaelor/commit/9f2b0f0c84226bbb73ce3443f93183c537265989))
* **pgutil:** extract TransferOwnership shared helper (DRY PR [#112](https://github.com/anatolykoptev/vaelor/issues/112)) ([#114](https://github.com/anatolykoptev/vaelor/issues/114)) ([2426783](https://github.com/anatolykoptev/vaelor/commit/24267836a5319cb340536b62b902c68aeedd95e1))
* rename Go module github.com/anatolykoptev/go-code -&gt; vaelor (Phase 1) ([#512](https://github.com/anatolykoptev/vaelor/issues/512)) ([ddc1419](https://github.com/anatolykoptev/vaelor/commit/ddc1419d194ff189e1f9b54a511ef99145abd407))
* rename service identity go-code -&gt; vaelor (cmd dir, serviceName, Dockerfile/Makefile) ([133f4ff](https://github.com/anatolykoptev/vaelor/commit/133f4ff8bb4294db98189c381aaeaacc1ef577a8))
* **repo_analyze:** slim XML output without losing agent value ([#16](https://github.com/anatolykoptev/vaelor/issues/16)) ([e0237c2](https://github.com/anatolykoptev/vaelor/commit/e0237c2b4caf57eadd0eae3f232dd33860ff853c))
* **tools:** trim noise from symbol_search and explore output ([#20](https://github.com/anatolykoptev/vaelor/issues/20)) ([32f5091](https://github.com/anatolykoptev/vaelor/commit/32f509199efc07c30c843bcffd8360f312b77bb9))
* **vaelor/cli:** extract newSemanticDeps + add search subcommand ([#541](https://github.com/anatolykoptev/vaelor/issues/541)) ([20a0fa1](https://github.com/anatolykoptev/vaelor/commit/20a0fa1ba64820de4d42ec6dd7827f8b7a872f29))
* **xml:** close Tree xmlCDATA gap + sync assertNoEmptyTag godoc ([#32](https://github.com/anatolykoptev/vaelor/issues/32)) ([b10ca20](https://github.com/anatolykoptev/vaelor/commit/b10ca20a70700878a1537b4327ed6040f30cd044))
* **xml:** convert empty-prone xmlCDATA fields to pointer-form ([#25](https://github.com/anatolykoptev/vaelor/issues/25)) ([387748d](https://github.com/anatolykoptev/vaelor/commit/387748d458a31cca6f10669d024dbd9fd7994f75))


### Documentation

* actualize README — 30 tools, expand Tools table to 27 rows ([9832942](https://github.com/anatolykoptev/vaelor/commit/9832942a202722960141b98fecb2c59fd7a9eeed))
* actualize README — 30 tools, expand Tools table to 27 rows ([759373b](https://github.com/anatolykoptev/vaelor/commit/759373bcf783c60ce8916ff6ec1c4bf370485b6f))
* add hero demo GIF to README ([#487](https://github.com/anatolykoptev/vaelor/issues/487)) ([167c54c](https://github.com/anatolykoptev/vaelor/commit/167c54cca0c12a0f7ea873132417e201e15f7b42))
* **adr:** 0002 environment detect & verify ([#297](https://github.com/anatolykoptev/vaelor/issues/297)) ([9636d75](https://github.com/anatolykoptev/vaelor/commit/9636d75661f6e96f533984c0351a9164b7f18bc3))
* **adr:** 0002 harden Phase 1 resolution per re-review ([#299](https://github.com/anatolykoptev/vaelor/issues/299)) ([ab5cb86](https://github.com/anatolykoptev/vaelor/commit/ab5cb860b7bc7269ff6700092fdb81e87d13d273))
* **adr:** 0002 Phase 1 design resolution — close 6 security-cost blockers (design-only) ([#298](https://github.com/anatolykoptev/vaelor/issues/298)) ([d22973d](https://github.com/anatolykoptev/vaelor/commit/d22973d720c5a08e823ad6c72cc421dac7342c04))
* **adr:** add 0003 callgraph resolver strategy ([#322](https://github.com/anatolykoptev/vaelor/issues/322)) ([55818b0](https://github.com/anatolykoptev/vaelor/commit/55818b014b351751f3a905f00bb4a17db47ef624))
* **CLAUDE:** language count 11 → 13 (added Svelte, Astro) ([#100](https://github.com/anatolykoptev/vaelor/issues/100)) ([0af6a01](https://github.com/anatolykoptev/vaelor/commit/0af6a014ad86bacdfc1ce847c7d96e755335b4a7))
* **debug_investigate:** align hint_kind count with code ([#328](https://github.com/anatolykoptev/vaelor/issues/328)) ([960bdec](https://github.com/anatolykoptev/vaelor/commit/960bdec9c72961e88035a44da886fc280eb4f0d6))
* drop FOLLOWUPS.md index — GitHub issues are the sole followup ledger ([#527](https://github.com/anatolykoptev/vaelor/issues/527)) ([5a617ea](https://github.com/anatolykoptev/vaelor/commit/5a617ea52d870538ffeddd99469c9f0d5f553403))
* fix v1.21 roadmap conflict + sync CLAUDE.md parser language count ([#103](https://github.com/anatolykoptev/vaelor/issues/103)) ([374f4c1](https://github.com/anatolykoptev/vaelor/commit/374f4c116ec52116c2d2769b68ad7c28385c7dda))
* **followups:** record fleet-wide codegraph route-&gt;graph breakage (FU-CG.1-6) [no-deploy] ([#166](https://github.com/anatolykoptev/vaelor/issues/166)) ([d9e5895](https://github.com/anatolykoptev/vaelor/commit/d9e589507ab663ab523d950ba240d2dd6a7e81df))
* **memos:** mark astro-template-refs memo as implemented ([#240](https://github.com/anatolykoptev/vaelor/issues/240)) ([b9690ff](https://github.com/anatolykoptev/vaelor/commit/b9690ffa59ec71d1027a773b2de5f4860166ebc8))
* **migration:** record as-run hardened ag_catalog backfill (executed 2026-05-31) ([#175](https://github.com/anatolykoptev/vaelor/issues/175)) ([2955b55](https://github.com/anatolykoptev/vaelor/commit/2955b55a99e8382315a3c9893796fb910f35e05b))
* phase 1 repowise smoke test findings [no-deploy] ([96947b3](https://github.com/anatolykoptev/vaelor/commit/96947b30e4e0e814ed57aa940aa268b0f7f78ee9))
* phase 2b smoke verified + BUG-FH-2b cold-latency followup [no-deploy] ([71b93ae](https://github.com/anatolykoptev/vaelor/commit/71b93ae9be9cb5ba6d2937439ab922bf1a45fe21))
* **plan:** mark Phase 1 complete (Tasks 1-4) ([#50](https://github.com/anatolykoptev/vaelor/issues/50)) ([9ae6b3d](https://github.com/anatolykoptev/vaelor/commit/9ae6b3d02e457cbb86ca9c49ef5bb32299e027d7))
* **plan:** mark Phase 2 complete (Tasks 5-8) ([#55](https://github.com/anatolykoptev/vaelor/issues/55)) ([b437f79](https://github.com/anatolykoptev/vaelor/commit/b437f79c0c670ae15238e6460bde0e4925ed5ec5))
* re-record hero demo on a production symbol + fix the Try-it command ([#489](https://github.com/anatolykoptev/vaelor/issues/489)) ([44c0194](https://github.com/anatolykoptev/vaelor/commit/44c01942633e26df8d7db691babcd78aac398cd8))
* re-record README hero-demo.gif to show vaelor [no-deploy] ([#532](https://github.com/anatolykoptev/vaelor/issues/532)) ([007116c](https://github.com/anatolykoptev/vaelor/commit/007116ccc573e76c008baa9a1ac981ab0e2c21c9))
* README hero-demo.gif shows real impact output [no-deploy] ([#535](https://github.com/anatolykoptev/vaelor/issues/535)) ([e2ef083](https://github.com/anatolykoptev/vaelor/commit/e2ef083779b430bcfb437eb6d418996d29946f8e))
* reconcile CLAUDE.md + README with source (tools 25→37, +Vue, LLM_MODEL default) ([#485](https://github.com/anatolykoptev/vaelor/issues/485)) ([0221e32](https://github.com/anatolykoptev/vaelor/commit/0221e326eb19fbc8194292e72bbf50ae40b277c3))
* replace Mac home paths with generic placeholder ([aa7f994](https://github.com/anatolykoptev/vaelor/commit/aa7f9947d6f15e981419b648f71b0e6f075db0b1))
* rewrite README for launch (capability-led, source-verified claims) ([#482](https://github.com/anatolykoptev/vaelor/issues/482)) ([b75bc63](https://github.com/anatolykoptev/vaelor/commit/b75bc63d7780d3bc756e7279e5132dd4b1a1baba))
* **ROADMAP:** add v1.21 — OTel Function Attribution shipped 2026-05-09 ([#101](https://github.com/anatolykoptev/vaelor/issues/101)) ([2cc071f](https://github.com/anatolykoptev/vaelor/commit/2cc071f2cc04b82e6edb45dda9a1cf838786a8b2))


### Dependencies

* bump go-stt to v0.3.0 ([#525](https://github.com/anatolykoptev/vaelor/issues/525)) ([20f23e2](https://github.com/anatolykoptev/vaelor/commit/20f23e2bc8c3e019cf6388440314d352d2833db7))

## [1.45.1](https://github.com/anatolykoptev/vaelor/compare/v1.45.0...v1.45.1) (2026-07-19)


### Changed

* **embeddings:** extract Store.WipeRepo + add wipe CLI subcommand ([#543](https://github.com/anatolykoptev/vaelor/issues/543)) ([6f5c593](https://github.com/anatolykoptev/vaelor/commit/6f5c59333d4a125bf7b4baa0d73842a2c0f70705))

## [1.45.0](https://github.com/anatolykoptev/vaelor/compare/v1.44.0...v1.45.0) (2026-07-19)


### Added

* **vaelor/cli:** add cobra root + status/init subcommands, migrate index-designs ([#540](https://github.com/anatolykoptev/vaelor/issues/540)) ([7c03f36](https://github.com/anatolykoptev/vaelor/commit/7c03f3614b55432d83ff70082c6d5f0573c5f1f2))


### Changed

* **vaelor/cli:** extract newSemanticDeps + add search subcommand ([#541](https://github.com/anatolykoptev/vaelor/issues/541)) ([20a0fa1](https://github.com/anatolykoptev/vaelor/commit/20a0fa1ba64820de4d42ec6dd7827f8b7a872f29))

## [1.44.0](https://github.com/anatolykoptev/vaelor/compare/v1.43.0...v1.44.0) (2026-07-19)


### Added

* **go-kit/watcher:** add thin go-filewatcher/v2 adapter with debounce + ignore-dir ([#538](https://github.com/anatolykoptev/vaelor/issues/538)) ([8f2ab6c](https://github.com/anatolykoptev/vaelor/commit/8f2ab6cfb3040561ada4063562c681e5455acc09))

## [1.43.0](https://github.com/anatolykoptev/vaelor/compare/v1.42.3...v1.43.0) (2026-07-19)


### Added

* **go-kit/cli:** add generic cobra scaffold + MCP config-snippet printer ([#537](https://github.com/anatolykoptev/vaelor/issues/537)) ([190bdfb](https://github.com/anatolykoptev/vaelor/commit/190bdfb83c64c66959afbe81b48102050f418670))

## [1.42.3](https://github.com/anatolykoptev/vaelor/compare/v1.42.2...v1.42.3) (2026-07-19)


### Fixed

* SSE + tool keepalive via go-mcpserver v0.17.0 ([#539](https://github.com/anatolykoptev/vaelor/issues/539)) ([daa5905](https://github.com/anatolykoptev/vaelor/commit/daa59059a67aa8b4c51cf86b8a60148d73aa8c08))

## [1.42.2](https://github.com/anatolykoptev/go-code/compare/v1.42.1...v1.42.2) (2026-07-19)


### Documentation

* README hero-demo.gif shows real impact output [no-deploy] ([#535](https://github.com/anatolykoptev/go-code/issues/535)) ([e2ef083](https://github.com/anatolykoptev/go-code/commit/e2ef083779b430bcfb437eb6d418996d29946f8e))

## [1.42.1](https://github.com/anatolykoptev/go-code/compare/v1.42.0...v1.42.1) (2026-07-19)


### Documentation

* re-record README hero-demo.gif to show vaelor [no-deploy] ([#532](https://github.com/anatolykoptev/go-code/issues/532)) ([007116c](https://github.com/anatolykoptev/go-code/commit/007116ccc573e76c008baa9a1ac981ab0e2c21c9))

## [1.42.0](https://github.com/anatolykoptev/go-code/compare/v1.41.5...v1.42.0) (2026-07-18)


### Added

* **mcp:** adopt NewServer + KeepAlive + SchemaCache + DisableLocalhostProtection ([#529](https://github.com/anatolykoptev/go-code/issues/529)) ([a1b963c](https://github.com/anatolykoptev/go-code/commit/a1b963c71e0d9ea2ee97f71b7c19e531b24a1de4))

## [1.41.5](https://github.com/anatolykoptev/vaelor/compare/v1.41.4...v1.41.5) (2026-07-18)


### Documentation

* drop FOLLOWUPS.md index — GitHub issues are the sole followup ledger ([#527](https://github.com/anatolykoptev/vaelor/issues/527)) ([5a617ea](https://github.com/anatolykoptev/vaelor/commit/5a617ea52d870538ffeddd99469c9f0d5f553403))

## [1.41.4](https://github.com/anatolykoptev/vaelor/compare/v1.41.3...v1.41.4) (2026-07-18)


### Dependencies

* bump go-stt to v0.3.0 ([#525](https://github.com/anatolykoptev/vaelor/issues/525)) ([20f23e2](https://github.com/anatolykoptev/vaelor/commit/20f23e2bc8c3e019cf6388440314d352d2833db7))

## [1.41.3](https://github.com/anatolykoptev/vaelor/compare/v1.41.2...v1.41.3) (2026-07-18)


### Fixed

* **release:** goreleaser main path cmd/go-code -&gt; cmd/vaelor ([5b4124f](https://github.com/anatolykoptev/vaelor/commit/5b4124f16148255ca997bfedcf69a7a381410326))

## [1.41.2](https://github.com/anatolykoptev/vaelor/compare/v1.41.1...v1.41.2) (2026-07-18)


### Fixed

* **pgutil:** alertable metric for frozen index marker ([#520](https://github.com/anatolykoptev/vaelor/issues/520)) ([9bc4935](https://github.com/anatolykoptev/vaelor/commit/9bc49350ffcaf28b41be9264d13181618a6f1b64))

## [1.41.1](https://github.com/anatolykoptev/vaelor/compare/v1.41.0...v1.41.1) (2026-07-18)


### Changed

* rename service identity go-code -&gt; vaelor (cmd dir, serviceName, Dockerfile/Makefile) ([133f4ff](https://github.com/anatolykoptev/vaelor/commit/133f4ff8bb4294db98189c381aaeaacc1ef577a8))

## [1.41.0](https://github.com/anatolykoptev/vaelor/compare/v1.40.0...v1.41.0) (2026-07-18)


### Added

* add recent_commits and top_coupled_files to explore ([c026f58](https://github.com/anatolykoptev/vaelor/commit/c026f58ba5c45de3a629e3324f99b99e761df9d2))
* **analyze:** rank.go fusion via WeightedRRF (opt-in via ANALYZE_RANK_FUSION_MODE=rrf) ([b92c1c0](https://github.com/anatolykoptev/vaelor/commit/b92c1c0f276aa643ceade112d2861be1fb3a73cf))
* **analyze:** rank.go fusion via WeightedRRF (opt-in) ([7acc8ba](https://github.com/anatolykoptev/vaelor/commit/7acc8ba662cfc84ef477ee5c791e17fb8a47d667))
* annotate review_pr removed symbols with dead_code_score ([641b186](https://github.com/anatolykoptev/vaelor/commit/641b186a4b6276bf97e63df86aa689b588a3d36f))
* annotate understand/call_trace callers with production/test kind ([#491](https://github.com/anatolykoptev/vaelor/issues/491)) ([#508](https://github.com/anatolykoptev/vaelor/issues/508)) ([eeea22c](https://github.com/anatolykoptev/vaelor/commit/eeea22cdce6e94db093a818f3905f348efec19af))
* async go/types warm-up for cold GOCACHE ([08f91c7](https://github.com/anatolykoptev/vaelor/commit/08f91c76f50f91fdf63ea2da2055c075691d04c1))
* **autoindex:** skip repos whose main branch hasn't moved ([#10](https://github.com/anatolykoptev/vaelor/issues/10)) ([54e76ca](https://github.com/anatolykoptev/vaelor/commit/54e76ca38de3a4d33a45f0def61d5c2ab4d5efca))
* **b3:** expand body window to +50 lines when EndLine unknown ([#86](https://github.com/anatolykoptev/vaelor/issues/86)) ([4b5a36d](https://github.com/anatolykoptev/vaelor/commit/4b5a36daaeae03ae4f12b97a8951af10392ad835))
* **bootstrap:** self-grant ownership + create perms; fail-fast on missing ag_catalog access ([#112](https://github.com/anatolykoptev/vaelor/issues/112)) ([cb22465](https://github.com/anatolykoptev/vaelor/commit/cb224655ff9b958a3ed8de1c4fa7311b88b6d5d8))
* **call_trace:** add refresh parameter to bypass in-memory cache ([#457](https://github.com/anatolykoptev/vaelor/issues/457)) ([09ba0c8](https://github.com/anatolykoptev/vaelor/commit/09ba0c867f69584bbf8d629c986f93163b9944a3))
* **call_trace:** fast path from AGE graph — avoid 2-60s repo reparse ([#434](https://github.com/anatolykoptev/vaelor/issues/434)) ([cfe8e3e](https://github.com/anatolykoptev/vaelor/commit/cfe8e3e670d6c888d4bf6b8d02d07a84d66d1f10))
* **callgraph:** eager GOCACHE warm at startup for AUTO_INDEX_DIRS Go repos ([#35](https://github.com/anatolykoptev/vaelor/issues/35)) ([6270d1b](https://github.com/anatolykoptev/vaelor/commit/6270d1bb298cca12412156fd171d785ea7d01cd7))
* **codegraph:** build FETCHES FromKey as Handler:File composite (Wave 5) ([#154](https://github.com/anatolykoptev/vaelor/issues/154)) ([b97aeff](https://github.com/anatolykoptev/vaelor/commit/b97aeff5e62459beb43df7d47a744cd584613f12))
* **codegraph:** build HANDLES FromKey as Handler:File composite (Wave 6) ([#155](https://github.com/anatolykoptev/vaelor/issues/155)) ([6da02bb](https://github.com/anatolykoptev/vaelor/commit/6da02bb1ea7d4efe14868f08d4d3b7dc4961da85))
* **codegraph:** populate Go IMPLEMENTS edges via go/types satisfaction ([#220](https://github.com/anatolykoptev/vaelor/issues/220)) ([ba11db7](https://github.com/anatolykoptev/vaelor/commit/ba11db70053325bc2be367b2b4adf48e23c9b87a))
* **codegraph:** preflight graph-existence check on read-path ([#43](https://github.com/anatolykoptev/vaelor/issues/43)) ([4772d38](https://github.com/anatolykoptev/vaelor/commit/4772d38e458ef1998acddc5f2a43a75c2189548e))
* **compare:** wire ParseCache through BuildSnapshot/CompareInput ([eee4715](https://github.com/anatolykoptev/vaelor/commit/eee47151596cb2ba5f77a615e866187912c6dcbf))
* debug_investigate MCP tool — Prometheus + Jaeger + symbol correlation ([#56](https://github.com/anatolykoptev/vaelor/issues/56)) ([28ae34e](https://github.com/anatolykoptev/vaelor/commit/28ae34ec42567403845e1a9a027ccba3ce2de496))
* **debug_investigate:** latency + saturation spike detection (Phase β.4) ([#63](https://github.com/anatolykoptev/vaelor/issues/63)) ([4cbf8c3](https://github.com/anatolykoptev/vaelor/commit/4cbf8c3ee1aa2ed22beaf60acfbb56f4fdb77baa))
* **debug_investigate:** Phase 3 — direct symbol resolution via OTEL code.* tags (closes [#74](https://github.com/anatolykoptev/vaelor/issues/74)) ([#77](https://github.com/anatolykoptev/vaelor/issues/77)) ([36bc2e5](https://github.com/anatolykoptev/vaelor/commit/36bc2e59002331ed07b8bff6eab773ab6a27eefe))
* **debug_investigate:** Phase 6 — log excerpts via dozor side-car (β.3b) ([#66](https://github.com/anatolykoptev/vaelor/issues/66)) ([6807b86](https://github.com/anatolykoptev/vaelor/commit/6807b86cfc7aa351feaa8a00252bf8dd4528d826))
* **debug_investigate:** Phase α — auto-discovery, sourcemap resolver, hint_kind, SRP split ([#61](https://github.com/anatolykoptev/vaelor/issues/61)) ([bbbe261](https://github.com/anatolykoptev/vaelor/commit/bbbe2614b9fe90c41d56f516d66a0fe61f63221e))
* **debug_investigate:** Phase γ.B — dead-code filter + impact + symbol body ([#69](https://github.com/anatolykoptev/vaelor/issues/69)) ([457f6ce](https://github.com/anatolykoptev/vaelor/commit/457f6ce94053e663ddbe5932353538f5f743b036))
* **debug_investigate:** Phase γ.C — historical incidents + hint-driven candidate hypotheses ([#70](https://github.com/anatolykoptev/vaelor/issues/70)) ([838c520](https://github.com/anatolykoptev/vaelor/commit/838c52073357e7525cc890ac21b0f8e260bcda3f))
* **debug_investigate:** Phase γ.D — multi-signal fusion + recent diff embedding ([#71](https://github.com/anatolykoptev/vaelor/issues/71)) ([4f6aac6](https://github.com/anatolykoptev/vaelor/commit/4f6aac64e903c23701faa9916ceb7a55f9029ef1))
* **debug_investigate:** Phase γ.E — LLM cache + structured next_check (machine-readable) ([#72](https://github.com/anatolykoptev/vaelor/issues/72)) ([0bcec13](https://github.com/anatolykoptev/vaelor/commit/0bcec13812f7826e64e3b1a47460a1dc37ab4300))
* **debug_investigate:** Prometheus alerts ingestion (Phase β.5) — captures constant-state invariant violations ([#64](https://github.com/anatolykoptev/vaelor/issues/64)) ([21c333b](https://github.com/anatolykoptev/vaelor/commit/21c333b9c83e08c3cca4f8003f8d1a6983f5ebd2))
* **debug_investigate:** Sprint B1 — function body in LLM context (deep code reasoning) ([#79](https://github.com/anatolykoptev/vaelor/issues/79)) ([397c286](https://github.com/anatolykoptev/vaelor/commit/397c286a946dad05dbe2b145aacf73abca2f9a19))
* **debug_investigate:** Sprint B2 — upstream callgraph walk for root-cause discovery ([#80](https://github.com/anatolykoptev/vaelor/issues/80)) ([3e5b113](https://github.com/anatolykoptev/vaelor/commit/3e5b113af14b3501e2344666703ef856751edab7))
* **debug_investigate:** Sprint B4/B5 — downstream callees walk + body excerpts top-5 ([#88](https://github.com/anatolykoptev/vaelor/issues/88)) ([87f1cbd](https://github.com/anatolykoptev/vaelor/commit/87f1cbd1d1680b346423497d8e5217f8a78ba47d))
* drop-in httpmw.NewServeMux + slogh trace correlation ([#95](https://github.com/anatolykoptev/vaelor/issues/95)) ([68d98c3](https://github.com/anatolykoptev/vaelor/commit/68d98c371982efe00479638896c6d01509a30db1))
* dual-read VAELOR_/GO_CODE_ env vars (rebrand) [no-deploy] ([386fc78](https://github.com/anatolykoptev/vaelor/commit/386fc78bf5e611b43f1eb10f95b59313b09a40e1))
* **embeddings:** autoindex concurrency cap + retry-with-backoff (28min→14min cold-start) ([#4](https://github.com/anatolykoptev/vaelor/issues/4)) ([f01941e](https://github.com/anatolykoptev/vaelor/commit/f01941ef67e2a53be16b4ea0ae16eea3ccdd0bbb))
* **embeddings:** cache symbol entries via go-kit cache.GetIfValid (-80% embed-server traffic) ([#5](https://github.com/anatolykoptev/vaelor/issues/5)) ([cd58aa3](https://github.com/anatolykoptev/vaelor/commit/cd58aa35aa53f1c3f0eb8528e513b472eaf267a3))
* **embeddings:** cut model from jina-code-v2 to code-rank-embed ([#231](https://github.com/anatolykoptev/vaelor/issues/231)) ([a4ebf24](https://github.com/anatolykoptev/vaelor/commit/a4ebf24d06d9e8a18e94da41419a5242b5ea4214))
* **embeddings:** enable graph, hotspot, and recency arms in semantic_search RRF ([d3c50ea](https://github.com/anatolykoptev/vaelor/commit/d3c50eae98bde3c04a29c592407426927d19abb3))
* **embeddings:** file-level IndexFile primitive for incremental indexing ([2264d0a](https://github.com/anatolykoptev/vaelor/commit/2264d0ab76b69f07ee1e7f07e15466580e27865f))
* **embeddings:** file-level IndexFile primitive for incremental indexing ([769860f](https://github.com/anatolykoptev/vaelor/commit/769860f31c82cff249e84db32494b54631480c02))
* **embeddings:** gocode_repo_info gauge — resolve opaque repo hash to path ([#227](https://github.com/anatolykoptev/vaelor/issues/227)) ([bfb3247](https://github.com/anatolykoptev/vaelor/commit/bfb3247d779000aa82899992d4bc825128d82db5))
* **embeddings:** IncrementalSync orchestrator using git-diff reconciliation ([0546414](https://github.com/anatolykoptev/vaelor/commit/0546414bc35a00301e197fad693f55ff078d1b0b))
* **embeddings:** IncrementalSync orchestrator using git-diff reconciliation ([0ebf6b4](https://github.com/anatolykoptev/vaelor/commit/0ebf6b4b13e1e049bd8d8456e037f8730c300ba9))
* **embeddings:** WeightedRRF static weights via RRF_WEIGHT_SEMANTIC/KEYWORD env ([#7](https://github.com/anatolykoptev/vaelor/issues/7)) ([e8f7f01](https://github.com/anatolykoptev/vaelor/commit/e8f7f014e768852de4d36e873824f0a1e7a73df3))
* **envdetect:** ADR 0002 Phase 0 — static build/test/install command detection ([#296](https://github.com/anatolykoptev/vaelor/issues/296)) ([eaff91b](https://github.com/anatolykoptev/vaelor/commit/eaff91b2b6422d2f0ee0ffffbdb9cdd86a81fffc))
* **eval:** offline retrieval-quality harness for go-code ([#6](https://github.com/anatolykoptev/vaelor/issues/6)) ([7d53d71](https://github.com/anatolykoptev/vaelor/commit/7d53d71dc41abaab6360c6266d593cb363b732fd))
* expose apply=true in go-code rewrite tool ([1fc1c3f](https://github.com/anatolykoptev/vaelor/commit/1fc1c3f6d45c2cfc73c2f1e40887627aa438aa74))
* **federate:** deadline-bounded federated_cochange with partial results + background prep ([#171](https://github.com/anatolykoptev/vaelor/issues/171)) ([4023320](https://github.com/anatolykoptev/vaelor/commit/40233203ba117223cf4a606ff8ecc5ed1206eb40))
* filter compiled artifacts from coupling and explore dead code ([6c3b38e](https://github.com/anatolykoptev/vaelor/commit/6c3b38e3358e5fbeb40d47ee1c898e5ceaab7522))
* find_duplicates — intra-repo semantic clone detector (5 phases) ([#215](https://github.com/anatolykoptev/vaelor/issues/215)) ([382805c](https://github.com/anatolykoptev/vaelor/commit/382805c34744b62140ad7d9c8fb9d91a103ad1e3))
* **fleet/ssh:** shadow-copy ~/.ssh to writable dir to bypass strict-mode check ([#130](https://github.com/anatolykoptev/vaelor/issues/130)) ([b6f8d8e](https://github.com/anatolykoptev/vaelor/commit/b6f8d8e0347c4f6f31a7e1cf97aef779759bd390))
* **fleet:** multi-host hosts[] input + cross-host SiblingDrift ([#132](https://github.com/anatolykoptev/vaelor/issues/132)) ([6714adf](https://github.com/anatolykoptev/vaelor/commit/6714adff17d6891f6e515ef74d661f6e1b774ed1))
* **fleet:** runtime binary version awareness — fleet_versions tool + debug_investigate Phase 7 ([#124](https://github.com/anatolykoptev/vaelor/issues/124)) ([5dcdfdc](https://github.com/anatolykoptev/vaelor/commit/5dcdfdcb99a357d652cc20c16c41e55d2619696d))
* **fleet:** upstream changelog correlation for TagDrift rows ([#133](https://github.com/anatolykoptev/vaelor/issues/133)) ([28bc6bb](https://github.com/anatolykoptev/vaelor/commit/28bc6bb606abf6067ab4da2cfd7df6d05dc31c31))
* **forge:** GitHub App authentication for separate rate-limit pool ([#39](https://github.com/anatolykoptev/vaelor/issues/39)) ([45d1e74](https://github.com/anatolykoptev/vaelor/commit/45d1e742d483507f7f63b3344299bccdaafe2cde))
* **github_code_search:** add max_fragment_chars and max_total_chars ([#383](https://github.com/anatolykoptev/vaelor/issues/383)) ([a2113ce](https://github.com/anatolykoptev/vaelor/commit/a2113ce8038fc2243c3733c1f182585c5e485df7))
* **go-code:** add nullable sparse_embedding sparsevec column (SPLADE P1) ([#194](https://github.com/anatolykoptev/vaelor/issues/194)) ([8787cca](https://github.com/anatolykoptev/vaelor/commit/8787ccad466161d7a893f86088615ba1c84728a9))
* **go-code:** binary stale-demote safety-net for missed orphans (defense-in-depth) ([#210](https://github.com/anatolykoptev/vaelor/issues/210)) ([01a43ec](https://github.com/anatolykoptev/vaelor/commit/01a43ec0e84d58cfd6d915ba87aace909b518c93))
* **go-code:** BM25F lexical search arm over trigram candidates (BM25F P3) ([#206](https://github.com/anatolykoptev/vaelor/issues/206)) ([4224932](https://github.com/anatolykoptev/vaelor/commit/4224932512c0e88d85dc6a5c5535202f286b9002))
* **go-code:** enable graph, hotspot, and recency RRF arms in semantic_search ([96eafed](https://github.com/anatolykoptev/vaelor/commit/96eafed43046561d65719a81eb3a383e1139582d))
* **go-code:** flag-gated BM25F keyword arm with grep fallback (BM25F P4) ([#207](https://github.com/anatolykoptev/vaelor/issues/207)) ([a6c31ac](https://github.com/anatolykoptev/vaelor/commit/a6c31ac29fc9c02a792fa3189fe64210cf72461f))
* **go-code:** gated SPLADE sparse-vector indexing, batched by server cap (SPLADE P2) ([#195](https://github.com/anatolykoptev/vaelor/issues/195)) ([cf35ba0](https://github.com/anatolykoptev/vaelor/commit/cf35ba059d0df931cd105fbac39cecf9c9721cd5))
* **go-code:** graph-candidate generator as dark-launched 4th RRF arm (graph-first P1) ([#212](https://github.com/anatolykoptev/vaelor/issues/212)) ([351b77b](https://github.com/anatolykoptev/vaelor/commit/351b77b7dcd21b7657f7e85982d48fdc3f6b068f))
* **go-code:** index-time named execution flows (graph-first Phase 2 CORE) ([#213](https://github.com/anatolykoptev/vaelor/issues/213)) ([a9c8713](https://github.com/anatolykoptev/vaelor/commit/a9c871326f509afc993569e22760ee44d12355c7))
* **go-code:** offline A/B harness for SPLADE arm (nDCG@10 + paired t-test gate, SPLADE P6) ([#199](https://github.com/anatolykoptev/vaelor/issues/199)) ([ee9df7d](https://github.com/anatolykoptev/vaelor/commit/ee9df7df7af9dd79f2ff92ddf79fc945f6be91eb))
* **go-code:** operator-triggered sparse_backfill MCP tool (SPLADE P5) ([#198](https://github.com/anatolykoptev/vaelor/issues/198)) ([6d0f695](https://github.com/anatolykoptev/vaelor/commit/6d0f6955393fd350578cdc805a961b347bc83f17))
* **go-code:** Phase 3a federated MCP foundation — repo resolver + cross-repo co-change ([#160](https://github.com/anatolykoptev/vaelor/issues/160)) ([ed9323d](https://github.com/anatolykoptev/vaelor/commit/ed9323d5ae7cce8b7bd73c313c9499c30eb38426))
* **go-code:** Phase 3a.1 — federated co-change signal quality (origin-dedup + lift + sw.js filter) ([#161](https://github.com/anatolykoptev/vaelor/issues/161)) ([c0246e5](https://github.com/anatolykoptev/vaelor/commit/c0246e5f92d8c9d9d94d3e6b85dbd28bd4c4d325))
* **go-code:** Phase 3a.2 — Dunning G² significance ranking (two-tier, support-first) ([#162](https://github.com/anatolykoptev/vaelor/issues/162)) ([ffb4358](https://github.com/anatolykoptev/vaelor/commit/ffb43587ac63aeac308d62a29bc77b288a664ff8))
* **go-code:** Phase 3a.3 — Wilson-LB ranking + ubiquitous-file filter (CodeScene/Evan-Miller port) ([#163](https://github.com/anatolykoptev/vaelor/issues/163)) ([9cb05f1](https://github.com/anatolykoptev/vaelor/commit/9cb05f18e4e52235582169bbe30387179124fda4))
* **go-code:** Phase B — semantic route-match verification (verified-first cross-repo coupling) ([#164](https://github.com/anatolykoptev/vaelor/issues/164)) ([fe0a037](https://github.com/anatolykoptev/vaelor/commit/fe0a037338b550818b05de3a428a59992d0f4b43))
* **go-code:** port repowise patterns — Phase 1 (_meta envelope + biomarkers + 2 new tools) ([#156](https://github.com/anatolykoptev/vaelor/issues/156)) ([784a026](https://github.com/anatolykoptev/vaelor/commit/784a02695677c72191f3554c287d78ac3529d06d))
* **go-code:** resolve relative TS/JS imports to their package container ([#187](https://github.com/anatolykoptev/vaelor/issues/187)) ([4958c53](https://github.com/anatolykoptev/vaelor/commit/4958c53a36e07476b784d2601fb08b39e72d2992))
* **go-code:** resolve TS $lib and @scope/workspace imports ([#189](https://github.com/anatolykoptev/vaelor/issues/189)) ([17a7679](https://github.com/anatolykoptev/vaelor/commit/17a76792984ec825a805aeb5e537296b7bd89f9e))
* **go-code:** sparse as dark-launched 3rd weighted-RRF arm (SPLADE P4) ([#197](https://github.com/anatolykoptev/vaelor/issues/197)) ([3d95701](https://github.com/anatolykoptev/vaelor/commit/3d957018c621aa5d4ddef00abb7ff315ef520f22))
* **go-code:** sparse retrieval + sparsevec HNSW index (SPLADE P3) ([#196](https://github.com/anatolykoptev/vaelor/issues/196)) ([a6cf22c](https://github.com/anatolykoptev/vaelor/commit/a6cf22c0c6b88c3223192d409fd904877e0f11a7))
* **html:** Wave 3 — applicable cross-cuts + docs 15→16 + MAJOR-2 fix ([#152](https://github.com/anatolykoptev/vaelor/issues/152)) ([10a4a98](https://github.com/anatolykoptev/vaelor/commit/10a4a9887cd8244babaefdb1778796e0ab3066a2))
* **html:** Wave 4 — enclosing-template scope tracking → Route.Handler ([#153](https://github.com/anatolykoptev/vaelor/issues/153)) ([49be4a1](https://github.com/anatolykoptev/vaelor/commit/49be4a11d3df8dd7d5c7108ed71856fdd4c3c98d))
* **image:** add openssh-client to runtime so fleet_versions ssh-probe works ([#129](https://github.com/anatolykoptev/vaelor/issues/129)) ([8d18624](https://github.com/anatolykoptev/vaelor/commit/8d186247037be9c2f7345bc0b45c0da1908b089a))
* **importresolve:** stopgap virtual:* module resolution to defining package ([#423](https://github.com/anatolykoptev/vaelor/issues/423)) ([#425](https://github.com/anatolykoptev/vaelor/issues/425)) ([7044d7d](https://github.com/anatolykoptev/vaelor/commit/7044d7dd2b0598f8a446c889b00fd166b38c2c2a))
* improve dead_code detection for Rust pub functions ([da6529c](https://github.com/anatolykoptev/vaelor/commit/da6529cad120c513c7ae17f2d3936a114714aa58))
* **ingest:** add MaxFiles cap to SnapshotOpts and IngestOpts ([43d7ffc](https://github.com/anatolykoptev/vaelor/commit/43d7ffc4cda3b9e8d9de8f8208977d148ef14334))
* **ingest:** INDEX_SKIP_DIRS override + gocode_ingest_skipped_dirs_total counter ([#211](https://github.com/anatolykoptev/vaelor/issues/211)) ([0fc9100](https://github.com/anatolykoptev/vaelor/commit/0fc9100730fa60a0c5a1c186f6ebc27f3447d471))
* **ingest:** surface skip reasons in IngestResult + index.go log ([#113](https://github.com/anatolykoptev/vaelor/issues/113)) ([56541f3](https://github.com/anatolykoptev/vaelor/commit/56541f3a980d07a7735e5df45e1fb01b8f40d640))
* **investigate:** Tasks 5+6 — OperationToFuncName + Hypothesis/RankHypotheses ([#51](https://github.com/anatolykoptev/vaelor/issues/51)) ([1a1e4aa](https://github.com/anatolykoptev/vaelor/commit/1a1e4aaf9ae1179db5f187db4264f005ad3ba428))
* **investigate:** Tasks 7+8 — InvestigationStore + BuildSystemPrompt ([#52](https://github.com/anatolykoptev/vaelor/issues/52)) ([3c58b86](https://github.com/anatolykoptev/vaelor/commit/3c58b868b75661fc42bb327ad134fc65579c3cf1))
* **jaegerclient:** bootstrap Jaeger HTTP client + ListServices + FindTraces + GetTrace ([#47](https://github.com/anatolykoptev/vaelor/issues/47)) ([d6bc36a](https://github.com/anatolykoptev/vaelor/commit/d6bc36af2f4ead43053acec31d1cc586a72d52d6))
* **kotlin:** Wave 3 — cross-cutting integration (tested_by, speculative, astdiff, importcat, apisurf, delta) ([#146](https://github.com/anatolykoptev/vaelor/issues/146)) ([4a59643](https://github.com/anatolykoptev/vaelor/commit/4a5964377d35c1a1d12d8abd6a48584f6e93b97c))
* **llm:** circuit breaker + observability middleware for LLM client ([#120](https://github.com/anatolykoptev/vaelor/issues/120)) ([5407fc8](https://github.com/anatolykoptev/vaelor/commit/5407fc8f585fff10276d2e6999591548b9020c0c))
* **llm:** configurable cooldown TTL via LLM_COOLDOWN_SECONDS (default 15m) ([#234](https://github.com/anatolykoptev/vaelor/issues/234)) ([74fb6d5](https://github.com/anatolykoptev/vaelor/commit/74fb6d52105342306e771b91233a9d9ec7e1800c))
* **llm:** expose LLM_PER_ATTEMPT_TIMEOUT for model chains ([14ca39a](https://github.com/anatolykoptev/vaelor/commit/14ca39a4f2f3e0f7e16593e8b94d5f04f53100c5))
* **llm:** make LLM optional (Completer iface + per-tool degrade policy) ([#118](https://github.com/anatolykoptev/vaelor/issues/118)) ([0ada4c8](https://github.com/anatolykoptev/vaelor/commit/0ada4c8522538261c128a7731a7c5583f731e388))
* **llm:** wire LLM_MODEL_FALLBACK chain (Phase 2) ([e0c02df](https://github.com/anatolykoptev/vaelor/commit/e0c02dfd1e3de4f11ed7048bfcba9b8b8127f67f))
* **llm:** wire LLM_MODEL_FALLBACK chain (Phase 2) ([4a3815a](https://github.com/anatolykoptev/vaelor/commit/4a3815ac4fee82a4413996457b29a6b64b79ed62))
* **llm:** wire per-model cooldown + bump go-kit v0.83.0 ([#233](https://github.com/anatolykoptev/vaelor/issues/233)) ([07b291a](https://github.com/anatolykoptev/vaelor/commit/07b291a335da42da2eb07c52a7c44c482e6894bb))
* LRU+TTL cache for CollectChurn (git log --numstat) ([735a3c7](https://github.com/anatolykoptev/vaelor/commit/735a3c7719b4233c011fa2baa9e33ff21660ae4f))
* LRU+TTL cache for CollectCoupling (git log co-change analysis) ([5f0efe1](https://github.com/anatolykoptev/vaelor/commit/5f0efe19d4ffbcb85e01e212c763def9c4720a01))
* markdown format for expanded code_search results ([86df19d](https://github.com/anatolykoptev/vaelor/commit/86df19dd350d83311539f4b79e2b3047ddcb87b1))
* **metrics:** code_health/code_graph build-failure counters + AGE staleness gauge ([6fac47e](https://github.com/anatolykoptev/vaelor/commit/6fac47e3ca6bf59a92109f06eefc6ff6eebb1d33))
* **metrics:** observability counters for slug-normalize, files-changed, forge-resolve ([#30](https://github.com/anatolykoptev/vaelor/issues/30)) ([7d80669](https://github.com/anatolykoptev/vaelor/commit/7d80669ef87bb6970b4e1ce637e1d585aa9d982e))
* **metrics:** wire ModelFilterObserver to Prometheus counters ([#230](https://github.com/anatolykoptev/vaelor/issues/230)) ([2f4b395](https://github.com/anatolykoptev/vaelor/commit/2f4b39557635da43c5b86ebcdd3c6f172a0ac799))
* **otel:** instrument go-code with go-kit/tracing — Jaeger integration ([#87](https://github.com/anatolykoptev/vaelor/issues/87)) ([5cad582](https://github.com/anatolykoptev/vaelor/commit/5cad5828e18df8b479420a5bff510f4e9969177b))
* ox-codes scoped keyword search in semantic_search ([e8f4e5e](https://github.com/anatolykoptev/vaelor/commit/e8f4e5e8e139eb132a8f4c2953780c8c4c072eb3))
* **oxcodes:** custom taint rules, anti-patterns, rewrite rejections, cache metrics ([#438](https://github.com/anatolykoptev/vaelor/issues/438)) ([59942f3](https://github.com/anatolykoptev/vaelor/commit/59942f33a15f2939165fd93db3886d75f7c30bc1))
* **parser, routes:** HTML/htmx Wave 2 — attribute extraction + routes/match_html ([#151](https://github.com/anatolykoptev/vaelor/issues/151)) ([5d9013d](https://github.com/anatolykoptev/vaelor/commit/5d9013d11561dd124d6c7feb309b1ab8f8f1e613))
* **parser:** astro alias resolution + vue SFC handler ([#241](https://github.com/anatolykoptev/vaelor/issues/241)) ([09fc9fc](https://github.com/anatolykoptev/vaelor/commit/09fc9fc2a15b92eba2454dfd1131f2ce70f28dfc))
* **parser:** Astro markup {expr} calls + refs via shared tsxLang reparse ([#269](https://github.com/anatolykoptev/vaelor/issues/269)) ([d687f70](https://github.com/anatolykoptev/vaelor/commit/d687f70a245c8d13fd653ba6d232750ada1b98e9))
* **parser:** HTML/htmx Wave 1 — handler + Go template preproc ([#150](https://github.com/anatolykoptev/vaelor/issues/150)) ([f93c209](https://github.com/anatolykoptev/vaelor/commit/f93c2090187b56f3fdfe0d90e36bd80c28cfefea))
* **parser:** Kotlin Wave 1 — handler + tag query ([#144](https://github.com/anatolykoptev/vaelor/issues/144)) ([45aace6](https://github.com/anatolykoptev/vaelor/commit/45aace66399856aad6d12b3f5a478261185ded45))
* **parser:** Kotlin Wave 2 — calls + rels + interface + sealed/enum ([#145](https://github.com/anatolykoptev/vaelor/issues/145)) ([5f517bb](https://github.com/anatolykoptev/vaelor/commit/5f517bb20c047f2611bd8b7903d6e600a49b7aa1))
* **parser:** Svelte component composition — TemplateRefs, USES edges, destructured $props() ([#270](https://github.com/anatolykoptev/vaelor/issues/270)) ([5fc3b26](https://github.com/anatolykoptev/vaelor/commit/5fc3b2688fbd4ff6d054be029aaf7460a91634b4))
* **parser:** Svelte template-expressions + control-flow-effective calls/refs ([#271](https://github.com/anatolykoptev/vaelor/issues/271)) ([e3b83e7](https://github.com/anatolykoptev/vaelor/commit/e3b83e7171319c5919018b02dd5df2f20ab15fba))
* **parser:** Swift Wave 1 — handler + tag query ([#147](https://github.com/anatolykoptev/vaelor/issues/147)) ([48af911](https://github.com/anatolykoptev/vaelor/commit/48af911823f81aa22fbec2a0d054a955d5fc941f))
* **parser:** Swift Wave 2 — calls + rels + protocol body + nits ([#148](https://github.com/anatolykoptev/vaelor/issues/148)) ([5b783be](https://github.com/anatolykoptev/vaelor/commit/5b783be24b67eac191bd4db935bbb9f53c421e51))
* port github_code_search from go-search to go-code ([#377](https://github.com/anatolykoptev/vaelor/issues/377)) ([daf5011](https://github.com/anatolykoptev/vaelor/commit/daf50115f83eaced609a8c5545c414615fa95e9f))
* **promclient:** bootstrap Prometheus HTTP client + QueryRange ([#46](https://github.com/anatolykoptev/vaelor/issues/46)) ([78f50f9](https://github.com/anatolykoptev/vaelor/commit/78f50f97287491489d54dc0bf344b46f613c1d0b))
* **repo_analyze:** surface ox-codes dataflow signals at deep mode ([#23](https://github.com/anatolykoptev/vaelor/issues/23)) ([5f12157](https://github.com/anatolykoptev/vaelor/commit/5f121577e579d42ed8a796e844bb1386a39974dd))
* **rerank:** env-tunable rerank timeouts (GOCODE_RERANK_TIMEOUT_S, GOCODE_SEMANTIC_RERANK_TIMEOUT_S) ([#110](https://github.com/anatolykoptev/vaelor/issues/110)) ([a345e8f](https://github.com/anatolykoptev/vaelor/commit/a345e8f6c4c079968f58e9d5fb2112e21e773a53))
* **resolve:** per-IP rate limit for POST /resolve ([#326](https://github.com/anatolykoptev/vaelor/issues/326)) ([a79e71a](https://github.com/anatolykoptev/vaelor/commit/a79e71ab0146bd7f25622cfde597c5cf825f6b26))
* **routes:** consolidate lineAt helper and add Line capture to 5 matchers (FU-CG.7) ([#331](https://github.com/anatolykoptev/vaelor/issues/331)) ([e5e598d](https://github.com/anatolykoptev/vaelor/commit/e5e598d11dc753a1458f01a2ad147b8760827d12))
* **scip:** extract IMPLEMENTS edges from Rust SCIP index — trait impl discovery ([#445](https://github.com/anatolykoptev/vaelor/issues/445)) ([12c99d0](https://github.com/anatolykoptev/vaelor/commit/12c99d0e0ffafa02f8e5e4b60f32f161b6462234))
* **scip:** filter stdlib method calls from SCIP edges to reduce call_trace noise ([#456](https://github.com/anatolykoptev/vaelor/issues/456)) ([31c011c](https://github.com/anatolykoptev/vaelor/commit/31c011c652de1aa996e6d5470f082cebf972c86f))
* **scip:** install scip-java for multi-language type-aware analysis ([#37](https://github.com/anatolykoptev/vaelor/issues/37)) ([3307894](https://github.com/anatolykoptev/vaelor/commit/3307894419866e43bf2f31709dd8c21602e85870))
* **scip:** run SCIP indexers for ALL detected languages, not just dominant ([#459](https://github.com/anatolykoptev/vaelor/issues/459)) ([b8a1c96](https://github.com/anatolykoptev/vaelor/commit/b8a1c96bac952483a13936ec6fbebd8796e12e1d))
* **semantic_search:** add code_graph hint to indexing status ([#359](https://github.com/anatolykoptev/vaelor/issues/359)) ([c64c7d8](https://github.com/anatolykoptev/vaelor/commit/c64c7d87f5a9c300f869ba313cd9a4ccee059661))
* **sourcemap:** make sourcemap max body size configurable ([#324](https://github.com/anatolykoptev/vaelor/issues/324)) ([970b9c4](https://github.com/anatolykoptev/vaelor/commit/970b9c404d7651d30e058a2ce301b11c2d732df6))
* structural call site count in prepare_change ([f69b76e](https://github.com/anatolykoptev/vaelor/commit/f69b76efd94e72fe7ade128bc96106f4ca55a23e))
* **suggestions:** replace embedding fallback with pg_trgm trigram search ([720ab8b](https://github.com/anatolykoptev/vaelor/commit/720ab8b7ac504639d33ca1da5f130ee75b72991b))
* **suggestions:** replace embedding fallback with pg_trgm trigram search ([bea82f3](https://github.com/anatolykoptev/vaelor/commit/bea82f358e9f1ae17c335aa8060115bf74dd1822))
* **swift:** Wave 3 — cross-cutting integration (tested_by, speculative, astdiff, importcat, apisurf, delta) ([#149](https://github.com/anatolykoptev/vaelor/issues/149)) ([555f80b](https://github.com/anatolykoptev/vaelor/commit/555f80be409d607e1bf4749352980aa90922b7ec))
* **symbol_search:** add ast-grep structural pattern mode ([#22](https://github.com/anatolykoptev/vaelor/issues/22)) ([ec98b17](https://github.com/anatolykoptev/vaelor/commit/ec98b173cea8f4b2ab1d2d4147cc7024f3653d58))
* **tracing:** wire httpmw.RegisterRoute for OTEL code.* attrs ([5cc1592](https://github.com/anatolykoptev/vaelor/commit/5cc15926251abd6a68b1feb342f2a1ca133d40a2))
* **tracing:** wire httpmw.RegisterRoute for OTEL code.* attrs on webhook route ([3d6c241](https://github.com/anatolykoptev/vaelor/commit/3d6c2411ad6f329d885fb3e731c59ffd258b01b5))


### Fixed

* add .svelte-kit to scip skipDirs ([a5006c4](https://github.com/anatolykoptev/vaelor/commit/a5006c405c58b1f67abe19db5a9d7d87a62cb6db))
* add FileGlob to ScopedSearchInput (server added it 2026-03-22) ([aa3dc4b](https://github.com/anatolykoptev/vaelor/commit/aa3dc4b3a5fd5c731e18eaef460b3f3f80f650a8))
* add GOCACHE, GOPATH, GOWORK=off for go/packages in container ([ecb0cfd](https://github.com/anatolykoptev/vaelor/commit/ecb0cfdc24f93d22d0f8f97a0da7aa1d818a7696))
* add Rust SCIP support and fix copyForIndexing for build dirs ([7796ab7](https://github.com/anatolykoptev/vaelor/commit/7796ab71243324b9e66ea94d132ecced60fd11b8))
* **age:** use $libdir/plugins/age path so non-superuser roles can LOAD ([#109](https://github.com/anatolykoptev/vaelor/issues/109)) ([6d9c2f9](https://github.com/anatolykoptev/vaelor/commit/6d9c2f99736313c397694d49c90ea9150f651acc))
* annotateWithPageRank uses batch TopPageRank instead of N Symbol() queries ([bc06de2](https://github.com/anatolykoptev/vaelor/commit/bc06de2a1910304544841ef62a000106c34b0616))
* **astro:** narrow alias-counter emit-gate to broken declared aliases ([#243](https://github.com/anatolykoptev/vaelor/issues/243)) ([ae14c69](https://github.com/anatolykoptev/vaelor/commit/ae14c6919af83cfed05d00db6eced8500cc709d2))
* async lazy-build for understand/call_trace cold-start ([#490](https://github.com/anatolykoptev/vaelor/issues/490)) ([#501](https://github.com/anatolykoptev/vaelor/issues/501)) ([1d8ff01](https://github.com/anatolykoptev/vaelor/commit/1d8ff01f426d34c4f700f88e449b0b2ececfe89c))
* **autoindex:** emit skipped_no_vendor outcome + assert no-WARN contract ([#180](https://github.com/anatolykoptev/vaelor/issues/180)) ([fa41fee](https://github.com/anatolykoptev/vaelor/commit/fa41fee6a6af62711c5a6ed75ff62bd3dbfbb1da))
* **autoindex:** skip eager-warm for repos without vendor/ (etsy-forge, dozor) ([#104](https://github.com/anatolykoptev/vaelor/issues/104)) ([a809cbd](https://github.com/anatolykoptev/vaelor/commit/a809cbd84559047c86b217cecadbe8cb0c4aa394))
* B1 relative path candidates + B2 cycle node skip + Source=Span seed test (closes [#81](https://github.com/anatolykoptev/vaelor/issues/81)) ([#82](https://github.com/anatolykoptev/vaelor/issues/82)) ([74dfb30](https://github.com/anatolykoptev/vaelor/commit/74dfb30a2ab7df82d00e99e73f8a26a231068a25))
* **b1:** service-aware path candidate /host/src/&lt;service&gt;/&lt;rel&gt; ([#83](https://github.com/anatolykoptev/vaelor/issues/83)) ([36dbe69](https://github.com/anatolykoptev/vaelor/commit/36dbe6935c481a4599de91bbbf4ad85a7ac137a5))
* **call_trace:** normalize direction values to callers/callees ([#320](https://github.com/anatolykoptev/vaelor/issues/320)) ([196360a](https://github.com/anatolykoptev/vaelor/commit/196360ab2f66774cc3f321438919bccfc296d2d1))
* **call_trace:** rewrite TraceFromAGE with iterative BFS (AGE lacks list comprehension) ([#436](https://github.com/anatolykoptev/vaelor/issues/436)) ([48fdb6f](https://github.com/anatolykoptev/vaelor/commit/48fdb6fcbb1a66329233f2b26039590cb9adac6a))
* caller_kind accuracy — IsTestFile gate + unresolved bucket ([#507](https://github.com/anatolykoptev/vaelor/issues/507)) ([#510](https://github.com/anatolykoptev/vaelor/issues/510)) ([593cb3a](https://github.com/anatolykoptev/vaelor/commit/593cb3a25dbdbba5fcb9454733afd4d2c6ac4783))
* **callgraph:** apply stdlib filter to tree-sitter path, not just SCIP ([#466](https://github.com/anatolykoptev/vaelor/issues/466)) ([#470](https://github.com/anatolykoptev/vaelor/issues/470)) ([042d156](https://github.com/anatolykoptev/vaelor/commit/042d1564f90ce04307fd26aa562b8761be991168))
* **callgraph:** filter callees to call_expression only, exclude member access and vars ([#28](https://github.com/anatolykoptev/vaelor/issues/28)) ([a02e013](https://github.com/anatolykoptev/vaelor/commit/a02e0130eb66e8e2a8e4b0a5f53a4bb013f804f1))
* **callgraph:** resolve generic-function callers in package-level var initializers ([#280](https://github.com/anatolykoptev/vaelor/issues/280)) ([134b7b7](https://github.com/anatolykoptev/vaelor/commit/134b7b73516064381b9789143c0cfc1546932a07))
* **callgraph:** unblock cold-cache prewarm with CGO_ENABLED=0 + log packages.Load failure ([#29](https://github.com/anatolykoptev/vaelor/issues/29)) ([208b22c](https://github.com/anatolykoptev/vaelor/commit/208b22c0f895f07c0fe2614ea5e7c3eaed3ce37f))
* **callgraph:** wire typed call-edge resolution into the AGE-graph path for dead-code accuracy (BUG A, gated default-off) ([3051854](https://github.com/anatolykoptev/vaelor/commit/3051854738f0902d26e625da66c1d37c2238243d))
* **clients:** stop allocating httputil.Client on every call ([#316](https://github.com/anatolykoptev/vaelor/issues/316)) ([6ab7016](https://github.com/anatolykoptev/vaelor/commit/6ab70163b6e341c39446b8d5ed19bd5bf572c2e2))
* **code_graph:** return building status instead of tool error ([#361](https://github.com/anatolykoptev/vaelor/issues/361)) ([dcf8bf8](https://github.com/anatolykoptev/vaelor/commit/dcf8bf8387ee463d93dc18cfb5031846cd50b54d))
* **code_health:** stop deleting a remote clone while the background snapshot is still reading it ([#246](https://github.com/anatolykoptev/vaelor/issues/246)) ([30d0486](https://github.com/anatolykoptev/vaelor/commit/30d0486cb73c004a5a3acf30a59a7f9f6be78b17))
* **codegraph:** add side to side-blind Route MATCH queries (FU-CG.8) ([#333](https://github.com/anatolykoptev/vaelor/issues/333)) ([2275e8f](https://github.com/anatolykoptev/vaelor/commit/2275e8f010aeb044b288a5933c0a85f627d4b5ad))
* **codegraph:** apply ageSetup search_path in bookkeeping-table accessors ([ceee1fc](https://github.com/anatolykoptev/vaelor/commit/ceee1fc1ec6861bff11b8d710f6d38b5f7ceadaa))
* **codegraph:** apply ageSetup search_path in bookkeeping-table accessors ([e4132bf](https://github.com/anatolykoptev/vaelor/commit/e4132bf2a7acdd60d53f356c9efa81a4a2a7207a))
* **codegraph:** emit IMPLEMENTS edge label for IsInterface call edges ([#447](https://github.com/anatolykoptev/vaelor/issues/447)) ([8573e03](https://github.com/anatolykoptev/vaelor/commit/8573e037a431d4714d204b037c5b9524785da055))
* **codegraph:** enable typed call enrichment by default ([#314](https://github.com/anatolykoptev/vaelor/issues/314)) ([7369296](https://github.com/anatolykoptev/vaelor/commit/7369296c4d6ff2f9e08fa1045e3db6ff134ab70d))
* **codegraph:** FU-CG.9 — make route edge counters truthful (built vs unmatched) ([#335](https://github.com/anatolykoptev/vaelor/issues/335)) ([59f7127](https://github.com/anatolykoptev/vaelor/commit/59f7127f6472699d112a2a7f875be254a6a40b68))
* **codegraph:** memory guard + chunked COPY to prevent OOM kernel panic ([#428](https://github.com/anatolykoptev/vaelor/issues/428)) ([#429](https://github.com/anatolykoptev/vaelor/issues/429)) ([fad1c41](https://github.com/anatolykoptev/vaelor/commit/fad1c4176c57a3bce38ba35017709d7d346506c1))
* **codegraph:** preflight guard for graph-missing on read-path ([#42](https://github.com/anatolykoptev/vaelor/issues/42)) ([c31b8ca](https://github.com/anatolykoptev/vaelor/commit/c31b8ca9ac4ff3ed34b394b27579089b8366e127))
* **codegraph:** prune stale dead-code scores when a function stops being an orphan ([#295](https://github.com/anatolykoptev/vaelor/issues/295)) ([d154853](https://github.com/anatolykoptev/vaelor/commit/d15485314dc9f7fc082e0985459208ed6c283662))
* **codegraph:** remove HasGoModule gate from buildAGECallGraph — enable SCIP for Rust ([#449](https://github.com/anatolykoptev/vaelor/issues/449)) ([e6228d8](https://github.com/anatolykoptev/vaelor/commit/e6228d829ac8278436a7b4fe72dccf9831da7f9f))
* **codegraph:** repair fleet-wide HANDLES/FETCHES=0 — route→graph edge builder ([#167](https://github.com/anatolykoptev/vaelor/issues/167)) ([878aa40](https://github.com/anatolykoptev/vaelor/commit/878aa404528848ef080174ad2b3c7c32ed777033))
* **codegraph:** write-path guards + replace fragile template count test ([#44](https://github.com/anatolykoptev/vaelor/issues/44)) ([dec2270](https://github.com/anatolykoptev/vaelor/commit/dec227083e73f45baa8d54c548669b1451947822))
* **compare,codegraph:** code_compare grade reflects freshness + language-aware isExported; [#253](https://github.com/anatolykoptev/vaelor/issues/253) cleanup ([ded3103](https://github.com/anatolykoptev/vaelor/commit/ded310373f1727c947593d8f1475c14f57fe8db5))
* **compare:** avoid duplicate BuildSnapshot when comparing a repo to itself ([f5a5db8](https://github.com/anatolykoptev/vaelor/commit/f5a5db8012554e945a7172b9bf24454e15d7b1d1))
* **compare:** avoid re-parsing files for type relationships ([b02687c](https://github.com/anatolykoptev/vaelor/commit/b02687c3987ee60dedf029359cf8b81fa5092f61))
* **compare:** cap code_compare deadlines to fit 100s proxy timeout ([9b6fdf5](https://github.com/anatolykoptev/vaelor/commit/9b6fdf5b3e786bba044e40a840fc11ad318ce846))
* **compare:** dedupe self-compare snapshots + ParseCache integration ([5963214](https://github.com/anatolykoptev/vaelor/commit/5963214588d593661fbe23e1d602543e527b0817))
* **compare:** deterministic cycle-pair order in find2Cycles (flaky test) ([#272](https://github.com/anatolykoptev/vaelor/issues/272)) ([32a92f7](https://github.com/anatolykoptev/vaelor/commit/32a92f7dbcb17e4734476c822de2dacb00ef7cf7))
* **compare:** raise code_compare deadline from 90s to 3m ([#309](https://github.com/anatolykoptev/vaelor/issues/309)) ([0eb598b](https://github.com/anatolykoptev/vaelor/commit/0eb598bd4710a2bb8ad631d7c718cc86119e43c1))
* **compare:** reuse tree-sitter parser per worker in BuildSnapshot ([#384](https://github.com/anatolykoptev/vaelor/issues/384)) ([3b54e23](https://github.com/anatolykoptev/vaelor/commit/3b54e233fd3d6386bfebc04958657ccf684e8627))
* **compare:** treat zero-dependency repos as N/A for freshness+vuln scoring ([e9f41e8](https://github.com/anatolykoptev/vaelor/commit/e9f41e8f5c4066de7c9e89be42176a607e5e0310))
* **compare:** treat zero-dependency repos as N/A in code_compare grade (match code_health/[#250](https://github.com/anatolykoptev/vaelor/issues/250)) ([25dd2bb](https://github.com/anatolykoptev/vaelor/commit/25dd2bb0b8292a6e66d4789853c86d866f789983))
* **complexity:** unify cyclomatic complexity on parser as single owner ([816b275](https://github.com/anatolykoptev/vaelor/commit/816b275e81fab975ead496d53a7f17e591adf708))
* content-hash staleness guard for cgCache L2 ([#497](https://github.com/anatolykoptev/vaelor/issues/497)) ([#504](https://github.com/anatolykoptev/vaelor/issues/504)) ([752e12a](https://github.com/anatolykoptev/vaelor/commit/752e12a95a63dda4e94a28e2d2692473fd8bccc7))
* **db:** reset pooled-conn search_path on release — bare code_* resolves public, not ag_catalog ([#173](https://github.com/anatolykoptev/vaelor/issues/173)) ([428e4ad](https://github.com/anatolykoptev/vaelor/commit/428e4ada8421798ba14aba3e91770746a541ee9f))
* **deadcode:** language-aware exported check for non-IsPublic languages ([#281](https://github.com/anatolykoptev/vaelor/issues/281)) ([4546b71](https://github.com/anatolykoptev/vaelor/commit/4546b71b59b524a87093ba040017c5c47738e117))
* **debug_investigate:** dedup historical incidents by (Repo, Symbol) ([#85](https://github.com/anatolykoptev/vaelor/issues/85)) ([9616c43](https://github.com/anatolykoptev/vaelor/commit/9616c439121d5b2a5a3b0126cd5057d7362d3b29)), closes [#84](https://github.com/anatolykoptev/vaelor/issues/84)
* **debug_investigate:** drop t.Skip and document %q/%s label choice ([#318](https://github.com/anatolykoptev/vaelor/issues/318)) ([3d44a00](https://github.com/anatolykoptev/vaelor/commit/3d44a000fc39d2236e6b67ccf4dcb25fb9fb3bc2))
* **debug_investigate:** faster polling + LLM timeout bump + service-&gt;repo body mapping ([#99](https://github.com/anatolykoptev/vaelor/issues/99)) ([f296435](https://github.com/anatolykoptev/vaelor/commit/f29643539f7c3348258ff31db0b3d1014b3460c6))
* **debug_investigate:** include code.function in subject + map /build/ paths to host ([49343d4](https://github.com/anatolykoptev/vaelor/commit/49343d4e8e17e93c29c7b46ae5c9d58ad13ea595))
* **debug_investigate:** include code.function in subject + map /build/ paths to host ([49343d4](https://github.com/anatolykoptev/vaelor/commit/49343d4e8e17e93c29c7b46ae5c9d58ad13ea595))
* **debug_investigate:** include code.function in subject + map /build/ paths to host ([e460ab5](https://github.com/anatolykoptev/vaelor/commit/e460ab5b14ecbda37f2ddabe2a8f53ce4b902a4c))
* **debug_investigate:** include repo in cache key + honor explicit repo arg ([#90](https://github.com/anatolykoptev/vaelor/issues/90)) ([b7723f3](https://github.com/anatolykoptev/vaelor/commit/b7723f3861f550f34ac5947f7114b698a4ce7d5f))
* **debug_investigate:** MetricsQueried in legacy path is += not = (closes [#75](https://github.com/anatolykoptev/vaelor/issues/75)) ([#76](https://github.com/anatolykoptev/vaelor/issues/76)) ([d858755](https://github.com/anatolykoptev/vaelor/commit/d858755e319ede2b235e8ba5b17b0a8d2f61b59b))
* **debug_investigate:** Phase 2 — baseline trace fetch (was error-only, starved symbol correlation) ([#73](https://github.com/anatolykoptev/vaelor/issues/73)) ([831c549](https://github.com/anatolykoptev/vaelor/commit/831c549b6c9bb39e01fd8e8d22bd4518817517c9))
* **embeddings:** delete only true orphans (positive IN-list), not per-chunk anti-join ([b6ddc6a](https://github.com/anatolykoptev/vaelor/commit/b6ddc6a48d97211c0a53c7f13ff2e6c302cf688f))
* **embeddings:** incremental sync froze indexed_sha on first unsupported file in diff ([#170](https://github.com/anatolykoptev/vaelor/issues/170)) ([5583fe9](https://github.com/anatolykoptev/vaelor/commit/5583fe9639715b91f4382a5a97ff5129d7b69b11))
* **embeddings:** NUL-separate in-memory symbol keys (colon-in-path safe) + document dedup lossiness ([fd9f7b3](https://github.com/anatolykoptev/vaelor/commit/fd9f7b34c4e5ff31cfe12ecbbd98af17e2213e88))
* **embeddings:** rate-gate autoindex concurrency to 1 for single-worker embed backend ([#217](https://github.com/anatolykoptev/vaelor/issues/217)) ([06b2e09](https://github.com/anatolykoptev/vaelor/commit/06b2e099eab1f24784d309f7c7b648829a6a24e1))
* **embeddings:** replace misleading freshness gauge with commits-behind + count SetRepoState write-failures ([#172](https://github.com/anatolykoptev/vaelor/issues/172)) ([458c9ff](https://github.com/anatolykoptev/vaelor/commit/458c9ff3e00bf094dd4463d8abe9983437b79cb7))
* **embeddings:** treat all 5xx as retryable; add embed_model per-row; continuous orphan gauge ([#232](https://github.com/anatolykoptev/vaelor/issues/232)) ([f599da6](https://github.com/anatolykoptev/vaelor/commit/f599da6840299064f3dd1dccc52c6f31e0dcc36f))
* **explore:** files_changed reflects single commit diff, not cumulative range ([#26](https://github.com/anatolykoptev/vaelor/issues/26)) ([7871abe](https://github.com/anatolykoptev/vaelor/commit/7871abefb5c887dfca3f4c9a51855c7effaa62f6))
* **explore:** label health score as approximate with hint ([#249](https://github.com/anatolykoptev/vaelor/issues/249)) ([3bb3e90](https://github.com/anatolykoptev/vaelor/commit/3bb3e90bfacafee052092e4479ab2ed0a8f31a77))
* **federate:** FU-1.1 — thread request ctx into ResolveRepos for cancellable origin dedup ([#337](https://github.com/anatolykoptev/vaelor/issues/337)) ([16d22f2](https://github.com/anatolykoptev/vaelor/commit/16d22f21ec2ea4b7dc9a0a9e4067ae9e69832804))
* **federate:** pass asOf time.Time to CrossRepoCoChange to avoid wall-clock git log --since ([1da47a2](https://github.com/anatolykoptev/vaelor/commit/1da47a209dc2e4a93b9759938cf9cf00d34999d6))
* **fleet/ssh:** pass -F flag explicitly and rewrite ~ paths in shadow config ([#131](https://github.com/anatolykoptev/vaelor/issues/131)) ([ee9f263](https://github.com/anatolykoptev/vaelor/commit/ee9f263731af23362d29b9fbb04e688861fa304c))
* **forge:** deflake metrics_test.go counter delta assertions ([#308](https://github.com/anatolykoptev/vaelor/issues/308)) ([13e2481](https://github.com/anatolykoptev/vaelor/commit/13e2481b193831f0dd7b48ea7dec11f679a66660))
* **forge:** ExtractSlug + DetectForge accept URL/SSH forms ([#27](https://github.com/anatolykoptev/vaelor/issues/27)) ([82f2286](https://github.com/anatolykoptev/vaelor/commit/82f228605d81f30384fc984630f263138eea6d13))
* **gitutil:** accept .git file form in worktree detection ([#36](https://github.com/anatolykoptev/vaelor/issues/36)) ([96ea0e1](https://github.com/anatolykoptev/vaelor/commit/96ea0e192e7790c7e9357def6b9a48f229380611))
* go build pre-warm + longer timeouts for go/types GOCACHE ([d3da897](https://github.com/anatolykoptev/vaelor/commit/d3da897c3043d7e3c3ba3aaace127e9d2e10afc9))
* **go-code:** accept owner/repo form in github_code_search tool ([#381](https://github.com/anatolykoptev/vaelor/issues/381)) ([fcd71df](https://github.com/anatolykoptev/vaelor/commit/fcd71df03c66db9586cf7cc96761b24976374463))
* **go-code:** batch build-time dead_code rerank to the server's per-request cap ([#191](https://github.com/anatolykoptev/vaelor/issues/191)) ([e24fbb4](https://github.com/anatolykoptev/vaelor/commit/e24fbb48dbe8cf100c561a9248b3619cc28f5821))
* **go-code:** embed HTTP timeout + bounded async index ctx + attributable cancel ([#216](https://github.com/anatolykoptev/vaelor/issues/216)) ([2c32684](https://github.com/anatolykoptev/vaelor/commit/2c326843b2b3059904636c5a23b9d0efaeee6da2))
* **go-code:** exclude *_test.go imports from circular-dep detection ([#184](https://github.com/anatolykoptev/vaelor/issues/184)) ([f24d75d](https://github.com/anatolykoptev/vaelor/commit/f24d75d1a344dec9abfc37d6618ec0d3ea28f42c))
* **go-code:** group archgraph queries by package path, not base name ([#186](https://github.com/anatolykoptev/vaelor/issues/186)) ([8d7ffa8](https://github.com/anatolykoptev/vaelor/commit/8d7ffa87f8904b68426f25bce93cc55919eee831))
* **go-code:** Phase 2a cleanup — 17 items (BUG-FH-1/2 closed, error encoding unified, +13 cosmetic) ([#157](https://github.com/anatolykoptev/vaelor/issues/157)) ([9d80b71](https://github.com/anatolykoptev/vaelor/commit/9d80b71473e102e829416fbfd5e94a005f4ea199))
* **go-code:** Phase 2b infra — Commits count, churn growth, since window, --follow, WithFreshness wiring ([#158](https://github.com/anatolykoptev/vaelor/issues/158)) ([f239728](https://github.com/anatolykoptev/vaelor/commit/f23972884ef4c9bbecad9150c06d59448e0a3ee6))
* **go-code:** pool AfterRelease RESET ALL, not DISCARD ALL (26000 regression) ([#176](https://github.com/anatolykoptev/vaelor/issues/176)) ([e24c92e](https://github.com/anatolykoptev/vaelor/commit/e24c92e63d756e8283f0fee0e7e69d784e3a891c))
* **go-code:** reconcile orphan embedding rows on full index + operator sweep (Bug B — phantom symbols) ([#209](https://github.com/anatolykoptev/vaelor/issues/209)) ([c255c4d](https://github.com/anatolykoptev/vaelor/commit/c255c4dd0a7d1b2a4533802c1f6ad116d80c43cc))
* **go-code:** rerank via go-kit/rerank.Client, drop hardcoded embed-server URL ([#190](https://github.com/anatolykoptev/vaelor/issues/190)) ([82fc468](https://github.com/anatolykoptev/vaelor/commit/82fc468c1ab784253fed3699bcdea7d274d3c3c5))
* **go-code:** self-index desync (SHA-gate data-aware) + HTTP-index-cancel observability ([#214](https://github.com/anatolykoptev/vaelor/issues/214)) ([3ce4f94](https://github.com/anatolykoptev/vaelor/commit/3ce4f941f4d40359b49ff4bc6871d79f8265fa0a))
* **go-code:** sparsevec batch size 500→100 (data-bound statement_timeout) + accurate write_failed counter ([#201](https://github.com/anatolykoptev/vaelor/issues/201)) ([cc7e29c](https://github.com/anatolykoptev/vaelor/commit/cc7e29c60f228fc2eabffd582b48b269818ddcd7))
* **go-code:** unify local package nodes (stop duplicate dir/import-path vertices) ([#185](https://github.com/anatolykoptev/vaelor/issues/185)) ([bdee2c6](https://github.com/anatolykoptev/vaelor/commit/bdee2c6f70e574afbaa12fe9aa29b09dd17a1635))
* **graph-arm:** invert pagerank sub-generator — keyword-relevant ranked by pagerank ([#219](https://github.com/anatolykoptev/vaelor/issues/219)) ([2654fbd](https://github.com/anatolykoptev/vaelor/commit/2654fbd1bb3226ea1b5bd214d380446ea523949b))
* **importresolve:** honor package.json exports map for workspace subpath imports ([#422](https://github.com/anatolykoptev/vaelor/issues/422)) ([#424](https://github.com/anatolykoptev/vaelor/issues/424)) ([45dc05a](https://github.com/anatolykoptev/vaelor/commit/45dc05ac604662e453df741b1d86fa28c871cdf9))
* **ingest,explore:** shallow clone depth=2 + shallow-boundary guard in countDiffTreeFiles ([#31](https://github.com/anatolykoptev/vaelor/issues/31)) ([06f542b](https://github.com/anatolykoptev/vaelor/commit/06f542ba37b4276121626433d1a60d944bfc76fb))
* **ingest:** accept comma-separated focus keywords ([#305](https://github.com/anatolykoptev/vaelor/issues/305)) ([f67814c](https://github.com/anatolykoptev/vaelor/commit/f67814c14f6dc6129f430c4d0de06cbfd8138cb8))
* **ingest:** atomic clone via renameat2 RENAME_EXCHANGE; errno breakdown for read_error ([#116](https://github.com/anatolykoptev/vaelor/issues/116)) ([b9d27eb](https://github.com/anatolykoptev/vaelor/commit/b9d27ebf731f8dbb53047e5d93ae9ca36eefff94))
* **ingest:** defensive copy in IngestRepo cache to prevent aliasing ([#477](https://github.com/anatolykoptev/vaelor/issues/477)) ([d7369ed](https://github.com/anatolykoptev/vaelor/commit/d7369eddf4347346f2124c9855c429a8d177ada7))
* **ingest:** NormalizeSlug accepts URL and SSH forms ([#24](https://github.com/anatolykoptev/vaelor/issues/24)) ([89e5280](https://github.com/anatolykoptev/vaelor/commit/89e528092c9135f4bb78edca6339b0a642d73093))
* **ingest:** refresh credentials via GIT_CONFIG before git fetch ([#107](https://github.com/anatolykoptev/vaelor/issues/107)) ([e6e8221](https://github.com/anatolykoptev/vaelor/commit/e6e82212d30b6fa8fdef6f8db98a3fcc9bd0a1fd))
* **ingest:** refresh on cache-hit to remote HEAD instead of trusting on-disk state ([#21](https://github.com/anatolykoptev/vaelor/issues/21)) ([a9ea497](https://github.com/anatolykoptev/vaelor/commit/a9ea497be23c174308bbde71a5f4e1d97d642ddc))
* **ingest:** use App installation token for clone when configured ([#105](https://github.com/anatolykoptev/vaelor/issues/105)) ([a9ce6ae](https://github.com/anatolykoptev/vaelor/commit/a9ce6ae5a7c7c131e7be9d8b02afd87df3facf1b))
* **llm-obs:** register metrics against go-code's registry, not default ([#121](https://github.com/anatolykoptev/vaelor/issues/121)) ([40a15ac](https://github.com/anatolykoptev/vaelor/commit/40a15ac789c35dfa9a5f93ab07cf48fd8b6b4ee4))
* **llm:** default per-attempt timeout for chain rotation + review_delta 120s ([#391](https://github.com/anatolykoptev/vaelor/issues/391)) ([#395](https://github.com/anatolykoptev/vaelor/issues/395)) ([0d6ebb5](https://github.com/anatolykoptev/vaelor/commit/0d6ebb53d83219f2d7631bb00a7e358401659dd8))
* **mcpmeta:** correct misleading stale-index remediation advice ([#169](https://github.com/anatolykoptev/vaelor/issues/169)) ([05e4ceb](https://github.com/anatolykoptev/vaelor/commit/05e4ceb568c4257ef914b36db5b5941bb57b10e0))
* **mcp:** raise code_graph timeout + non-blocking narrative + branch cleanup ([#433](https://github.com/anatolykoptev/vaelor/issues/433)) ([bd72573](https://github.com/anatolykoptev/vaelor/commit/bd7257319ef6f537e538eee938ceea8497894048))
* **mcp:** return tool results as application/json, not single-line SSE ([#245](https://github.com/anatolykoptev/vaelor/issues/245)) ([47cd6c6](https://github.com/anatolykoptev/vaelor/commit/47cd6c6e228392f9199aa2b5d5bb0d0fdf23c167))
* **mcp:** reverse-map container paths in outputs + zero-result hint ([#45](https://github.com/anatolykoptev/vaelor/issues/45)) ([f30d6b5](https://github.com/anatolykoptev/vaelor/commit/f30d6b56fd87bf364ae50bbccdb2fc8e12adba9f))
* **metrics:** add per-symbol cognitive complexity and fix JS docRatio ([#247](https://github.com/anatolykoptev/vaelor/issues/247)) ([eef6282](https://github.com/anatolykoptev/vaelor/commit/eef62824bc2d45ef76eb74cf7df5d09f1b196265))
* **metrics:** pre-register alert-facing series at boot (graph age, zero-embeddings) ([#287](https://github.com/anatolykoptev/vaelor/issues/287)) ([e7c605e](https://github.com/anatolykoptev/vaelor/commit/e7c605e7f5af51d7c9b0ab3af06c2c222cdda247))
* **metrics:** record outcome=error on resolve failure + drop unemitted skipped label ([0810d34](https://github.com/anatolykoptev/vaelor/commit/0810d3451e2b9701dc826dcb5ffd478e68f5c821))
* **metrics:** scope code-graph age gauge to AUTO_INDEX_DIRS repos ([#291](https://github.com/anatolykoptev/vaelor/issues/291)) ([61c7bd6](https://github.com/anatolykoptev/vaelor/commit/61c7bd6dae46629da4fda44fdfeaf71b1884b4d7))
* **metrics:** unify health score and add arch fallback for unindexed repos ([#248](https://github.com/anatolykoptev/vaelor/issues/248)) ([78c7e55](https://github.com/anatolykoptev/vaelor/commit/78c7e55706a014565b294aefeec527601be6db21))
* only pass format=markdown to ox-codes when expand is requested ([937b4bc](https://github.com/anatolykoptev/vaelor/commit/937b4bcb662df1ee308871c11f2434813a2de545))
* **oxcodes:** bump structural-search HTTP timeout 10s-&gt;30s ([#168](https://github.com/anatolykoptev/vaelor/issues/168)) ([639cb1b](https://github.com/anatolykoptev/vaelor/commit/639cb1bddcdfcb9d33ecdbef9e68b76a085a6be8))
* ParseCache drops call sites and ignores includeBody on hit ([#286](https://github.com/anatolykoptev/vaelor/issues/286)) ([f6dc7eb](https://github.com/anatolykoptev/vaelor/commit/f6dc7ebf87c0dfd4cf70ebbed7411de09d5b9e31))
* **parser:** dual-emit rune symbols so $state query finds all bound declarations ([#108](https://github.com/anatolykoptev/vaelor/issues/108)) ([f69810e](https://github.com/anatolykoptev/vaelor/commit/f69810ed07d3f7e831d415a46df51fc13b23582c))
* **parser:** JS/TS-family Symbol.Language parity — .jsx/.js/.mjs/.cjs emit javascript ([#268](https://github.com/anatolykoptev/vaelor/issues/268)) ([eb64edf](https://github.com/anatolykoptev/vaelor/commit/eb64edfe523a08e7b7f5221de09a2f4a06c8ffaa))
* **parser:** route Vue call extraction through the two-region ScriptCalls/MarkupCalls split ([#409](https://github.com/anatolykoptev/vaelor/issues/409)) ([#414](https://github.com/anatolykoptev/vaelor/issues/414)) ([1b6d8b1](https://github.com/anatolykoptev/vaelor/commit/1b6d8b18f67204a88e1042d1a05e46a3622db4a5))
* pass Logger to mcpserver.Run to preserve slogh wrapper ([5baf8e9](https://github.com/anatolykoptev/vaelor/commit/5baf8e93e2b04870b0c9ce70f9227e08bc71910d))
* **pipeline-file:** mirror indexRepo filters (isTestFile + maxIndexFileBytes) ([9247295](https://github.com/anatolykoptev/vaelor/commit/92472953e07bad4a93e6d10ad8616febe09b3fc9))
* **pipeline-incremental:** bind ctx to git diff exec + surface stderr ([f6a8589](https://github.com/anatolykoptev/vaelor/commit/f6a85894abd2e0cf3206d34096440770c89a6f0e))
* **polyglot/pinned:** don't abort Collect walk on permission errors ([#126](https://github.com/anatolykoptev/vaelor/issues/126)) ([f15aa1f](https://github.com/anatolykoptev/vaelor/commit/f15aa1f7226b211deab42b4e15a594a6b4ec2324))
* **polyglot/pinned:** resolve compose include: directive recursively ([#125](https://github.com/anatolykoptev/vaelor/issues/125)) ([ff0e593](https://github.com/anatolykoptev/vaelor/commit/ff0e593f31fccd63cf708c2974951947f9cfba44))
* **polyglot/pinned:** skip nested git repos, submodules, and .claude worktrees ([#127](https://github.com/anatolykoptev/vaelor/issues/127)) ([e485e59](https://github.com/anatolykoptev/vaelor/commit/e485e5904c1fec3527676f7d233d562ccb935cdd))
* put tracemcpmw first so hooks receive span context ([5fc68fb](https://github.com/anatolykoptev/vaelor/commit/5fc68fb56c6f49a30f9cbd308d8695af37ac672c))
* regex patterns in speculative call resolution ([d57a56b](https://github.com/anatolykoptev/vaelor/commit/d57a56be33cbc98805e22589eb664fb7e3d5ec0c))
* **release-please:** guard auto-merge step when no release PR ([#311](https://github.com/anatolykoptev/vaelor/issues/311)) ([355a08c](https://github.com/anatolykoptev/vaelor/commit/355a08c8b482962fb68031d90d07ef7d28f4266c))
* **release:** amd64 CGO CC override + consolidate to one goreleaser config ([#277](https://github.com/anatolykoptev/vaelor/issues/277)) ([a46412e](https://github.com/anatolykoptev/vaelor/commit/a46412e50e7d2353a6d669efb2448e1296cd5294))
* remove Go-only file filter from CollectCoupling ([3e7b394](https://github.com/anatolykoptev/vaelor/commit/3e7b394479e897996f922174dddad1665366d7f4))
* **repo_analyze:** omit empty &lt;signature&gt; tag entirely (not just content) ([#19](https://github.com/anatolykoptev/vaelor/issues/19)) ([b913f22](https://github.com/anatolykoptev/vaelor/commit/b913f22504bfa09b1386ac8bd02976e75360a72c))
* **resolve:** prefer local /host/src checkout over clone for matching slugs ([c07c970](https://github.com/anatolykoptev/vaelor/commit/c07c97052cae7bfa727097df4254ff6808c184e7))
* **resolve:** prefer local /host/src checkout over clone for matching slugs ([be97d8e](https://github.com/anatolykoptev/vaelor/commit/be97d8e79d806cd83328190f21db5f20ce779b58))
* **resolve:** resolve bare repo names against LocalRepoDirs registry ([#226](https://github.com/anatolykoptev/vaelor/issues/226)) ([d3e4589](https://github.com/anatolykoptev/vaelor/commit/d3e45890c2ea8b3d2ed68d1eec5c94842329d403))
* retry-safe, lock-safe embeddings & designmd schema init ([#495](https://github.com/anatolykoptev/vaelor/issues/495), [#496](https://github.com/anatolykoptev/vaelor/issues/496)) ([#499](https://github.com/anatolykoptev/vaelor/issues/499)) ([d2c3064](https://github.com/anatolykoptev/vaelor/commit/d2c306408d4d324a9aefb5fe8cc3bd2e0ef73561))
* **review_pr:** pass FETCH_HEAD to diff, not warm-clone HEAD ([#12](https://github.com/anatolykoptev/vaelor/issues/12)) ([76e3081](https://github.com/anatolykoptev/vaelor/commit/76e30819c089a38e9c67d3e97c0918930c7cfdbe))
* **review_pr:** worktree-isolated checkout for call graph analysis ([#13](https://github.com/anatolykoptev/vaelor/issues/13)) ([ef084e9](https://github.com/anatolykoptev/vaelor/commit/ef084e9ed24d7d6488f4a730cea8ad6d5eeda8ee))
* **review:** correct untested-symbol false positives in review_delta ([#392](https://github.com/anatolykoptev/vaelor/issues/392)) ([5f23f64](https://github.com/anatolykoptev/vaelor/commit/5f23f6401d2965365652457b2924004817498645))
* **review:** route PR-post write path through the multi-forge registry ([#284](https://github.com/anatolykoptev/vaelor/issues/284)) ([ae86487](https://github.com/anatolykoptev/vaelor/commit/ae8648726f94eddcf11e6552d66886e407ac35d9))
* **review:** use valid ox-codes scope "function_bodies" in review_delta ([#420](https://github.com/anatolykoptev/vaelor/issues/420)) ([3d9f499](https://github.com/anatolykoptev/vaelor/commit/3d9f49906f661abf3e432ce5e1fde82c2d754ec3)), closes [#419](https://github.com/anatolykoptev/vaelor/issues/419)
* **review:** worktree-aware git invocation via --git-dir + PathRewrite ([#38](https://github.com/anatolykoptev/vaelor/issues/38)) ([677b10c](https://github.com/anatolykoptev/vaelor/commit/677b10c39d351b32557ad74422a363ec9a0978b0))
* safe type assertion in resolveMethodSelection ([1c8e89d](https://github.com/anatolykoptev/vaelor/commit/1c8e89ddf03e7e0dcb632a50eace7b25244011eb))
* sane fresh-deploy defaults for LLM model and /resolve rate limit ([#412](https://github.com/anatolykoptev/vaelor/issues/412)) ([2dc7076](https://github.com/anatolykoptev/vaelor/commit/2dc7076cad4b1dd453d6c075b69a3f7dab836da5))
* **scip:** use content hash instead of mtimes for CacheKey — no false misses on git checkout ([#458](https://github.com/anatolykoptev/vaelor/issues/458)) ([cb2a0a1](https://github.com/anatolykoptev/vaelor/commit/cb2a0a1ff5b3415b585e30615c5463fd4ff2f6dd))
* **scip:** wire Cache into trySCIPResolution — skip re-indexing on cache hit ([#443](https://github.com/anatolykoptev/vaelor/issues/443)) ([0a71e0b](https://github.com/anatolykoptev/vaelor/commit/0a71e0b5e537eab8043dcc14efaaa4d4c60a7726))
* scoped call site search + safe go/types type assertion ([fc2f5e6](https://github.com/anatolykoptev/vaelor/commit/fc2f5e6f64d67d0ab8dbe2012508d1ba0679433e))
* **semantic_search:** strip AGE agtype quotes from complexity values ([7f54db3](https://github.com/anatolykoptev/vaelor/commit/7f54db3a8043d1b15607d0da5e3dcd3da01b884c))
* **semantic-fallback:** cap embed query at 5s sub-context ([166ef7e](https://github.com/anatolykoptev/vaelor/commit/166ef7e9eb3cca5ea06b617a1f3c4eb3d8c8f56a))
* **semantic-search:** dedup semantic-only + CE-rerank results by file:symbol (Bug A) ([#208](https://github.com/anatolykoptev/vaelor/issues/208)) ([c7ae2d0](https://github.com/anatolykoptev/vaelor/commit/c7ae2d0007e94e3c327904ce1423506915327a89))
* **semhealth:** eliminate two find_duplicates false-positive classes ([#218](https://github.com/anatolykoptev/vaelor/issues/218)) ([1880548](https://github.com/anatolykoptev/vaelor/commit/18805484fc75c9404017909495a55c7239874552))
* **semhealth:** guard self-join by repo size + statement_timeout ([77a5460](https://github.com/anatolykoptev/vaelor/commit/77a546081bd805ae045d4b103cd781604fad6a5d))
* serialize EnsureGraph provisioning to fix pg_type 23505 race ([#417](https://github.com/anatolykoptev/vaelor/issues/417)) ([48c1aae](https://github.com/anatolykoptev/vaelor/commit/48c1aaedb0b71a42035f892d7803ad6f3ef63da8))
* shrink code_compare LLM prompt to fit 8k-token fleet models ([#398](https://github.com/anatolykoptev/vaelor/issues/398)) ([429438d](https://github.com/anatolykoptev/vaelor/commit/429438d3576c5258a0f8260057bb8e74b16ac3d5))
* **test:** make TestSignalHitsLiveIntegration self-contained (nightly green) ([#389](https://github.com/anatolykoptev/vaelor/issues/389)) ([4ff1205](https://github.com/anatolykoptev/vaelor/commit/4ff1205b5607598b231c60061a9de45c5c44ed2a))
* three go-code anomalies from 2026-06-12 investigation ([#228](https://github.com/anatolykoptev/vaelor/issues/228)) ([2877d8d](https://github.com/anatolykoptev/vaelor/commit/2877d8d0a8614b75fe1225afd3c61bc40e37fdb5))
* **toolserver:** add understand to ToolTimeouts (30s) ([6047f7b](https://github.com/anatolykoptev/vaelor/commit/6047f7bb00ca5b7105063d359ccf6e1feada320a))
* **tracing:** cast webhook handler to concrete type for correct code.* attrs ([4b3ed8e](https://github.com/anatolykoptev/vaelor/commit/4b3ed8eb0e4001fc216b497469cc24d674a1e867))
* **tracing:** cast webhook handler to concrete type for correct code.* attrs ([8e68b82](https://github.com/anatolykoptev/vaelor/commit/8e68b822eb981603adb98fd952f61f3efa1fba4e))
* **tracing:** use method expression for real code.* source location ([905aa24](https://github.com/anatolykoptev/vaelor/commit/905aa24dd8f07212e961af390cf51dc9abc06dcb))
* **tracing:** use method expression for real source location in code.* attrs ([67f1104](https://github.com/anatolykoptev/vaelor/commit/67f11045a09fd5386c8cad71b64588e27e6352bb))
* transfer table ownership on learnings + designmd store init ([#265](https://github.com/anatolykoptev/vaelor/issues/265)) ([6d726b4](https://github.com/anatolykoptev/vaelor/commit/6d726b4d2d9b650a31d32b45a63b087d1e205659))
* **understand:** bound semantic fallback + add tool timeout + guard semhealth self-join ([1d02992](https://github.com/anatolykoptev/vaelor/commit/1d0299262d4a424e46bb7187183f7c8078a5bf6e))
* update dataflow tool description for TS/JS/Rust support ([0ab90b7](https://github.com/anatolykoptev/vaelor/commit/0ab90b76d40820c0456335141cbe2f144a8bbb23))
* use -mod=vendor for go/packages when vendor/ exists ([49b93be](https://github.com/anatolykoptev/vaelor/commit/49b93be62d6cff0601f2614a940b8fef481d9f4d))
* use concrete slog.TextHandler as slogh base to avoid log bridge deadlock ([#96](https://github.com/anatolykoptev/vaelor/issues/96)) ([d600307](https://github.com/anatolykoptev/vaelor/commit/d600307f0dba544e82c547b7c73d19083a80c27c))
* use golang:1.26-alpine runtime to enable go/types enhanced call resolution ([88a80ce](https://github.com/anatolykoptev/vaelor/commit/88a80ce6691e4769fa1cca97698f3e04acea8743))
* use slog.InfoContext in hooks for trace_id correlation ([#97](https://github.com/anatolykoptev/vaelor/issues/97)) ([f6e31aa](https://github.com/anatolykoptev/vaelor/commit/f6e31aafc51f57d14077ef37d1d5cc64a6d399e8))
* use structural search for prepare_change call_site_count ([c174be9](https://github.com/anatolykoptev/vaelor/commit/c174be96872c150dd64e6c5875308bfcd6a44c58))
* **vendor:** commit tree-sitter PHP cgo headers stripped by go mod vendor ([#17](https://github.com/anatolykoptev/vaelor/issues/17)) ([209811c](https://github.com/anatolykoptev/vaelor/commit/209811c4399b329a3d26a8c16c816c8b2654fbed))
* word-boundary guards for short symbol names in ox-codes scoped search ([25444f5](https://github.com/anatolykoptev/vaelor/commit/25444f5cabd6c7c844bed267328ac3a35fb5c5a3))


### Performance

* **ci:** -short merge gate + nightly full suite (26m -&gt; ~min) ([#301](https://github.com/anatolykoptev/vaelor/issues/301)) ([b04c379](https://github.com/anatolykoptev/vaelor/commit/b04c379a136491e5397e996240f77f5225e7348f))
* compact hand-built XML formatters + code_compare metrics json ([#261](https://github.com/anatolykoptev/vaelor/issues/261)) ([e5c3f22](https://github.com/anatolykoptev/vaelor/commit/e5c3f22d05b60e79ed8432575964bf78e42c2de4))
* **debug_investigate:** Sprint A — parallel Prom queries + skip LLM on quiet signal (6× speedup) ([#78](https://github.com/anatolykoptev/vaelor/issues/78)) ([e7e9ae9](https://github.com/anatolykoptev/vaelor/commit/e7e9ae9b90488a23cd97e8fc28b39c07e8523c6e))
* drop MCP response indentation + duration-only meta footer ([#260](https://github.com/anatolykoptev/vaelor/issues/260)) ([3ee5282](https://github.com/anatolykoptev/vaelor/commit/3ee5282769063fffa3daaf4600473df010f6692e))
* **go-code:** batch sparse-embedding writes + raise backfill deadline ([#200](https://github.com/anatolykoptev/vaelor/issues/200)) ([bdb72ab](https://github.com/anatolykoptev/vaelor/commit/bdb72ab88bf34876aa7b9aa73e62432f8576de15))
* **go-code:** Phase 2c — batch initialCreationLines (BUG-FH-2b cold latency 34s→~3s) ([#159](https://github.com/anatolykoptev/vaelor/issues/159)) ([cb8bf39](https://github.com/anatolykoptev/vaelor/commit/cb8bf39fdc7eb4f4630b4a4ce199a5b953aaa6a7))
* **ingest:** process-level IngestRepo cache to eliminate redundant walks ([#464](https://github.com/anatolykoptev/vaelor/issues/464)) ([#474](https://github.com/anatolykoptev/vaelor/issues/474)) ([7238702](https://github.com/anatolykoptev/vaelor/commit/7238702d1bf6d48d299fde71313234c2102ce683))
* optional go-kit/cache Redis L2 for ingestRepoCache and cgCache ([#493](https://github.com/anatolykoptev/vaelor/issues/493), [#494](https://github.com/anatolykoptev/vaelor/issues/494)) ([#498](https://github.com/anatolykoptev/vaelor/issues/498)) ([0afd748](https://github.com/anatolykoptev/vaelor/commit/0afd7488a29a153aaedbb8eae356ec7c6535a452))
* parallelize ox-codes dead code string ref checks (N serial → 10-concurrent) ([0e9f19f](https://github.com/anatolykoptev/vaelor/commit/0e9f19fa2db9180ecba1d5d1601a8c9e9626a5fa))
* **parser:** add BenchmarkParseFile and BenchmarkBuildSnapshot ([#404](https://github.com/anatolykoptev/vaelor/issues/404)) ([6461bf6](https://github.com/anatolykoptev/vaelor/commit/6461bf629a32ccfae76dd4f8f568614706351ed2))
* **parser:** share one tree between ParseFile and ExtractCalls ([#400](https://github.com/anatolykoptev/vaelor/issues/400)) ([#408](https://github.com/anatolykoptev/vaelor/issues/408)) ([eafcae6](https://github.com/anatolykoptev/vaelor/commit/eafcae6038adbf1bcb15864af64a1eb20c4739ce))
* **parser:** single-parse Svelte runes instead of double parse ([#406](https://github.com/anatolykoptev/vaelor/issues/406)) ([41970bd](https://github.com/anatolykoptev/vaelor/commit/41970bd58dc9c56e4b45c438dbb73e3d67d06911)), closes [#401](https://github.com/anatolykoptev/vaelor/issues/401)
* **review:** cap review_delta impacted_symbols by default ([#391](https://github.com/anatolykoptev/vaelor/issues/391)) ([#415](https://github.com/anatolykoptev/vaelor/issues/415)) ([29b28ca](https://github.com/anatolykoptev/vaelor/commit/29b28cac1c32db0349759269cb59134ffac52723))
* **scip:** parallelize multi-language SCIP indexing ([#465](https://github.com/anatolykoptev/vaelor/issues/465)) ([#471](https://github.com/anatolykoptev/vaelor/issues/471)) ([378f607](https://github.com/anatolykoptev/vaelor/commit/378f607aaf35de06effae9d70c489b4f994ecd9a))
* **test:** parallelize DB-free test packages (gate ~8m -&gt; ~3.2m) ([#302](https://github.com/anatolykoptev/vaelor/issues/302)) ([c5636f6](https://github.com/anatolykoptev/vaelor/commit/c5636f62a2b3b3d77cc18ad1d8c23bc7058ececc))


### Changed

* **age:** drop per-connection LOAD; rely on shared_preload_libraries with startup check ([#111](https://github.com/anatolykoptev/vaelor/issues/111)) ([e17b4fa](https://github.com/anatolykoptev/vaelor/commit/e17b4fa6eae5b88caf038d9055773c6a9f8b1874))
* **cache:** migrate ParseCache onto generic cache.LRU + per-cache tests + semhealth fixture ([cac9d1c](https://github.com/anatolykoptev/vaelor/commit/cac9d1c8a8d4d12d82b4e3bc7a87df97e9dee09a))
* **callgraph:** move extractGoImplements into EnrichWithTypedResolution ([#467](https://github.com/anatolykoptev/vaelor/issues/467)) ([#472](https://github.com/anatolykoptev/vaelor/issues/472)) ([902890b](https://github.com/anatolykoptev/vaelor/commit/902890b236d2f1ca4acc3da56969d98f45200d6c))
* **callgraph:** unified ingest→parse→build→enrich pipeline ([#463](https://github.com/anatolykoptev/vaelor/issues/463)) ([#475](https://github.com/anatolykoptev/vaelor/issues/475)) ([#478](https://github.com/anatolykoptev/vaelor/issues/478)) ([ae8e74e](https://github.com/anatolykoptev/vaelor/commit/ae8e74e23198e57f6d76f2eaecb20840767895cf))
* **clients:** migrate websearch/oxcodes onto httputil.Client ([#283](https://github.com/anatolykoptev/vaelor/issues/283)) ([b2e62e4](https://github.com/anatolykoptev/vaelor/commit/b2e62e4907ed172df8c916bd7c8cb737a81b7b93))
* **codegraph:** unify IMPLEMENTS edge paths — single construction via buildRelationshipEdges ([#461](https://github.com/anatolykoptev/vaelor/issues/461)) ([e48fa4e](https://github.com/anatolykoptev/vaelor/commit/e48fa4e93e7bd2e18c4b43709b8cb800871e8940))
* consolidate dominant-language argmax into one canonical helper ([#285](https://github.com/anatolykoptev/vaelor/issues/285)) ([22d9db3](https://github.com/anatolykoptev/vaelor/commit/22d9db31b65b5cd2df51e6c56445af04c2e792ed))
* **embeddings:** use go-kit cache.WithMetrics (v0.33.0 bump) ([#8](https://github.com/anatolykoptev/vaelor/issues/8)) ([3c55d28](https://github.com/anatolykoptev/vaelor/commit/3c55d28f7d119cf2b780f4381ed95a670c4f5350))
* **embed:** migrate to go-kit/embed v0.30.0 ([#2](https://github.com/anatolykoptev/vaelor/issues/2)) ([f76f95f](https://github.com/anatolykoptev/vaelor/commit/f76f95f1d2c87ca1e85b7b30325cf225ae7ed593))
* generic cache.LRU for 4 caches + dedup 3 helpers ([5be753e](https://github.com/anatolykoptev/vaelor/commit/5be753e0a3a710cebac7481109f35ed6392b97b5))
* **go-code:** decompose computeHealth into per-subscore helpers ([#183](https://github.com/anatolykoptev/vaelor/issues/183)) ([18227a9](https://github.com/anatolykoptev/vaelor/commit/18227a99308a8baafa1ca41089096ee7c4826bc3))
* **go-code:** decompose formatInvestigationResult into per-section writers ([#182](https://github.com/anatolykoptev/vaelor/issues/182)) ([d8c6834](https://github.com/anatolykoptev/vaelor/commit/d8c68345a097d254cbe6da12360513d01abe4153))
* **go-code:** decompose ScanHtmxRefs (cyclomatic 57→3) ([#204](https://github.com/anatolykoptev/vaelor/issues/204)) ([09606fd](https://github.com/anatolykoptev/vaelor/commit/09606fdc79d0a18c4d69d7909cd84315c329341a))
* **go-code:** dedup 3 copy-paste blocks (dupl → 0 repo-wide) ([#181](https://github.com/anatolykoptev/vaelor/issues/181)) ([111a5a4](https://github.com/anatolykoptev/vaelor/commit/111a5a42162d3699834f7084e295c8d51c9b4446))
* **go-code:** migrate error/no-match + design_search/semantic XML onto typed structs + xml.Marshal ([#263](https://github.com/anatolykoptev/vaelor/issues/263)) ([f1924c8](https://github.com/anatolykoptev/vaelor/commit/f1924c8d7f238bff7b2e8097c7effeb7fffdbe3d))
* **go-code:** migrate final 3 hand-rolled XML formatters onto xml.Marshal + collapse error/json clones ([#266](https://github.com/anatolykoptev/vaelor/issues/266)) ([7d805d5](https://github.com/anatolykoptev/vaelor/commit/7d805d591c20bd143e0378eabe33d7f47732eae5))
* **go-code:** migrate site_analyze/site_crawl/debug_investigate XML onto typed structs + xml.Marshal ([#262](https://github.com/anatolykoptev/vaelor/issues/262)) ([792d3bd](https://github.com/anatolykoptev/vaelor/commit/792d3bd9fededc7661b75b1e238cc4be11dc192a))
* **go-code:** split AGE/data connection pools + schema-qualification guards ([#178](https://github.com/anatolykoptev/vaelor/issues/178)) ([bdb55fd](https://github.com/anatolykoptev/vaelor/commit/bdb55fd8f9563cee66e34b5cd875b219b6a4d69a))
* **go-code:** unify 3 import resolvers into internal/importresolve ([#188](https://github.com/anatolykoptev/vaelor/issues/188)) ([437653a](https://github.com/anatolykoptev/vaelor/commit/437653af5b9e5364c879527e720e0d6a04783716))
* **go-code:** unify tokenization + stopwords into internal/lextoken leaf (BM25F P2) ([#205](https://github.com/anatolykoptev/vaelor/issues/205)) ([c7378da](https://github.com/anatolykoptev/vaelor/commit/c7378da07481a570bc85edc0254f7fd69c77be2e))
* **ingest:** unify parseFilesParallel into shared ingest.ParseFilesParallel ([#469](https://github.com/anatolykoptev/vaelor/issues/469)) ([#473](https://github.com/anatolykoptev/vaelor/issues/473)) ([b95d7e0](https://github.com/anatolykoptev/vaelor/commit/b95d7e00611c57382142847f3ec8a0a31824ec9c))
* **llm-obs:** swap direct-prom histogram for kit Registry.ObserveSeconds ([#122](https://github.com/anatolykoptev/vaelor/issues/122)) ([c4af302](https://github.com/anatolykoptev/vaelor/commit/c4af302d68e2904d242363e8c1045b21e5849c4d))
* migrate to go-kit/rerank.RRF (v0.32.0 bump) ([#3](https://github.com/anatolykoptev/vaelor/issues/3)) ([9f2b0f0](https://github.com/anatolykoptev/vaelor/commit/9f2b0f0c84226bbb73ce3443f93183c537265989))
* **pgutil:** extract TransferOwnership shared helper (DRY PR [#112](https://github.com/anatolykoptev/vaelor/issues/112)) ([#114](https://github.com/anatolykoptev/vaelor/issues/114)) ([2426783](https://github.com/anatolykoptev/vaelor/commit/24267836a5319cb340536b62b902c68aeedd95e1))
* remove unused buildSingleEdge function ([78614d8](https://github.com/anatolykoptev/vaelor/commit/78614d87e3a1c41a23370168084b29d981a55a84))
* rename Go module github.com/anatolykoptev/go-code -&gt; vaelor (Phase 1) ([#512](https://github.com/anatolykoptev/vaelor/issues/512)) ([ddc1419](https://github.com/anatolykoptev/vaelor/commit/ddc1419d194ff189e1f9b54a511ef99145abd407))
* **repo_analyze:** slim XML output without losing agent value ([#16](https://github.com/anatolykoptev/vaelor/issues/16)) ([e0237c2](https://github.com/anatolykoptev/vaelor/commit/e0237c2b4caf57eadd0eae3f232dd33860ff853c))
* **tools:** trim noise from symbol_search and explore output ([#20](https://github.com/anatolykoptev/vaelor/issues/20)) ([32f5091](https://github.com/anatolykoptev/vaelor/commit/32f509199efc07c30c843bcffd8360f312b77bb9))
* **xml:** close Tree xmlCDATA gap + sync assertNoEmptyTag godoc ([#32](https://github.com/anatolykoptev/vaelor/issues/32)) ([b10ca20](https://github.com/anatolykoptev/vaelor/commit/b10ca20a70700878a1537b4327ed6040f30cd044))
* **xml:** convert empty-prone xmlCDATA fields to pointer-form ([#25](https://github.com/anatolykoptev/vaelor/issues/25)) ([387748d](https://github.com/anatolykoptev/vaelor/commit/387748d458a31cca6f10669d024dbd9fd7994f75))


### Documentation

* actualize README — 30 tools, expand Tools table to 27 rows ([9832942](https://github.com/anatolykoptev/vaelor/commit/9832942a202722960141b98fecb2c59fd7a9eeed))
* actualize README — 30 tools, expand Tools table to 27 rows ([759373b](https://github.com/anatolykoptev/vaelor/commit/759373bcf783c60ce8916ff6ec1c4bf370485b6f))
* add hero demo GIF to README ([#487](https://github.com/anatolykoptev/vaelor/issues/487)) ([167c54c](https://github.com/anatolykoptev/vaelor/commit/167c54cca0c12a0f7ea873132417e201e15f7b42))
* **adr:** 0002 environment detect & verify ([#297](https://github.com/anatolykoptev/vaelor/issues/297)) ([9636d75](https://github.com/anatolykoptev/vaelor/commit/9636d75661f6e96f533984c0351a9164b7f18bc3))
* **adr:** 0002 harden Phase 1 resolution per re-review ([#299](https://github.com/anatolykoptev/vaelor/issues/299)) ([ab5cb86](https://github.com/anatolykoptev/vaelor/commit/ab5cb860b7bc7269ff6700092fdb81e87d13d273))
* **adr:** 0002 Phase 1 design resolution — close 6 security-cost blockers (design-only) ([#298](https://github.com/anatolykoptev/vaelor/issues/298)) ([d22973d](https://github.com/anatolykoptev/vaelor/commit/d22973d720c5a08e823ad6c72cc421dac7342c04))
* **adr:** add 0003 callgraph resolver strategy ([#322](https://github.com/anatolykoptev/vaelor/issues/322)) ([55818b0](https://github.com/anatolykoptev/vaelor/commit/55818b014b351751f3a905f00bb4a17db47ef624))
* **CLAUDE:** language count 11 → 13 (added Svelte, Astro) ([#100](https://github.com/anatolykoptev/vaelor/issues/100)) ([0af6a01](https://github.com/anatolykoptev/vaelor/commit/0af6a014ad86bacdfc1ce847c7d96e755335b4a7))
* **debug_investigate:** align hint_kind count with code ([#328](https://github.com/anatolykoptev/vaelor/issues/328)) ([960bdec](https://github.com/anatolykoptev/vaelor/commit/960bdec9c72961e88035a44da886fc280eb4f0d6))
* fix v1.21 roadmap conflict + sync CLAUDE.md parser language count ([#103](https://github.com/anatolykoptev/vaelor/issues/103)) ([374f4c1](https://github.com/anatolykoptev/vaelor/commit/374f4c116ec52116c2d2769b68ad7c28385c7dda))
* **followups:** record fleet-wide codegraph route-&gt;graph breakage (FU-CG.1-6) [no-deploy] ([#166](https://github.com/anatolykoptev/vaelor/issues/166)) ([d9e5895](https://github.com/anatolykoptev/vaelor/commit/d9e589507ab663ab523d950ba240d2dd6a7e81df))
* **memos:** mark astro-template-refs memo as implemented ([#240](https://github.com/anatolykoptev/vaelor/issues/240)) ([b9690ff](https://github.com/anatolykoptev/vaelor/commit/b9690ffa59ec71d1027a773b2de5f4860166ebc8))
* **migration:** record as-run hardened ag_catalog backfill (executed 2026-05-31) ([#175](https://github.com/anatolykoptev/vaelor/issues/175)) ([2955b55](https://github.com/anatolykoptev/vaelor/commit/2955b55a99e8382315a3c9893796fb910f35e05b))
* phase 1 repowise smoke test findings [no-deploy] ([96947b3](https://github.com/anatolykoptev/vaelor/commit/96947b30e4e0e814ed57aa940aa268b0f7f78ee9))
* phase 2b smoke verified + BUG-FH-2b cold-latency followup [no-deploy] ([71b93ae](https://github.com/anatolykoptev/vaelor/commit/71b93ae9be9cb5ba6d2937439ab922bf1a45fe21))
* **plan:** mark Phase 1 complete (Tasks 1-4) ([#50](https://github.com/anatolykoptev/vaelor/issues/50)) ([9ae6b3d](https://github.com/anatolykoptev/vaelor/commit/9ae6b3d02e457cbb86ca9c49ef5bb32299e027d7))
* **plan:** mark Phase 2 complete (Tasks 5-8) ([#55](https://github.com/anatolykoptev/vaelor/issues/55)) ([b437f79](https://github.com/anatolykoptev/vaelor/commit/b437f79c0c670ae15238e6460bde0e4925ed5ec5))
* re-record hero demo on a production symbol + fix the Try-it command ([#489](https://github.com/anatolykoptev/vaelor/issues/489)) ([44c0194](https://github.com/anatolykoptev/vaelor/commit/44c01942633e26df8d7db691babcd78aac398cd8))
* reconcile CLAUDE.md + README with source (tools 25→37, +Vue, LLM_MODEL default) ([#485](https://github.com/anatolykoptev/vaelor/issues/485)) ([0221e32](https://github.com/anatolykoptev/vaelor/commit/0221e326eb19fbc8194292e72bbf50ae40b277c3))
* replace Mac home paths with generic placeholder ([aa7f994](https://github.com/anatolykoptev/vaelor/commit/aa7f9947d6f15e981419b648f71b0e6f075db0b1))
* rewrite README for launch (capability-led, source-verified claims) ([#482](https://github.com/anatolykoptev/vaelor/issues/482)) ([b75bc63](https://github.com/anatolykoptev/vaelor/commit/b75bc63d7780d3bc756e7279e5132dd4b1a1baba))
* **ROADMAP:** add v1.21 — OTel Function Attribution shipped 2026-05-09 ([#101](https://github.com/anatolykoptev/vaelor/issues/101)) ([2cc071f](https://github.com/anatolykoptev/vaelor/commit/2cc071f2cc04b82e6edb45dda9a1cf838786a8b2))

## [1.40.0](https://github.com/anatolykoptev/vaelor/compare/v1.39.2...v1.40.0) (2026-07-18)


### Added

* dual-read VAELOR_/GO_CODE_ env vars (rebrand) [no-deploy] ([8cd3b86](https://github.com/anatolykoptev/vaelor/commit/8cd3b860e86a5b0b27b7db337a38bad41ee98847))

## [1.39.2](https://github.com/anatolykoptev/go-code/compare/v1.39.1...v1.39.2) (2026-07-18)


### Changed

* rename Go module github.com/anatolykoptev/go-code -&gt; vaelor (Phase 1) ([#512](https://github.com/anatolykoptev/go-code/issues/512)) ([b49089f](https://github.com/anatolykoptev/go-code/commit/b49089fb9d9d1749932175f5a3294da92728a945))

## [1.39.1](https://github.com/anatolykoptev/go-code/compare/v1.39.0...v1.39.1) (2026-07-17)


### Fixed

* caller_kind accuracy — IsTestFile gate + unresolved bucket ([#507](https://github.com/anatolykoptev/go-code/issues/507)) ([#510](https://github.com/anatolykoptev/go-code/issues/510)) ([406fe91](https://github.com/anatolykoptev/go-code/commit/406fe91b890fb2aec936b91835462a43daf1babb))

## [1.39.0](https://github.com/anatolykoptev/go-code/compare/v1.38.7...v1.39.0) (2026-07-17)


### Added

* annotate understand/call_trace callers with production/test kind ([#491](https://github.com/anatolykoptev/go-code/issues/491)) ([#508](https://github.com/anatolykoptev/go-code/issues/508)) ([e5bf97d](https://github.com/anatolykoptev/go-code/commit/e5bf97d87d00a8ece72558964a84a0008bba50f5))

## [1.38.7](https://github.com/anatolykoptev/go-code/compare/v1.38.6...v1.38.7) (2026-07-17)


### Fixed

* content-hash staleness guard for cgCache L2 ([#497](https://github.com/anatolykoptev/go-code/issues/497)) ([#504](https://github.com/anatolykoptev/go-code/issues/504)) ([218f7eb](https://github.com/anatolykoptev/go-code/commit/218f7eb34a6ced73f5469f529a3c47ffa343a51c))

## [1.38.6](https://github.com/anatolykoptev/go-code/compare/v1.38.5...v1.38.6) (2026-07-17)


### Fixed

* async lazy-build for understand/call_trace cold-start ([#490](https://github.com/anatolykoptev/go-code/issues/490)) ([#501](https://github.com/anatolykoptev/go-code/issues/501)) ([3907d43](https://github.com/anatolykoptev/go-code/commit/3907d43fe7f8a8b4d2c1cee042fcae8a748a2845))

## [1.38.5](https://github.com/anatolykoptev/go-code/compare/v1.38.4...v1.38.5) (2026-07-17)


### Fixed

* retry-safe, lock-safe embeddings & designmd schema init ([#495](https://github.com/anatolykoptev/go-code/issues/495), [#496](https://github.com/anatolykoptev/go-code/issues/496)) ([#499](https://github.com/anatolykoptev/go-code/issues/499)) ([0b47569](https://github.com/anatolykoptev/go-code/commit/0b475693a3c4b2e5349b0db3a7cf83d74ec054c8))


### Performance

* optional go-kit/cache Redis L2 for ingestRepoCache and cgCache ([#493](https://github.com/anatolykoptev/go-code/issues/493), [#494](https://github.com/anatolykoptev/go-code/issues/494)) ([#498](https://github.com/anatolykoptev/go-code/issues/498)) ([5616866](https://github.com/anatolykoptev/go-code/commit/56168663d99f03e8732c3cc172ed32db46072b74))

## [1.38.4](https://github.com/anatolykoptev/go-code/compare/v1.38.3...v1.38.4) (2026-07-17)


### Documentation

* re-record hero demo on a production symbol + fix the Try-it command ([#489](https://github.com/anatolykoptev/go-code/issues/489)) ([1477fc8](https://github.com/anatolykoptev/go-code/commit/1477fc8d317e8ee788ef267ce93957e674031456))

## [1.38.3](https://github.com/anatolykoptev/go-code/compare/v1.38.2...v1.38.3) (2026-07-17)


### Documentation

* add hero demo GIF to README ([#487](https://github.com/anatolykoptev/go-code/issues/487)) ([c120bd1](https://github.com/anatolykoptev/go-code/commit/c120bd1063aa3c1c1f0c1759396f2a0999a003a9))

## [1.38.2](https://github.com/anatolykoptev/go-code/compare/v1.38.1...v1.38.2) (2026-07-17)


### Documentation

* reconcile CLAUDE.md + README with source (tools 25→37, +Vue, LLM_MODEL default) ([#485](https://github.com/anatolykoptev/go-code/issues/485)) ([84e04fc](https://github.com/anatolykoptev/go-code/commit/84e04fccc05e6c5cff29a07a13f28885bf98bbba))

## [1.38.1](https://github.com/anatolykoptev/go-code/compare/v1.38.0...v1.38.1) (2026-07-17)


### Documentation

* rewrite README for launch (capability-led, source-verified claims) ([#482](https://github.com/anatolykoptev/go-code/issues/482)) ([f47d66b](https://github.com/anatolykoptev/go-code/commit/f47d66b5dedcb022a8decdc3f8c84a5f089fd2da))

## [1.38.0](https://github.com/anatolykoptev/go-code/compare/v1.37.2...v1.38.0) (2026-07-17)


Maintenance release ahead of the public launch: genericized example service references across docs and tests. No functional or behavioral changes.


## [1.37.2](https://github.com/anatolykoptev/go-code/compare/v1.37.1...v1.37.2) (2026-07-16)


### Changed

* **callgraph:** unified ingest→parse→build→enrich pipeline ([#463](https://github.com/anatolykoptev/go-code/issues/463)) ([#475](https://github.com/anatolykoptev/go-code/issues/475)) ([#478](https://github.com/anatolykoptev/go-code/issues/478)) ([2844f0f](https://github.com/anatolykoptev/go-code/commit/2844f0fe37104eb4007cc1fdcb63f2b0b541abcc))

## [1.37.1](https://github.com/anatolykoptev/go-code/compare/v1.37.0...v1.37.1) (2026-07-16)


### Fixed

* **callgraph:** apply stdlib filter to tree-sitter path, not just SCIP ([#466](https://github.com/anatolykoptev/go-code/issues/466)) ([#470](https://github.com/anatolykoptev/go-code/issues/470)) ([72984c1](https://github.com/anatolykoptev/go-code/commit/72984c183e02c3c0147dd9b9b084d11d0803284d))
* **ingest:** defensive copy in IngestRepo cache to prevent aliasing ([#477](https://github.com/anatolykoptev/go-code/issues/477)) ([9e3378f](https://github.com/anatolykoptev/go-code/commit/9e3378faff0887ec6955d0971a8c441624689aab))


### Performance

* **ingest:** process-level IngestRepo cache to eliminate redundant walks ([#464](https://github.com/anatolykoptev/go-code/issues/464)) ([#474](https://github.com/anatolykoptev/go-code/issues/474)) ([1256d34](https://github.com/anatolykoptev/go-code/commit/1256d34ab2ee9b6e443b3955aa7ac247924528c2))
* **scip:** parallelize multi-language SCIP indexing ([#465](https://github.com/anatolykoptev/go-code/issues/465)) ([#471](https://github.com/anatolykoptev/go-code/issues/471)) ([03cad27](https://github.com/anatolykoptev/go-code/commit/03cad27d5bb472b3c17cb90a279e9565bd0a6c53))


### Changed

* **callgraph:** move extractGoImplements into EnrichWithTypedResolution ([#467](https://github.com/anatolykoptev/go-code/issues/467)) ([#472](https://github.com/anatolykoptev/go-code/issues/472)) ([896f4b7](https://github.com/anatolykoptev/go-code/commit/896f4b7817db3d2903aa255e9028c5cde06bd715))
* **ingest:** unify parseFilesParallel into shared ingest.ParseFilesParallel ([#469](https://github.com/anatolykoptev/go-code/issues/469)) ([#473](https://github.com/anatolykoptev/go-code/issues/473)) ([0f06050](https://github.com/anatolykoptev/go-code/commit/0f060506f86b7870ae23974d8daa1f624e3f1344))

## [1.37.0](https://github.com/anatolykoptev/go-code/compare/v1.36.0...v1.37.0) (2026-07-16)


### Added

* **scip:** run SCIP indexers for ALL detected languages, not just dominant ([#459](https://github.com/anatolykoptev/go-code/issues/459)) ([749f236](https://github.com/anatolykoptev/go-code/commit/749f2368c92e8a0fc3ade6282a9d827c72aa1bc5))


### Changed

* **codegraph:** unify IMPLEMENTS edge paths — single construction via buildRelationshipEdges ([#461](https://github.com/anatolykoptev/go-code/issues/461)) ([d708e33](https://github.com/anatolykoptev/go-code/commit/d708e33d3aaf644da3f3bbc051b4fec57e924af5))

## [1.36.0](https://github.com/anatolykoptev/go-code/compare/v1.35.2...v1.36.0) (2026-07-16)


### Added

* **call_trace:** add refresh parameter to bypass in-memory cache ([#457](https://github.com/anatolykoptev/go-code/issues/457)) ([bc916a4](https://github.com/anatolykoptev/go-code/commit/bc916a47430ff31c83ecc2b2423eb19915467909))
* **scip:** filter stdlib method calls from SCIP edges to reduce call_trace noise ([#456](https://github.com/anatolykoptev/go-code/issues/456)) ([99feb08](https://github.com/anatolykoptev/go-code/commit/99feb0839e52e922129cbc67cbec797feaca0c85))


### Fixed

* **scip:** use content hash instead of mtimes for CacheKey — no false misses on git checkout ([#458](https://github.com/anatolykoptev/go-code/issues/458)) ([5417846](https://github.com/anatolykoptev/go-code/commit/54178469dd2302a6444d0771a541eff9001f1a03))

## [1.35.2](https://github.com/anatolykoptev/go-code/compare/v1.35.1...v1.35.2) (2026-07-16)


### Fixed

* **codegraph:** remove HasGoModule gate from buildAGECallGraph — enable SCIP for Rust ([#449](https://github.com/anatolykoptev/go-code/issues/449)) ([daeb2b0](https://github.com/anatolykoptev/go-code/commit/daeb2b014bca971e50e9ed3c63dcf57fb0b1da49))

## [1.35.1](https://github.com/anatolykoptev/go-code/compare/v1.35.0...v1.35.1) (2026-07-16)


### Fixed

* **codegraph:** emit IMPLEMENTS edge label for IsInterface call edges ([#447](https://github.com/anatolykoptev/go-code/issues/447)) ([1ab0d54](https://github.com/anatolykoptev/go-code/commit/1ab0d5439471261e5dae0d3288d985d67369b567))

## [1.35.0](https://github.com/anatolykoptev/go-code/compare/v1.34.1...v1.35.0) (2026-07-16)


### Added

* **scip:** extract IMPLEMENTS edges from Rust SCIP index — trait impl discovery ([#445](https://github.com/anatolykoptev/go-code/issues/445)) ([b839988](https://github.com/anatolykoptev/go-code/commit/b8399887a384efda16140d7180545096678339e8))

## [1.34.1](https://github.com/anatolykoptev/go-code/compare/v1.34.0...v1.34.1) (2026-07-16)


### Fixed

* **scip:** wire Cache into trySCIPResolution — skip re-indexing on cache hit ([#443](https://github.com/anatolykoptev/go-code/issues/443)) ([3b07816](https://github.com/anatolykoptev/go-code/commit/3b07816bca3c527d6229339c3c3a8f48fa01cf73))

## [1.34.0](https://github.com/anatolykoptev/go-code/compare/v1.33.1...v1.34.0) (2026-07-16)


### Added

* **oxcodes:** custom taint rules, anti-patterns, rewrite rejections, cache metrics ([#438](https://github.com/anatolykoptev/go-code/issues/438)) ([bfc5104](https://github.com/anatolykoptev/go-code/commit/bfc510452298fcec3e8a47ba4b7079993fc6335d))

## [1.33.1](https://github.com/anatolykoptev/go-code/compare/v1.33.0...v1.33.1) (2026-07-16)


### Fixed

* **call_trace:** rewrite TraceFromAGE with iterative BFS (AGE lacks list comprehension) ([#436](https://github.com/anatolykoptev/go-code/issues/436)) ([580da03](https://github.com/anatolykoptev/go-code/commit/580da03d3e1dce4c853a86a2f65e8cd1b4caa839))

## [1.33.0](https://github.com/anatolykoptev/go-code/compare/v1.32.3...v1.33.0) (2026-07-16)


### Added

* **call_trace:** fast path from AGE graph — avoid 2-60s repo reparse ([#434](https://github.com/anatolykoptev/go-code/issues/434)) ([e842112](https://github.com/anatolykoptev/go-code/commit/e842112f09098808ce4c5e23ca12987ad831ea34))

## [1.32.3](https://github.com/anatolykoptev/go-code/compare/v1.32.2...v1.32.3) (2026-07-16)


### Fixed

* **mcp:** raise code_graph timeout + non-blocking narrative + branch cleanup ([#433](https://github.com/anatolykoptev/go-code/issues/433)) ([2001cc6](https://github.com/anatolykoptev/go-code/commit/2001cc6dd85f41aecf6f96b4e9d54cabc9a83bc0))

## [1.32.2](https://github.com/anatolykoptev/go-code/compare/v1.32.1...v1.32.2) (2026-07-16)


### Fixed

* **codegraph:** prune stale dead-code scores when a function stops being an orphan ([#295](https://github.com/anatolykoptev/go-code/issues/295)) ([8d8c88b](https://github.com/anatolykoptev/go-code/commit/8d8c88b44120f0e0e791365faddcc1e69752a58d))
* **compare:** reuse tree-sitter parser per worker in BuildSnapshot ([#384](https://github.com/anatolykoptev/go-code/issues/384)) ([cc9d35d](https://github.com/anatolykoptev/go-code/commit/cc9d35dafa5eb5c45d50b912a535e3a2393895a2))

## [1.32.1](https://github.com/anatolykoptev/go-code/compare/v1.32.0...v1.32.1) (2026-07-16)


### Fixed

* **codegraph:** memory guard + chunked COPY to prevent OOM kernel panic ([#428](https://github.com/anatolykoptev/go-code/issues/428)) ([#429](https://github.com/anatolykoptev/go-code/issues/429)) ([9e188ad](https://github.com/anatolykoptev/go-code/commit/9e188ad3c947675279c58cdd9f5fd9a2accc1921))

## [1.32.0](https://github.com/anatolykoptev/go-code/compare/v1.31.11...v1.32.0) (2026-07-16)


### Added

* **importresolve:** stopgap virtual:* module resolution to defining package ([#423](https://github.com/anatolykoptev/go-code/issues/423)) ([#425](https://github.com/anatolykoptev/go-code/issues/425)) ([f24aff8](https://github.com/anatolykoptev/go-code/commit/f24aff86c6f543434617e179a9bae5975a92e864))

## [1.31.11](https://github.com/anatolykoptev/go-code/compare/v1.31.10...v1.31.11) (2026-07-16)


### Fixed

* **importresolve:** honor package.json exports map for workspace subpath imports ([#422](https://github.com/anatolykoptev/go-code/issues/422)) ([#424](https://github.com/anatolykoptev/go-code/issues/424)) ([0cff3a6](https://github.com/anatolykoptev/go-code/commit/0cff3a6ff83ca14387e0c85eaea9da3405964c7d))

## [1.31.10](https://github.com/anatolykoptev/go-code/compare/v1.31.9...v1.31.10) (2026-07-15)


### Fixed

* **review:** use valid ox-codes scope "function_bodies" in review_delta ([#420](https://github.com/anatolykoptev/go-code/issues/420)) ([cb7ea2c](https://github.com/anatolykoptev/go-code/commit/cb7ea2cc32f185052f1eb8c78d9a0dcdeb34d094)), closes [#419](https://github.com/anatolykoptev/go-code/issues/419)

## [1.31.9](https://github.com/anatolykoptev/go-code/compare/v1.31.8...v1.31.9) (2026-07-15)


### Fixed

* serialize EnsureGraph provisioning to fix pg_type 23505 race ([#417](https://github.com/anatolykoptev/go-code/issues/417)) ([1fbf8a4](https://github.com/anatolykoptev/go-code/commit/1fbf8a48631ecfe117421fdbfb9e15dd71a04028))

## [1.31.8](https://github.com/anatolykoptev/go-code/compare/v1.31.7...v1.31.8) (2026-07-14)


### Fixed

* **parser:** route Vue call extraction through the two-region ScriptCalls/MarkupCalls split ([#409](https://github.com/anatolykoptev/go-code/issues/409)) ([#414](https://github.com/anatolykoptev/go-code/issues/414)) ([7276002](https://github.com/anatolykoptev/go-code/commit/727600228c934f2286b2f3071018f958034d397d))


### Performance

* **review:** cap review_delta impacted_symbols by default ([#391](https://github.com/anatolykoptev/go-code/issues/391)) ([#415](https://github.com/anatolykoptev/go-code/issues/415)) ([600e5e0](https://github.com/anatolykoptev/go-code/commit/600e5e0cc0588fd79b15089a54e4ae738505e95d))

## [1.31.7](https://github.com/anatolykoptev/go-code/compare/v1.31.6...v1.31.7) (2026-07-14)


### Fixed

* sane fresh-deploy defaults for LLM model and /resolve rate limit ([#412](https://github.com/anatolykoptev/go-code/issues/412)) ([cba14fb](https://github.com/anatolykoptev/go-code/commit/cba14fb842451dcd73e9ce96ece7481345a283cf))

## [1.31.6](https://github.com/anatolykoptev/go-code/compare/v1.31.5...v1.31.6) (2026-07-14)


### Performance

* **parser:** share one tree between ParseFile and ExtractCalls ([#400](https://github.com/anatolykoptev/go-code/issues/400)) ([#408](https://github.com/anatolykoptev/go-code/issues/408)) ([1331695](https://github.com/anatolykoptev/go-code/commit/13316954ea28ca29b2191fdea9244633c804e900))

## [1.31.5](https://github.com/anatolykoptev/go-code/compare/v1.31.4...v1.31.5) (2026-07-14)


### Performance

* **parser:** add BenchmarkParseFile and BenchmarkBuildSnapshot ([#404](https://github.com/anatolykoptev/go-code/issues/404)) ([9b41631](https://github.com/anatolykoptev/go-code/commit/9b41631b646ab9b1c6993a70c8ad9539e5343b97))
* **parser:** single-parse Svelte runes instead of double parse ([#406](https://github.com/anatolykoptev/go-code/issues/406)) ([f453d47](https://github.com/anatolykoptev/go-code/commit/f453d475c4ab50ee4f339f1ffa01bf767c3557fd)), closes [#401](https://github.com/anatolykoptev/go-code/issues/401)

## [1.31.4](https://github.com/anatolykoptev/go-code/compare/v1.31.3...v1.31.4) (2026-07-14)


### Fixed

* shrink code_compare LLM prompt to fit 8k-token fleet models ([#398](https://github.com/anatolykoptev/go-code/issues/398)) ([5f74acd](https://github.com/anatolykoptev/go-code/commit/5f74acd664dd71ee35c540f9857eab3afe65d024))

## [1.31.3](https://github.com/anatolykoptev/go-code/compare/v1.31.2...v1.31.3) (2026-07-14)


### Fixed

* **llm:** default per-attempt timeout for chain rotation + review_delta 120s ([#391](https://github.com/anatolykoptev/go-code/issues/391)) ([#395](https://github.com/anatolykoptev/go-code/issues/395)) ([c65210c](https://github.com/anatolykoptev/go-code/commit/c65210c34b9f3a1d7b39df36ffdc15c756a85445))

## [1.31.2](https://github.com/anatolykoptev/go-code/compare/v1.31.1...v1.31.2) (2026-07-14)


### Fixed

* **review:** correct untested-symbol false positives in review_delta ([#392](https://github.com/anatolykoptev/go-code/issues/392)) ([b414402](https://github.com/anatolykoptev/go-code/commit/b4144027c3a89cd26accbd522dea8a27410c2e73))

## [1.31.1](https://github.com/anatolykoptev/go-code/compare/v1.31.0...v1.31.1) (2026-07-14)


### Fixed

* **test:** make TestSignalHitsLiveIntegration self-contained (nightly green) ([#389](https://github.com/anatolykoptev/go-code/issues/389)) ([39b3bdb](https://github.com/anatolykoptev/go-code/commit/39b3bdb5fcdae190117205efd9c4c4fe44ccdcda))

## [1.31.0](https://github.com/anatolykoptev/go-code/compare/v1.30.1...v1.31.0) (2026-07-14)


### Added

* **github_code_search:** add max_fragment_chars and max_total_chars ([#383](https://github.com/anatolykoptev/go-code/issues/383)) ([50cad3d](https://github.com/anatolykoptev/go-code/commit/50cad3dfa1eed923b7abb304f9e6d52796d50d04))

## [1.30.1](https://github.com/anatolykoptev/go-code/compare/v1.30.0...v1.30.1) (2026-07-14)


### Fixed

* **go-code:** accept owner/repo form in github_code_search tool ([#381](https://github.com/anatolykoptev/go-code/issues/381)) ([4ae079a](https://github.com/anatolykoptev/go-code/commit/4ae079a821604a5e113ca84d1b26763474f7b1eb))

## [1.30.0](https://github.com/anatolykoptev/go-code/compare/v1.29.0...v1.30.0) (2026-07-14)


### Added

* **compare:** wire ParseCache through BuildSnapshot/CompareInput ([d36a72c](https://github.com/anatolykoptev/go-code/commit/d36a72c41566f6967ca32475fa1ed549d9198915))


### Fixed

* **compare:** dedupe self-compare snapshots + ParseCache integration ([01891bf](https://github.com/anatolykoptev/go-code/commit/01891bfb489c24e5c201d94dd82f8b4ba429cf2d))

## [1.29.0](https://github.com/anatolykoptev/go-code/compare/v1.28.1...v1.29.0) (2026-07-14)


### Added

* port github_code_search from go-search to go-code ([#377](https://github.com/anatolykoptev/go-code/issues/377)) ([b970624](https://github.com/anatolykoptev/go-code/commit/b970624b88d878ed8a775ef9dd63ab867a6ee938))

## [1.28.1](https://github.com/anatolykoptev/go-code/compare/v1.28.0...v1.28.1) (2026-07-14)


### Fixed

* **code_graph:** return building status instead of tool error ([#361](https://github.com/anatolykoptev/go-code/issues/361)) ([6de3bba](https://github.com/anatolykoptev/go-code/commit/6de3bba70a919694fe3815bb3d6765f6636d3d95))

## [1.28.0](https://github.com/anatolykoptev/go-code/compare/v1.27.0...v1.28.0) (2026-07-14)


### Added

* **semantic_search:** add code_graph hint to indexing status ([#359](https://github.com/anatolykoptev/go-code/issues/359)) ([2d20399](https://github.com/anatolykoptev/go-code/commit/2d203997651f2548163489349b124f52a03259f9))

## [1.27.0](https://github.com/anatolykoptev/go-code/compare/v1.26.3...v1.27.0) (2026-07-14)


### Added

* **embeddings:** enable graph, hotspot, and recency arms in semantic_search RRF ([8c228c9](https://github.com/anatolykoptev/go-code/commit/8c228c912862b8d7c9c614a174a3b8953f518a53))
* **go-code:** enable graph, hotspot, and recency RRF arms in semantic_search ([ee30f88](https://github.com/anatolykoptev/go-code/commit/ee30f88fea203832b2139988bb4b7d80e13d1811))


### Fixed

* **federate:** pass asOf time.Time to CrossRepoCoChange to avoid wall-clock git log --since ([9325d79](https://github.com/anatolykoptev/go-code/commit/9325d79a7402b784b7332a8c4384b7fdef1c0521))
* **semantic_search:** strip AGE agtype quotes from complexity values ([d0ba33d](https://github.com/anatolykoptev/go-code/commit/d0ba33d3f2a030a6f48ed63648c3d62ed7167c70))

## [1.26.3](https://github.com/anatolykoptev/go-code/compare/v1.26.2...v1.26.3) (2026-07-13)


### Fixed

* **federate:** FU-1.1 — thread request ctx into ResolveRepos for cancellable origin dedup ([#337](https://github.com/anatolykoptev/go-code/issues/337)) ([fc12d53](https://github.com/anatolykoptev/go-code/commit/fc12d53400912e6d4e4f37c37baaaff5633c22d1))

## [1.26.2](https://github.com/anatolykoptev/go-code/compare/v1.26.1...v1.26.2) (2026-07-13)


### Fixed

* **codegraph:** FU-CG.9 — make route edge counters truthful (built vs unmatched) ([#335](https://github.com/anatolykoptev/go-code/issues/335)) ([2cce1a5](https://github.com/anatolykoptev/go-code/commit/2cce1a531dbfd898ae0a731332d4c9c0af26b7db))

## [1.26.1](https://github.com/anatolykoptev/go-code/compare/v1.26.0...v1.26.1) (2026-07-13)


### Fixed

* **codegraph:** add side to side-blind Route MATCH queries (FU-CG.8) ([#333](https://github.com/anatolykoptev/go-code/issues/333)) ([4ee9f1f](https://github.com/anatolykoptev/go-code/commit/4ee9f1fa14040223231508d8d6742f8524da21e0))

## [1.26.0](https://github.com/anatolykoptev/go-code/compare/v1.25.0...v1.26.0) (2026-07-13)


### Added

* **routes:** consolidate lineAt helper and add Line capture to 5 matchers (FU-CG.7) ([#331](https://github.com/anatolykoptev/go-code/issues/331)) ([513e808](https://github.com/anatolykoptev/go-code/commit/513e8085460993e5670029e684b65d26f5aafccc))


### Documentation

* **debug_investigate:** align hint_kind count with code ([#328](https://github.com/anatolykoptev/go-code/issues/328)) ([982ef21](https://github.com/anatolykoptev/go-code/commit/982ef2192ede4aeab4c37080f6817d8b701112b0))

## [1.25.0](https://github.com/anatolykoptev/go-code/compare/v1.24.0...v1.25.0) (2026-07-13)


### Added

* **resolve:** per-IP rate limit for POST /resolve ([#326](https://github.com/anatolykoptev/go-code/issues/326)) ([97cc6ed](https://github.com/anatolykoptev/go-code/commit/97cc6ed390a76b596f34ff3f60c4234c66100095))

## [1.24.0](https://github.com/anatolykoptev/go-code/compare/v1.23.6...v1.24.0) (2026-07-13)


### Added

* **sourcemap:** make sourcemap max body size configurable ([#324](https://github.com/anatolykoptev/go-code/issues/324)) ([a1ebdaf](https://github.com/anatolykoptev/go-code/commit/a1ebdafe84a42185bc4b10bb95cce430772b0aba))

## [1.23.6](https://github.com/anatolykoptev/go-code/compare/v1.23.5...v1.23.6) (2026-07-13)


### Documentation

* **adr:** add 0003 callgraph resolver strategy ([#322](https://github.com/anatolykoptev/go-code/issues/322)) ([4a8976a](https://github.com/anatolykoptev/go-code/commit/4a8976a5e919594a08898e1d27b82943f1883907))

## [1.23.5](https://github.com/anatolykoptev/go-code/compare/v1.23.4...v1.23.5) (2026-07-13)


### Fixed

* **call_trace:** normalize direction values to callers/callees ([#320](https://github.com/anatolykoptev/go-code/issues/320)) ([fbc1c1f](https://github.com/anatolykoptev/go-code/commit/fbc1c1face793cb117d9a135a701ccc2ae0bc31c))

## [1.23.4](https://github.com/anatolykoptev/go-code/compare/v1.23.3...v1.23.4) (2026-07-13)


### Fixed

* **debug_investigate:** drop t.Skip and document %q/%s label choice ([#318](https://github.com/anatolykoptev/go-code/issues/318)) ([f37117f](https://github.com/anatolykoptev/go-code/commit/f37117fa39c5965856327b917d1026a9d3db347f))

## [1.23.3](https://github.com/anatolykoptev/go-code/compare/v1.23.2...v1.23.3) (2026-07-13)


### Fixed

* **clients:** stop allocating httputil.Client on every call ([#316](https://github.com/anatolykoptev/go-code/issues/316)) ([a8c168c](https://github.com/anatolykoptev/go-code/commit/a8c168cd3b0809e0c36b73a487a2bedb10ccfed0))

## [1.23.2](https://github.com/anatolykoptev/go-code/compare/v1.23.1...v1.23.2) (2026-07-13)


### Fixed

* **codegraph:** enable typed call enrichment by default ([#314](https://github.com/anatolykoptev/go-code/issues/314)) ([c425f89](https://github.com/anatolykoptev/go-code/commit/c425f895ce14e606692b87587125d0fb23eab4e0))

## [1.23.1](https://github.com/anatolykoptev/go-code/compare/v1.23.0...v1.23.1) (2026-07-13)


### Fixed

* **release-please:** guard auto-merge step when no release PR ([#311](https://github.com/anatolykoptev/go-code/issues/311)) ([6c355f0](https://github.com/anatolykoptev/go-code/commit/6c355f0846b6c8e84259877dcbaef85dc0a171db))

## [1.23.0](https://github.com/anatolykoptev/go-code/compare/v1.22.2...v1.23.0) (2026-07-13)


### Features

* **envdetect:** ADR 0002 Phase 0 — static build/test/install command detection ([#296](https://github.com/anatolykoptev/go-code/issues/296)) ([011eddd](https://github.com/anatolykoptev/go-code/commit/011eddd82299d7d3a0e4cdad42c33406cd9ba375))


### Bug Fixes

* **callgraph:** wire typed call-edge resolution into the AGE-graph path for dead-code accuracy (BUG A, gated default-off) ([328b7d2](https://github.com/anatolykoptev/go-code/commit/328b7d2923dc416bdeaebb5b40bfe833aa1dafde))
* **compare:** raise code_compare deadline from 90s to 3m ([#309](https://github.com/anatolykoptev/go-code/issues/309)) ([987559e](https://github.com/anatolykoptev/go-code/commit/987559e95738d1d951b2d049b1b0864890a2dc73))
* **forge:** deflake metrics_test.go counter delta assertions ([#308](https://github.com/anatolykoptev/go-code/issues/308)) ([05a906e](https://github.com/anatolykoptev/go-code/commit/05a906ecbc06db16a803ee119cfb76801ae86160))
* **ingest:** accept comma-separated focus keywords ([#305](https://github.com/anatolykoptev/go-code/issues/305)) ([7148b9f](https://github.com/anatolykoptev/go-code/commit/7148b9f13e62642746fcfc57d4f3c9f92499fb61))
* **metrics:** scope code-graph age gauge to AUTO_INDEX_DIRS repos ([#291](https://github.com/anatolykoptev/go-code/issues/291)) ([808976e](https://github.com/anatolykoptev/go-code/commit/808976e32975abe643d1d3d412f36b0b70dd6d55))


### Performance Improvements

* **ci:** -short merge gate + nightly full suite (26m -&gt; ~min) ([#301](https://github.com/anatolykoptev/go-code/issues/301)) ([deba927](https://github.com/anatolykoptev/go-code/commit/deba927b56db92e4438fb010ca05919afbd575bf))
* **test:** parallelize DB-free test packages (gate ~8m -&gt; ~3.2m) ([#302](https://github.com/anatolykoptev/go-code/issues/302)) ([0d47c6f](https://github.com/anatolykoptev/go-code/commit/0d47c6fc74afac50349518de0dd458eec587b7fb))

## [1.22.2](https://github.com/anatolykoptev/go-code/compare/v1.22.1...v1.22.2) (2026-07-02)


### Bug Fixes

* **callgraph:** resolve generic-function callers in package-level var initializers ([#280](https://github.com/anatolykoptev/go-code/issues/280)) ([599cf02](https://github.com/anatolykoptev/go-code/commit/599cf02b6d17f912ca53a12a31a56ec1ad87e95e))
* **deadcode:** language-aware exported check for non-IsPublic languages ([#281](https://github.com/anatolykoptev/go-code/issues/281)) ([157f9b9](https://github.com/anatolykoptev/go-code/commit/157f9b91b62a56684fceefaa7c8aa7cf0667d218))
* **metrics:** pre-register alert-facing series at boot (graph age, zero-embeddings) ([#287](https://github.com/anatolykoptev/go-code/issues/287)) ([6ae4294](https://github.com/anatolykoptev/go-code/commit/6ae42941e3659afb247c2129806c5c0f6e3b8984))
* ParseCache drops call sites and ignores includeBody on hit ([#286](https://github.com/anatolykoptev/go-code/issues/286)) ([52427a3](https://github.com/anatolykoptev/go-code/commit/52427a3c9df882b56f413e1fc3b5c3803f83b71f))
* **review:** route PR-post write path through the multi-forge registry ([#284](https://github.com/anatolykoptev/go-code/issues/284)) ([bf32802](https://github.com/anatolykoptev/go-code/commit/bf3280267cd96440a5fccb5acb83ba32dcd009c3))

## [1.22.1](https://github.com/anatolykoptev/go-code/compare/v1.22.0...v1.22.1) (2026-07-01)


### Bug Fixes

* **release:** amd64 CGO CC override + consolidate to one goreleaser config ([#277](https://github.com/anatolykoptev/go-code/issues/277)) ([b49b0b3](https://github.com/anatolykoptev/go-code/commit/b49b0b32505c202d15e82f8cabed2ae79f265240))

## [1.22.0](https://github.com/anatolykoptev/go-code/compare/v1.21.0...v1.22.0) (2026-07-01)


### Features

* **metrics:** code_health/code_graph build-failure counters + AGE staleness gauge ([f115569](https://github.com/anatolykoptev/go-code/commit/f115569195dbd0525178bb7f996ecc6ce8905d7b))
* **parser:** astro alias resolution + vue SFC handler ([#241](https://github.com/anatolykoptev/go-code/issues/241)) ([d7724cf](https://github.com/anatolykoptev/go-code/commit/d7724cfe6b640edcbbe289d1f246dd863cad5a2b))
* **parser:** Astro markup {expr} calls + refs via shared tsxLang reparse ([#269](https://github.com/anatolykoptev/go-code/issues/269)) ([b5777c5](https://github.com/anatolykoptev/go-code/commit/b5777c5eb8bb9d86d78ecd30683f0ec69b188449))
* **parser:** Svelte component composition — TemplateRefs, USES edges, destructured $props() ([#270](https://github.com/anatolykoptev/go-code/issues/270)) ([fcba841](https://github.com/anatolykoptev/go-code/commit/fcba841e073d76d1380ecfc7a51c378840e40f04))
* **parser:** Svelte template-expressions + control-flow-effective calls/refs ([#271](https://github.com/anatolykoptev/go-code/issues/271)) ([c4302db](https://github.com/anatolykoptev/go-code/commit/c4302db1f7bb01b24b515fa5d9210638d691542e))


### Bug Fixes

* **astro:** narrow alias-counter emit-gate to broken declared aliases ([#243](https://github.com/anatolykoptev/go-code/issues/243)) ([c42afd4](https://github.com/anatolykoptev/go-code/commit/c42afd48669b11ae11d08425e3a4cdbf537dedfe))
* **code_health:** stop deleting a remote clone while the background snapshot is still reading it ([#246](https://github.com/anatolykoptev/go-code/issues/246)) ([02e0357](https://github.com/anatolykoptev/go-code/commit/02e035787d632d9c94d57a477404c77f8d539913))
* **compare,codegraph:** code_compare grade reflects freshness + language-aware isExported; [#253](https://github.com/anatolykoptev/go-code/issues/253) cleanup ([9e2c05f](https://github.com/anatolykoptev/go-code/commit/9e2c05f247f5433fcc6b48c0af82ae48b9fbc98d))
* **compare:** deterministic cycle-pair order in find2Cycles (flaky test) ([#272](https://github.com/anatolykoptev/go-code/issues/272)) ([dafcecf](https://github.com/anatolykoptev/go-code/commit/dafcecf1609ce270a9e3c88286ca77dc54e2c88c))
* **compare:** treat zero-dependency repos as N/A for freshness+vuln scoring ([2763dcd](https://github.com/anatolykoptev/go-code/commit/2763dcd3912b8c5badc5a665b22de351698cbb8b))
* **compare:** treat zero-dependency repos as N/A in code_compare grade (match code_health/[#250](https://github.com/anatolykoptev/go-code/issues/250)) ([aa6c42c](https://github.com/anatolykoptev/go-code/commit/aa6c42c8c4173f69db168ca183b8efb2c5895e13))
* **complexity:** unify cyclomatic complexity on parser as single owner ([39e89bb](https://github.com/anatolykoptev/go-code/commit/39e89bb6ccb0ae2869846760b7f70f6ec20b6879))
* **embeddings:** delete only true orphans (positive IN-list), not per-chunk anti-join ([728fe3c](https://github.com/anatolykoptev/go-code/commit/728fe3c062cccebef79b89c18bf46ea3b65950e4))
* **embeddings:** NUL-separate in-memory symbol keys (colon-in-path safe) + document dedup lossiness ([f20afcc](https://github.com/anatolykoptev/go-code/commit/f20afcc5330a8dbc38997db8e919f754fbe65753))
* **explore:** label health score as approximate with hint ([#249](https://github.com/anatolykoptev/go-code/issues/249)) ([7d6137a](https://github.com/anatolykoptev/go-code/commit/7d6137a3e5380ad97bc36d53b8a53e492aadf449))
* **mcp:** return tool results as application/json, not single-line SSE ([#245](https://github.com/anatolykoptev/go-code/issues/245)) ([7c9da8b](https://github.com/anatolykoptev/go-code/commit/7c9da8b0e0ef167c9ebb8f925e18a3e8255dd489))
* **metrics:** add per-symbol cognitive complexity and fix JS docRatio ([#247](https://github.com/anatolykoptev/go-code/issues/247)) ([4099691](https://github.com/anatolykoptev/go-code/commit/40996914774b232cf3cbd773376d4a316a1aad2a))
* **metrics:** unify health score and add arch fallback for unindexed repos ([#248](https://github.com/anatolykoptev/go-code/issues/248)) ([6db6ef2](https://github.com/anatolykoptev/go-code/commit/6db6ef2fdf413651996c386235ca93729a1a6070))
* **parser:** JS/TS-family Symbol.Language parity — .jsx/.js/.mjs/.cjs emit javascript ([#268](https://github.com/anatolykoptev/go-code/issues/268)) ([b758db4](https://github.com/anatolykoptev/go-code/commit/b758db41af3ae39c415e39823b7514fd0fe5c7c5))
* transfer table ownership on learnings + designmd store init ([#265](https://github.com/anatolykoptev/go-code/issues/265)) ([7768df5](https://github.com/anatolykoptev/go-code/commit/7768df5726d625db7e0f7350ee5eb240d10448da))


### Performance Improvements

* compact hand-built XML formatters + code_compare metrics json ([#261](https://github.com/anatolykoptev/go-code/issues/261)) ([4c8d2a2](https://github.com/anatolykoptev/go-code/commit/4c8d2a27abec9596a88bbaeccd069ee7c22a5e53))
* drop MCP response indentation + duration-only meta footer ([#260](https://github.com/anatolykoptev/go-code/issues/260)) ([e76ae8b](https://github.com/anatolykoptev/go-code/commit/e76ae8b0f286b65b4191aeb8d053dcc198c5aed4))

## [1.21.0](https://github.com/anatolykoptev/go-code/compare/v1.20.0...v1.21.0) (2026-06-20)


### Features

* **codegraph:** populate Go IMPLEMENTS edges via go/types satisfaction ([#220](https://github.com/anatolykoptev/go-code/issues/220)) ([beaa484](https://github.com/anatolykoptev/go-code/commit/beaa484edd59202b0f4b0394c9aff3a718131629))
* **embeddings:** cut model from jina-code-v2 to code-rank-embed ([#231](https://github.com/anatolykoptev/go-code/issues/231)) ([eceed51](https://github.com/anatolykoptev/go-code/commit/eceed511dc56b69fe9e483f3bc899310780693e6))
* **embeddings:** gocode_repo_info gauge — resolve opaque repo hash to path ([#227](https://github.com/anatolykoptev/go-code/issues/227)) ([bc67080](https://github.com/anatolykoptev/go-code/commit/bc670806af73c40ef44ae4b9323897ab3e7a15e0))
* **federate:** deadline-bounded federated_cochange with partial results + background prep ([#171](https://github.com/anatolykoptev/go-code/issues/171)) ([33f0e3d](https://github.com/anatolykoptev/go-code/commit/33f0e3da190410f61051ac41c549a113da99454e))
* find_duplicates — intra-repo semantic clone detector (5 phases) ([#215](https://github.com/anatolykoptev/go-code/issues/215)) ([8d68938](https://github.com/anatolykoptev/go-code/commit/8d68938f5fdf790258efebdbb324e0e309c7cc17))
* **go-code:** add nullable sparse_embedding sparsevec column (SPLADE P1) ([#194](https://github.com/anatolykoptev/go-code/issues/194)) ([ce90fde](https://github.com/anatolykoptev/go-code/commit/ce90fde117d87caa8b14aa6529b44aa7f4af82af))
* **go-code:** binary stale-demote safety-net for missed orphans (defense-in-depth) ([#210](https://github.com/anatolykoptev/go-code/issues/210)) ([bb2db6e](https://github.com/anatolykoptev/go-code/commit/bb2db6e817a3fa698731377f355b26eb95c020d5))
* **go-code:** BM25F lexical search arm over trigram candidates (BM25F P3) ([#206](https://github.com/anatolykoptev/go-code/issues/206)) ([a1c8c8e](https://github.com/anatolykoptev/go-code/commit/a1c8c8e78e5b876e36287a85e023da268488cd07))
* **go-code:** flag-gated BM25F keyword arm with grep fallback (BM25F P4) ([#207](https://github.com/anatolykoptev/go-code/issues/207)) ([7e2480f](https://github.com/anatolykoptev/go-code/commit/7e2480f7e41defca79d533b937ea97fd45167cb8))
* **go-code:** gated SPLADE sparse-vector indexing, batched by server cap (SPLADE P2) ([#195](https://github.com/anatolykoptev/go-code/issues/195)) ([44f9e7f](https://github.com/anatolykoptev/go-code/commit/44f9e7f0ea3e3ad459fac32c6259455319c3a246))
* **go-code:** graph-candidate generator as dark-launched 4th RRF arm (graph-first P1) ([#212](https://github.com/anatolykoptev/go-code/issues/212)) ([de3a099](https://github.com/anatolykoptev/go-code/commit/de3a099005ad32f498d79faf5c0657f8494c568a))
* **go-code:** index-time named execution flows (graph-first Phase 2 CORE) ([#213](https://github.com/anatolykoptev/go-code/issues/213)) ([bbecd49](https://github.com/anatolykoptev/go-code/commit/bbecd492ee12185ca50d778b63eea77f43af88ec))
* **go-code:** offline A/B harness for SPLADE arm (nDCG@10 + paired t-test gate, SPLADE P6) ([#199](https://github.com/anatolykoptev/go-code/issues/199)) ([5595fcf](https://github.com/anatolykoptev/go-code/commit/5595fcfde5a82fdc59760be531584e06e952e04f))
* **go-code:** operator-triggered sparse_backfill MCP tool (SPLADE P5) ([#198](https://github.com/anatolykoptev/go-code/issues/198)) ([9c200da](https://github.com/anatolykoptev/go-code/commit/9c200daf61d29bea6b33a88d504ca1046e0020c1))
* **go-code:** Phase 3a federated MCP foundation — repo resolver + cross-repo co-change ([#160](https://github.com/anatolykoptev/go-code/issues/160)) ([4969a70](https://github.com/anatolykoptev/go-code/commit/4969a70e9e2a0e6e460cbd0ee877928160d46b1c))
* **go-code:** Phase 3a.1 — federated co-change signal quality (origin-dedup + lift + sw.js filter) ([#161](https://github.com/anatolykoptev/go-code/issues/161)) ([3d69779](https://github.com/anatolykoptev/go-code/commit/3d69779cff5eff749060df282987c548ab41c6f3))
* **go-code:** Phase 3a.2 — Dunning G² significance ranking (two-tier, support-first) ([#162](https://github.com/anatolykoptev/go-code/issues/162)) ([7d050db](https://github.com/anatolykoptev/go-code/commit/7d050dbd628d76945bbca546ac2ba2130efc3e1c))
* **go-code:** Phase 3a.3 — Wilson-LB ranking + ubiquitous-file filter (CodeScene/Evan-Miller port) ([#163](https://github.com/anatolykoptev/go-code/issues/163)) ([a595370](https://github.com/anatolykoptev/go-code/commit/a5953705ca1964c2b8f937e548b600d9fba40c1d))
* **go-code:** Phase B — semantic route-match verification (verified-first cross-repo coupling) ([#164](https://github.com/anatolykoptev/go-code/issues/164)) ([98c388e](https://github.com/anatolykoptev/go-code/commit/98c388e4dca03e80ab57fd0f428f828ec3209fbc))
* **go-code:** resolve relative TS/JS imports to their package container ([#187](https://github.com/anatolykoptev/go-code/issues/187)) ([059ec4c](https://github.com/anatolykoptev/go-code/commit/059ec4ca37350c379c0c71f7206315070f58bb95))
* **go-code:** resolve TS $lib and @scope/workspace imports ([#189](https://github.com/anatolykoptev/go-code/issues/189)) ([aa86f18](https://github.com/anatolykoptev/go-code/commit/aa86f188b97e7f48f556d25519363158521b3633))
* **go-code:** sparse as dark-launched 3rd weighted-RRF arm (SPLADE P4) ([#197](https://github.com/anatolykoptev/go-code/issues/197)) ([bbd1f3d](https://github.com/anatolykoptev/go-code/commit/bbd1f3d45f324eb16b08a77bfbf44559c9ddc9d6))
* **go-code:** sparse retrieval + sparsevec HNSW index (SPLADE P3) ([#196](https://github.com/anatolykoptev/go-code/issues/196)) ([9d26c27](https://github.com/anatolykoptev/go-code/commit/9d26c271aa20909b36e274399b40f47037e8bc50))
* **ingest:** INDEX_SKIP_DIRS override + gocode_ingest_skipped_dirs_total counter ([#211](https://github.com/anatolykoptev/go-code/issues/211)) ([4d4602f](https://github.com/anatolykoptev/go-code/commit/4d4602f2396f4028e02ff6be2a9a94e68020f17e))
* **llm:** configurable cooldown TTL via LLM_COOLDOWN_SECONDS (default 15m) ([#234](https://github.com/anatolykoptev/go-code/issues/234)) ([413e636](https://github.com/anatolykoptev/go-code/commit/413e636fe461fe8bc399c97cc16e587544a96fad))
* **llm:** wire per-model cooldown + bump go-kit v0.83.0 ([#233](https://github.com/anatolykoptev/go-code/issues/233)) ([233681d](https://github.com/anatolykoptev/go-code/commit/233681d36d4848c78a74702bc366ed52f21a28df))
* **metrics:** wire ModelFilterObserver to Prometheus counters ([#230](https://github.com/anatolykoptev/go-code/issues/230)) ([f41597a](https://github.com/anatolykoptev/go-code/commit/f41597a1f4ca75210c2a08096ca84c5cbdfea7e0))


### Bug Fixes

* **autoindex:** emit skipped_no_vendor outcome + assert no-WARN contract ([#180](https://github.com/anatolykoptev/go-code/issues/180)) ([0508198](https://github.com/anatolykoptev/go-code/commit/0508198d020db22c2cac00395cdb433dbbd9b063))
* **codegraph:** repair fleet-wide HANDLES/FETCHES=0 — route→graph edge builder ([#167](https://github.com/anatolykoptev/go-code/issues/167)) ([0d6832d](https://github.com/anatolykoptev/go-code/commit/0d6832d1c29c8bfc33f28eab4e25fcc32329d146))
* **db:** reset pooled-conn search_path on release — bare code_* resolves public, not ag_catalog ([#173](https://github.com/anatolykoptev/go-code/issues/173)) ([a7842c2](https://github.com/anatolykoptev/go-code/commit/a7842c2c8c3d4fd5dff71ba2fc890f9b5fbdfe22))
* **embeddings:** incremental sync froze indexed_sha on first unsupported file in diff ([#170](https://github.com/anatolykoptev/go-code/issues/170)) ([e5f19c3](https://github.com/anatolykoptev/go-code/commit/e5f19c37c7d5fc4c30500ec8cc95c26f46b2da82))
* **embeddings:** rate-gate autoindex concurrency to 1 for single-worker embed backend ([#217](https://github.com/anatolykoptev/go-code/issues/217)) ([d46237e](https://github.com/anatolykoptev/go-code/commit/d46237e2e4e9287fcf7499fd086c42a8dd54e65d))
* **embeddings:** replace misleading freshness gauge with commits-behind + count SetRepoState write-failures ([#172](https://github.com/anatolykoptev/go-code/issues/172)) ([98085c0](https://github.com/anatolykoptev/go-code/commit/98085c0f4eece07ff2e7756bc5ddf6a2b3eb6f3b))
* **embeddings:** treat all 5xx as retryable; add embed_model per-row; continuous orphan gauge ([#232](https://github.com/anatolykoptev/go-code/issues/232)) ([50d40b2](https://github.com/anatolykoptev/go-code/commit/50d40b20ac8e9a623603201b9cc4149edb70a52c))
* **go-code:** batch build-time dead_code rerank to the server's per-request cap ([#191](https://github.com/anatolykoptev/go-code/issues/191)) ([8f05159](https://github.com/anatolykoptev/go-code/commit/8f05159b62bee95081ea41c107123bd9dddae242))
* **go-code:** embed HTTP timeout + bounded async index ctx + attributable cancel ([#216](https://github.com/anatolykoptev/go-code/issues/216)) ([b527a0b](https://github.com/anatolykoptev/go-code/commit/b527a0b3451681739ad82467b150dc7bde2cf552))
* **go-code:** exclude *_test.go imports from circular-dep detection ([#184](https://github.com/anatolykoptev/go-code/issues/184)) ([cd5ce38](https://github.com/anatolykoptev/go-code/commit/cd5ce3817f321a6051ea742948e9580fd7e57c52))
* **go-code:** group archgraph queries by package path, not base name ([#186](https://github.com/anatolykoptev/go-code/issues/186)) ([b6b2360](https://github.com/anatolykoptev/go-code/commit/b6b2360bfc5d9f735fc4d2fa13b6f6af2d7254ab))
* **go-code:** Phase 2a cleanup — 17 items (BUG-FH-1/2 closed, error encoding unified, +13 cosmetic) ([#157](https://github.com/anatolykoptev/go-code/issues/157)) ([bacce97](https://github.com/anatolykoptev/go-code/commit/bacce979fff85bc2da46de45484752a3f2a762dc))
* **go-code:** Phase 2b infra — Commits count, churn growth, since window, --follow, WithFreshness wiring ([#158](https://github.com/anatolykoptev/go-code/issues/158)) ([f7a0cb5](https://github.com/anatolykoptev/go-code/commit/f7a0cb5c3acb38c2831e1b0eedec8b930e40c9ac))
* **go-code:** pool AfterRelease RESET ALL, not DISCARD ALL (26000 regression) ([#176](https://github.com/anatolykoptev/go-code/issues/176)) ([68fa170](https://github.com/anatolykoptev/go-code/commit/68fa17005d221693faf884829a5ce63073380572))
* **go-code:** reconcile orphan embedding rows on full index + operator sweep (Bug B — phantom symbols) ([#209](https://github.com/anatolykoptev/go-code/issues/209)) ([caa34d5](https://github.com/anatolykoptev/go-code/commit/caa34d518baf65ea8103b846a1ff93ddba29d801))
* **go-code:** rerank via go-kit/rerank.Client, drop hardcoded embed-server URL ([#190](https://github.com/anatolykoptev/go-code/issues/190)) ([adead81](https://github.com/anatolykoptev/go-code/commit/adead81f0ae4b19f9a071a4588201940ce6428fb))
* **go-code:** self-index desync (SHA-gate data-aware) + HTTP-index-cancel observability ([#214](https://github.com/anatolykoptev/go-code/issues/214)) ([ec215cc](https://github.com/anatolykoptev/go-code/commit/ec215cc2cde0035540d40ca5268273b130824fa9))
* **go-code:** sparsevec batch size 500→100 (data-bound statement_timeout) + accurate write_failed counter ([#201](https://github.com/anatolykoptev/go-code/issues/201)) ([b0e5a8b](https://github.com/anatolykoptev/go-code/commit/b0e5a8bfe46209225e265e76df391e95bd1317ab))
* **go-code:** unify local package nodes (stop duplicate dir/import-path vertices) ([#185](https://github.com/anatolykoptev/go-code/issues/185)) ([ef83e22](https://github.com/anatolykoptev/go-code/commit/ef83e227ad1e9ea7a1945a7eae54b1a673623217))
* **graph-arm:** invert pagerank sub-generator — keyword-relevant ranked by pagerank ([#219](https://github.com/anatolykoptev/go-code/issues/219)) ([93b413f](https://github.com/anatolykoptev/go-code/commit/93b413f5e9da81667b2237e7aac5ff665708a4d9))
* **mcpmeta:** correct misleading stale-index remediation advice ([#169](https://github.com/anatolykoptev/go-code/issues/169)) ([2d2b9b6](https://github.com/anatolykoptev/go-code/commit/2d2b9b66553575629090c9e3da84e8ab67c6793e))
* **oxcodes:** bump structural-search HTTP timeout 10s-&gt;30s ([#168](https://github.com/anatolykoptev/go-code/issues/168)) ([c71889f](https://github.com/anatolykoptev/go-code/commit/c71889fedf084d3b6d6407100ab2da74e05c5960))
* **resolve:** resolve bare repo names against LocalRepoDirs registry ([#226](https://github.com/anatolykoptev/go-code/issues/226)) ([62adfe3](https://github.com/anatolykoptev/go-code/commit/62adfe3f818c1979c73f3f0ba63c4667537afc24))
* **semantic-search:** dedup semantic-only + CE-rerank results by file:symbol (Bug A) ([#208](https://github.com/anatolykoptev/go-code/issues/208)) ([c6650b9](https://github.com/anatolykoptev/go-code/commit/c6650b9d004f9181baca97133d38a08d7c250d00))
* **semhealth:** eliminate two find_duplicates false-positive classes ([#218](https://github.com/anatolykoptev/go-code/issues/218)) ([eba3622](https://github.com/anatolykoptev/go-code/commit/eba3622dec54f2a35a91afce947eb9a110623ba6))
* three go-code anomalies from 2026-06-12 investigation ([#228](https://github.com/anatolykoptev/go-code/issues/228)) ([23e245f](https://github.com/anatolykoptev/go-code/commit/23e245f95053acef5ed1b8b9c0ebee6517f54888))


### Performance Improvements

* **go-code:** batch sparse-embedding writes + raise backfill deadline ([#200](https://github.com/anatolykoptev/go-code/issues/200)) ([ea7e0ac](https://github.com/anatolykoptev/go-code/commit/ea7e0acd6b1f78ab2464c38281dd46d9d1c795e2))
* **go-code:** Phase 2c — batch initialCreationLines (BUG-FH-2b cold latency 34s→~3s) ([#159](https://github.com/anatolykoptev/go-code/issues/159)) ([3449902](https://github.com/anatolykoptev/go-code/commit/3449902a535d7d7392e7bd3a9d94d994a0c3d37a))
