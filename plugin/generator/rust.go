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

// RsMethodInfo carries Rust-specific identifiers for a single RPC method.
type RsMethodInfo struct {
	RsMethodName    string // snake_case method name, e.g. create_todo
	ConstName       string // SCREAMING_SNAKE constant prefix, e.g. TODO_SERVICE_CREATE_TODO
	ToolName        string // MCP tool name, e.g. todo_v1_TodoService_CreateTodo
	Description     string // method description
	MethodOpts      *MCPMethodOpts
	StreamProgress  *StreamProgressInfo // Non-nil when server-streaming with MCPProgress
	RequestType     string              // e.g. CountRequest
	ResponseType    string              // e.g. CountResponse or result type for streaming
	StreamChunkType string              // for streaming: e.g. CountStreamChunk
}

// RsTplParams is the top-level data fed into the Rust code template.
type RsTplParams struct {
	Version          string
	SourcePath       string
	SchemaJSON       map[string]string                  // key: ServiceName_MethodName -> input schema JSON
	OutputSchemaJSON map[string]string                  // key: ServiceName_MethodName -> output schema JSON
	ToolMeta         map[string]ToolMeta                // key: ServiceName_MethodName
	Services         map[string]map[string]RsMethodInfo // key: ServiceName -> MethodName -> info
	ServiceBasePaths map[string]string                  // key: ServiceName -> default base path
	ServiceOpts      map[string]*MCPServiceOpts         // key: ServiceName
}

// RustFileGenerator produces a single *_mcp.rs file from a protobuf file.
type RustFileGenerator struct {
	f   *protogen.File
	gen *protogen.Plugin
}

// NewRustFileGenerator creates a RustFileGenerator for the given protobuf file.
func NewRustFileGenerator(f *protogen.File, gen *protogen.Plugin) *RustFileGenerator {
	gen.SupportedFeatures |= uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)
	return &RustFileGenerator{f: f, gen: gen}
}

// Generate produces the *_mcp.rs output file.
func (g *RustFileGenerator) Generate() {
	file := g.f
	if len(file.Services) == 0 {
		return
	}

	// Use the proto file stem (e.g. "audio_service") to produce one
	// MCP file per proto source file (e.g. "todo/v1/audio_service.mcp.rs").
	// This avoids collisions when multiple services share the same proto
	// package -- the previous per-package naming caused only the last
	// service's code to survive.
	dir := filepath.Dir(file.Desc.Path())
	stem := strings.TrimSuffix(filepath.Base(file.Desc.Path()), ".proto")
	outName := filepath.Join(dir, stem+".mcp.rs")
	gf := g.gen.NewGeneratedFile(outName, "")

	params := g.buildRsParams()
	if err := rustTemplate.Execute(gf, params); err != nil {
		g.gen.Error(err)
	}
}

// buildRsParams iterates over all services/methods and builds the Rust template data.
func (g *RustFileGenerator) buildRsParams() RsTplParams {
	services := make(map[string]map[string]RsMethodInfo)
	schemaJSON := make(map[string]string)
	outputSchemaJSON := make(map[string]string)
	toolMeta := make(map[string]ToolMeta)
	serviceBasePaths := make(map[string]string)
	serviceOpts := make(map[string]*MCPServiceOpts)

	for _, svc := range g.f.Services {
		methods := make(map[string]RsMethodInfo)

		resolveType := func(ident protogen.GoIdent) string {
			return string(ident.GoName)
		}
		for _, meth := range svc.Methods {
			if meth.Desc.IsStreamingClient() {
				continue
			}
			streamProgress := DetectProgressStream(meth, resolveType)
			if meth.Desc.IsStreamingServer() && streamProgress == nil {
				continue // Server-streaming without MCPProgress convention is not supported
			}

			key := string(svc.Desc.Name()) + "_" + meth.GoName
			toolName := BuildToolName(string(meth.Desc.FullName()))

			// Apply method-level option overrides.
			methOpts := ExtractMethodOptions(meth)
			if methOpts != nil {
				if methOpts.ToolName != "" {
					toolName = methOpts.ToolName
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

			// outputSchema comes from the response message — the result arm of the
			// progress oneof for a streaming RPC, not the chunk wrapper.
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
				Description: desc,
			}
			if methOpts != nil {
				meta.Title = methOpts.ToolTitle
				meta.Icons = methOpts.ToolIcons
				meta.Hints = methOpts.Hints
			}
			toolMeta[key] = meta

			reqType := string(meth.Input.Desc.Name())
			respType := string(meth.Output.Desc.Name())
			streamChunkType := ""
			if streamProgress != nil && streamProgress.ResultMessage != nil {
				respType = string(streamProgress.ResultMessage.Desc.Name())
				streamChunkType = string(meth.Output.Desc.Name())
			}

			methods[meth.GoName] = RsMethodInfo{
				RsMethodName:    toSnakeCase(meth.GoName),
				ConstName:       toScreamingSnakeCase(key),
				ToolName:        toolName,
				Description:     desc,
				MethodOpts:      methOpts,
				StreamProgress:  streamProgress,
				RequestType:     reqType,
				ResponseType:    respType,
				StreamChunkType: streamChunkType,
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

	return RsTplParams{
		Version:          PluginVersion,
		SourcePath:       g.f.Desc.Path(),
		SchemaJSON:       schemaJSON,
		OutputSchemaJSON: outputSchemaJSON,
		ToolMeta:         toolMeta,
		Services:         services,
		ServiceBasePaths: serviceBasePaths,
		ServiceOpts:      serviceOpts,
	}
}
