# AI-Assisted Development Guide

BrickKit's component model happens to fit AI-assisted development well. This explains why, and how to actually use it.

New to the component contract itself? [Build your first component from scratch](guide/10-build-your-own.md) walks through the same Manifest/env-var/health-check basics an AI-generated component still has to follow — worth reading once before handing the rest to an AI, so you can tell whether what it produces is actually correct.

## Why BrickKit suits AI-written code

### 1. Component size matches an AI's context window

Measured across the 10 real components this repository ships (`tests/components/`), size ranges from 200 to 3,500 lines. Combined with the clear contract boundary a `component.yaml` draws, that's small enough for an AI to read and understand an entire component in one pass, instead of guessing its way through a half-million-line monolith.

### 2. Contract-first design gives an AI a clear boundary

Every component's `component.yaml` declares:

- which components it depends on (exact versions, required or optional)
- which ports it exposes (HTTP/gRPC)
- which config it needs (`configSchema`)
- which resources it needs (database, Redis, etc.)

An AI never has to understand the whole system — reading the current component's `component.yaml`, plus the API contract a dependency publishes (the `api-contract` entry under `artifacts`, usually `openapi.json`), is enough to write a complete, independently runnable component.

### 3. Env injection means an AI never touches config complexity

AI-generated component code only ever needs to read config like this:

```python
import os

# Required dependency
dep_endpoint = os.environ.get("DEP_ENDPOINT")

# Optional dependency — must be read defensively
optional_endpoint = os.environ.get("OPTIONAL_ENDPOINT")
if not optional_endpoint:
    # Degrade here — a missing optional dependency means this variable
    # is never injected at all; os.environ["OPTIONAL_ENDPOINT"] would
    # crash with a KeyError
    pass

# Own config
page_size = int(os.environ.get("PAGE_SIZE", "20"))
```

An AI never has to understand service discovery, config centers, or registration SDKs — the platform already solved that with environment-variable injection.

### 4. Exact versions + coexistence make an AI's "breaking change" safe

An AI-generated v2 can run right alongside v1:

- `my-component-1-0-0` and `my-component-2-0-0` are two independent containers/service names
- other components depending on v1 are unaffected
- callers can migrate over gradually, with no forced cutover

## Using AI to develop a component

Five steps in a loop, each with real feedback — not "have the AI write the whole component in one shot":

```mermaid
graph LR
    S1["1. Feed context<br/>AGENTS.md + skills"] --> S2["2. Spec first<br/>Manifest + contract"]
    S2 --> S3["3. Tests first<br/>they must be red"]
    S3 --> S4["4. Implement<br/>until the tests go green"]
    S4 --> S5["5. Run it<br/>debug if it doesn't work"]
    S5 -.->|paste the error back| S5
```

### Step 1: give the AI context on your project

`brickkit init` already generates a `.claude/skills/` directory with several AI-assistant skill files in your project. Feed it the root `AGENTS.md` too — it compresses the platform's positioning, terminology, and design principles into one file, so the AI has real judgment instead of you re-explaining "what BrickKit is" every session. If you're developing inside a standalone component repo (a `component.yaml`, no `brickkit.yaml`), run `brickkit skills update` in that directory: it installs the `brickkit-component` skill into `.claude/skills/` — the one a component author needs most.

### Step 2: have the AI write the spec first — the Manifest and the API contract, no implementation

Example prompt:

```
I need a BrickKit component:
- ID: my-scope/user-profile
- Version: 1.0.0
- Language: Python (FastAPI)
- Dependencies: people/basic@1.0.0 (required), infra/redis@1.0.0 (optional)
- Config: page_size (integer, default 20)
- Port: 8080 (HTTP)
- Health check: /healthz
- Public interface: look up and update a user's profile by user ID (describe your own interface here)

Generate only the two specs first — no implementation code at all:
1. component.yaml
2. openapi.json (OpenAPI 3.0, covering every endpoint's request/response schema)
```

The contract is written from the requirements, not reverse-engineered from code — anything depending on this component, whether an AI or a person wrote it, can start building against it right now, without waiting for `user-profile`'s implementation and without ever reading its source. Once it's written, add the component to a project (`brickkit add --local`) and run `brickkit up --dry-run`: a misspelled config key or a required config with no value surfaces at this step, with no containers started (real output in [Testing patterns](patterns/testing.md#writing-components-with-ai-spec-first-implementation-second)).

### Step 3: have the AI write the tests first, and confirm they're red

```
Based on openapi.json and the requirements above, generate:
1. Contract tests (verify every endpoint's request/response matches openapi.json)
2. Business-rule tests (write each rule as one "given … when … then …" sentence
   first, then translate it into a test)
3. Integration tests (covering at least /healthz)

Don't write the implementation yet — I want to see these fail first, because
"the interface isn't implemented".
```

A test that passes on its very first run tested nothing. This step comes before the implementation because a contract and tests reverse-engineered from the implementation can only restate it: if the contract was generated from the code, "verify the implementation matches the contract" becomes the code reconciling against itself.

### Step 4: have the AI write the implementation until the tests go green

```
Based on component.yaml, openapi.json and the tests above, generate:
1. main.py
2. Dockerfile
3. migrations/001_init.sql

Then run the tests until they all pass.
```

### Step 5: have the AI help you debug

```
My user-profile component crashes on startup with:
KeyError: 'PEOPLE_BASIC_ENDPOINT'

What's likely going on?
```

(The real cause here is almost always an optional dependency that isn't running, or component code using `os.environ[]` instead of `os.environ.get()` — see [Troubleshooting](troubleshooting.md).)

## Best practices

### Have the AI write one component at a time

Don't have an AI write several mutually dependent components in one pass — finish one, get it running, then move to the next. Every step gets real feedback that way, instead of piling up code that can only be cross-checked against other unverified code.

### Have the AI read the API contract, not the dependency's source

When you're asking an AI to write a component that depends on `people/basic`, point it at `people/basic`'s `openapi.json`, not `people/basic`'s implementation. That keeps the generated code dependent only on the contract, in line with component autonomy — whatever `people/basic` refactors internally never has to ripple outward.

### Tests before the implementation

Tests are the most direct way to check whether AI-generated code is actually correct, and the **order** matters more than whether they exist: write the tests, watch them go red, then have the AI write the implementation, and the tests genuinely constrain it; derive tests from the implementation afterwards and they only check that "the code does what it does". The full order and the reasoning are in [Testing patterns](patterns/testing.md#writing-components-with-ai-spec-first-implementation-second).

## What this doesn't replace

### Things AI can't do for you

- **Domain modeling:** how many components a feature should split into, and where the boundaries go, needs your (and a domain expert's) judgment — an AI doesn't have that business context
- **Data-consistency decisions:** which operations need a transaction vs. which can be eventually consistent is a business call, not a style preference
- **Performance tuning:** AI-generated code is usually "correct but not optimal" — tuning against real load is still on you

### Mistakes AI tends to make

- **Over-engineering:** piling up abstraction layers for a simple feature — push back and ask for the simpler version
- **Happy-path-only error handling:** edge cases like a missing optional dependency or a failed migration are easy to miss
- **Incomplete test coverage:** AI-generated tests often only cover the normal path — you still need to add the edge cases

## Read further

- [Component Design Guidelines](patterns/component-design.md) — the domain research to do before writing a component
- [Testing patterns for components built on BrickKit](patterns/testing.md) — which layers AI-generated tests should cover
- [AGENTS.md](../../AGENTS.md) — the whole platform compressed for an AI to read
