package main

import (
	"go/ast"
	"strings"
)

func hasDeprecatedDocumentation(doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}
	for paragraph := range strings.SplitSeq(doc.Text(), "\n\n") {
		if strings.HasPrefix(paragraph, "Deprecated: ") {
			return true
		}
	}
	return false
}
