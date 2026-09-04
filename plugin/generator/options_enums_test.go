// Copyright 2026 The Protobuf Project authors.
// SPDX-License-Identifier: Apache-2.0

package generator

import (
	"testing"

	mcppb "github.com/the-protobuf-project/mcp/protobuf/mcppb"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestFieldFormatKeyword(t *testing.T) {
	tests := []struct {
		format mcppb.MCPFieldFormat
		want   string
	}{
		{mcppb.MCPFieldFormat_MCP_FIELD_FORMAT_UNSPECIFIED, ""},
		{mcppb.MCPFieldFormat_MCP_FIELD_FORMAT_DATE_TIME, "date-time"},
		{mcppb.MCPFieldFormat_MCP_FIELD_FORMAT_URI, "uri"},
		{mcppb.MCPFieldFormat_MCP_FIELD_FORMAT_BYTE, "byte"},
	}
	for _, tt := range tests {
		if got := fieldFormatKeyword(tt.format); got != tt.want {
			t.Errorf("fieldFormatKeyword(%v) = %q, want %q", tt.format, got, tt.want)
		}
	}
}

// Every declared format must map to a keyword, or an author who sets a new enum
// value gets silence instead of a format in the schema.
func TestFieldFormatKeywordCoversEveryValue(t *testing.T) {
	values := mcppb.MCPFieldFormat_name
	for num, name := range values {
		format := mcppb.MCPFieldFormat(num)
		if format == mcppb.MCPFieldFormat_MCP_FIELD_FORMAT_UNSPECIFIED {
			continue
		}
		if fieldFormatKeyword(format) == "" {
			t.Errorf("%s has no JSON Schema keyword mapping", name)
		}
	}
}

func TestFieldTypeKeywordCoversEveryValue(t *testing.T) {
	for num, name := range mcppb.MCPFieldType_name {
		typ := mcppb.MCPFieldType(num)
		if typ == mcppb.MCPFieldType_MCP_FIELD_TYPE_UNSPECIFIED {
			continue
		}
		if fieldTypeKeyword(typ) == "" {
			t.Errorf("%s has no JSON Schema keyword mapping", name)
		}
	}
}

func TestRoleNameDefaultsToUser(t *testing.T) {
	tests := []struct {
		role mcppb.MCPRole
		want string
	}{
		{mcppb.MCPRole_MCP_ROLE_UNSPECIFIED, "user"},
		{mcppb.MCPRole_MCP_ROLE_USER, "user"},
		{mcppb.MCPRole_MCP_ROLE_ASSISTANT, "assistant"},
	}
	for _, tt := range tests {
		if got := roleName(tt.role); got != tt.want {
			t.Errorf("roleName(%v) = %q, want %q", tt.role, got, tt.want)
		}
	}
}

func TestElicitationModeInference(t *testing.T) {
	tests := []struct {
		name string
		mode mcppb.MCPElicitationMode
		url  string
		want string
	}{
		{"unspecified with no url infers form", mcppb.MCPElicitationMode_MCP_ELICITATION_MODE_UNSPECIFIED, "", elicitModeForm},
		{"unspecified with url infers url", mcppb.MCPElicitationMode_MCP_ELICITATION_MODE_UNSPECIFIED, "https://example.com", elicitModeURL},
		{"explicit form wins over url", mcppb.MCPElicitationMode_MCP_ELICITATION_MODE_FORM, "https://example.com", elicitModeForm},
		{"explicit url with no url set", mcppb.MCPElicitationMode_MCP_ELICITATION_MODE_URL, "", elicitModeURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := elicitationMode(tt.mode, tt.url); got != tt.want {
				t.Errorf("elicitationMode(%v, %q) = %q, want %q", tt.mode, tt.url, got, tt.want)
			}
		})
	}
}

// MCPFieldOptions.format was a string before it became an enum. Protos built
// against the older published schema still carry the string, and protobuf-go
// parks it in unknown fields because the wire type no longer matches.
func TestLegacyFieldFormatReadsPreEnumString(t *testing.T) {
	opts := &mcppb.MCPFieldOptions{Description: "a field"}
	// Field 4, wire type 2 (bytes), value "uri" — how the pre-enum schema encoded it.
	opts.ProtoReflect().SetUnknown(protoreflect.RawFields{0x22, 0x03, 'u', 'r', 'i'})

	if got := opts.GetFormat(); got != mcppb.MCPFieldFormat_MCP_FIELD_FORMAT_UNSPECIFIED {
		t.Fatalf("GetFormat() = %v, want UNSPECIFIED (the string cannot decode into the enum)", got)
	}
	if got := legacyFieldFormat(opts); got != "uri" {
		t.Errorf("legacyFieldFormat() = %q, want %q", got, "uri")
	}
}

func TestLegacyFieldFormatAbsentWhenNoUnknownFields(t *testing.T) {
	opts := &mcppb.MCPFieldOptions{Format: mcppb.MCPFieldFormat_MCP_FIELD_FORMAT_URI}
	if got := legacyFieldFormat(opts); got != "" {
		t.Errorf("legacyFieldFormat() = %q, want \"\" for a proto with no legacy bytes", got)
	}
}

func TestLegacyFieldFormatNil(t *testing.T) {
	if got := legacyFieldFormat(nil); got != "" {
		t.Errorf("legacyFieldFormat(nil) = %q, want \"\"", got)
	}
}
