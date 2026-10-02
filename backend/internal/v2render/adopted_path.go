package v2render

import (
	"strings"

	"photoeditor/backend/internal/v2input"
)

// adoptionChildPath translates a closure edge relative to its verified parent.
// It is syntax conversion only: callers must still prove the child reference,
// parent provenance and exact first-visit inventory occurrence through SQL.
// Adoption's schema walker replaces every array index with a literal '*'.
func adoptionChildPath(parentInventoryPath, parentClosurePath, childClosurePath string) (string, error) {
	if parentInventoryPath != "$" && !adoptedChildPath.MatchString(parentInventoryPath) ||
		len(parentInventoryPath) > 512 || parentClosurePath == "" ||
		!strings.HasPrefix(childClosurePath, parentClosurePath) {
		return "", v2input.ErrInvalid
	}
	edge := childClosurePath[len(parentClosurePath):]
	if edge == "" || len(edge) > 4096 || edge[0] != '.' && edge[0] != '[' {
		return "", v2input.ErrInvalid
	}
	var suffix strings.Builder
	for i := 0; i < len(edge); {
		switch edge[i] {
		case '.':
			i++
			start := i
			for i < len(edge) && pathIdentifierByte(edge[i]) {
				i++
			}
			if i == start {
				return "", v2input.ErrInvalid
			}
			suffix.WriteByte('/')
			suffix.WriteString(edge[start:i])
		case '[':
			i++
			start := i
			for i < len(edge) && edge[i] >= '0' && edge[i] <= '9' {
				i++
			}
			if i == start || i >= len(edge) || edge[i] != ']' || i-start > 1 && edge[start] == '0' {
				return "", v2input.ErrInvalid
			}
			i++
			suffix.WriteString("/*")
		default:
			return "", v2input.ErrInvalid
		}
		if len(parentInventoryPath)+suffix.Len() > 512 {
			return "", v2input.ErrInvalid
		}
	}
	return parentInventoryPath + suffix.String(), nil
}

func pathIdentifierByte(b byte) bool {
	return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_'
}
