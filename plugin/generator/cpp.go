// Copyright 2026 The Protobuf Project authors.
// SPDX-License-Identifier: Apache-2.0

package generator

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"
)

// CppMethodInfo carries C++-specific identifiers for a single RPC method.
type CppMethodInfo struct {
	CppMethodName string // snake_case, e.g. create_todo
	GoName        string // original RPC name for stub call, e.g. CreateTodo
	ConstName     string // SCREAMING_SNAKE constant prefix
	ToolName      string // MCP tool name
	Description   string // method description
	InputType     string // C++ request type, e.g. CreateTodoRequest
	OutputType    string // C++ response type, e.g. Todo
	MethodOpts    *MCPMethodOpts
}

// CppTplParams is the top-level data fed into the C++ code templates.
type CppTplParams struct {
	Version           string
	SourcePath        string
	ProtoPackage      string // e.g. "todo.v1"
	CppNamespace      string // e.g. "todo::v1"
	NamespaceOpen     string // e.g. "namespace todo { namespace v1 {"
	NamespaceClose    string // e.g. "} } // namespace todo::v1"
	IncludePath       string // e.g. "todo/v1/todo_service.mcp.h"
	GrpcInclude       string // e.g. "todo/v1/todo_service.grpc.pb.h"
	SchemaJSON        map[string]string
	OutputSchemaJSON  map[string]string
	ToolMeta          map[string]ToolMeta
	Services          map[string]map[string]CppMethodInfo
	ServiceBasePaths  map[string]string
	ServiceOpts       map[string]*MCPServiceOpts
	CrateName         string // e.g. "todo_mcp_cpp"
	McpCcPath         string // e.g. "todo/v1/todo_service.mcp.cc"
	GenRoot           string // e.g. ".." (relative to rust/)
	FirstServiceName  string // e.g. "TodoService"
	FirstServiceSnake string // e.g. "todo_service"
}

// CppFileGenerator produces the C++ MCP adapter and Rust bridge/handler files.
type CppFileGenerator struct {
	f   *protogen.File
	gen *protogen.Plugin
}

// NewCppFileGenerator creates a CppFileGenerator for the given protobuf file.
func NewCppFileGenerator(f *protogen.File, gen *protogen.Plugin) *CppFileGenerator {
	gen.SupportedFeatures |= uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)
	return &CppFileGenerator{f: f, gen: gen}
}

// Generate produces per-file outputs (.mcp.h, .mcp.cc) and optionally shared
// outputs (rust/*, Makefile, main.cc). When multiple proto packages exist,
// emitShared should be true only for the first file to avoid duplicate outputs.
func (g *CppFileGenerator) Generate(emitShared bool) {
	file := g.f
	if len(file.Services) == 0 {
		return
	}

	dir := filepath.Dir(file.Desc.Path())
	stem := strings.TrimSuffix(filepath.Base(file.Desc.Path()), ".proto")
	params := g.buildCppParams(dir, stem)

	genFile := func(outPath, tplName string) {
		tpl, ok := cppTemplates[tplName]
		if !ok {
			g.gen.Error(fmt.Errorf("embedded template %s not found", tplName))
			return
		}
		gf := g.gen.NewGeneratedFile(outPath, "")
		if err := tpl.Execute(gf, params); err != nil {
			g.gen.Error(err)
		}
	}

	// 1. C++ header
	genFile(filepath.Join(dir, stem+".mcp.h"), "cpp/mcp.h.tpl")

	// 2. C++ implementation
	genFile(filepath.Join(dir, stem+".mcp.cc"), "cpp/impl.tpl")

	if emitShared {
		genFile("rust/lib.rs", "cpp/bridge.tpl")
		genFile("rust/mcp_handler.rs", "cpp/handler.tpl")
		genFile("rust/Cargo.toml", "cpp/cargo_toml.tpl")
		genFile("rust/build.rs", "cpp/build_rs.tpl")
		genFile("rust/mcp_include.h", "cpp/mcp_include.tpl")
		genFile("Makefile", "cpp/makefile.tpl")
		genFile("main.cc", "cpp/main.tpl")
	}
}

