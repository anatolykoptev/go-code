package main

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/research"
)

// code_research response types migrated from hand-rolled fmt.Fprintf onto
// encoding/xml.Marshal (failure class: manual XML string-concatenation).
//
// Unlike the error-fragment migrations, this formatter had a LIVE malformed-XML
// bug: the <map> body was interpolated with a raw %s carrying r.Map -- the
// Aider-style code map, which embeds function signatures and (with
// include_body) full bodies. Those routinely contain <, & and > (Go <-chan,
// generics like Vec<T>, pointer &x), so <map>raw</map> produced malformed XML
// for essentially every real repository. The <seeds>/<graph> attributes were
// likewise emitted with %q (Go quoting, NOT XML escaping) on path/why/kind/
// source. Both are now correct BY CONSTRUCTION: <map> is a ,chardata node and
// every attribute is an xml.Marshal attr, so <, & and " are escaped and the
// value round-trips. Floats (score) are pre-formatted %.4f -- a raw float64
// marshal drops trailing zeros and breaks attribute equivalence.
//
// <stats.../> serializes long-form <stats ...></stats> (xml.Marshal never
// self-closes), decoder-equivalent to the prior self-closing form. No xml.Header
// prolog (fragment consumed by the MCP caller).

type researchRespXML struct {
	XMLName xml.Name         `xml:"response"`
	Tool    string           `xml:"tool,attr"`
	Query   string           `xml:"query"`
	Repo    string           `xml:"repo"`
	Mode    string           `xml:"mode"`
	Stats   researchStatsXML `xml:"stats"`
	// Map is the Aider-style code map, carried verbatim in a CDATA section via
	// the shared xmlCDATA carrier. CDATA is byte-neutral, where ,chardata would
	// escape every <-chan / Vec<T> / &x to a 4-5 byte entity and inflate the
	// response tokens on this heavy tool. A nil pointer omits the element,
	// matching the prior `if r.Map != ""` guard.
	//
	// Emitted BEFORE Seeds/Graph (#571): the map is the primary LLM-consumable
	// verdict/summary; Seeds and Graph are detail sections that get cut off
	// first when the response hits the client truncation ceiling. XML element
	// order follows struct field order, so Map must precede Seeds/Graph here.
	Map *xmlCDATA `xml:"map,omitempty"`
	// Warnings carries meta-signals about the analysis tier (e.g. a go/types
	// warming note). Nil/omitted when there are no warnings — the common case.
	Warnings *researchWarningsXML `xml:"warnings,omitempty"`
	// Seeds/Graph are nil (omitted) in compact mode or when empty, matching the
	// prior guard `!input.Compact && len(...) > 0`.
	Seeds *researchSeedsXML `xml:"seeds,omitempty"`
	Graph *researchGraphXML `xml:"graph,omitempty"`
}

type researchWarningsXML struct {
	Items []string `xml:"warning"`
}

type researchStatsXML struct {
	Seeds           int `xml:"seeds,attr"`
	GraphFiles      int `xml:"graph_files,attr"`
	Pruned          int `xml:"pruned,attr"`
	EstimatedTokens int `xml:"estimated_tokens,attr"`
}

type researchSeedsXML struct {
	Files []researchSeedFileXML `xml:"file"`
}

type researchSeedFileXML struct {
	Path    string               `xml:"path,attr"`
	Score   string               `xml:"score,attr"`
	Symbols []researchSeedSymXML `xml:"symbol"`
}

type researchSeedSymXML struct {
	Kind   string `xml:"kind,attr"`
	Line   int    `xml:"line,attr"`
	Source string `xml:"source,attr"`
	Value  string `xml:",chardata"`
}

type researchGraphXML struct {
	Files []researchGraphFileXML `xml:"file"`
}

type researchGraphFileXML struct {
	Path     string                `xml:"path,attr"`
	Distance int                   `xml:"distance,attr"`
	Why      string                `xml:"why,attr"`
	Score    string                `xml:"score,attr"`
	Symbols  []researchGraphSymXML `xml:"symbol"`
}

type researchGraphSymXML struct {
	Kind  string `xml:"kind,attr"`
	Line  int    `xml:"line,attr"`
	Value string `xml:",chardata"`
}

// buildResearchSeeds groups seed symbols by their (root-stripped) file path,
// preserving the prior formatter's dedup-first-occurrence + inner-collect logic
// exactly so the emitted tree is unchanged.
func buildResearchSeeds(seeds []research.SeedSymbol, stripRoot string) *researchSeedsXML {
	out := &researchSeedsXML{}
	seen := make(map[string]bool)
	for _, s := range seeds {
		relFile := strings.TrimPrefix(s.File, stripRoot)
		if seen[relFile] {
			continue
		}
		seen[relFile] = true
		file := researchSeedFileXML{
			Path:  relFile,
			Score: fmt.Sprintf("%.4f", s.Score),
		}
		for _, s2 := range seeds {
			if strings.TrimPrefix(s2.File, stripRoot) == relFile && s2.Name != "" {
				file.Symbols = append(file.Symbols, researchSeedSymXML{
					Kind:   s2.Kind,
					Line:   s2.Line,
					Source: s2.Source,
					Value:  s2.Name,
				})
			}
		}
		out.Files = append(out.Files, file)
	}
	return out
}

// buildResearchGraph mirrors the prior graph-section loop: skip files with no
// symbols, root-strip the path, and pre-format the score.
func buildResearchGraph(graph []research.LinkedFile, stripRoot string) *researchGraphXML {
	out := &researchGraphXML{}
	for _, lf := range graph {
		if len(lf.Symbols) == 0 {
			continue
		}
		file := researchGraphFileXML{
			Path:     strings.TrimPrefix(lf.RelPath, stripRoot),
			Distance: lf.Distance,
			Why:      lf.WhyLinked,
			Score:    fmt.Sprintf("%.4f", lf.Score),
		}
		for _, sym := range lf.Symbols {
			file.Symbols = append(file.Symbols, researchGraphSymXML{
				Kind:  string(sym.Kind),
				Line:  int(sym.StartLine),
				Value: sym.Name,
			})
		}
		out.Files = append(out.Files, file)
	}
	return out
}
