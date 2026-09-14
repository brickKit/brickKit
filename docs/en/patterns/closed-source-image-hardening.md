# Protecting Closed-Source Components from Image-Based Extraction

A closed-source component on brickKit keeps its Git repository private — the
marketplace stores only the Manifest, the image reference, and (if the
component serves an API) its contract (AGENTS.md §5.9). Nobody outside your
team ever gets your repository that way.

But an image reference is a completely different thing from a repository.
Pulling it — or exporting it with `docker save`, which needs no special
access beyond what running the component already requires — hands over
every byte the container will ever execute: compiled binaries, interpreted
source files, bundled assets, configuration, all of it, across every layer
that was ever built into the image. "Closed-source" describes where your
*repository* lives. It says nothing about what a customer holding the
*image* can recover from it. Those are two independent questions, and this
guide is about the second one — including being upfront about how much of
it a software-only platform can actually close.

## Threat model: what this protects against, and what it doesn't

This guide raises the cost of extracting your logic from a distributed
image enough to stop casual and moderately-resourced attempts — a customer
poking around out of curiosity, a competitor doing a quick look, an AI
coding assistant asked to "figure out how this works" pointed at an
extracted filesystem. It does not, and cannot, stop a well-funded, skilled,
motivated attacker. Every technique in this guide operates on bytes you
handed the attacker — given enough time and expertise, bytes can always be
read. Closing that gap the rest of the way needs hardware-backed
approaches (confidential computing, trusted execution environments) that
are entirely outside what a software distribution platform can provide,
and outside this guide's scope.

This asymmetry is deliberate, not a shortcoming: someone with the budget
and skill to defeat all of the following is also expensive enough that
attacking your specific component is rarely their best use of that skill.
The goal is raising the floor for everyone else, not building a wall for
everyone.

## Two different problems — don't solve them with the same tool

**Can someone read my logic from the image?** That's what this guide
covers: build-time hardening, and Docker image hygiene that keeps things
out of the image in the first place.

**Can someone run a copy of my software without a license, once they have
the image?** That's licensing and machine-fingerprint binding — checking a
signed license against a hardware or environment fingerprint before the
component will serve traffic. It's a real, useful control, but it solves a
different problem: it doesn't hide anything, it gates *execution*. It's
also entirely your own component's business logic, the same way
multi-tenancy or communication governance is (AGENTS.md §4.1's rejection
list) — brickKit provides no mechanism for it and shouldn't. If you need
it, implement it as ordinary startup logic in your own component, the same
way you'd implement any other business rule.

Conflating the two leads to bad decisions — e.g., assuming an obfuscated
binary is "protected" because it's also license-gated, when the gate and
the obfuscation defend against completely different people.

## Docker image hygiene — do this regardless of language

These apply to every component, whatever it's written in, and they're
usually the biggest lever: a naive single-stage image built with `COPY . .`
ships your entire source tree — README, internal design notes, test
fixtures, and (for anything with a package manager) every dev dependency —
whether or not the running process ever touches any of it.

- **Multi-stage builds; only the runtime artifact crosses into the final
  stage.** Build tools, source files, and package manager caches belong in
  an earlier stage that never gets pushed. If your final `FROM` line pulls
  in a full SDK image instead of a minimal runtime base, ask why.
- **Layers are additive — deleting something in a later layer doesn't
  remove it from an earlier one.** `COPY secrets.env .` in one layer
  followed by `RUN rm secrets.env` in the next still ships `secrets.env` in
  full: `docker save` (or any tool that walks image layers directly) reads
  the earlier layer just fine, restart from `rm` or not. The fix isn't a
  later cleanup step — it's never copying the sensitive thing into a layer
  that ships in the first place. This is one of the most common and most
  overlooked mistakes, because the running container genuinely doesn't
  have the file — inspecting a live container tells you nothing about what
  the image layers still hold.
