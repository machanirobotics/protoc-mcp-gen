// Copyright 2026 The Protobuf Project authors.
// SPDX-License-Identifier: Apache-2.0

package generator

import (
	"strings"
	"testing"

	"github.com/the-protobuf-project/protokit/factory"
	"google.golang.org/protobuf/compiler/protogen"
)

func TestProtoSourceBuildSelectsGenerateFiles(t *testing.T) {
	want := []*protogen.File{{Generate: true}, {Generate: true}}
	plugin := &protogen.Plugin{
		Files: []*protogen.File{want[0], {Generate: false}, want[1]},
	}

	m, err := ProtoSource{}.Build(factory.Ctx{Plugin: plugin})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(m.Files) != len(want) {
		t.Fatalf("got %d files, want %d", len(m.Files), len(want))
	}
	for i, f := range m.Files {
		if f != want[i] {
			t.Errorf("file %d: got %p, want %p (order must follow protoc's)", i, f, want[i])
		}
	}
}

func TestProtoSourceBuildRequiresPlugin(t *testing.T) {
	if _, err := (ProtoSource{}).Build(factory.Ctx{}); err == nil {
		t.Fatal("Build with no plugin: got nil error, want one")
	}
}

func TestTargetGenerateRequiresPlugin(t *testing.T) {
	if err := (Target{}).Generate(factory.Ctx{}, &Model{}, LangGo); err == nil {
		t.Fatal("Generate with no plugin: got nil error, want one")
	}
}

func TestTargetGenerateRejectsUnknownLanguage(t *testing.T) {
	ctx := factory.Ctx{Plugin: &protogen.Plugin{}}
	err := Target{}.Generate(ctx, &Model{}, "haskell")
	if err == nil {
		t.Fatal("Generate with unknown language: got nil error, want one")
	}
	if !strings.Contains(err.Error(), "haskell") {
		t.Errorf("error %q does not name the offending language", err)
	}
}

func TestResolveLanguages(t *testing.T) {
	tgt := Target{}
	tests := []struct {
		name    string
		lang    string
		want    []string
		wantErr bool
	}{
		{name: "all expands to every supported language", lang: LangAll, want: tgt.Languages()},
		{name: "single language", lang: LangRust, want: []string{LangRust}},
		{name: "cpp is selectable on its own", lang: LangCpp, want: []string{LangCpp}},
		{name: "unsupported", lang: "haskell", wantErr: true},
		{name: "empty", lang: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveLanguages(tgt, tt.lang)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveLanguages(%q): got nil error, want one", tt.lang)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveLanguages(%q): %v", tt.lang, err)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("resolveLanguages(%q) = %v, want %v", tt.lang, got, tt.want)
			}
		})
	}
}

func TestRegistryHoldsSourceAndTarget(t *testing.T) {
	reg := Registry("pbv1")

	src, ok := reg.Sources["proto"]
	if !ok {
		t.Fatalf("no %q source registered", "proto")
	}
	if src.Name() != "proto" {
		t.Errorf("source name = %q, want %q", src.Name(), "proto")
	}

	tgt, ok := reg.Targets["mcp"]
	if !ok {
		t.Fatalf("no %q target registered (have: %s)", "mcp", reg.TargetNames())
	}
	if tgt.Name() != "mcp" {
		t.Errorf("target name = %q, want %q", tgt.Name(), "mcp")
	}
	if got := tgt.(Target).PackageSuffix; got != "pbv1" {
		t.Errorf("PackageSuffix = %q, want %q", got, "pbv1")
	}
}

func TestGenerateRejectsUnknownLanguage(t *testing.T) {
	if err := Generate(&protogen.Plugin{}, "haskell", ""); err == nil {
		t.Fatal("Generate with unknown language: got nil error, want one")
	}
}
