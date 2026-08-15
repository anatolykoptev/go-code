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
// repo-taking tool does.
//
// Mutation that must turn it RED: delete `ctx = seedProvenanceSlot(ctx)` from
// addTool, or delete `recordProvenance(ctx, repo, root)` from resolveRoot.
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
}

// A response that resolved no repo must report nothing — not the lag of
// whatever checkout the process happens to be sitting in.
//
// gitDir joins its argument with ".git", so WithCheckoutLag("") stats ".git"
// CWD-relative. `go test ./cmd/vaelor/` runs with CWD at cmd/vaelor/, which
// holds no .git, so the suite cannot stumble onto this bug — the test has to
// plant the condition deliberately or it asserts nothing.
//
// Mutation that must turn it RED: drop `resolved != "" &&` from the provenance
// guard in applyBudgetAndTook.
func TestApplyBudgetAndTook_NoRepoResolved_DoesNotReportProcessCWD(t *testing.T) {
	dir := t.TempDir()
	writeLaggingGitDir(t, dir)
	t.Chdir(dir)

	// The fixture must be live: with CWD inside a lagging checkout, the exact
	// production call must produce a signal. If it does not, this test would
	// pass with or without the guard and prove nothing.
	if probe := mcpmeta.WithCheckoutLag(mcpmeta.Envelope{}, ""); probe.CheckoutLag == "" {
		t.Fatal("fixture is inert: CWD does not read as a lagging checkout, " +
			"so this test cannot distinguish a working guard from a missing one")
	}

	res := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "a response that touched no repo"}},
	}
	applyBudgetAndTook(res, time.Millisecond, "", "")

	got := textContentOf(t, res)
	if strings.Contains(got, "<!-- meta:") {
		t.Fatalf("a response that resolved no repo must carry no provenance; "+
			"the process CWD is not the answer to a question nobody asked, got:\n%s", got)
	}
}

// A body that QUOTES the footer sentinel must still receive a real footer.
//
// vaelor indexes its own source, so a code search over this repo returns the
// line in helpers.go that builds the footer. A substring idempotence check
// reads that hit as "already attached" and silently drops the provenance from
// exactly the query most likely to be run here.
//
// Mutation that must turn it RED: restore HasMetaFooter to
// `strings.Contains(text, metaFooterPrefix)`.
func TestApplyBudgetAndTook_BodyQuotingSentinel_StillGetsFooter(t *testing.T) {
	root := mkLaggingRepo(t)

	// A search hit whose CONTENT is the footer-building line itself.
	quoted := `cmd/vaelor/helpers.go:69:	return body + "\n\n<!-- meta: " + string(js) + " -->"` + "\n"
	body := quoted + strings.Repeat("line of content\n", 1000)

	res := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: body}},
	}
	// requested == resolved, so source_path stays silent and checkout_lag is
	// the signal under test.
	applyBudgetAndTook(res, 5*time.Millisecond, root, root)

	got := textContentOf(t, res)
	if !strings.Contains(got, "<!-- meta: ") || !strings.Contains(got, "checkout_lag") {
		t.Fatalf("a body quoting the footer sentinel must still receive the real "+
			"footer, got tail:\n%s", truncForLog(got, 400))
	}
}
