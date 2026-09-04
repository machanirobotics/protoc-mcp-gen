// Copyright 2026 The Protobuf Project authors.
// SPDX-License-Identifier: Apache-2.0

package generator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"slices"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// goServiceFile describes one proto file for goFiles. Unlike serviceFile it
// carries the Go import path, because what this file tests is what happens when
// two protos share one.
type goServiceFile struct {
	path       string
	pkg        string
	service    string
	goPackage  string // e.g. "example.com/gen/estate/v1;estatev1"
	sourceName string // proto file stem, used to keep message names unique
}

// goFiles compiles specs into the *protogen.File values generateGo works on.
//
// Message names are qualified by the file stem: two protos in one proto package
// may not both declare a Req, and the whole point here is to put two protos in
// one package.
func goFiles(t *testing.T, specs ...goServiceFile) *protogen.Plugin {
	t.Helper()

	req := &pluginpb.CodeGeneratorRequest{}
	for _, spec := range specs {
		stem := spec.sourceName
		req.FileToGenerate = append(req.FileToGenerate, spec.path)
		req.ProtoFile = append(req.ProtoFile, &descriptorpb.FileDescriptorProto{
			Name:    proto.String(spec.path),
			Package: proto.String(spec.pkg),
			Syntax:  proto.String("proto3"),
			MessageType: []*descriptorpb.DescriptorProto{
				{Name: proto.String(stem + "Req")},
				{Name: proto.String(stem + "Resp")},
			},
			Service: []*descriptorpb.ServiceDescriptorProto{{
				Name: proto.String(spec.service),
				Method: []*descriptorpb.MethodDescriptorProto{{
					Name:       proto.String("Do"),
					InputType:  proto.String("." + spec.pkg + "." + stem + "Req"),
					OutputType: proto.String("." + spec.pkg + "." + stem + "Resp"),
				}},
			}},
			Options: &descriptorpb.FileOptions{GoPackage: proto.String(spec.goPackage)},
		})
	}

	plugin, err := protogen.Options{}.New(req)
	if err != nil {
		t.Fatalf("build plugin: %v", err)
	}
	return plugin
}

// runGoTarget generates plugin's files and returns the response's files keyed by
// name. It fails the test if generation reported an error.
func runGoTarget(t *testing.T, plugin *protogen.Plugin, packageSuffix string) map[string]string {
	t.Helper()

	model := &Model{}
	for _, f := range plugin.Files {
		if f.Generate {
			model.Files = append(model.Files, f)
		}
	}
	Target{PackageSuffix: packageSuffix}.generateGo(plugin, model)

	resp := plugin.Response()
	if resp.Error != nil {
		t.Fatalf("generate: %s", resp.GetError())
	}
	out := make(map[string]string, len(resp.File))
	for _, f := range resp.File {
		if _, dup := out[f.GetName()]; dup {
			t.Fatalf("response contains %s twice", f.GetName())
		}
		out[f.GetName()] = f.GetContent()
	}
	return out
}

// topLevelFuncs returns the package-level function names declared in src.
func topLevelFuncs(t *testing.T, name, src string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	var funcs []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		funcs = append(funcs, fn.Name.Name)
	}
	return funcs
}

// A Go package holding two service-bearing protos generates two files, and
// before the helpers moved into a per-package file each one declared its own
// newTool, boolPtr and structuredResult. That is a redeclaration: the package
// did not compile, and the error named generated files rather than anything the
// author wrote. Every example in this repo has one service per package, which
// is why nothing here caught it.
func TestGoHelpersAreDeclaredOncePerPackage(t *testing.T) {
	plugin := goFiles(t,
		goServiceFile{
			path: "estate/v1/property_service.proto", pkg: "estate.v1",
			service: "PropertyService", sourceName: "Property",
			goPackage: "example.com/gen/estate/v1;estatev1",
		},
		goServiceFile{
			path: "estate/v1/licence_service.proto", pkg: "estate.v1",
			service: "LicenceService", sourceName: "Licence",
			goPackage: "example.com/gen/estate/v1;estatev1",
		},
	)

	out := runGoTarget(t, plugin, "")

	helpers := []string{"newTool", "boolPtr", "structuredResult"}

	counts := make(map[string]int)
	for name, src := range out {
		for _, fn := range topLevelFuncs(t, name, src) {
			counts[fn]++
		}
	}
	for _, helper := range helpers {
		if counts[helper] != 1 {
			t.Errorf("%s declared %d times across the package, want 1", helper, counts[helper])
		}
	}

	// The declaration lives in the shared file, not in one of the per-proto
	// files: a package regenerated one proto at a time must converge on the same
	// output rather than moving the helpers to whichever file was generated last.
	//
	// Counted within the shared file, not just across the package: a single
	// declaration sitting in property_service.pb.mcp.go satisfies the totals
	// above while leaving the shared file empty, which is the arrangement this
	// is here to rule out.
	shared := "example.com/gen/estate/v1/mcp_shared.pb.mcp.go"
	src, ok := out[shared]
	if !ok {
		t.Fatalf("no shared file at %s; generated: %s", shared, strings.Join(sortedKeys(out), ", "))
	}
	sharedCounts := make(map[string]int)
	for _, fn := range topLevelFuncs(t, shared, src) {
		sharedCounts[fn]++
	}
	for _, helper := range helpers {
		if sharedCounts[helper] != 1 {
			t.Errorf("shared file declares %s %d times, want 1", helper, sharedCounts[helper])
		}
	}
	for fn := range sharedCounts {
		if !slices.Contains(helpers, fn) {
			t.Errorf("shared file declares unexpected func %s", fn)
		}
	}
}

