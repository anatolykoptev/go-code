package main

// TestProvenance_AllTools_SurfaceCheckoutLag was removed: with M=1 (the
// single wrapper render site) there is nothing per-tool left to pin. The
// end-to-end test TestAddTool_ProvenanceReachesTheClient_EndToEnd covers the
// single site, and TestAddTool_MergeKeepsBothContributions_EndToEnd covers
// the merge. The 5-row table that drove each tool's annotateEnv call
// individually has no equivalent once annotateEnv is deleted — recorded here
// so the removal cannot vanish quietly in a diff.
//
// mkLaggingSourceRepo was also removed with the table — it existed only to
// build the searchable-source-tree + lagging-checkout fixture the rows drove.
