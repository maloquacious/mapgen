# Project Workflow

Guidance for agents when working in this repository.

This repository is `mapgen`: a Go module, with small commands around it, that generates Earth-like world maps for games in stages.
What we are building, and its scope, are in [CLAUDE.md](CLAUDE.md). Read it before writing code.

## Priorities

Fix bugs before adding features.
When proposing what to work on next, first check for open bugs (`gh issue list --label bug --state open`).
If any are open, propose working on them instead of new features or documentation.
An explicit request from the user takes precedence; do the requested work.

## Build and test

Prefer `go run` to `go build`:

```sh
go run ./cmd/continents --seed 42 --out tmp/continents
go run ./cmd/landmask --in tmp/continents --out tmp/landmask
```

Put any binary you do build, and every other scratch artifact, under `tmp/`.
That includes generated artifact directories and preview PNGs.
Never write build artifacts or output files to the repository root.
`tmp/` is git-ignored, and `go.mod` has an `ignore` directive that keeps `./...` out of it.

Stage commands refuse to replace an existing output directory where their README says so (`landmask` does).
Use a new directory under `tmp/` for each alternative, or remove the old one first.

The full-size default grid (6666 x 3333 cells of 6 km) is slow and large.
For quick checks, pass a smaller `--cols` and `--rows` to `continents`.

The checks, from the repository root:

```sh
go vet ./...
go test ./...
```

Before committing, both must pass if the commit changes Go code, `go.mod`, or `go.sum`.
Documentation-only changes need no checks.

## Verification

The model is verified in Go, by tests.

- Every algorithm lands with the tests that define what correct means for it: an analytic case, an invariant, or a conservation budget.
- A test that needs a grid uses the smallest grid that shows the behavior, so `go test ./...` stays fast.
  Long runs belong behind `testing.Short`.
- A run must be repeatable: the same seed, flags, and input artifact give the same output artifact, byte for byte, on the same CPU architecture.
  Any change that alters the output for a given seed says so in its commit message.
- Output contracts carry their own version strings: `prng.DerivationVersion`, `fracture.Version`, and each artifact package's `Format`.
  Bump the relevant one when its contract changes, in addition to the module version.

## Versioning

- Semantic versioning.
  The current version lives in `version.go` as a `semver.Version` literal (`Major`, `Minor`, `Patch`, `PreRelease`), from `github.com/maloquacious/semver`.
  Artifact manifests record it as `generator_version`.
- **Every commit that changes Go code (including `go.mod` and `go.sum`) must bump the version in `version.go`.**
  Documentation-only commits are encouraged to bump it but are not required to.
- Choose the bump by the change: patch for fixes, minor for new features, major for any breaking change.

### Tags

- Don't tag commits.

## Git workflow

- Commit directly to `main`.
  No feature branches or pull requests; history is the recovery mechanism.
- Agents are authorized to commit and push each task to `origin` as it is completed, provided all checks pass and Go code changes include a version bump.
- Agents are authorized to create GitHub labels as needed.

## CI

Do not create GitHub Actions workflows or other GitHub CI actions.
Run the checks above locally instead.
