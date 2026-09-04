// Copyright 2026 The Protobuf Project authors.
// SPDX-License-Identifier: Apache-2.0

package generator

// schema_wkt.go contains JSON Schema converters for protobuf map fields
// and well-known types (WKTs) such as Timestamp, Duration, Struct, etc.

import "google.golang.org/protobuf/reflect/protoreflect"

// mapSchema handles protobuf map<K,V> fields.
// mapSchemaPath carries the recursion path into the map's value type, which can
// itself be a message that reaches back to an enclosing one.
func mapSchemaPath(fd protoreflect.FieldDescriptor, openAI bool, path map[protoreflect.FullName]bool) map[string]any {
	keyConstraints := map[string]any{"type": "string"}
	switch fd.MapKey().Kind() {
	case protoreflect.BoolKind:
		keyConstraints["enum"] = []string{"true", "false"}
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		keyConstraints["pattern"] = `^(0|[1-9]\d*)$`
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		keyConstraints["pattern"] = `^-?(0|[1-9]\d*)$`
	}

	if openAI {
		return map[string]any{
			"type": "array", "description": "List of key-value pairs",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key":   map[string]any{"type": "string"},
					"value": map[string]any{"type": fieldSchemaPath(fd.MapValue(), openAI, path)["type"]},
				},
				"required": []string{"key", "value"}, "additionalProperties": false,
			},
		}
	}
	return map[string]any{
		"type": "object", "propertyNames": keyConstraints,
		"additionalProperties": fieldSchemaPath(fd.MapValue(), openAI, path),
	}
}

// messageFieldSchema handles message-typed fields including well-known types.
func messageFieldSchemaPath(fd protoreflect.FieldDescriptor, openAI bool, path map[protoreflect.FullName]bool) map[string]any {
	switch fullName := string(fd.Message().FullName()); fullName {
	case "google.protobuf.Timestamp":
		return map[string]any{"type": []string{"string", "null"}, "format": "date-time"}
	case "google.protobuf.Duration":
		return map[string]any{"type": []string{"string", "null"}, "pattern": `^-?[0-9]+(\.[0-9]+)?s$`}
	case "google.protobuf.Struct":
		if openAI {
			return map[string]any{"type": "string", "description": "JSON-encoded object (google.protobuf.Struct)."}
		}
		return map[string]any{"type": "object", "additionalProperties": true}
	case "google.protobuf.Value":
		if openAI {
			return map[string]any{"type": "string", "description": "JSON-encoded value (google.protobuf.Value)."}
		}
		return map[string]any{"description": "Dynamic JSON value (google.protobuf.Value)."}
	case "google.protobuf.ListValue":
		if openAI {
			return map[string]any{"type": "string", "description": "JSON-encoded array (google.protobuf.ListValue)."}
		}
		return map[string]any{"type": "array", "description": "JSON array of values (google.protobuf.ListValue).", "items": map[string]any{}}
	case "google.protobuf.FieldMask":
		if openAI {
			return map[string]any{"type": []string{"string", "null"}}
		}
		return map[string]any{"type": "string"}
	case "google.protobuf.Any":
		s := map[string]any{
			"type":       "object",
			"properties": map[string]any{"@type": map[string]any{"type": "string"}, "value": map[string]any{}},
			"required":   []string{"@type"},
		}
		if !openAI {
			s["type"] = []string{"object", "null"}
		}
		return s
	case "google.protobuf.DoubleValue", "google.protobuf.FloatValue",
		"google.protobuf.Int32Value", "google.protobuf.UInt32Value":
		return map[string]any{"type": "number", "nullable": true}
	case "google.protobuf.Int64Value", "google.protobuf.UInt64Value":
		return map[string]any{"type": "string", "nullable": true}
	case "google.protobuf.StringValue":
		return map[string]any{"type": "string", "nullable": true}
	case "google.protobuf.BoolValue":
		return map[string]any{"type": "boolean", "nullable": true}
	case "google.protobuf.BytesValue":
		s := map[string]any{"type": "string", "nullable": true}
		if !openAI {
			s["format"] = "byte"
		}
		return s
	default:
		return messageSchemaPath(fd.Message(), openAI, "", path)
	}
}
