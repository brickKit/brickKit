# Test types and scenario acceptance

This reference accompanies [Testing strategy](02-testing-strategy.md): look up applicability, minimum outcomes and
specific acceptance criteria by type. The strategy owns selection, layering, responsibilities and execution evidence;
this page keeps the details available for consultation as needed. These are recommendations, not claims that every suite
exists or universal component release conditions. Components choose their language, framework, business protocols,
degradation and recovery; the platform verifies mechanical assembly and projects verify assembled outcomes.

## Test catalog

IDs connect tests to acceptance records. General techniques and specific scenarios overlap: one suite may prove several
outcomes. Boundary, error, compatibility and security dimensions apply throughout. Suggested tiers are development,
pre-commit, release/integration acceptance, scheduled and runtime. Budget quick regressions separately from extended
fuzzing, mutation, performance comparisons and drills.

### Behaviour, interfaces and real dependencies

| ID / type | Condition and owner | Minimum result | Suggested tier |
| --- | --- | --- | --- |
| T01 Unit | Platform, component and library implementers | Specified return values, error codes, state and effects; branch coverage with controlled time and randomness | Development, pre-commit |
| T02 Business rules and state transitions | Components with business invariants | Implementation-independent rules, accepted and rejected transitions, idempotency and boundaries; expectations from requirements tables | Development, pre-commit |
| T03 Interface contracts and compatibility | API, event, configuration and CLI contract maintainers | Correct shapes, errors and payloads; detect breaking changes, preserve supported old-file semantics and independent-release combinations | Pre-commit, release |
| T04 Real cross-component protocol | Components and callers | Prepare data through real interfaces; verify results through serialization, authorization and error propagation without writing another component's storage | Release/integration acceptance |
| T05 Real-dependency integration | Database, queue, object-store or engine users | Correct queries, constraints, real commits, locks, persistence, protocols and wiring; query counts satisfy interface budgets | Pre-commit or integration environment |
| T06 Migration and repeat execution | Components with storage migrations | Real-store schema, data and repeated execution follow the migration contract | Pre-commit, release |
| T07 Permission and data-scope matrix | Components with identity, tenant, ownership or field permissions | Identity by operation by scope by field; correct grants, denials, exclusion and trimming; no omitted new entry points | Pre-commit |
| T08 Optional and failing dependencies | Components with optional dependencies or external calls | Missing variables, unreachable dependencies and timeouts follow the component's failure or declared degradation contract | Development, integration acceptance |

### Finding counterexamples and checking shared specifications

| ID / type | Condition and owner | Minimum result | Suggested tier |
| --- | --- | --- | --- |
| T09 Property | Owners of graphs, calculations, ordering and merge rules with many combinations | Independent conservation, idempotency, ordering, node preservation or round-trip properties; reproducible inputs and seeds | Small pre-commit runs, larger scheduled runs |
| T10 Fuzz | YAML, reference-expression, file and protocol parser maintainers | Malformed input cannot crash or bypass constraints; required rejections occur, valid inputs preserve semantics or round-trip properties | Corpus pre-commit, extended scheduled runs |
| T11 Golden, snapshot and characterization | Large-output, rendering and pre-refactor behaviour maintainers | Reviewable differences, normalized timestamps/random identifiers, missing baselines fail; explicit business assertions remain | Pre-commit |
| T12 Scoped mutation | Core rule and validation maintainers, especially AI-written code | Broken conditions or removed validation are detected; explain surviving mutations without treating a score as correctness | Critical changes, scheduled |
| T13 Semantic vectors / known answers | Shared multilingual, codec or signature semantics; contract owners supply, implementers integrate | Standard results/errors for common inputs, including rejection cases; explicit bytes, time and randomness; never automatically accept answers from the implementation under test | Development, pre-commit |
| T14 Black-box runtime conformance | Shared runtime protocol owners and implementers | Running authorization, errors, idempotency, configuration and lifecycle satisfy the protocol; interface files alone cannot prove it | Pre-commit subset, full release suite |
| T15 Interchangeable implementation conformance | Shared-contract owners and interchangeable components, adapters or third-party implementations | The same versioned suite proves interchangeability, declared capability differences and consumer compatibility | Each implementation's release |
| T16 Fake/real shared contract | Teams maintaining both a substitute and a real implementation | Both run identical assertions; the fake passing does not replace real-implementation evidence | Pre-commit, real-dependency acceptance |
| T17 Checker and suite self-tests | Custom lint, architecture gate, checklist and black-box suite maintainers | Good fixtures pass; each singly broken fixture fails the intended rule; invoke the actual checker | Checker changes; fast cases pre-commit |

