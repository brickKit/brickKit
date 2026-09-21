package msgid

// internal/yamlcheck
const (
	YamlcheckUnknownField        = "yamlcheck.unknown_field"
	YamlcheckDidYouMean          = "yamlcheck.did_you_mean"
	YamlcheckAvailableFields     = "yamlcheck.available_fields"
	YamlcheckValueMustBeString   = "yamlcheck.value_must_be_string"
	YamlcheckValueMustNotBeEmpty = "yamlcheck.value_must_not_be_empty"
	YamlcheckValueQuoteIt        = "yamlcheck.value_quote_it"
)

// internal/yamlcheck/kind.go
const (
	YamlcheckKindScalar  = "yamlcheck.kind.scalar"
	YamlcheckKindMapping = "yamlcheck.kind.mapping"
	YamlcheckKindArray   = "yamlcheck.kind.array"
	YamlcheckKindAlias   = "yamlcheck.kind.alias"
	YamlcheckKindUnknown = "yamlcheck.kind.unknown"
)
