// Copyright 2026 The Protobuf Project authors.
// SPDX-License-Identifier: Apache-2.0

package generator

import (
	mcppb "github.com/the-protobuf-project/mcp/protobuf/mcppb"
)

// Elicitation modes as they appear in MCPElicitationOpts.Mode.
const (
	elicitModeForm = "form"
	elicitModeURL  = "url"
)

// fieldFormatKeywords maps MCPFieldFormat to its JSON Schema "format" keyword.
// MCP_FIELD_FORMAT_UNSPECIFIED is absent so the keyword is omitted entirely.
var fieldFormatKeywords = map[mcppb.MCPFieldFormat]string{
	mcppb.MCPFieldFormat_MCP_FIELD_FORMAT_DATE_TIME: "date-time",
	mcppb.MCPFieldFormat_MCP_FIELD_FORMAT_DATE:      "date",
	mcppb.MCPFieldFormat_MCP_FIELD_FORMAT_TIME:      "time",
	mcppb.MCPFieldFormat_MCP_FIELD_FORMAT_URI:       "uri",
	mcppb.MCPFieldFormat_MCP_FIELD_FORMAT_UUID:      "uuid",
	mcppb.MCPFieldFormat_MCP_FIELD_FORMAT_EMAIL:     "email",
	mcppb.MCPFieldFormat_MCP_FIELD_FORMAT_BYTE:      "byte",
}

// fieldTypeKeywords maps MCPFieldType to its JSON Schema "type" keyword.
var fieldTypeKeywords = map[mcppb.MCPFieldType]string{
	mcppb.MCPFieldType_MCP_FIELD_TYPE_STRING:  "string",
	mcppb.MCPFieldType_MCP_FIELD_TYPE_NUMBER:  "number",
	mcppb.MCPFieldType_MCP_FIELD_TYPE_BOOLEAN: "boolean",
	mcppb.MCPFieldType_MCP_FIELD_TYPE_INTEGER: "integer",
}

// iconThemeNames maps MCPIconTheme to the MCP wire value. UNSPECIFIED maps to
// "" so the icon is treated as suitable for any background.
var iconThemeNames = map[mcppb.MCPIconTheme]string{
	mcppb.MCPIconTheme_MCP_ICON_THEME_LIGHT: "light",
	mcppb.MCPIconTheme_MCP_ICON_THEME_DARK:  "dark",
}

// fieldFormatKeyword returns the JSON Schema format keyword for f, or "" when
// no keyword should be emitted.
func fieldFormatKeyword(f mcppb.MCPFieldFormat) string { return fieldFormatKeywords[f] }

// fieldTypeKeyword returns the JSON Schema type keyword for t, or "" when the
// type should be inferred from the proto field instead.
func fieldTypeKeyword(t mcppb.MCPFieldType) string { return fieldTypeKeywords[t] }

// iconTheme returns the MCP theme name for t, or "" for any background.
func iconTheme(t mcppb.MCPIconTheme) string { return iconThemeNames[t] }

// roleName maps MCPRole to the MCP message role. MCP_ROLE_UNSPECIFIED defaults
// to "user", matching the field documentation on MCPPrompt.role.
func roleName(r mcppb.MCPRole) string {
	if r == mcppb.MCPRole_MCP_ROLE_ASSISTANT {
		return "assistant"
	}
	return "user"
}

// elicitationMode resolves MCPElicitationMode to "form" or "url". When the mode
// is unspecified it is inferred: URL if a url is set, otherwise form. This
// mirrors the inference documented on MCPElicitation.mode so that every
// downstream template sees a concrete mode.
func elicitationMode(m mcppb.MCPElicitationMode, url string) string {
	switch m {
	case mcppb.MCPElicitationMode_MCP_ELICITATION_MODE_URL:
		return elicitModeURL
	case mcppb.MCPElicitationMode_MCP_ELICITATION_MODE_FORM:
		return elicitModeForm
	default:
		if url != "" {
			return elicitModeURL
		}
		return elicitModeForm
	}
}

// cacheScopeNames maps MCPCacheScope to the wire value. UNSPECIFIED maps to ""
// so an undeclared scope stays absent rather than asserting "public".
var cacheScopeNames = map[mcppb.MCPCacheScope]string{
	mcppb.MCPCacheScope_MCP_CACHE_SCOPE_PUBLIC:  "public",
	mcppb.MCPCacheScope_MCP_CACHE_SCOPE_PRIVATE: "private",
}

// cacheHint converts a declared hint for templates, or nil when none is set.
//
// The schema states the TTL as a Duration because it is an elapsed span; MCP
// puts milliseconds on the wire, so the conversion happens here. Rounding is
// toward zero, so a sub-millisecond TTL becomes 0 — immediately stale — rather
// than being rounded up into a cache window the author did not ask for.
func cacheHint(h *mcppb.MCPCacheHint) *MCPCacheOpts {
	if h == nil {
		return nil
	}
	return &MCPCacheOpts{
		TTLMs: h.GetTtl().AsDuration().Milliseconds(),
		Scope: cacheScopeNames[h.GetScope()],
	}
}