### Concurrency, recovery and runtime forms

| ID / type | Condition and owner | Minimum result | Suggested tier |
| --- | --- | --- | --- |
| T18 Runtime race detection | Implementers with concurrent shared memory | No detected data races on exercised paths; no inference about cross-instance business idempotency | Pre-commit |
| T19 Multi-instance business contention | Concurrent jobs, consumers, claims, leases or locks; component/library maintainers | Real instances compete for one key, job or time slot; verify execution counts, data and final state, including exits and retries | Integration environment, release |
| T20 Failure injection and workflow recovery | Transactions, components or stores crossed by a flow; delivery, compensation, retry and reconciliation owners | Recovery after step failure, crashes, duplicate/out-of-order delivery or a lost downstream-success response; no duplicate business effects | Integration, release; scheduled as needed |
| T21 Atomic operations and failure rollback | CLI installation, removal, upgrade, release or atomic-write maintainers | Observable files, tags, caches and business state at each failure point match that operation's guarantee, beyond an error return | Pre-commit |
| T22 Lifecycle and shutdown | Process supervisors, background work and shell/component maintainers | Initialization failure, cancellation, exit, signals, children and unfinished work are handled as contracted | Pre-commit, real-runtime acceptance |
| T23 Standalone/shell equivalence | Members supporting both forms and their shell maintainers | Same version and black-box cases in each form; correct isolated configuration, permissions, errors, jobs, observation identity and shutdown | Relevant changes, release |

### Versions, real deployment and delivery

| ID / type | Condition and owner | Minimum result | Suggested tier |
| --- | --- | --- | --- |
| T24 Existing-data upgrade | Persistent component upgrades; component maintainers | Representative data from every supported old version remains usable through real interfaces, including in-flight flows, jobs and messages; fresh installation is insufficient | Release |
| T25 Mixed-version coexistence | Rolling updates, supported coexistence or independent release units; service/contract owners | Supported new/old calls and reads/writes, including migrated storage and new events; jobs are not duplicated | Release |
| T26 Rollback compatibility | Explicit rollback promises; component/delivery maintainers | New writes and changed environments remain usable after rollback; irreversible steps follow the stated policy, with no assumption of automatic downgrade | Release |
| T27 Real-engine assembly/integration acceptance | Assembling projects and platform integration maintainers | Actual migrations, startup, health, configuration, addresses, authorization and repeated deployment on selected Docker, Podman or Kubernetes targets | Release/integration acceptance |
| T28 Complete business flow | Critical cross-component flow owners and projects | Real entry to observable business outcome, verifying data and events rather than startup alone | Release/integration acceptance, milestones |
| T29 Artifact installation/release smoke | CLI and installable-delivery maintainers | Actual shipped artifacts install, verify and execute key commands; state whether services were really deployed | Release, after installation |
| T30 Environment compatibility matrix | Supported OS, CPU, engine, dependency or install variants; delivery maintainers | Supported combinations actually complete key install, upgrade and runtime flows; separate compilation and execution evidence | Release |
| T31 Disconnected/offline delivery | Explicit offline promises; delivery maintainers | Declared flows work without network access; image/dependency/contract preparation is explicit and missing materials fail correctly | Before offline delivery |

### Frontends and localization

