package generator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"
)

const generatedFilenameExtension = ".pb.mcp.go"

// ToolMeta holds the MCP tool name and description for a single RPC method.
type ToolMeta struct {
	Name        string
	Title       string
	Description string
	Icons       []MCPIconOpts
	// Hints is nil when the RPC declares no behavioural hints.
	Hints *MCPToolHints
}

// MethodInfo carries the Go type identifiers needed by the code template.
type MethodInfo struct {
	RequestType    string
	ResponseType   string
	MethodOpts     *MCPMethodOpts
	StreamProgress *StreamProgressInfo // Non-nil when server-streaming with MCPProgress
	// FullMethod is the gRPC full method name ("/pkg.Service/Method", proto
	// names) passed to the unary interceptor chain so MCP-dispatched calls are
	// indistinguishable from wire RPCs to middleware.
	FullMethod string
}

// TplParams is the top-level data fed into the code template.
type TplParams struct {
	Version           string
	SourcePath        string
	GoPackage         string
	ExtraImports      []string          // e.g. `emptypb "google.golang.org/.../emptypb"`
	SchemaJSON        map[string]string // key: ServiceName_MethodName -> input schema JSON
	OutputSchemaJSON  map[string]string // key: ServiceName_MethodName -> output schema JSON
	ToolMeta          map[string]ToolMeta
	Services          map[string]map[string]MethodInfo
	ServiceBasePaths  map[string]string          // key: ServiceName -> default base path e.g. "/todo/v1/TodoService"
	ServiceOpts       map[string]*MCPServiceOpts // key: ServiceName
	HasStreamProgress bool                       // true if any method uses server streaming with progress
	HasAnyMethods     bool                       // true if any service has any methods (needed for grpc/protojson imports)
}

// GoPackageOutput identifies the Go package one generated file belongs to, and
// where its siblings are written. [GenerateGoShared] emits one shared file per
// distinct value.
//
// It is resolved during generation rather than read off the protogen.File
// because package_suffix rewrites both the package name and the output path.
type GoPackageOutput struct {
	// Dir is the directory the package's generated files are written to, in the
	// same address space as GeneratedFilenamePrefix: source-relative under
	// paths=source_relative, and import-path-prefixed otherwise.
	Dir string
	// PackageName is the Go package clause the generated files carry.
	PackageName string
	// ImportPath is the package's Go import path.
	ImportPath protogen.GoImportPath
}

// FileGenerator produces a single *.pb.mcp.go file from a protobuf file.
type FileGenerator struct {
	f             *protogen.File
	gen           *protogen.Plugin
	gf            *protogen.GeneratedFile
	genImportPath protogen.GoImportPath
	pkg           GoPackageOutput
	generated     bool
}

// NewFileGenerator creates a FileGenerator for the given protobuf file.
func NewFileGenerator(f *protogen.File, gen *protogen.Plugin) *FileGenerator {
	gen.SupportedFeatures |= uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)
	return &FileGenerator{f: f, gen: gen}
}