- **No source maps in production builds.** A source map turns minified or
  obfuscated JavaScript straight back into readable, close-to-original
  source. Whatever bundler you use, confirm its production configuration
  doesn't emit or ship them (webpack's `devtool: false`/`hidden-source-map`
  kept out of the image, esbuild's `--sourcemap` left off for the shipped
  build, etc.).
- **Don't ship the compiler or package manager in the final image** unless
  the running process genuinely needs it at runtime (a REPL, a
  plugin-loading system that compiles on demand). If it's only there
  because the base image happened to include it, that's a free target for
  nothing.
- **`.dockerignore` should exclude `.git/`, README and internal
  documentation, test fixtures, and CI configuration** — none of it runs,
  none of it needs to be a byte inside the image, and `.git/` in particular
  hands over your entire commit history, not just the current tree.
- **The image reference you publish should resolve to a content digest
  (`image@sha256:...`), not stay a mutable tag** — and `brickkit publish`
  already does this for you by default. brickKit's own signing model is
  worth being precise about here: the marketplace signature covers the
  Manifest (AGENTS.md §5.9), and the image reference is a string inside
  that Manifest — so the signature guarantees the *string* wasn't altered,
  not that the string still resolves to the same bytes it did when you
  signed it. A mutable tag can be repointed at a different image after the
  fact with the signed Manifest never changing at all. `brickkit publish`
  closes exactly this gap before it ever signs anything: it resolves
  whatever tag `deployment.image` names to that image's current digest and
  rewrites the Manifest to the digest form, so the signature ends up
  covering a reference that can't be silently repointed. Skipping this
  needs an explicit `--no-pin-digest` flag, and doing so prints a loud
  warning naming this exact risk — it's a deliberate opt-out, not
  something that happens by forgetting a step. For exactly what gets
  signed and why it has to be a canonicalized payload rather than the
  Manifest's raw bytes, see [Signing and the trust
  model](../architecture/signing-and-trust.md).

## Hardening by language — the achievable ceiling is set by the compilation model, not by effort

This is the part that doesn't generalize, and pretending it does is the
fastest way to waste effort on the wrong technique. What's achievable is
set almost entirely by how your language turns source into what actually
ships — pick the wrong technique for your category and you get the
appearance of protection with none of the substance.