| ID / type | Condition and owner | Minimum result | Suggested tier |
| --- | --- | --- | --- |
| T32 Frontend pure logic | Formatting, validation, state or stores; frontend maintainers | Correct normal, boundary, error and state-change behaviour | Development, pre-commit |
| T33 UI components/interactions | Shared UI or page interactions; frontend maintainers | Observable input, rendering, interaction, loading, empty and failure states; shared parts first | Development, pre-commit |
| T34 Real-browser/real-backend E2E | Critical frontend flows; frontend and project maintainers | Real login, navigation, submission, backend denials and failure states work; substitutes are not actual wiring evidence | Stable pages, release/integration acceptance |
| T35 Visual regression | Stable interfaces with appearance requirements; frontend maintainers | Reviewable layout, density, spacing and shared-component changes with fixed fonts, rendering environment and baselines | Stable-page pre-commit |
| T36 Frontend permission visibility | Permission-based menus, buttons or routes; frontend maintainers | Unauthorized entries are hidden/disabled; direct access is denied as specified; backend authorization remains independent | Pre-commit, role E2E |
| T37 Internationalization/pseudolocalization | Multilingual CLI/UI; message and frontend maintainers | Matching keys, placeholders, translations and message sources; UI also covers expanded text, plurals, dates, time zones and clipping | Pre-commit, critical-page regression |
| T38 Browser/device compatibility | Multiple supported browsers, screens or devices; frontend maintainers | Key combinations and real-device flows, input and layout work | Scheduled, release |
| T39 Accessibility | Interfaces with accessibility requirements; frontend maintainers | Keyboard, focus, labels, screen readers, contrast and error messages; separate automated and manual evidence | Pre-commit, manual release acceptance |

Writing and running T34/T35 follows the existing page-stability, cost and user-authorization guidance in
[Testing strategy](02-testing-strategy.md).

### Static guards and traceability

| ID / type | Condition and owner | Minimum result | Suggested tier |
| --- | --- | --- | --- |
| T40 Static/architecture gates | Platform, component and project rule maintainers | Mechanical formatting, dependency-direction, forbidden-pattern, version and configuration rules hold; checkers also have T17 | Development, pre-commit |
| T41 Documentation/example consistency | Documentation maintainers | Links, anchors, current references, fields, commands, output and bilingual structure match the real implementation | Pre-commit |
| T42 Generated-file freshness/stability | Schema, message, bundle and code-generator maintainers | Committed output matches regeneration from authoritative sources; repeated generation is stable | Pre-commit |
| T43 Checklists and executable evidence | User-promise and test-entry maintainers | Named tests exist, are selected and run; empty suites or incorrect filters cannot pass silently; assertions prove the checklist promise | Pre-commit |

### Performance, observability, security and manual acceptance

| ID / type | Condition and owner | Minimum result | Suggested tier |
| --- | --- | --- | --- |
| T44 Performance/resource baseline | Critical computations, interfaces or resource budgets; their maintainers | Record versions, hardware and workload; actually measure latency, throughput, memory and allocations; distinguish benchmarks from ordinary tests | Release, scheduled |
| T45 Load/stress/soak | Peak, capacity or long-running goals; service/shell capacity owners | Target load met, stress thresholds known, no unacceptable long-term leaks and promised member budgets respected | Scheduled, before capacity changes |
| T46 Logs/audit/metrics/traces/alerts | Implementers and operations with these outputs | Complete event fields, no leaked secrets, distinct member identities, connected traces, bounded metric cardinality; constructed data triggers and delivers alerts | Pre-commit, after install/upgrade |
| T47 Dependency/security scans/attack surface | Dependency, secret and external-entry owners; security and implementation maintainers | Known vulnerabilities, licenses, leaked credentials and entry risks checked; dynamic scans/pentests depend on exposure and requirements, not a replacement for T07 | Pre-commit, release, scheduled |
| T48 Backup recovery/disaster recovery | Persistent data or recovery promises; service and operations owners | Real restoration works within time and data-loss promises; HA/disaster-recovery systems also test failover | Scheduled, delivery acceptance |
| T49 Exploration/manual acceptance | CLI, applications and assemblies; users and project owners | Mistake-prone configuration, error suggestions, flows and usability meet needs; record discoveries and remaining manual coverage | Each milestone, release |
| T50 Controlled chaos/reliability drills | Timeout, retry, isolation, degradation or HA promises; service and operations owners | Protection and recovery under disconnection, node exits or overload meet promises; observability, rollback and limited impact are in place | Acceptance environments, scheduled |