// One shared file per Go package, not per request: a request spanning two
// packages — what `buf generate` over a whole module produces — needs the
// helpers in both, since neither package can reference the other's.
func TestGoSharedFileIsEmittedForEveryPackage(t *testing.T) {
	plugin := goFiles(t,
		goServiceFile{
			path: "estate/v1/property_service.proto", pkg: "estate.v1",
			service: "PropertyService", sourceName: "Property",
			goPackage: "example.com/gen/estate/v1;estatev1",
		},
		goServiceFile{
			path: "booking/v1/hold_service.proto", pkg: "booking.v1",
			service: "HoldService", sourceName: "Hold",
			goPackage: "example.com/gen/booking/v1;bookingv1",
		},
	)

	out := runGoTarget(t, plugin, "")

	for _, want := range []string{
		"example.com/gen/estate/v1/mcp_shared.pb.mcp.go",
		"example.com/gen/booking/v1/mcp_shared.pb.mcp.go",
	} {
		src, ok := out[want]
		if !ok {
			t.Fatalf("no shared file at %s; generated: %s", want, strings.Join(sortedKeys(out), ", "))
		}
		// Each carries its own package clause, so neither is a copy of the other.
		wantClause := "package " + path.Base(strings.TrimSuffix(path.Dir(want), "/v1")) + "v1"
		if !strings.Contains(src, wantClause) {
			t.Errorf("%s: missing %q", want, wantClause)
		}
	}
}

// package_suffix rewrites both the package name and the output directory, and
// the shared file has to land in the rewritten package or it declares helpers
// for a package nothing generates into.
func TestGoSharedFileFollowsPackageSuffix(t *testing.T) {
	plugin := goFiles(t,
		goServiceFile{
			path: "estate/v1/property_service.proto", pkg: "estate.v1",
			service: "PropertyService", sourceName: "Property",
			goPackage: "example.com/gen/estate/v1;estatev1",
		},
	)

	out := runGoTarget(t, plugin, "mcp")

	want := "example.com/gen/estate/v1/estatev1mcp/mcp_shared.pb.mcp.go"
	src, ok := out[want]
	if !ok {
		t.Fatalf("no shared file at %s; generated: %s", want, strings.Join(sortedKeys(out), ", "))
	}
	if !strings.Contains(src, "package estatev1mcp") {
		t.Errorf("%s: want package clause estatev1mcp", want)
	}
}

// A proto whose own output would claim the shared file's name is reported,
// rather than emitted twice for protoc to reject with a duplicate-file error
// that explains nothing.
func TestGoSharedFileNameCollisionIsReported(t *testing.T) {
	plugin := goFiles(t,
		goServiceFile{
			path: "estate/v1/mcp_shared.proto", pkg: "estate.v1",
			service: "PropertyService", sourceName: "Property",
			goPackage: "example.com/gen/estate/v1;estatev1",
		},
	)

	model := &Model{Files: plugin.Files}
	Target{}.generateGo(plugin, model)

	resp := plugin.Response()
	if resp.Error == nil {
		t.Fatal("a proto named mcp_shared.proto generated without an error")
	}
	if !strings.Contains(resp.GetError(), "mcp_shared.proto") {
		t.Errorf("error %q does not name the colliding proto", resp.GetError())
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
