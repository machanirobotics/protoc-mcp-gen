// Copyright 2026 The Protobuf Project authors.
// SPDX-License-Identifier: Apache-2.0

package generator

import (
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
)

// ExtractGoogleAPIResources scans all methods in a service and collects
// google.api.resource annotations from response message types. Each unique
// resource pattern is returned as an MCPResourceOpts with a URI template
// derived from the pattern and a scheme based on the resource's singular name.
func ExtractGoogleAPIResources(svc *protogen.Service) []MCPResourceOpts {
	seen := make(map[string]bool)
	var resources []MCPResourceOpts

	for _, meth := range svc.Methods {
		if meth.Desc.IsStreamingClient() || meth.Desc.IsStreamingServer() {
			continue
		}
		msg := meth.Output
		mopts := msg.Desc.Options()
		if mopts == nil {
			continue
		}
		rd, ok := proto.GetExtension(mopts, annotations.E_Resource).(*annotations.ResourceDescriptor)
		if !ok || rd == nil {
			continue
		}
		// Derive scheme from singular name, or fallback to the type's resource kind.
		scheme := rd.GetSingular()
		if scheme == "" {
			parts := strings.SplitN(rd.GetType(), "/", 2)
			if len(parts) == 2 {
				scheme = strings.ToLower(parts[1])
			} else {
				scheme = "resource"
			}
		}
		// Derive display name from the resource kind (after the /).
		displayName := scheme
		if typeParts := strings.SplitN(rd.GetType(), "/", 2); len(typeParts) == 2 {
			displayName = typeParts[1]
		}

		for _, pattern := range rd.GetPattern() {
			uriTemplate := scheme + "://" + pattern
			if seen[uriTemplate] {
				continue
			}
			seen[uriTemplate] = true
			resources = append(resources, MCPResourceOpts{
				URITemplate: uriTemplate,
				Name:        displayName,
				Description: displayName + " resource (" + pattern + ")",
				MimeType:    "application/json",
			})
		}
	}
	return resources
}

// mergeResources appends auto-detected resources to the ones declared on the
// service, skipping any whose URI or URI template a declaration already covers.
// Declarations carry metadata (title, size, annotations, icons) that detection
// cannot infer, so they must not be displaced by a same-URI detection.
func mergeResources(declared, detected []MCPResourceOpts) []MCPResourceOpts {
	claimed := make(map[string]bool, len(declared)*2)
	for _, res := range declared {
		if res.URI != "" {
			claimed[res.URI] = true
		}
		if res.URITemplate != "" {
			claimed[res.URITemplate] = true
		}
	}
	out := declared
	for _, res := range detected {
		if (res.URI != "" && claimed[res.URI]) || (res.URITemplate != "" && claimed[res.URITemplate]) {
			continue
		}
		out = append(out, res)
	}
	return out
}
