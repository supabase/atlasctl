package plan

import (
	"fmt"
	"strings"
)

// tagPrefix is the fixed start of every atlasctl description tag.
const tagPrefix = "[atlasctl:"

// FormatTag produces the description tag that atlasctl embeds in every RIPE
// Atlas measurement description. The tag format is:
//
//	[atlasctl:<name>:<round>]
//
// This allows discovery via the RIPE Atlas API if the state file is lost:
//
//	goat fm -my -deschas "[atlasctl:"
//
// Constraint: name and round must not contain "]" or the first ":" after the
// prefix — both are used as delimiters. Config validation enforces simple
// identifiers, so this is satisfied in practice.
func FormatTag(name, round string) string {
	return fmt.Sprintf("%s%s:%s]", tagPrefix, name, round)
}

// ParseTag extracts the (name, round) pair from a string that contains an
// atlasctl description tag. The tag may appear anywhere in the string (e.g.
// amid a longer RIPE Atlas description).
//
// Returns ok=false if:
//   - the tag prefix is not present
//   - the closing "]" is missing
//   - name or round would be empty
func ParseTag(desc string) (name, round string, ok bool) {
	start := strings.Index(desc, tagPrefix)
	if start == -1 {
		return "", "", false
	}

	rest := desc[start+len(tagPrefix):]

	end := strings.Index(rest, "]")
	if end == -1 {
		return "", "", false
	}

	inner := rest[:end] // "<name>:<round>"

	// Split on the first colon only so round names may contain colons.
	sep := strings.Index(inner, ":")
	if sep <= 0 || sep == len(inner)-1 {
		// sep<=0: colon missing or name is empty
		// sep==len(inner)-1: round is empty
		return "", "", false
	}

	return inner[:sep], inner[sep+1:], true
}
