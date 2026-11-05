# Native bounded policy rejection proposal

Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

Exact source base: `a74f122c88269ec3b2300aae952d52e7d7befa94` in the native
Open Ontologies fork. This directory has a different base from the historical
`../open-ontologies/` series. Do not apply those older patches as prerequisites.

`0001-bounded-enforcement-report.patch` limits the native mutation rejection's
serialized report to three examples, bounded Unicode names/targets and exact
counts. All policy evaluations and the transaction's deny decision remain in
the original engine. The separate advisory policy-check API remains unchanged.
A proxy cannot recover a complete policy verdict from an oversized lost reply
or prevent a native process from constructing that reply; this is engine work.

Qualification: `git apply --check` passed against the matching native source.
**Not applied, not compiled, not runtime-tested, not deployed.** This preserves
a submission candidate; it is not a claim that native report construction is
fixed. The proposal still builds the original complete rule-results vector and
performs every evaluation. It bounds serialization, not evaluation CPU, target
collection or that vector's memory. Bounded collection needs additional design.

Before submission, add native tests for exact distinct-target/rule counts,
Unicode truncation and <8 KiB rejection envelopes over large policies, plus
allowed/denied/error transactions and graph/registry preservation. Compile the
full native crate and run the policy/transaction suites with the pinned features.
Verify that consumers distinguish summary example counts from total counts.
Never apply this proposal directly inside a running container.
