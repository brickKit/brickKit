package msgid

// internal/resolver/resolver.go
const (
	ResolverStrongDependencyMissing   = "resolver.strong_dependency_missing"
	ResolverLabelMissingDependency    = "resolver.label.missing_dependency"
	ResolverHintCheckSources          = "resolver.hint.check_sources"
	ResolverHintCheckPublished        = "resolver.hint.check_published"
	ResolverHintCheckVersion          = "resolver.hint.check_version"
	ResolverOptionalDependencyMissing = "resolver.optional_dependency_missing"
	ResolverLabelAffectedComponent    = "resolver.label.affected_component"
	ResolverOptionalImpactDetail      = "resolver.optional_impact_detail"
	ResolverOptionalTip               = "resolver.optional_tip"
	ResolverDependencyCycleDetected   = "resolver.dependency_cycle_detected"
	ResolverLabelCyclePath            = "resolver.label.cycle_path"
	ResolverCycleReasonDetail         = "resolver.cycle_reason_detail"
	ResolverHintCheckManifestDeps     = "resolver.hint.check_manifest_deps"
	ResolverHintMakeOptional          = "resolver.hint.make_optional"
	ResolverResourceDependenciesUnmet = "resolver.resource_dependencies_unmet"
	ResolverHintDisableComponent      = "resolver.hint.disable_component"
	ResolverUnboundDetailValue        = "resolver.unbound_detail_value"
	ResolverHintNotDeclared           = "resolver.hint.not_declared"
	ResolverHintDeclaredNotBound      = "resolver.hint.declared_not_bound"
	ResolverHintEngineMismatch        = "resolver.hint.engine_mismatch"
)
