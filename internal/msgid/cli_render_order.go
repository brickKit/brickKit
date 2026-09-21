package msgid

// internal/cli/render_order.go
const (
	CliRenderOrderComponentStateCalculation            = "cli.render.order.component_state_calculation"
	CliRenderOrderStartOrderTopologicalSort            = "cli.render.order.start_order_topological_sort"
	CliRenderOrderCanStartOnTheirOwn                   = "cli.render.order.can_start_on_their_own"
	CliRenderOrderOnlyReferencedByOptionalDependencies = "cli.render.order.only_referenced_by_optional_dependencies"
	CliRenderOrderLongestDependencyChainLevels         = "cli.render.order.longest_dependency_chain_levels"
	CliRenderOrderComponentsNotOnThisChain             = "cli.render.order.components_not_on_this_chain"
	CliRenderOrderNoDependencies                       = "cli.render.order.no_dependencies"
	CliRenderOrderDependsOn                            = "cli.render.order.depends_on"
	CliRenderOrderOptional                             = "cli.render.order.optional"
	CliRenderOrderOptionalNotInstalled                 = "cli.render.order.optional_not_installed"
	CliRenderOrderDependencyGraph                      = "cli.render.order.dependency_graph"
)
