// Copyright 2026 The Protobuf Project authors.
// SPDX-License-Identifier: Apache-2.0

package generator

import (
	"fmt"
	"strings"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	mcppb "github.com/the-protobuf-project/mcp/protobuf/mcppb"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// kindToType maps a protobuf scalar kind to a JSON Schema type string.
func kindToType(kind protoreflect.Kind) string {
	switch kind {
	case protoreflect.BoolKind:
		return "boolean"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return "integer"
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return "number"
	default: // string, bytes, enum, int64 variants (JSON-encoded as strings)
		return "string"
	}
}

// isFieldRequired checks whether a field has REQUIRED google.api.field_behavior.
func isFieldRequired(fd protoreflect.FieldDescriptor) bool {
	if !proto.HasExtension(fd.Options(), annotations.E_FieldBehavior) {
		return false
	}
	behaviors := proto.GetExtension(fd.Options(), annotations.E_FieldBehavior).([]annotations.FieldBehavior)
	for _, b := range behaviors {
		if b == annotations.FieldBehavior_REQUIRED {
			return true
		}
	}
	return false
}

// extractValidateConstraints reads buf.validate.field rules and returns JSON Schema constraints.
func extractValidateConstraints(fd protoreflect.FieldDescriptor) map[string]any {
	constraints := make(map[string]any)
	if !proto.HasExtension(fd.Options(), validate.E_Field) {
		return constraints
	}
	rules := proto.GetExtension(fd.Options(), validate.E_Field).(*validate.FieldRules)
	if rules == nil {
		return constraints
	}

	if sr := rules.GetString(); sr != nil {
		if sr.GetUuid() {
			constraints["format"] = "uuid"
		}
		if sr.GetEmail() {
			constraints["format"] = "email"
		}
		if p := sr.GetPattern(); p != "" {
			constraints["pattern"] = p
		}
		if sr.HasMinLen() {
			constraints["minLength"] = int(sr.GetMinLen())
		}
		if sr.HasMaxLen() {
			constraints["maxLength"] = int(sr.GetMaxLen())
		}
	}

	applyIntRange := func(hasGt bool, gt int, hasGte bool, gte int, hasLt bool, lt int, hasLte bool, lte int) {
		if hasGt {
			constraints["minimum"] = gt + 1
		} else if hasGte {
			constraints["minimum"] = gte
		}
		if hasLt {
			constraints["maximum"] = lt - 1
		} else if hasLte {
			constraints["maximum"] = lte
		}
	}
	if r := rules.GetInt32(); r != nil {
		applyIntRange(r.HasGt(), int(r.GetGt()), r.HasGte(), int(r.GetGte()), r.HasLt(), int(r.GetLt()), r.HasLte(), int(r.GetLte()))
	}
	if r := rules.GetInt64(); r != nil {
		applyIntRange(r.HasGt(), int(r.GetGt()), r.HasGte(), int(r.GetGte()), r.HasLt(), int(r.GetLt()), r.HasLte(), int(r.GetLte()))
	}

	return constraints
}

// messageSchema converts a protobuf message descriptor into a JSON Schema map.
// If schemaDesc is non-empty, it is set as the root-level description (per MCP inputSchema convention).
func messageSchema(md protoreflect.MessageDescriptor, openAI bool, schemaDesc string) map[string]any {
	return messageSchemaPath(md, openAI, schemaDesc, map[protoreflect.FullName]bool{})
}

// messageSchemaPath is messageSchema carrying the set of message types already
// open on the current branch.
//
// A self-referential message — `Node { repeated Node children }`, the shape of
// every tree and linked list — would otherwise recurse until the stack gives
// out. JSON Schema cannot express an infinitely nested object literally, so at
// the point the cycle closes the walk stops and emits a permissive object,
// which is true (the value is an object) without claiming to enumerate it.
func messageSchemaPath(md protoreflect.MessageDescriptor, openAI bool, schemaDesc string, path map[protoreflect.FullName]bool) map[string]any {
	if path[md.FullName()] {
		return map[string]any{
			"type":                 "object",
			"additionalProperties": true,
			"description": fmt.Sprintf(
				"Recursive reference to %s; nested occurrences are not expanded.", md.FullName()),
		}
	}
	path[md.FullName()] = true
	defer delete(path, md.FullName())

	required, props := []string{}, map[string]any{}
	oneOfGroups := map[string][]map[string]any{}
	for i := 0; i < md.Fields().Len(); i++ {
		fd := md.Fields().Get(i)
		name := string(fd.Name())
		if oo := fd.ContainingOneof(); oo != nil && !oo.IsSynthetic() {
			if !openAI {
				key := string(oo.Name())
				oneOfGroups[key] = append(oneOfGroups[key], map[string]any{
					"properties": map[string]any{name: fieldSchemaPath(fd, openAI, path)}, "required": []string{name},
				})
			} else {
				s := fieldSchemaPath(fd, openAI, path)
				if t, ok := s["type"].(string); ok {
					s["type"] = []string{t, "null"}
				}
				s["description"] = fmt.Sprintf("Note: Part of the '%s' oneof group. Only one field in this group can be set. Setting multiple fields WILL result in an error.", oo.Name())
				props[name] = s
				required = append(required, name)
			}
		} else {
			props[name] = fieldSchemaPath(fd, openAI, path)
			if isFieldRequired(fd) || openAI {
				required = append(required, name)
			}
		}
	}
	result := map[string]any{"type": "object", "properties": props, "required": required}
	if schemaDesc != "" {
		result["description"] = schemaDesc
	}
	if len(oneOfGroups) > 0 {
		var anyOf []map[string]any
		for _, entries := range oneOfGroups {
			anyOf = append(anyOf, map[string]any{"oneOf": entries, "$comment": "Protobuf oneOf group."})
		}
		result["anyOf"] = anyOf
	}
	if openAI {
		result["additionalProperties"] = false
		if t, ok := result["type"].(string); ok {
			result["type"] = []string{t, "null"}
		}
	}
	return result
}

// getFieldDescription returns the field description from (mcp.field) if set,
// otherwise the fallback (e.g. from the leading comment).
func getFieldDescription(fd protoreflect.FieldDescriptor, fallback string) string {
	if proto.HasExtension(fd.Options(), mcppb.E_Field) {
		opts := proto.GetExtension(fd.Options(), mcppb.E_Field).(*mcppb.MCPFieldOptions)
		if opts != nil && opts.Description != "" {
			return opts.Description
		}
	}
	return fallback
}

// applyMCPFieldOptions applies (mcp.field) options to the schema.
// Format from MCPFieldOptions overrides any from buf.validate.
// For enum fields with (mcp.enum) or (mcp.enum_value), enum descriptions take precedence over field description.
func applyMCPFieldOptions(fd protoreflect.FieldDescriptor, schema map[string]any, descFallback string) {
	if !proto.HasExtension(fd.Options(), mcppb.E_Field) {
		if descFallback != "" {
			schema["description"] = descFallback
		}
		return
	}
	opts := proto.GetExtension(fd.Options(), mcppb.E_Field).(*mcppb.MCPFieldOptions)
	if opts == nil {
		return
	}
	// For enum fields, prefer enum-level and enum-value descriptions over field description
	if fd.Kind() == protoreflect.EnumKind && schema["description"] != nil && schema["description"] != "" {
		// Enum descriptions already set by enumSchema; skip field description
	} else if opts.Description != "" {
		schema["description"] = opts.Description
	} else if descFallback != "" {
		schema["description"] = descFallback
	}
	if len(opts.Examples) > 0 {
		examples := make([]any, len(opts.Examples))
		for i, e := range opts.Examples {
			examples[i] = e
		}
		schema["examples"] = examples
	}
	if opts.Deprecated {
		schema["deprecated"] = true
	}
	format := fieldFormatKeyword(opts.GetFormat())
	if format == "" {
		format = legacyFieldFormat(opts)
	}
	if format != "" {
		schema["format"] = format
	}
	// An explicit type overrides the one inferred from the proto field, for
	// fields whose wire type does not match their intended schema type (e.g. an
	// int64 that should appear as "string" in JSON).
	//
	// Inferred numeric bounds survive a numeric-to-numeric override, where they
	// still describe the value; they are dropped only when the value stops being
	// a number, because "minimum" against a string is meaningless.
	if typ := fieldTypeKeyword(opts.GetType()); typ != "" && schema["type"] != typ {
		schema["type"] = typ
		if typ != "integer" && typ != "number" {
			delete(schema, "minimum")
			delete(schema, "maximum")
		}
	}
}

// fieldSchemaPath converts a single protobuf field descriptor to a JSON Schema
// map, carrying the set of message types already open on this branch so a
// recursive field can be cut rather than followed.
func fieldSchemaPath(fd protoreflect.FieldDescriptor, openAI bool, path map[protoreflect.FullName]bool) map[string]any {
	if fd.IsMap() {
		return mapSchemaPath(fd, openAI, path)
	}
	var schema map[string]any
	switch fd.Kind() {
	case protoreflect.MessageKind:
		schema = messageFieldSchemaPath(fd, openAI, path)
	case protoreflect.EnumKind:
		schema = enumSchema(fd)
	default:
		schema = scalarSchema(fd, openAI)
	}
	for k, v := range extractValidateConstraints(fd) {
		schema[k] = v
	}
	applyMCPFieldOptions(fd, schema, "")
	if fd.IsList() {
		return map[string]any{"type": "array", "items": schema}
	}
	return schema
}

// enumDescriptions holds enum-level and per-value descriptions for schema output.
type enumDescriptions struct {
	enumDesc string
	values   map[string]string // value name -> description
}

func getEnumDescriptions(ed protoreflect.EnumDescriptor) enumDescriptions {
	out := enumDescriptions{values: make(map[string]string)}
	if proto.HasExtension(ed.Options(), mcppb.E_Enum) {
		opts := proto.GetExtension(ed.Options(), mcppb.E_Enum).(*mcppb.MCPEnumOptions)
		if opts != nil && opts.Description != "" {
			out.enumDesc = opts.Description
		}
	}
	vals := ed.Values()
	for i := 0; i < vals.Len(); i++ {
		vd := vals.Get(i)
		if proto.HasExtension(vd.Options(), mcppb.E_EnumValue) {
			opts := proto.GetExtension(vd.Options(), mcppb.E_EnumValue).(*mcppb.MCPEnumValueOptions)
			if opts != nil && opts.Description != "" {
				out.values[string(vd.Name())] = opts.Description
			}
		}
	}
	return out
}

// enumSchema returns a JSON Schema for a protobuf enum field.
func enumSchema(fd protoreflect.FieldDescriptor) map[string]any {
	ed := fd.Enum()
	vals := make([]string, ed.Values().Len())
	for i := range vals {
		vals[i] = string(ed.Values().Get(i).Name())
	}
	schema := map[string]any{"type": "string", "enum": vals}
	descs := getEnumDescriptions(ed)
	if descs.enumDesc != "" || len(descs.values) > 0 {
		var parts []string
		if descs.enumDesc != "" {
			parts = append(parts, strings.TrimSuffix(descs.enumDesc, "."))
		}
		for _, v := range vals {
			if d, ok := descs.values[v]; ok {
				parts = append(parts, fmt.Sprintf("%s: %s", v, d))
			}
		}
		schema["description"] = strings.Join(parts, ". ")
		if len(descs.values) > 0 {
			schema["enumDescriptions"] = descs.values
		}
	}
	return schema
}

// scalarSchema returns a JSON Schema for a protobuf scalar field.
func scalarSchema(fd protoreflect.FieldDescriptor, openAI bool) map[string]any {
	s := map[string]any{"type": kindToType(fd.Kind())}
	if fd.Kind() == protoreflect.BytesKind {
		s["contentEncoding"] = "base64"
		if !openAI {
			s["format"] = "byte"
		}
	}
	return s
}

var strippedCommentPrefixes = []string{"buf:lint:", "@ignore-comment"}

// CleanComment strips annotation prefixes that should not appear in MCP tool descriptions.
func CleanComment(comment string) string {
	var out []string
outer:
	for _, line := range strings.Split(comment, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, prefix := range strippedCommentPrefixes {
			if strings.HasPrefix(trimmed, prefix) {
				continue outer
			}
		}
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return strings.TrimSpace(strings.Join(out, " "))
}
