// Copyright 2026 The Protobuf Project authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/the-protobuf-project/mcp/plugin/generator"
	"google.golang.org/protobuf/compiler/protogen"
)

// version is set at build time via:
//
//	go build -ldflags "-X main.version=v0.2.0"
//
// When installed from a tagged module, Go automatically populates
// debug.ReadBuildInfo().Main.Version with the module version (e.g. v0.1.0).
// We fall back to that if ldflags is not set.
var version = ""

func resolveVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("protoc-gen-mcp %s\n", resolveVersion())
		os.Exit(0)
	}
	var flags flag.FlagSet
	lang := flags.String(
		"lang",
		"go",
		`Target language for generated MCP code (go, rust, cpp, all).`,
	)
	packageSuffix := flags.String(
		"package_suffix",
		"",
		"(Go only) Sub-package suffix for generated files (empty = same package as .pb.go files).",
	)

	generator.PluginVersion = resolveVersion()

	protogen.Options{
		ParamFunc: flags.Set,
	}.Run(func(gen *protogen.Plugin) error {
		return generator.Generate(gen, *lang, *packageSuffix)
	})
}
