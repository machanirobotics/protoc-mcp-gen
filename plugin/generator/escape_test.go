// Copyright 2026 The Protobuf Project authors.
// SPDX-License-Identifier: Apache-2.0

package generator

import (
	"fmt"
	"go/ast"
	"go/parser"
	"strconv"
	"testing"
)

// Proto option values are arbitrary text, and they are emitted straight into
// generated string literals. Escaping only the quote character leaves two ways
// to produce a file that will not build and — worse — one way to produce a file
// that builds with the wrong value.
func TestEscapeQuotesProducesAValidGoLiteral(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"windows path", `C:\path\to\file`},
		{"trailing backslash", `ends with a backslash\`},
		{"escape-like sequence", `a\tb`},
		{"embedded quote", `say "hello"`},
		{"newline", "first\nsecond"},
		{"tab", "col\tcol"},
		{"carriage return", "line\r"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := `"` + escapeQuotes(tt.value) + `"`
			lit, err := parser.ParseExpr(src)
			if err != nil {
				t.Fatalf("escaped %q into %s, which is not valid Go: %v", tt.value, src, err)
			}
			// Valid syntax is not enough: `a\tb` parses but must not become a tab.
			got, err := literalValue(lit)
			if err != nil {
				t.Fatalf("unquote %s: %v", src, err)
			}
			if got != tt.value {
				t.Errorf("round trip changed the value: got %q, want %q", got, tt.value)
			}
		})
	}
}

// The Rust and C++ escaper shares Go's escape syntax for everything it emits,
// so the same round-trip check applies.
func TestLiteralEscapeRoundTrips(t *testing.T) {
	for _, value := range []string{
		`C:\path`, `trailing\`, `a\tb`, `say "hello"`, "first\nsecond", "col\tcol", "line\r",
	} {
		src := `"` + literalEscape(value) + `"`
		lit, err := parser.ParseExpr(src)
		if err != nil {
			t.Errorf("escaped %q into %s, which is not a valid literal: %v", value, src, err)
			continue
		}
		got, err := literalValue(lit)
		if err != nil {
			t.Errorf("unquote %s: %v", src, err)
			continue
		}
		if got != value {
			t.Errorf("round trip changed the value: got %q, want %q", got, value)
		}
	}
}

// literalValue unquotes a parsed Go string literal back to its value.
func literalValue(expr ast.Expr) (string, error) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok {
		return "", fmt.Errorf("expression is %T, want *ast.BasicLit", expr)
	}
	return strconv.Unquote(lit.Value)
}
