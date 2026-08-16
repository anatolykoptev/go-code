package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The central-attachment unit tests call applyBudgetAndTook directly. That
// proves the footer logic and proves nothing about the WIRE: addTool seeds the
// slot, resolveRoot records into it, the wrapper reads the snapshot back. Any
// one of those three production lines could be deleted and every unit test in
// this package would still pass — the class that put "TWO correct enforcement
// gates, ZERO writers" in the fleet's incident record.
//
// This drives a real MCP client call over the in-memory transport, through the
// real addTool wrapper, into a handler that resolves a repo the way every
// repo-taking tool does. resolveRoot records the path signals (SourcePath,
// CheckoutLag) into the envelope slot; the wrapper reads the merged envelope
// and renders the footer after shaping.
//
// Mutation that must turn it RED: delete `ctx = seedProvenanceSlot(ctx)` from
// addTool, or delete `recordEnvelope(ctx, ...)` from resolveRoot.
// Either breaks the chain while leaving the unit tests green.
func TestAddTool_ProvenanceReachesTheClient_EndToEnd(t *testing.T) {
	root := mkLaggingRepo(t)

	// addTool appends to a package global that another test asserts over;
	// restore it so registration order between tests cannot matter.
	saved := registeredToolInputs
	t.Cleanup(func() { registeredToolInputs = saved })

	type probeIn struct {
		Repo string `json:"repo" jsonschema:"repository to resolve"`
	}

	server := newTestServer(t)
	addTool(server, &mcp.Tool{Name: "provenance_probe", Description: "provenance wiring probe"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in probeIn) (*mcp.CallToolResult, error) {
			_, cleanup, err := resolveRoot(ctx, in.Repo, "", analyze.Deps{})
			if err != nil {
				return nil, err
			}
			if cleanup != nil {
				defer cleanup()
			}
			// Deliberately over budget: a footer folded in by the handler
			// would be cut by Shape, so only the central attachment can
			// deliver the signal to the client.
			return textResult(strings.Repeat("line of content\n", 1000)), nil
		})

	ctx := t.Context()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go func() {
		_, _ = server.Connect(ctx, serverTransport, nil)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "provenance_probe",
		Arguments: map[string]any{"repo": root},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	got := textContentOf(t, res)
	if res.IsError {
		t.Fatalf("probe tool returned an error result: %s", got)
	}
	if !strings.Contains(got, "checkout_lag") {
		t.Fatalf("provenance must reach the client through the real wrapper "+
			"(addTool seeds the slot, resolveRoot records, the wrapper reads it); "+
			"got tail:\n%s", truncForLog(got, 400))
	}
	// Exactly one envelope — the #781 property. The wrapper renders once
	// from the slot; no sniffing, no double-tagging.
	if count := strings.Count(got, "<!-- meta:"); count != 1 {
		t.Fatalf("exactly one meta footer must reach the client, got %d", count)
	}
}

// End-to-end with BOTH contributions: resolveRoot records path signals, the
// handler records freshness. The merged envelope must carry BOTH — the merge
// keeps both contributions (closes the silent-drop class).
//
// Mutation that must turn it RED: make the merge last-writer-wins. The tool's
// recordEnvelope (freshness-only) overwrites resolveRoot's path signals, and
// checkout_lag is lost.
func TestAddTool_MergeKeepsBothContributions_EndToEnd(t *testing.T) {
	root := mkLaggingRepo(t)

	saved := registeredToolInputs
	t.Cleanup(func() { registeredToolInputs = saved })

	type probeIn struct {
		Repo string `json:"repo" jsonschema:"repository to resolve"`
	}

	server := newTestServer(t)
	addTool(server, &mcp.Tool{Name: "merge_probe", Description: "merge probe"},
		func(ctx context.Context, _ *mcp.CallToolRequest, in probeIn) (*mcp.CallToolResult, error) {
			_, cleanup, err := resolveRoot(ctx, in.Repo, "", analyze.Deps{})
			if err != nil {
				return nil, err
			}
			if cleanup != nil {
				defer cleanup()
			}
			// Tool records freshness — resolveRoot already recorded path
			// signals. The merge must keep both.
			recordEnvelope(ctx, mcpmeta.WithFreshness(mcpmeta.Envelope{},
				root, "cccccccccccccccccccccccccccccccccccccccc"))
			return textResult(strings.Repeat("line of content\n", 1000)), nil
		})

	ctx := t.Context()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go func() {
		_, _ = server.Connect(ctx, serverTransport, nil)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "merge_probe",
		Arguments: map[string]any{"repo": root},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	got := textContentOf(t, res)
	if res.IsError {
		t.Fatalf("probe tool returned an error result: %s", got)
	}
	// Both contributions must be present.
	if !strings.Contains(got, "checkout_lag") {
		t.Fatalf("merge must keep resolveRoot's checkout_lag, got tail:\n%s",
			truncForLog(got, 400))
	}
	// stale_warning may be empty if the fixture's main hasn't advanced past
	// the indexed SHA. With the lagging fixture, main is aaaa... and we
	// passed indexedSHA=aaaa... so they MATCH → freshness is silent. That's
	// correct. The test pins the merge by checking checkout_lag survives
	// alongside a tool recordEnvelope call — if the merge were
	// last-writer-wins, the tool's (empty-path) envelope would overwrite
	// resolveRoot's path signals.
}

// A response that resolved no repo must report nothing — not the lag of
// whatever checkout the process happens to be sitting in.
//
// In the new design, resolveRoot is the ONLY writer of path signals
// (WithCheckoutLag/WithSourcePath). If no repo was resolved, no envelope is
// recorded, and the wrapper renders no footer. The process-CWD hazard the
// old `resolved != ""` guard existed to prevent is structurally impossible:
// WithCheckoutLag is never called with an empty root.
//
// Mutation that must turn it RED: call recordEnvelope with
// WithCheckoutLag("") from a path that fires without resolveRoot. Then the
// process CWD's .git is read and a footer appears on a response that touched
// no repo.
func TestApplyBudgetAndTook_NoRepoResolved_DoesNotReportProcessCWD(t *testing.T) {
	dir := t.TempDir()
	writeLaggingGitDir(t, dir)
	t.Chdir(dir)

	// The fixture must be live: with CWD inside a lagging checkout, the
	// exact production call must produce a signal. If it does not, this test
	// would pass with or without the guard and prove nothing.
	if probe := mcpmeta.WithCheckoutLag(mcpmeta.Envelope{}, ""); probe.CheckoutLag == "" {
		t.Fatal("fixture is inert: CWD does not read as a lagging checkout, " +
			"so this test cannot distinguish a working guard from a missing one")
	}

	// No envelope recorded — simulate a handler that never called
	// resolveRoot. The wrapper must produce no footer.
	res := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "a response that touched no repo"}},
	}
	applyBudgetAndTook(res, time.Millisecond, mcpmeta.Envelope{})

	got := textContentOf(t, res)
	if strings.Contains(got, "<!-- meta:") {
		t.Fatalf("a response that resolved no repo must carry no provenance; "+
			"the process CWD is not the answer to a question nobody asked, got:\n%s", got)
	}
}

