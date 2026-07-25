package main

// fieldAccessDesc is the canonical description for the field_access MCP input.
// Both UnderstandInput.FieldAccess and CallTraceInput.FieldAccess must carry
// this exact text in their jsonschema struct tags.
// Verified by TestFieldAccessDescParity in tool_consts_test.go.
const fieldAccessDesc = "When true, include heuristic argument-reference call sites" +
	" (struct field accesses, identifier args) as callees even when they don't resolve" +
	" to a known function — legacy permissive behaviour." +
	" Default false: only true call expressions and resolved function references are reported."
