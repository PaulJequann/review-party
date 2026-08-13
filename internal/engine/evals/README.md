# Packaged evaluation corpora

`general-bugs/` retains the first corpus directory name for local history, but
its manifest now identifies `global:canary-bugs@canary-bugs-v1`. Those atomic
fixtures validate evaluation plumbing and result-contract compatibility only.

`realistic-general-bugs/` contains `global:general-bugs@general-bugs-v2`. Its
cases use multi-file reviewer views and adversarial known-clean changes. Files
named `go.mod.txt` are an embedding-only representation; packaged fixture
materialization presents them to the Reviewer as ordinary `go.mod` files. This
conversion does not apply to Caller-owned filesystem suites.

Case authority (`case.json`, expected Findings, and clean evidence) stays
outside each `base/` and `head/` tree and must never enter a Synthetic Review
Subject.
