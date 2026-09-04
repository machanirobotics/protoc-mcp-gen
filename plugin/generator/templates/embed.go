// Copyright 2026 The Protobuf Project authors.
// SPDX-License-Identifier: Apache-2.0

package templates

import "embed"

//go:embed *.tpl cpp/*.tpl
var FS embed.FS
