package main

import (
	"reflect"
	"testing"
)

// TestFieldAccessDescParity verifies that UnderstandInput.FieldAccess and
// CallTraceInput.FieldAccess carry identical descriptions and that both equal
// the fieldAccessDesc const. Prevents silent drift between the two tool
// schemas. Reads the live `jsonschema` tag (the one google/jsonschema-go
// actually emits) with a fallback to the legacy `jsonschema_description` tag
// for tools not yet migrated (batch B, issue #684).
func TestFieldAccessDescParity(t *testing.T) {
	understandTag := descTag(t, reflect.TypeOf(UnderstandInput{}), "FieldAccess")

	callTraceTag := descTag(t, reflect.TypeOf(CallTraceInput{}), "FieldAccess")

	if understandTag != fieldAccessDesc {
		t.Errorf("UnderstandInput.FieldAccess description does not match fieldAccessDesc\ngot:  %q\nwant: %q",
			understandTag, fieldAccessDesc)
	}
	if callTraceTag != fieldAccessDesc {
		t.Errorf("CallTraceInput.FieldAccess description does not match fieldAccessDesc\ngot:  %q\nwant: %q",
			callTraceTag, fieldAccessDesc)
	}
}

// descTag returns the live jsonschema description tag for a struct field,
// falling back to the legacy jsonschema_description tag if the live one is
// absent (tools not yet migrated to the correct tag — batch B, issue #684).
func descTag(t *testing.T, rt reflect.Type, fieldName string) string {
	t.Helper()
	f := rt.Field(indexOf(t, rt, fieldName))
	if v, ok := f.Tag.Lookup("jsonschema"); ok && v != "" {
		return v
	}
	return f.Tag.Get("jsonschema_description")
}

// indexOf returns the struct field index for the given field name.
// Uses t.Fatalf (not panic) so a missing field produces a proper FAIL line
// and does not crash the entire test binary.
func indexOf(t *testing.T, rt reflect.Type, name string) int {
	t.Helper()
	for i := range rt.NumField() {
		if rt.Field(i).Name == name {
			return i
		}
	}
	t.Fatalf("field %q not found in %v", name, rt)
	return -1
}