func (g *CppFileGenerator) buildCppParams(dir, stem string) CppTplParams {
	services := make(map[string]map[string]CppMethodInfo)
	schemaJSON := make(map[string]string)
	outputSchemaJSON := make(map[string]string)
	toolMeta := make(map[string]ToolMeta)
	serviceBasePaths := make(map[string]string)
	serviceOpts := make(map[string]*MCPServiceOpts)
	pkg := string(g.f.Desc.Package())

	for _, svc := range g.f.Services {
		methods := make(map[string]CppMethodInfo)

		for _, meth := range svc.Methods {
			if meth.Desc.IsStreamingClient() || meth.Desc.IsStreamingServer() {
				continue
			}

			key := string(svc.Desc.Name()) + "_" + meth.GoName
			toolName := BuildToolName(string(meth.Desc.FullName()))

			methOpts := ExtractMethodOptions(meth)
			if methOpts != nil {
				if methOpts.ToolName != "" {
					toolName = methOpts.ToolName
				}
				if methOpts.Prompt != nil && methOpts.Prompt.Schema != "" {
					for _, sf := range ResolveSchemaFields(g.gen, methOpts.Prompt.Schema) {
						methOpts.Prompt.Arguments = append(methOpts.Prompt.Arguments, MCPPromptArgOpts(sf))
					}
				}
				if methOpts.Elicitation != nil && methOpts.Elicitation.Schema != "" {
					for _, sf := range ResolveSchemaFields(g.gen, methOpts.Elicitation.Schema) {
						methOpts.Elicitation.Fields = append(methOpts.Elicitation.Fields, MCPElicitFieldOpts(sf))
					}
				}
			}

			desc := strings.TrimSpace(CleanComment(string(meth.Comments.Leading)))
			if methOpts != nil && methOpts.ToolDescription != "" {
				desc = methOpts.ToolDescription
			}

			// Standard schema (root description = tool description, per MCP inputSchema convention)
			stdSchema := messageSchema(meth.Input.Desc, false, desc)
			stdBytes, err := json.Marshal(stdSchema)
			if err != nil {
				panic(fmt.Sprintf("marshal standard schema: %v", err))
			}
			schemaJSON[key] = string(stdBytes)

			// outputSchema comes from the response message, mirroring the input side.
			outBytes, err := json.Marshal(messageSchema(meth.Output.Desc, false, ""))
			if err != nil {
				panic(fmt.Sprintf("marshal output schema: %v", err))
			}
			outputSchemaJSON[key] = string(outBytes)

			meta := ToolMeta{
				Name:        toolName,
				Description: desc,
			}
			if methOpts != nil {
				meta.Title = methOpts.ToolTitle
				meta.Icons = methOpts.ToolIcons
				meta.Hints = methOpts.Hints
			}
			toolMeta[key] = meta

			methods[meth.GoName] = CppMethodInfo{
				CppMethodName: toSnakeCase(meth.GoName),
				GoName:        meth.GoName,
				ConstName:     toScreamingSnakeCase(key),
				ToolName:      toolName,
				Description:   desc,
				InputType:     cppTypeName(meth.Input, pkg),
				OutputType:    cppTypeName(meth.Output, pkg),
				MethodOpts:    methOpts,
			}
		}

		svcName := string(svc.Desc.Name())
		services[svcName] = methods
		serviceBasePaths[svcName] = "/" + strings.ToLower(strings.ReplaceAll(string(svc.Desc.FullName()), ".", "/")) + "/mcp"
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

	cppNs := strings.ReplaceAll(pkg, ".", "::")
	parts := strings.Split(pkg, ".")

	var nsOpen []string
	for _, p := range parts {
		nsOpen = append(nsOpen, "namespace "+p+" {")
	}
	closers := make([]string, len(parts))
	for i := range parts {
		closers[i] = "}"
	}

	crateName := strings.ReplaceAll(pkg, ".", "_") + "_mcp_cpp"
	mcpCcPath := filepath.Join(dir, stem+".mcp.cc")
	genRoot := ".."

	var firstSvcName, firstSvcSnake string
	for svcName := range services {
		firstSvcName = svcName
		firstSvcSnake = toSnakeCase(svcName)
		break
	}

	return CppTplParams{
		Version:           PluginVersion,
		SourcePath:        g.f.Desc.Path(),
		ProtoPackage:      pkg,
		CppNamespace:      cppNs,
		NamespaceOpen:     strings.Join(nsOpen, " "),
		NamespaceClose:    strings.Join(closers, " ") + " // namespace " + cppNs,
		IncludePath:       filepath.Join(dir, stem+".mcp.h"),
		GrpcInclude:       filepath.Join(dir, stem+".grpc.pb.h"),
		SchemaJSON:        schemaJSON,
		OutputSchemaJSON:  outputSchemaJSON,
		ToolMeta:          toolMeta,
		Services:          services,
		ServiceBasePaths:  serviceBasePaths,
		ServiceOpts:       serviceOpts,
		CrateName:         crateName,
		McpCcPath:         mcpCcPath,
		GenRoot:           genRoot,
		FirstServiceName:  firstSvcName,
		FirstServiceSnake: firstSvcSnake,
	}
}
