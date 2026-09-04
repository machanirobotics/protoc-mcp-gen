// Copyright 2026 The Protobuf Project authors.
// SPDX-License-Identifier: Apache-2.0

package generator

import (
	"io/fs"
	"strconv"
	"strings"
	"text/template"

	"github.com/the-protobuf-project/mcp/plugin/generator/templates"
)

// Supported languages for code generation.
const (
	LangGo   = "go"
	LangRust = "rust"
	LangCpp  = "cpp"
)

// escapeQuotes escapes s for embedding between the double quotes of a generated
// string literal. Every language's func map exposes it as "escapeQuotes".
//
// Escaping only the quote character is not enough: proto option values are
// arbitrary text, and a backslash in one is what a Go, Rust, or C++ compiler
// reads as the start of an escape sequence. `C:\path` would not parse, a
// trailing backslash would swallow the closing quote, and — worst of the three
// because nothing reports it — `a\tb` would compile with a real tab in place of
// the two characters the proto actually said.
//
// strconv.Quote produces exactly the escaping a Go literal needs, including
// control and non-printable runes; the surrounding quotes it adds are stripped
// because the templates supply their own. Rust and C++ string literals share Go's
// escape syntax for every sequence this can emit.
func escapeQuotes(s string) string {
	quoted := strconv.Quote(s)
	return quoted[1 : len(quoted)-1]
}

// safeRawString wraps s in a backtick raw-string literal. If s itself contains a
// backtick (e.g. from Markdown code spans in proto comments), it splits on
// backticks and emits a concatenation expression so the generated source is
// still valid Go syntax. Example: "foo`bar" -> `foo` + "`" + `bar`
func safeRawString(s string) string {
	if !strings.Contains(s, "`") {
		return "`" + s + "`"
	}
	parts := strings.Split(s, "`")
	var segments []string
	for i, p := range parts {
		if i > 0 {
			segments = append(segments, `"`+"`"+`"`)
		}
		if p != "" {
			segments = append(segments, "`"+p+"`")
		}
	}
	return strings.Join(segments, " + ")
}

// Per-language template func maps. They are stateless, so they are built once
// here rather than per generated file.
var (
	goFuncMap = template.FuncMap{
		"backtick":      func() string { return "`" },
		"escapeQuotes":  escapeQuotes,
		"safeRawString": safeRawString,
	}

	rustFuncMap = template.FuncMap{
		"snakeCase":          toSnakeCase,
		"screamingSnakeCase": toScreamingSnakeCase,
		"lower":              strings.ToLower,
		"rsEscape":           rsStringEscape,
		"escapeQuotes":       escapeQuotes,
	}

	cppFuncMap = template.FuncMap{
		"snakeCase":          toSnakeCase,
		"screamingSnakeCase": toScreamingSnakeCase,
		"rsEscape":           rsStringEscape,
		"cppEscape":          cppStringEscape,
		"escapeQuotes":       escapeQuotes,
	}
)

// Templates are parsed once at package initialization. Parsing is independent
// of the file being generated, so doing it per file re-parsed the same source
// for every proto in the request.
//
// protokit's templates.MustParse is not used here: it parses via template.ParseFS
// without a FuncMap, and text/template rejects a template referencing an unknown
// function at parse time, so the funcs above must be registered first.
var (
	goTemplate       = mustParseTemplate("gen", "go.tpl", goFuncMap)
	goSharedTemplate = mustParseTemplate("gensh", "go_shared.tpl", goFuncMap)
	rustTemplate     = mustParseTemplate("rsgen", "rust.tpl", rustFuncMap)
	cppTemplates     = mustParseGlob("cpp/*.tpl", cppFuncMap)
)

// mustParseTemplate parses a single embedded template. It panics on failure, so
// a malformed template fails at startup rather than mid-generation.
func mustParseTemplate(name, path string, funcs template.FuncMap) *template.Template {
	b, err := templates.FS.ReadFile(path)
	if err != nil {
		panic("generator: embedded template " + path + " not found: " + err.Error())
	}
	tpl, err := template.New(name).Funcs(funcs).Parse(string(b))
	if err != nil {
		panic("generator: parse embedded template " + path + ": " + err.Error())
	}
	return tpl
}

// mustParseGlob parses every embedded template matching pattern, keyed by its
// embedded path (e.g. "cpp/handler.tpl").
func mustParseGlob(pattern string, funcs template.FuncMap) map[string]*template.Template {
	paths, err := fs.Glob(templates.FS, pattern)
	if err != nil {
		panic("generator: glob embedded templates " + pattern + ": " + err.Error())
	}
	if len(paths) == 0 {
		panic("generator: no embedded templates match " + pattern)
	}
	out := make(map[string]*template.Template, len(paths))
	for _, p := range paths {
		out[p] = mustParseTemplate(p, p, funcs)
	}
	return out
}