// Generate produces the *.pb.mcp.go output file.  It is a no-op when the
// protobuf file contains no service definitions.
func (g *FileGenerator) Generate(packageSuffix string) {
	file := g.f
	if len(file.Services) == 0 {
		return
	}

	goImportPath := file.GoImportPath
	if packageSuffix != "" {
		if !token.IsIdentifier(packageSuffix) {
			g.gen.Error(fmt.Errorf("package_suffix %q is not a valid Go identifier", packageSuffix))
			return
		}
		file.GoPackageName += protogen.GoPackageName(packageSuffix)
		prefix := filepath.ToSlash(file.GeneratedFilenamePrefix)
		file.GeneratedFilenamePrefix = path.Join(
			path.Dir(prefix),
			string(file.GoPackageName),
			path.Base(prefix),
		)
		goImportPath = protogen.GoImportPath(path.Join(
			string(file.GoImportPath),
			string(file.GoPackageName),
		))
	}

	g.gf = g.gen.NewGeneratedFile(
		file.GeneratedFilenamePrefix+generatedFilenameExtension,
		goImportPath,
	)
	g.genImportPath = goImportPath
	g.pkg = GoPackageOutput{
		Dir:         path.Dir(filepath.ToSlash(file.GeneratedFilenamePrefix)),
		PackageName: string(file.GoPackageName),
		ImportPath:  goImportPath,
	}
	g.generated = true

	params := g.buildParams()
	var buf bytes.Buffer
	if err := goTemplate.Execute(&buf, params); err != nil {
		g.gen.Error(err)
		return
	}

	// Validate generated Go source is syntactically correct.
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "", buf.Bytes(), parser.AllErrors); err != nil {
		g.gen.Error(fmt.Errorf("%s: unparsable Go source: %v", file.GeneratedFilenamePrefix+generatedFilenameExtension, err))
		return
	}

	if _, err := g.gf.Write(buf.Bytes()); err != nil {
		g.gen.Error(err)
	}
}

