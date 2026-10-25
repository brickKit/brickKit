# Checking that an AI can find its way

The other pages say how to write a project's `AGENTS.md`, its routing and its components' documents for an AI. This one
says how to find out whether it worked: whether an AI that knows nothing about the project really reaches the right file
and answers from it.

Reading the documents yourself can't tell you. You already know the project, so every gap is filled from your own head
without your noticing — the very gaps an assistant falls into. The only check is to hand the documents to an assistant
that knows nothing, ask it what a real requester would ask, and see where it goes.

## When to run it

After a change to what an assistant reads first or uses to find its way: the project's `AGENTS.md` (its Where to look
table above all), a skill, a component's `BRICKKIT.md` or `AGENTS.md`, the layout of `docs/`. A few questions are
enough for a small change; a new project or a reorganised `docs/` deserves a fuller set.

## The examinee: a new session that loads only the project

The examinee is a new session started in the project's directory that loads only what the project itself provides: its
`AGENTS.md` and `CLAUDE.md`, its `.claude/skills/`, the cached `.brickkit/manifests/`. Nothing from your own user-level
settings, no memory, no earlier conversation — the situation of a teammate's assistant opening the project for the
first time.

**A subagent doesn't count.** A subagent started from your session carries your session's instructions and context with
it: it knows what you know, including instructions the project has since changed. In one project's tests an examinee
subagent reported, by itself, that it had been given the parent session's older project instructions — its answers
measured the parent's knowledge, not the documents.

How to get such a session depends on the tool. With Claude Code, for example: run `claude -p "<question>"` in the
project directory with `CLAUDE_CONFIG_DIR` pointing at a directory that holds only the login credentials, so no
user-level `CLAUDE.md`, skills or memory load, and deny reading your home directory in its permissions.

## The questions

- **In the requester's own words.** Ask what a user would actually type — "after a refund the order page still shows the
  old total" — not a question that already names the file ("what does `BRICKKIT.md` of `erp/order` say about refunds").
  Finding the file is the thing being tested.
- **Reference points written before the run.** For each question: which file and section it should reach, and the few
  points a right answer contains. Written afterwards, they bend towards whatever the examinee said.
- **Cover what the documents route.** Where something lives, how to configure it, a change request (which component owns
  it, which version to raise), a failure to diagnose.
- **Keep the questions in the project** (for example `dev/routing-tests/`), so the same set runs again after the next
  change and the results compare.

## Running and grading

1. Run each question **at least twice**: answers vary from run to run, and one success proves little.
2. Run **at least one weaker model** as well. A strong model makes up for a gap by searching the code; a weaker one stops
   at it, which is what you want to see.
3. Keep what the examinee **read**, not only what it answered: a right answer reached by reading the whole source tree
   is still a routing failure.
4. **Grade in another new session**, never in the examinee's: give the grader the question, the reference points and the
   examinee's answer, and have it score each point 0 or 1.

## What it turns up

From one project's runs: an entry file longer than the assistant reads in one go, cut off before the part that mattered;
a stale component table at the end of `AGENTS.md` leading examinees to the wrong component (the reason BrickKit moved the
table into its own file, `COMPONENTS.md`); a skill and the project's own rules disagreeing on which version number to
raise, so examinees stopped to ask. Each was a fix in the documents; the same questions, run again, confirmed it.

## What BrickKit does and doesn't do here

BrickKit has no command for this. Which questions matter is the project's own knowledge, and running and grading models
is outside what a deployment CLI does. What it does check is the mechanical part — `brickkit lint` reports missing files
and sections, broken links, code-map paths that are gone and files nothing leads to. This check covers what lint can't:
whether an assistant, starting from nothing, finds and understands.
