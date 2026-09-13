# Testing patterns for components built on BrickKit

This is **recommended practice**, not a BrickKit platform requirement. BrickKit itself has no opinion on how a component tests its own internals — component autonomy is one of the twelve design principles in [`AGENTS.md`](../../../AGENTS.md): language, framework, API shape, transaction handling, and log format are all the component's own call, and the platform asks for exactly three things (a manifest, a health check, environment variables). Test strategy has never been part of that list. What follows is a testing methodology that one real production deployment — 14 components, covering one complete business workflow end to end — validated in practice and fed back as a suggestion. Take from it what's useful for a project assembled on BrickKit; adopting any of it is entirely optional.

## Four layers, plus two cross-cutting categories

| Layer | What it checks | What it covers |
| --- | --- | --- |
| **L1 — Contract tests** | The interface's external shape | The contract itself has no breaking change, and every interface can actually be called across a process boundary — over the real protocol with real serialization, not just a passing local function call |
| **L2 — Business-rule tests** | Properties the feature must hold | Invariants (a balance never goes negative), valid state-transition paths, idempotency, edge cases |
| **L3 — Unit tests** | Branches of the current implementation | Every branch, every conditional, every exception path the code actually walks through in this specific version |
| **L4 — Integration tests** | The real end-to-end path | A real database, a real message queue, running the whole path from request in to row committed or message published |

Beyond the four layers there are two **cross-cutting categories** — not a fifth layer stacked after L4, but a dimension that should be threaded through tests at every one of the four layers:

| Cross-cutting category | What it covers |
| --- | --- |
| **Permission / scope-boundary tests** | What data a given identity or ownership scope should and shouldn't see — this dimension belongs in contract, business-rule, unit, and integration tests alike, not off in its own box tested once and forgotten |
| **Cross-component tests** | Verifying that a call into another component hits the real thing, not a stand-in — see the criteria under "Cross-component testing: hit real dependencies, not mocks" below |

## The one criterion that tells L2 from L3

L2 and L3 are easy to confuse — both look like they're testing "does this feature work." One question separates them:

> If you deleted this feature's entire implementation and rewrote it from scratch in a different language, would this test still have to hold? Yes → L2. No → L3.

For example: "two requests with the same idempotency key leave behind exactly one record" must still hold whether the feature is written in Go or Python — it's testing the business rule itself, so it's L2. But "the amount field gets parsed as a `Decimal` rather than a `float`" is only meaningful for this specific implementation — rewrite it in another language, or pick a different numeric type, and the assertion may not even apply anymore. That's L3.

## Two pitfalls a real deployment actually hit

**Pitfall one: idempotency tests that only replay once**

| Don't | Symptom | Do instead |
| --- | --- | --- |
| Test only a single request replayed serially — send it, send it again, assert the two outcomes match | An implementation that runs a `SELECT` to check existence before deciding to `INSERT` will, if two concurrent requests both land in that not-yet-found window, each go ahead and insert — producing a duplicate row. A serial-replay test can never land in that race window, so it can never catch this bug; it passes cleanly every time | Replace the check-then-write with one atomic operation — `INSERT ... ON CONFLICT DO NOTHING`, or your database's equivalent — and make sure the test suite includes the scenario where several concurrent requests carrying the same idempotency key arrive at once and only one of them actually executes, not just a serial replay |

**Pitfall two: permission-boundary tests that only check the two extremes**

| Don't | Symptom | Do instead |
| --- | --- | --- |
| Test only "a user with permission can see the data they're allowed to see" and "a user with zero permission sees nothing at all" | Suppose the permission check has a bug that makes the code return an entire table's worth of rows: the zero-permission user never even reaches that code path (something earlier already blocked them), and the permitted user's results aren't wrong either — the rows they're supposed to see are in there, just alongside rows belonging to someone else. Both extreme-case tests pass every time, and the bug never surfaces | Actually create two records that belong to different scopes (say, different `owner`, `department`, or `warehouse` values), query as one of those identities, and assert that the result set contains none of the rows belonging to the other identity — not just "the response was non-empty" or "the response was empty" |

## Cross-component testing: hit real dependencies, not mocks

Three criteria decide whether a cross-component test is doing its job:

1. **Produce the data by going through the dependency's real interface first**, rather than reaching past that interface into the dependency's own storage;
2. **If the dependency's contract can't yet produce the data you need, add that capability to the dependency's contract** — don't route around the contract, and don't write to the dependency's database with raw SQL either;
3. **A mock can prove, at best, that a call happened** — the right arguments, the right number of times — but it can never prove the call's result was correct. Only standing up the real dependency can verify that.

---

How to plan seed data and test data — which rows belong in migration scripts versus which should be constructed fresh at test time — is a separate question, and it's the subject of the next doc in this series (`docs/en/patterns/data-construction.md`). It hasn't been written yet, so no link to it appears here.
