package generator

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/the-protobuf-project/protokit/factory"
	"google.golang.org/protobuf/compiler/protogen"
)

// PluginVersion is set by the protoc-gen-mcp binary before generation.
var PluginVersion = "dev"

// LangAll requests every language the target supports.
const LangAll = "all"

// Model is the MCP model a source produces: the proto files selected for
// generation, in the order protoc listed them.
//
// It is the plugin-defined model type M that protokit's factory is generic
// over; protokit itself stays free of it.
type Model struct {
	Files []*protogen.File
}

// ProtoSource builds the Model from the CodeGeneratorRequest protoc hands the
// plugin.
type ProtoSource struct{}

// Name identifies the source in a [factory.Registry].
func (ProtoSource) Name() string { return "proto" }

// Build collects the files protoc marked for generation.
func (ProtoSource) Build(ctx factory.Ctx) (*Model, error) {
	if ctx.Plugin == nil {
		return nil, fmt.Errorf("proto source requires plugin (protoc) mode")
	}
	m := &Model{}
	for _, f := range ctx.Plugin.Files {
		if !f.Generate {
			continue
		}
		m.Files = append(m.Files, f)
	}
	return m, nil
}

// Target renders the MCP model into MCP server bindings for one language.
type Target struct {
	// PackageSuffix is Go-specific: sub-package suffix for generated files.
	PackageSuffix string
}

// Name identifies the target in a [factory.Registry].
func (Target) Name() string { return "mcp" }

// Languages lists the languages this target can emit.
func (Target) Languages() []string { return []string{LangGo, LangRust, LangCpp} }

// Generate renders m for a single language.
func (t Target) Generate(ctx factory.Ctx, m *Model, lang string) error {
	if ctx.Plugin == nil {
		return fmt.Errorf("mcp target requires plugin (protoc) mode")
	}
	switch lang {
	case LangGo:
		t.generateGo(ctx.Plugin, m)
	case LangRust:
		for _, f := range m.Files {
			NewRustFileGenerator(f, ctx.Plugin).Generate()
		}
	case LangCpp:
		t.generateCpp(ctx.Plugin, m)
	default:
		return fmt.Errorf("unsupported language %q (supported: %s)",
			lang, strings.Join(t.Languages(), ", "))
	}
	return nil
}

// generateGo emits a *.pb.mcp.go per service-bearing file, then the shared
// helper file each Go package needs exactly one of.
//
// The grouping is what makes a package with two service-bearing protos compile:
// see [GenerateGoShared]. Packages are emitted in the order protoc listed their
// first file so the response is deterministic, and the sibling stems are
// collected per package to catch a proto that would claim the shared file's
// name.
func (t Target) generateGo(gen *protogen.Plugin, m *Model) {
	siblings := make(map[GoPackageOutput][]string)
	var order []GoPackageOutput

	for _, f := range m.Files {
		fg := NewFileGenerator(f, gen)
		fg.Generate(t.PackageSuffix)
		pkg, ok := fg.PackageOutput()
		if !ok {
			continue
		}
		if _, seen := siblings[pkg]; !seen {
			order = append(order, pkg)
		}
		// Read after Generate: package_suffix rewrites the prefix in place.
		siblings[pkg] = append(siblings[pkg], path.Base(filepath.ToSlash(f.GeneratedFilenamePrefix)))
	}

	for _, pkg := range order {
		GenerateGoShared(gen, pkg, siblings[pkg])
	}
}

// generateCpp emits C++ for every file that declares a service, in path order.
// The shared files (rust/*, Makefile, main.cc) are emitted once, for a single
// file, so that a multi-file request does not write them repeatedly.
func (t Target) generateCpp(gen *protogen.Plugin, m *Model) {
	files := make([]*protogen.File, 0, len(m.Files))
	for _, f := range m.Files {
		if len(f.Services) > 0 {
			files = append(files, f)
		}
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Desc.Path() < files[j].Desc.Path()
	})
	shared := cppSharedFileIndex(files)
	for i, f := range files {
		NewCppFileGenerator(f, gen).Generate(i == shared)
	}
}

// cppSharedFileIndex picks which file the shared C++ outputs are generated
// from: the first one, in path order, that yields at least one tool.
//
// The shared files define the whole MCP surface the C++ binary serves -- the
// cxx bridge names one service, and main.cc starts that service and no other.
// Taking files[0] unconditionally ties that choice to alphabetical order, which
// silently produced an MCP server with an empty tool list here: the C++ target
// does not support streaming RPCs (see buildCppParams), so counter/v1, whose
// only RPC streams, contributes no tools -- yet it sorts before todo/v1 and so
// claimed the bridge. Compiling and linking still succeeded, because "no tools"
// is a valid program.
//
// Skipping past files that produce nothing keeps that failure from being
// reachable by renaming a proto. It does not make C++ serve more than one
// service; that limitation is unchanged, and a request whose files all lack
// eligible RPCs still falls back to the first file so the shared outputs (and
// the build that needs them) are emitted either way.
func cppSharedFileIndex(files []*protogen.File) int {
	for i, f := range files {
		for _, svc := range f.Services {
			for _, meth := range svc.Methods {
				if !meth.Desc.IsStreamingClient() && !meth.Desc.IsStreamingServer() {
					return i
				}
			}
		}
	}
	return 0
}

// Registry returns the registry of sources and targets this plugin ships.
func Registry(packageSuffix string) *factory.Registry[*Model] {
	reg := factory.NewRegistry[*Model]()
	reg.AddSource(ProtoSource{})
	reg.AddTarget(Target{PackageSuffix: packageSuffix})
	return reg
}

// Generate builds the MCP model from gen and renders it for each requested
// language. A lang of [LangAll] expands to every language the target supports.
func Generate(gen *protogen.Plugin, lang, packageSuffix string) error {
	reg := Registry(packageSuffix)
	ctx := factory.Ctx{Plugin: gen}

	src, ok := reg.Sources["proto"]
	if !ok {
		return fmt.Errorf("no %q source registered", "proto")
	}
	model, err := src.Build(ctx)
	if err != nil {
		return fmt.Errorf("build model from %s source: %w", src.Name(), err)
	}

	tgt, ok := reg.Targets["mcp"]
	if !ok {
		return fmt.Errorf("no %q target registered (have: %s)", "mcp", reg.TargetNames())
	}

	langs, err := resolveLanguages(tgt, lang)
	if err != nil {
		return err
	}
	for _, l := range langs {
		if err := tgt.Generate(ctx, model, l); err != nil {
			return fmt.Errorf("target %s (%s): %w", tgt.Name(), l, err)
		}
	}
	return nil
}

// resolveLanguages expands [LangAll] and validates a single language against
// what the target actually emits.
func resolveLanguages(tgt factory.Target[*Model], lang string) ([]string, error) {
	supported := tgt.Languages()
	if lang == LangAll {
		return supported, nil
	}
	if slices.Contains(supported, lang) {
		return []string{lang}, nil
	}
	return nil, fmt.Errorf("unsupported language %q (supported: %s, %s)",
		lang, strings.Join(supported, ", "), LangAll)
}
