package plan

import (
	"fmt"
	"strings"
)

// DefaultTagPrefix is the tag prefix used when no custom prefix is configured.
const DefaultTagPrefix = "[atlasctl:"

// TagCodec encodes and decodes atlasctl description tags using a configurable
// prefix. The tag format is:
//
//	<prefix><name>:<cohort>]
//
// Changing the prefix after measurements have been created is a breaking
// operation — existing measurements will appear as orphans during drift
// detection since their descriptions no longer match. Treat the prefix as a
// structural attribute: set it once and leave it.
type TagCodec struct {
	prefix string
}

// NewTagCodec returns a TagCodec using the given prefix. If prefix is empty,
// DefaultTagPrefix is used.
func NewTagCodec(prefix string) TagCodec {
	if prefix == "" {
		prefix = DefaultTagPrefix
	}
	return TagCodec{prefix: prefix}
}

// Prefix returns the raw prefix string, useful for API filter calls.
func (tc TagCodec) Prefix() string { return tc.prefix }

// Format produces the description tag for a (name, cohort) pair.
func (tc TagCodec) Format(name, cohort string) string {
	return fmt.Sprintf("%s%s:%s]", tc.prefix, name, cohort)
}

// Parse extracts the (name, cohort) pair from a string that may contain an
// atlasctl description tag. The tag may appear anywhere in the string.
//
// Returns ok=false if the prefix is absent, the closing "]" is missing, or
// name or cohort would be empty.
func (tc TagCodec) Parse(desc string) (name, cohort string, ok bool) {
	start := strings.Index(desc, tc.prefix)
	if start == -1 {
		return "", "", false
	}

	rest := desc[start+len(tc.prefix):]

	end := strings.Index(rest, "]")
	if end == -1 {
		return "", "", false
	}

	inner := rest[:end] // "<name>:<cohort>"

	// Split on the first colon only so cohort names may contain colons.
	sep := strings.Index(inner, ":")
	if sep <= 0 || sep == len(inner)-1 {
		return "", "", false
	}

	return inner[:sep], inner[sep+1:], true
}

// FormatTag is a package-level convenience using DefaultTagPrefix.
// Prefer TagCodec.Format when a configured prefix is available.
func FormatTag(name, cohort string) string {
	return NewTagCodec("").Format(name, cohort)
}

// ParseTag is a package-level convenience using DefaultTagPrefix.
// Prefer TagCodec.Parse when a configured prefix is available.
func ParseTag(desc string) (name, cohort string, ok bool) {
	return NewTagCodec("").Parse(desc)
}