## Proving the specific scenarios

### Contention, recovery and real commits

T18 only detects races within exercised process-memory paths; it cannot prove T19's business guarantee across instances.
Start real instances, coordinate competition, then assert counts, data and state, rather than calls to a locking method.
For T20 inject failures at write, message-commit, downstream-response and compensation boundaries, then check independent
business invariants. Operations crossing transactions in one process qualify. T50 can share injection tools but proves
availability and protective behaviour rather than replacing workflow-state recovery.

Integration isolation must allow the behaviour under test to happen. Tests wrapped in rollback-only transactions cannot
alone prove visibility to another connection, contention, deferred constraints, worker/Outbox claims or genuine rollback.
Use isolated databases or other environments supporting real commits. Across component boundaries, continue preparing
data through the owning interface; see [Seed data](04-seed-data.md) for isolation.
Count data queries/round trips for list and detail requests at different result sizes and realistic permission scopes.
Assert the interface's budget and absence of unexpected per-row growth; there is no universal SQL-count limit.

### Shared suites and shell equivalence

Contract maintainers review T13 answers and T14 suites against the specification and version them with it. Implementations
pin the version and declare optional capabilities; failing implementations cannot silently change expected answers.
The [interchangeable component family](01-component-design.md#component-or-config-switch-recognising-a-slot-family)
corresponds to T15; teams with fake and real implementations also check T16. T14, T15 and T23 may reuse black-box cases,
but prove protocol conformance, interchangeability and runtime-form equivalence respectively. A second codebase is not
required.

Run the same member version standalone and inside the shell. Where applicable, verify distinct configurations cannot
overwrite one another, identities and scopes stay isolated, error semantics match, jobs are not duplicated, observation
identities remain distinct, connection/concurrency budgets hold, and initialization failure and shutdown behave correctly.
Assertions on generated DNS, ports, environment values and JSON prove platform assembly; actual member behaviour needs
runtime evidence. Components can choose to wire suites into [release checks](../03-component-guide/07-release-workflow.md);
the platform does not define a universal business protocol.

### File, data and version compatibility

Keep actual three-layer and manifest files saved by supported historical releases. Verify reading, validation, defaults,
references and derived semantics. Round-tripping files written by the current code can hide simultaneous writer/reader
mistakes. Define the support range; unsupported formats fail with migration guidance, without adding implicit rewrites,
rename guesses or type conversion.

T06 repeatable migration, T24 existing-data upgrade, T25 mixed versions and T26 rollback prove different outcomes. Have
old versions produce representative data, then check it through real interfaces after upgrading. Exercise both versions'
reads/writes for coexistence, and new-version writes followed by old-code access where rollback is promised. Persistent
flows, jobs and messages also need pending, claimed and retrying states from the old version. An upgrade allowing active
work must prove continuation and deduplication; a drain-first upgrade must prove draining and remaining-state handling.
Completed records alone are insufficient.

Independently released CLI/market, component interfaces and frontend/backend pairs exercise supported combinations through
real calls, artifacts and error responses. Many independently released callers may justify consumer-driven contracts.
Exact version pins do not prove semantic compatibility. Shell members still require exact compiled-in versions: test
valid combinations and mismatches being refused, rather than promising arbitrary member-version compatibility.
See [Multiple versions at once](../03-component-guide/09-multi-version-coexistence.md) for ownership of data coexistence;
CLI configuration migration and business-storage upgrade have separate evidence. Mark unpromised rollback, offline,
accessibility, capacity and recovery targets as pending rather than creating product guarantees.

### Real assembly, complete flows and artifacts

T27 proves integration on a real engine, T28 proves an entry-to-outcome business flow and T29 proves installation and
execution of actual released artifacts. Record targets, engine/dependency versions, configuration, migrations and request
results. Compilation, generated files, startup logs and dry-run cannot replace every outcome. Across deployment targets,
compare shared environment values, addresses and runtime contracts rather than identical Docker and Kubernetes files.
