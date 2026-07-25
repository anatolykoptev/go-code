package main

import (
	"reflect"
	"testing"
)

// TestFieldAccessDescParity verifies that UnderstandInput.FieldAccess and
// CallTraceInput.FieldAccess carry identical descriptions and that both equal
// the fieldAccessDesc const. Prevents silent drift between the two tool
// schemas. Reads the live `jsonschema` tag (the one google/jsonschema-go
// actually emits).
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

// descTag returns the live jsonschema description tag for a struct field.
func descTag(t *testing.T, rt reflect.Type, fieldName string) string {
	t.Helper()
	f := rt.Field(indexOf(t, rt, fieldName))
	return f.Tag.Get("jsonschema")
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
