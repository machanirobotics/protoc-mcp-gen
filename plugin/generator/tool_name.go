// Copyright 2026 The Protobuf Project authors.
// SPDX-License-Identifier: Apache-2.0

package generator

import (
	"crypto/sha1"
	"math/big"
	"regexp"
	"strings"
)

const maxToolNameLen = 64

var versionRe = regexp.MustCompile(`^v\d+$`)

// BuildToolName produces a compact, lowercase MCP tool name in the format
// servicename_methodname_v{version} from the protobuf full method name
// (e.g. "store.apps.utilities.clock.v1.ClockService.ConvertTime").
// The result is always lowercase and capped at 64 characters.
func BuildToolName(fullName string) string {
	parts := strings.Split(fullName, ".")
	if len(parts) < 2 {
		// Fallback: lowercase the whole thing with underscores.
		name := strings.ToLower(strings.ReplaceAll(fullName, ".", "_"))
		return MangleHeadIfTooLong(name, maxToolNameLen)
	}

	methodName := parts[len(parts)-1]
	svcName := parts[len(parts)-2]

	// Find version segment (e.g. "v1") by scanning from the end.
	version := ""
	for i := len(parts) - 3; i >= 0; i-- {
		if versionRe.MatchString(parts[i]) {
			version = parts[i]
			break
		}
	}

	// Convert CamelCase to snake_case for readability.
	svcSnake := toSnakeCase(svcName)
	methSnake := toSnakeCase(methodName)

	var name string
	if version != "" {
		name = svcSnake + "-" + methSnake + "_" + version
	} else {
		name = svcSnake + "-" + methSnake
	}

	return MangleHeadIfTooLong(name, maxToolNameLen)
}

// MangleHeadIfTooLong truncates the head of name and prepends a short hash
// when name exceeds maxLen.  The tail (most-specific part) is preserved.
func MangleHeadIfTooLong(name string, maxLen int) string {
	if len(name) <= maxLen {
		return name
	}
	hash := sha1.Sum([]byte(name))
	prefix := base36(hash[:])[:6]
	available := maxLen - len(prefix) - 1
	if available <= 0 {
		return prefix
	}
	return prefix + "_" + name[len(name)-available:]
}

func base36(b []byte) string {
	n := new(big.Int).SetBytes(b)
	return n.Text(36)
}
