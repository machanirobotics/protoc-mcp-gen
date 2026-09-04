// Copyright 2026 The Protobuf Project authors.
// SPDX-License-Identifier: Apache-2.0

package generator

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"path"

	"google.golang.org/protobuf/compiler/protogen"
)

// goSharedBasename is the stem of the per-package shared file, chosen not to
// collide with a name protoc-gen-go would derive from a real proto. A proto
// actually named mcp_shared.proto is rejected rather than silently overwritten;
// see [GenerateGoShared].
const goSharedBasename = "mcp_shared"

// goSharedParams is the data the shared-file template needs. It carries no
// per-proto information by construction: anything file-specific belongs in the
// per-file template, not in a file the whole package shares.
type goSharedParams struct {
	Version   string
	GoPackage string
}

// PackageOutput reports the Go package the file was generated into. ok is false
// when Generate emitted nothing, either because the proto declares no services
// or because generation failed.
func (g *FileGenerator) PackageOutput() (GoPackageOutput, bool) {
	return g.pkg, g.generated
}

// GenerateGoShared emits the helpers every *.pb.mcp.go in pkg calls, as a single
// file for the whole package.
//
// The helpers used to be emitted into each per-file output. That compiles only
// while a Go package holds one service-bearing proto, which is what every
// example here happens to do; a package whose services are split across two
// protos got two copies of newTool, boolPtr and structuredResult and failed to
// build with a redeclaration error naming generated files the author never
// wrote. Emitting them once per package makes the number of protos in a package
// irrelevant.
//
// The output path is a pure function of the package, so a partial regeneration
// — protoc run over one proto of a package, as a per-file Makefile rule does —
// rewrites the same file with the same content rather than moving the helpers
// into whichever file happened to be generated.
//
// siblings are the generated-file stems of the package's protos, used only to
// reject a proto whose own output would claim this file's name.
func GenerateGoShared(gen *protogen.Plugin, pkg GoPackageOutput, siblings []string) {
	for _, base := range siblings {
		if base != goSharedBasename {
			continue
		}
		gen.Error(fmt.Errorf(
			"%s: a proto named %s.proto collides with the shared file protoc-gen-mcp emits for package %s; rename the proto",
			path.Join(pkg.Dir, goSharedBasename+generatedFilenameExtension), goSharedBasename, pkg.ImportPath))
		return
	}

	outPath := path.Join(pkg.Dir, goSharedBasename+generatedFilenameExtension)

	var buf bytes.Buffer
	params := goSharedParams{Version: PluginVersion, GoPackage: pkg.PackageName}
	if err := goSharedTemplate.Execute(&buf, params); err != nil {
		gen.Error(err)
		return
	}

	// Validate generated Go source is syntactically correct, as the per-file
	// output is.
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "", buf.Bytes(), parser.AllErrors); err != nil {
		gen.Error(fmt.Errorf("%s: unparsable Go source: %v", outPath, err))
		return
	}

	gf := gen.NewGeneratedFile(outPath, pkg.ImportPath)
	if _, err := gf.Write(buf.Bytes()); err != nil {
		gen.Error(err)
	}
}
