package plan

import (
	"fmt"
	"strings"
)

// DefaultNamespace is the namespace used when none is configured.
const DefaultNamespace = "atlasctl"

// TagCodec encodes and decodes atlasctl description tags using a configurable
// namespace. The description tag format is:
//
//	[<namespace>:<name>:<cohort>]
//
// The namespace is also stored as a standalone Atlas tag on every measurement,
// enabling tag-based API filtering independent of the description field.
//
// Changing the namespace after measurements have been created is a breaking
// operation: existing measurements will appear as orphans during drift
// detection since their descriptions no longer match. Treat the namespace as a
// structural attribute: set it once and leave it.
type TagCodec struct {
	namespace string
}

// NewTagCodec returns a TagCodec using the given namespace. If namespace is empty,
// DefaultNamespace is used.
func NewTagCodec(namespace string) TagCodec {
	if namespace == "" {
		namespace = DefaultNamespace
	}
	return TagCodec{namespace: namespace}
}

// Namespace returns the namespace string, used as an Atlas tag and API filter.
func (tc TagCodec) Namespace() string { return tc.namespace }

// Format produces the description tag for a (name, cohort) pair.
func (tc TagCodec) Format(name, cohort string) string {
	return fmt.Sprintf("[%s:%s:%s]", tc.namespace, name, cohort)
}

// Parse extracts the (name, cohort) pair from a string that may contain an
// atlasctl description tag. The tag may appear anywhere in the string.
//
// Returns ok=false if the namespace is absent, the closing "]" is missing, or
// name or cohort would be empty.
func (tc TagCodec) Parse(desc string) (name, cohort string, ok bool) {
	anchor := tc.namespace + ":"
	start := strings.Index(desc, anchor)
	if start == -1 {
		return "", "", false
	}

	rest := desc[start+len(anchor):]

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

// FormatTag is a package-level convenience using DefaultNamespace.
// Prefer TagCodec.Format when a configured namespace is available.
func FormatTag(name, cohort string) string {
	return NewTagCodec("").Format(name, cohort)
}

// ParseTag is a package-level convenience using DefaultNamespace.
// Prefer TagCodec.Parse when a configured namespace is available.
func ParseTag(desc string) (name, cohort string, ok bool) {
	return NewTagCodec("").Parse(desc)
}
