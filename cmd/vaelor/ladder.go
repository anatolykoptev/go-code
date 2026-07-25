package main

import (
	"fmt"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
)

// renderLadder runs the progressive result-shortening ladder and returns the
// final body text. It is the single shared glue extracted from code_search's
// handleCodeSearchInner (#685 part 2) so every adopting tool gets the five
// hard-won invariants for free — no caller can forget one by copy-pasting a
// divergent copy of a subtle invariant.
//
// Invariants enforced here:
//
//  1. Every rung is a self-consistent, parseable document; never a partial
//     one (except the marked last-resort cut). The caller's rung closures
//     produce complete documents; PickFitting never returns a partial
//     document on the normal path.
//  2. The returned text satisfies len(text) <= budget AND
//     mcpmeta.Shape(text, budget, "") == text. The addTool wrapper re-shapes
//     anything it does not recognise as shaped at DefaultBudget; it does NOT
//     recognise the XML-comment markers, so the body MUST fit within budget
//     for the wrapper's Shape to be a provable no-op. The reserve mechanism
//     (invariant 3) guarantees body+pointer <= budget.
//  3. Reserve headroom for the file pointer BEFORE picking a rung, computed
//     from the format string (fileSavePointerUpperBound), charged only when
//     outputDir != "" (no file-save possible otherwise).
//  4. File-save fires when the ladder CONDENSED (rung > 1), not on a byte
//     threshold. The ladder condenses above the budget, so any separate
//     threshold leaves a band where the full result is reachable nowhere.
//  5. The fullest rung is rendered at most once. PickFitting returns the
//     rung-1 rendering as `full`; the file-save path persists it without
//     re-rendering.
func renderLadder(ladder mcpmeta.Ladder, toolName, outputDir string, budget int) string {
	// Invariant 3: reserve headroom for the file pointer before picking a
	// rung. The reserve is an upper bound on the pointer length; it is only
	// charged when outputDir is set (no file-save possible otherwise).
	reserve := 0
	if outputDir != "" {
		reserve = fileSavePointerUpperBound(toolName, outputDir)
	}
	effectiveBudget := budget - reserve
	if effectiveBudget < mcpmeta.MinBudget {
		effectiveBudget = mcpmeta.MinBudget
	}
	// Invariant 5: PickFitting renders rung 1 at most once and returns it as
	// `full`; the file-save path uses `full` without re-rendering.
	body, full, rung := mcpmeta.PickFitting(ladder, effectiveBudget)
	// Invariant 4: file-save fires when the ladder CONDENSED (rung > 1),
	// not on a byte threshold. The gate is "the ladder condensed", NOT a
	// maxInlineCharsDefault threshold: the ladder condenses as soon as
	// `full` exceeds budget, so a separate threshold leaves a band where
	// the full result is reachable nowhere (not inline, not on disk).
	if outputDir != "" && rung > 1 {
		if path, ok := saveToFile(full, toolName, outputDir); ok {
			// Invariant 2: defensive ceiling guard. The reserve mechanism
			// guarantees body+pointer <= budget for any realistic outputDir.
			// For a pathologically long outputDir (pointer alone > budget),
			// drop the pointer rather than overflow — the file is still on
			// disk; the body stays under budget (the ceiling invariant wins
			// over the pointer).
			if ptr := fileSavePointer(len(full), path); len(body)+len(ptr) <= budget {
				body += ptr
			}
		}
	}
	return body
}

// fileSavePointerUpperBound returns a safe upper bound on the byte length of
// the pointer produced by fileSavePointer for any char count and any
// timestamped path under outputDir. It overestimates the char-count digits
// (int64 max = 19 digits) and the timestamp digits (int64 max = 19 digits)
// so the caller can reserve headroom BEFORE PickFitting and guarantee
// body+pointer <= budget (invariant 3). The bound is computed from the
// format string itself, not hand-counted, so it stays correct if the format
// changes.
func fileSavePointerUpperBound(toolName, outputDir string) int {
	const maxDigits = 20 // covers int64 max (19 digits) + 1 safety
	// Filename: "<toolName>_<millis>.txt" — millis is int64, bounded by maxDigits.
	maxFilename := toolName + "_" + strings.Repeat("9", maxDigits) + ".txt"
	// filepath.Join can only shorten (collapsing slashes/trailing separators),
	// so len(outputDir)+1+len(maxFilename) is a safe upper bound on the joined path.
	maxPathLen := len(outputDir) + 1 + len(maxFilename)
	// Build the pointer with max-length fields and measure its byte length.
	// The em-dash and other non-ASCII chars are multi-byte in UTF-8; len()
	// counts bytes, matching len(body) and budget (both byte measures).
	return len(fmt.Sprintf("\n\n<!-- full-result: %s chars saved to: %s — Use Read tool to access the file. -->",
		strings.Repeat("9", maxDigits), strings.Repeat("x", maxPathLen)))
}

// fileSavePointer builds the XML-comment pointer appended to the inline body
// when the full rendering is persisted to a file. It is an XML comment so the
// envelope stays well-formed under a strict parser (same precedent as
// appendMetaFooter). The sentinel prefix is greppable.
func fileSavePointer(charCount int, path string) string {
	return fmt.Sprintf("\n\n<!-- full-result: %d chars saved to: %s — Use Read tool to access the file. -->",
		charCount, path)
}