// buildParams iterates over all services/methods and builds the template data.
func (g *FileGenerator) buildParams() TplParams {
	services := make(map[string]map[string]MethodInfo)
	schemaJSON := make(map[string]string)
	outputSchemaJSON := make(map[string]string)
	toolMeta := make(map[string]ToolMeta)
	serviceBasePaths := make(map[string]string)
	serviceOpts := make(map[string]*MCPServiceOpts)
	extraImportMap := make(map[protogen.GoImportPath]string)

	resolveType := func(ident protogen.GoIdent) string {
		if ident.GoImportPath == g.genImportPath {
			return ident.GoName
		}
		alias := path.Base(string(ident.GoImportPath))
		extraImportMap[ident.GoImportPath] = alias
		return alias + "." + ident.GoName
	}

	for _, svc := range g.f.Services {
		methods := make(map[string]MethodInfo)

		for _, meth := range svc.Methods {
			// Skip client-streaming; support unary and server-streaming (progress) RPCs.
			if meth.Desc.IsStreamingClient() {
				continue
			}
			streamProgress := DetectProgressStream(meth, resolveType)
			if meth.Desc.IsStreamingServer() && streamProgress == nil {
				continue // Server-streaming without MCPProgress convention is not supported
			}

			key := string(svc.Desc.Name()) + "_" + meth.GoName
			toolName := BuildToolName(string(meth.Desc.FullName()))
			toolDesc := CleanComment(string(meth.Comments.Leading))

			// Apply method-level option overrides.
			methOpts := ExtractMethodOptions(meth)
			if methOpts != nil {
				if methOpts.ToolName != "" {
					toolName = methOpts.ToolName
				}
				if methOpts.ToolDescription != "" {
					toolDesc = methOpts.ToolDescription
				}
				// Resolve prompt schema → populate Arguments from proto message fields.
				if methOpts.Prompt != nil && methOpts.Prompt.Schema != "" {
					for _, sf := range ResolveSchemaFields(g.gen, methOpts.Prompt.Schema) {
						methOpts.Prompt.Arguments = append(methOpts.Prompt.Arguments, MCPPromptArgOpts(sf))
					}
				}
				// Resolve elicitation schema → populate Fields from proto message fields.
				if methOpts.Elicitation != nil && methOpts.Elicitation.Schema != "" {
					for _, sf := range ResolveSchemaFields(g.gen, methOpts.Elicitation.Schema) {
						methOpts.Elicitation.Fields = append(methOpts.Elicitation.Fields, MCPElicitFieldOpts(sf))
					}
				}
			}

			// Standard schema (root description = tool description, per MCP inputSchema convention)
			stdSchema := messageSchema(meth.Input.Desc, false, toolDesc)
			stdBytes, err := json.Marshal(stdSchema)
			if err != nil {
				panic(fmt.Sprintf("marshal standard schema: %v", err))
			}
			schemaJSON[key] = string(stdBytes)

			// outputSchema describes what the tool returns, and comes from the
			// response message exactly as inputSchema comes from the request. For
			// a streaming RPC that is the result arm of the progress oneof, not
			// the chunk wrapper the client never sees.
			outMsg := meth.Output.Desc
			if streamProgress != nil && streamProgress.ResultMessage != nil {
				outMsg = streamProgress.ResultMessage.Desc
			}
			outBytes, err := json.Marshal(messageSchema(outMsg, false, ""))
			if err != nil {
				panic(fmt.Sprintf("marshal output schema: %v", err))
			}
			outputSchemaJSON[key] = string(outBytes)

			meta := ToolMeta{
				Name:        toolName,
				Description: toolDesc,
			}
			if methOpts != nil {
				meta.Title = methOpts.ToolTitle
				meta.Icons = methOpts.ToolIcons
				meta.Hints = methOpts.Hints
			}
			toolMeta[key] = meta

			responseType := resolveType(meth.Output.GoIdent)
			if streamProgress != nil {
				responseType = streamProgress.ResultType
			}
			methods[meth.GoName] = MethodInfo{
				RequestType:    resolveType(meth.Input.GoIdent),
				ResponseType:   responseType,
				MethodOpts:     methOpts,
				StreamProgress: streamProgress,
				FullMethod:     "/" + string(svc.Desc.FullName()) + "/" + string(meth.Desc.Name()),
			}
		}

		svcName := string(svc.Desc.Name())
		services[svcName] = methods
		serviceBasePaths[svcName] = "/" + strings.ToLower(strings.ReplaceAll(string(svc.Desc.FullName()), ".", "/")) + "/mcp"

		// Extract explicit MCP service options + auto-detect google.api.resource.
		// Resources declared on the service win over an auto-detected resource
		// with the same URI, so an author can annotate a response message and
		// still override the generated title, icons, or annotations by hand.
		svcOpt := ExtractServiceOptions(svc)
		apiResources := ExtractGoogleAPIResources(svc)
		if len(apiResources) > 0 {
			if svcOpt == nil {
				svcOpt = &MCPServiceOpts{}
			}
			svcOpt.Resources = mergeResources(svcOpt.Resources, apiResources)
		}
		serviceOpts[svcName] = svcOpt
	}

	// Third-party imports go out as one sorted block. gofmt sorts within a block
	// but never across blocks, so grouping the fixed imports separately from the
	// per-file ones would emit unsorted output whenever a resolved proto package
	// sorts before google.golang.org — which any github.com path does.
	var extraImports []string
	for importPath, alias := range extraImportMap {
		extraImports = append(extraImports, alias+" "+`"`+string(importPath)+`"`)
	}

	hasStreamProgress := false
	hasAnyMethods := false
	for _, methods := range services {
		if len(methods) > 0 {
			hasAnyMethods = true
		}
		for _, info := range methods {
			if info.StreamProgress == nil {
				continue
			}
			hasStreamProgress = true
		}
	}
	if hasAnyMethods {
		extraImports = append(extraImports,
			`"google.golang.org/grpc"`,
			`"google.golang.org/protobuf/encoding/protojson"`,
		)
	}
	sort.Strings(extraImports)

	return TplParams{
		Version:           PluginVersion,
		SourcePath:        g.f.Desc.Path(),
		GoPackage:         string(g.f.GoPackageName),
		ExtraImports:      extraImports,
		SchemaJSON:        schemaJSON,
		OutputSchemaJSON:  outputSchemaJSON,
		ToolMeta:          toolMeta,
		Services:          services,
		ServiceBasePaths:  serviceBasePaths,
		ServiceOpts:       serviceOpts,
		HasStreamProgress: hasStreamProgress,
		HasAnyMethods:     hasAnyMethods,
	}
}
