// Copyright 2026 The Protobuf Project authors.
// SPDX-License-Identifier: Apache-2.0

package generator

import (
	mcppb "github.com/the-protobuf-project/mcp/protobuf/mcppb"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
)

// ExtractServiceOptions reads the mcp.v1.service extension from a service descriptor.
func ExtractServiceOptions(svc *protogen.Service) *MCPServiceOpts {
	opts := svc.Desc.Options()
	if opts == nil {
		return nil
	}
	ext, ok := proto.GetExtension(opts, mcppb.E_Service).(*mcppb.MCPServiceOptions)
	if !ok || ext == nil {
		return nil
	}
	result := &MCPServiceOpts{}
	if app := ext.GetApp(); app != nil {
		result.App = &MCPAppOpts{
			Name:        app.GetDisplayName(),
			Title:       app.GetTitle(),
			Version:     app.GetVersion(),
			Description: app.GetDescription(),
			WebsiteURL:  app.GetWebsiteUrl(),
			Icons:       convertIcons(app.GetIcons()),
		}
	}
	result.Cache = cacheHint(ext.GetCache())
	for _, res := range ext.GetResources() {
		result.Resources = append(result.Resources, convertResource(res))
	}
	return result
}

// convertResource maps a declared MCPResource onto the template view. The
// uri/pattern oneof decides whether this registers a concrete resource or a
// resource template.
func convertResource(res *mcppb.MCPResource) MCPResourceOpts {
	out := MCPResourceOpts{
		URI:         res.GetUri(),
		URITemplate: res.GetPattern(),
		Name:        res.GetId(),
		Title:       res.GetTitle(),
		Description: res.GetDescription(),
		MimeType:    res.GetMimeType(),
	}
	if res.Size != nil {
		out.Size = res.GetSize()
		out.HasSize = true
	}
	if ann := res.GetAnnotations(); ann != nil {
		converted := &MCPAnnotationsOpts{
			LastModified: ann.GetLastModified(),
		}
		for _, role := range ann.GetAudience() {
			converted.Audience = append(converted.Audience, roleName(role))
		}
		if ann.Priority != nil {
			converted.Priority = ann.GetPriority()
			converted.HasPriority = true
		}
		out.Annotations = converted
	}
	out.Icons = convertIcons(res.GetIcons())
	out.Cache = cacheHint(res.GetCache())
	return out
}

// ExtractMethodOptions reads mcp.v1.tool, mcp.v1.prompt, and mcp.v1.elicitation
// extensions from a method descriptor and merges them into a single MCPMethodOpts.
func ExtractMethodOptions(meth *protogen.Method) *MCPMethodOpts {
	opts := meth.Desc.Options()
	if opts == nil {
		return nil
	}

	result := &MCPMethodOpts{}
	hasAnything := false

	// mcp.v1.tool — name/description overrides and the progress declaration
	toolExt, ok := proto.GetExtension(opts, mcppb.E_Tool).(*mcppb.MCPToolOptions)
	if ok && toolExt != nil {
		result.ToolName = toolExt.GetId()
		result.ToolTitle = toolExt.GetTitle()
		result.ToolDescription = toolExt.GetDescription()
		result.ToolIcons = convertIcons(toolExt.GetIcons())
		result.Progress = toolExt.GetProgress()
		result.Hints = toolHints(toolExt)
		hasAnything = true
	}

	// mcp.v1.prompt — per-RPC prompt template with schema reference
	promptExt, ok := proto.GetExtension(opts, mcppb.E_Prompt).(*mcppb.MCPPrompt)
	if ok && promptExt != nil {
		result.Prompt = &MCPPromptOpts{
			Name:        promptExt.GetId(),
			Title:       promptExt.GetTitle(),
			Description: promptExt.GetDescription(),
			Icons:       convertIcons(promptExt.GetIcons()),
			Schema:      promptExt.GetSchema(),
			Role:        roleName(promptExt.GetRole()),
		}
		hasAnything = true
	}

	// mcp.v1.elicitation — form- or URL-mode user input before the tool runs
	elicitExt, ok := proto.GetExtension(opts, mcppb.E_Elicitation).(*mcppb.MCPElicitation)
	if ok && elicitExt != nil {
		result.Elicitation = &MCPElicitationOpts{
			Message:       elicitExt.GetMessage(),
			Schema:        elicitExt.GetSchema(),
			Mode:          elicitationMode(elicitExt.GetMode(), elicitExt.GetUrl()),
			URL:           elicitExt.GetUrl(),
			ElicitationID: elicitExt.GetElicitationId(),
			Required:      elicitExt.GetRequired(),
		}
		hasAnything = true
	}

	if !hasAnything {
		return nil
	}
	return result
}

// toolHints collects the behavioural hints an RPC declares, or nil when it
// declares none — an all-absent Hints and no Hints mean the same thing, and nil
// lets templates skip the annotations block entirely.
func toolHints(ext *mcppb.MCPToolOptions) *MCPToolHints {
	hints := &MCPToolHints{
		ReadOnly:    ext.ReadOnly,
		Destructive: ext.Destructive,
		Idempotent:  ext.Idempotent,
		OpenWorld:   ext.OpenWorld,
	}
	if hints.ReadOnly == nil && hints.Destructive == nil &&
		hints.Idempotent == nil && hints.OpenWorld == nil {
		return nil
	}
	return hints
}

// convertIcons maps declared icons onto the template view. Tools, prompts,
// resources and the app all carry the same shape.
func convertIcons(icons []*mcppb.MCPIcon) []MCPIconOpts {
	if len(icons) == 0 {
		return nil
	}
	out := make([]MCPIconOpts, 0, len(icons))
	for _, icon := range icons {
		out = append(out, MCPIconOpts{
			Src:      icon.GetSrc(),
			MimeType: icon.GetMimeType(),
			Sizes:    icon.GetSizes(),
			Theme:    iconTheme(icon.GetTheme()),
		})
	}
	return out
}
