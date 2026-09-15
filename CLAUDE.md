@AGENTS.md

# Development principle for this repo

When fixing a bug or adding a feature, look for the architecturally correct,
complete solution rather than the smallest patch that makes the reported
symptom go away. Effort/workload is not a constraint to optimize for here —
find the right design, then build it, even if that means touching more files
or revisiting a decision made a day ago. If a narrow fix turns out to rest on
a deeper architectural inconsistency, fix the inconsistency, don't route
around it.
