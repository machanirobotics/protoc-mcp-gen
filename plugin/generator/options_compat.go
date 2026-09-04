// Copyright 2026 The Protobuf Project authors.
// SPDX-License-Identifier: Apache-2.0

package generator

import (
	mcppb "github.com/the-protobuf-project/mcp/protobuf/mcppb"
	"google.golang.org/protobuf/encoding/protowire"
)

// legacyFormatFieldNumber is MCPFieldOptions.format. The field held a free-form
// string before it became the MCPFieldFormat enum.
const legacyFormatFieldNumber = 4

// legacyFieldFormat recovers a format keyword written against the pre-enum
// schema.
//
// Changing MCPFieldOptions.format from string to enum changed its wire type
// from bytes to varint, so protobuf-go cannot decode the old value into the new
// field and parks it in unknown fields instead. Protos annotated against a
// published schema older than that change are still common — the plugin ships
// ahead of the registry — and silently dropping their format keyword would be a
// regression, so read it back out.
//
// Returns "" when no legacy value is present.
func legacyFieldFormat(opts *mcppb.MCPFieldOptions) string {
	if opts == nil {
		return ""
	}
	unknown := opts.ProtoReflect().GetUnknown()
	for len(unknown) > 0 {
		num, typ, n := protowire.ConsumeTag(unknown)
		if n < 0 {
			return ""
		}
		unknown = unknown[n:]
		if num == legacyFormatFieldNumber && typ == protowire.BytesType {
			value, n := protowire.ConsumeBytes(unknown)
			if n < 0 {
				return ""
			}
			return string(value)
		}
		n = protowire.ConsumeFieldValue(num, typ, unknown)
		if n < 0 {
			return ""
		}
		unknown = unknown[n:]
	}
	return ""
}
