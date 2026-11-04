package catalog

// Test adapters exercise the same compiled matcher used by catalogue imports.
func nameExcluded(c Channel, name string) bool {
	return compileChannel(c).excludes(parseSourceLabel(name, ""))
}
func nameMatches(c Channel, name, region string) bool {
	return compileChannel(c).matches(parseSourceLabel(name, region))
}