**Natively-compiled languages (Go, Rust, C, C++, Zig).** This category
starts strong for free: ahead-of-time compilation to machine code means a
decompiler can recover control flow and logic, but not original variable
names, comments, or source structure — reconstructing anything close to
your original source is genuinely hard, not just inconvenient. Stripping
debug symbols (`go build -ldflags="-s -w"`, `strip` for C/C++ binaries,
Rust's `strip = true` release profile) removes most of what a decompiler
would otherwise use to make its output readable, and for most components
this is already most of the achievable protection. Go specifically ships
more metadata than people expect even after stripping — package paths,
some string literals, and reflection-related type information survive
`-ldflags="-s -w"` — and
[`garble`](https://github.com/burrowers/garble) exists specifically to
close that gap: it rewrites the build to obfuscate identifiers, strip
additional build info, and obfuscate literals. Whole-program obfuscators
for Rust/C++ (LLVM-pass-based tools like `ollvm`) exist too, but the
marginal benefit over strip-and-ship drops quickly while the maintenance
and toolchain-compatibility cost keeps climbing — reach for them only if
stripped binaries genuinely aren't enough for your threat model.

**Bytecode-VM languages (Java, .NET/C#).** This category is the one where
skipping hardening is closest to publishing source directly: JVM bytecode
and CIL are both designed to preserve enough structure that decompilers
(CFR, Procyon, dnSpy, ILSpy) routinely reconstruct output close enough to
the original to read comfortably, class and method names included. An
obfuscator isn't a nice-to-have for these ecosystems, it's close to
mandatory if the logic matters to you: ProGuard or R8 for Java/Android,
DashO or Zelix KlassMaster for Java more broadly, ConfuserEx, Dotfuscator,
or Obfuscar for .NET. All of them rename identifiers, strip unnecessary
metadata, and (the stronger ones) apply control-flow obfuscation — the
combination is what actually defeats a straightforward decompile, not any
one technique alone.

**Interpreted and scripting languages (Python, Ruby, PHP, plain
unbundled Node.js/JavaScript).** The weakest category, because for most of
these the artifact that ships *is* the source, or close enough that the
distinction barely matters: Python's `.pyc` bytecode decompiles back to
close-to-original Python with mature, freely available tools
(`decompyle3` and successors); unminified JavaScript is just... readable.
Treat this category as needing either a genuine compilation step or an
honest acceptance that protection here is weak:
- **Python**: [Cython](https://cython.org/) compiles Python to a native C
  extension — this moves the component into the natively-compiled
  category above and is the strongest practical option, though it
  requires your code to be Cython-compatible and adds a real build step.
  [PyArmor](https://pyarmor.readthedocs.io/) encrypts and obfuscates
  bytecode without a Cython-style rewrite, at a materially lower — but
  non-zero — bar than Cython.
- **Node.js/TypeScript**: bundling and minification alone (webpack,
  esbuild, terser) is close to worthless against a deliberate attempt —
  identifiers get mangled, but control flow and structure stay fully
  legible. A real
  [JavaScript obfuscator](https://github.com/javascript-obfuscator/javascript-obfuscator)
  (control-flow flattening, string encryption) gets you into the same
  tier as a mediocre bytecode-VM obfuscator. Bundling into a single
  executable (`pkg`, `nexe`, or a runtime's built-in single-executable
  packaging) embeds V8 bytecode rather than source text, which is a
  materially higher bar than either of the above, though V8-bytecode
  extraction tooling exists too.
- **PHP**: `ionCube` and Zend Guard play the same role here as PyArmor
  does for Python — commercial bytecode encoders, not something brickKit
  has any opinion on beyond noting the category needs one.

Tool landscapes like this age — a name in this list may be unmaintained or
superseded by the time you read it. The category-level principle is what
should outlive any specific tool: native compilation starts strong and
mostly needs stripping; bytecode VMs need a real obfuscator to avoid
publishing source-equivalent output; interpreted languages need either a
genuine compilation step or a clear-eyed acceptance that what's shipped is
close to source either way.

## What none of this protects against

Everything above hardens the *static artifact* — the bytes sitting in the
image. None of it touches the *running process*: an attacker who can
attach a debugger, dump process memory, or trace system calls against a
live instance sees decrypted strings, deobfuscated data structures, and
plaintext values, regardless of how the on-disk binary was hardened.
Defending against that requires controlling the runtime environment itself
(who can exec into or attach to your containers, host-level isolation) —
outside what any of the above techniques, or brickKit, can provide. A
security posture that only mentions static hardening and never says this
is not being honest with its readers, so this guide says it plainly:
static hardening keeps a copy of your image from freely giving up its
logic; it does not protect a process a legitimate operator is deliberately
inspecting while it runs.

## What brickKit itself does here

Two things, and both are already built: the marketplace signs the
Manifest with cosign, verified by the CLI with no dependency on cosign
being installed (AGENTS.md §5.9); and `brickkit publish` pins a mutable
image tag to its digest by default before signing, so the signature ends
up covering a reference that can't be silently repointed later — closing
exactly the gap this guide would otherwise have to ask you to close by
hand. Everything else in this guide is your own build pipeline's
responsibility, not brickKit's. That split isn't an oversight —
it's the same principle behind every item on the platform's own
"won't do" list (AGENTS.md §4.1): brickKit connects and orchestrates
components, it doesn't audit, scan, or make business decisions about what's
inside them. An image-content scanner that tried to flag "this looks like
it might leak source" would face the exact same choice the platform
already rejected for third-party component security review (AGENTS.md
§9.11) — a high false-positive rate or a high false-negative rate, with no
middle ground a generic tool can find. Hardening your own image is squarely
your call to make, using the tools above that fit what you actually ship.