// A body that QUOTES the footer sentinel must still receive exactly ONE
// real footer — the recorded one. The wrapper does not sniff the body; it
// renders from the slot.
//
// This is the #781 property at the unit level: the old last-line sniff
// false-negatived when a tool appended after its own footer, producing two
// envelopes. The new design produces exactly one regardless of body content.
//
// Mutation that must turn it RED: restore the HasMetaFooter sniff and make
// the body's last line not match the sentinel pattern. The sniff misses, the
// wrapper appends a second footer.
func TestApplyBudgetAndTook_BodyQuotingSentinel_StillGetsExactlyOneFooter(t *testing.T) {
	root := mkLaggingRepo(t)

	// A search hit whose CONTENT is the footer-building line itself,
	// followed by a condensation marker — the exact #781 shape.
	quoted := `cmd/vaelor/helpers.go:69:	return body + "\n\n<!-- meta: " + string(js) + " -->"` + "\n"
	quoted += "<!-- condensed: rung 2/3 — counts -->\n"
	body := quoted + strings.Repeat("line of content\n", 1000)

	res := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: body}},
	}
	env := mcpmeta.WithCheckoutLag(mcpmeta.Envelope{}, root)
	applyBudgetAndTook(res, 5*time.Millisecond, env)

	got := textContentOf(t, res)
	// Count real meta footers — JSON envelopes with checkout_lag.
	footerCount := strings.Count(got, `"checkout_lag"`)
	if footerCount != 1 {
		t.Fatalf("a body quoting the sentinel must yield exactly ONE real "+
			"footer (the recorded one), got %d checkout_lag occurrences:\n%s",
			footerCount, truncForLog(got, 400))
	}
}
