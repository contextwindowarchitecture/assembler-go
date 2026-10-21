# Contributing to assembler-go

This is a Go assembler for the [Context Window Architecture](https://contextwindowarchitecture.io) draft. The specification lives in its own repository, [contextwindowarchitecture/contextwindowarchitecture](https://github.com/contextwindowarchitecture/contextwindowarchitecture). If a change alters what an assembler must do, it starts there, and this repository follows.

[AGENTS.md](AGENTS.md) holds the full rules. Coding agents and people follow the same ones. This page is the short version.

## Set up

You need Go 1.26 or newer, and Python 3 for the scripts under `scripts/`. Install the `jsonschema` package if you want `check_report.py` to validate against the schema.

```sh
go mod download
go build ./...
go test ./...    # the full suite, conformance cases included; it must pass before every commit
```

## How changes are made

**Built from the spec alone.** This port is built from the vendored contract, not from another implementation. Don't read or copy the Python, TypeScript or Rust assemblers. Two independent ports that disagree while both pass every case expose a gap in the spec, and that gap is worth finding. Where the spec leaves a behavior open, the fix goes into the specification repository first, with a case that pins it.

**Test first.** Every behavior change starts with a test that fails for the right reason. Next comes the least code that makes it pass, then cleanup with the suite green. A bug fix starts with a test that reproduces the bug. Before claiming a test protects something, break the code on purpose and watch the test fail.

**`Assemble` stays pure.** It never touches the network, the filesystem, the environment, the clock or a model. Anything that can change the payload arrives in the snapshot.

**Documentation moves with the code.** A commit that changes behavior also updates the doc comments of the API it changes, and the README section a caller would read. The README is written for application developers. Keep its Status section to a few sentences.

## Commits

- One behavior, or one refactor, per commit, and every commit passes the full suite. When you pipe test output, run `set -o pipefail` first, or a failing run looks green.
- Use [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/): `type(scope): summary`, lower case, imperative, at most 72 characters. The body says why and lists the requirement IDs affected. AGENTS.md lists the types and scopes.
- Sign off every commit (`git commit -s`).
- The changelog is generated from commit subjects by [git-cliff](https://git-cliff.org) (`cliff.toml`), so never edit it by hand. Regenerate it at a release with `uvx git-cliff --tag vX.Y.Z -o CHANGELOG.md`.

## Conformance

The conformance cases come from the specification repository. Each one is a snapshot in, and the exact payload and trace out. The rejection cases are snapshots that must be refused before assembly. `go test ./...` runs both natively. The committed report is written through the adapter in `cmd/adapter`:

```sh
go build -o /tmp/cwa-adapter ./cmd/adapter
python3 scripts/conformance.py --command /tmp/cwa-adapter \
  --name github.com/contextwindowarchitecture/assembler-go --version 0.0.1 --language Go
python3 scripts/check_report.py
```

Rerun it after any change to the assembler or the vendored contract, and commit the report with the change. A test fails while the report is stale.

A case passes only when its payload matches byte for byte, and its trace matches field for field apart from `trace_id`, `timings` and `recovery.detail`. The runner reads the required tokenizers and renderers from the vendored `conformance/README.md`. It skips a case only when the component it lacks is optional, and fails a case that needs a required one. This port provides every published component, so nothing is skipped today. The report's `contract` member names the specification repository, the commit and whether that checkout was dirty. `check_report.py` rejects any other shape.

## The contract is vendored

`vendor/cwa/` holds the schemas, contract data and conformance cases, copied from the specification repository at the same paths. `vendor/cwa.lock.json` pins each file by SHA-256, and records the repository, the commit (`spec_commit`) and whether that checkout was dirty. Don't edit these files here, because a test fails if you do. Change the specification repository, commit there, then re-vendor:

```sh
python3 scripts/vendor_contract.py --spec ../contextwindowarchitecture   # copy from a checkout and rewrite the lock
python3 scripts/vendor_contract.py --verify                              # check vendor/cwa/ against its lock
python3 scripts/vendor_contract.py --check                               # compare vendor/cwa/ with the checkout, file for file
python3 scripts/generate_contract.py                                     # refresh the embedded schemas and tables in internal/generated/
```

Re-vendor only from a committed state of the specification repository. `internal/generated/` is never edited by hand; `TestGeneratedContractCurrent` fails while it is stale.

## CI and releases

CI ([.github/workflows/ci.yml](.github/workflows/ci.yml)) builds and tests on Go 1.26 and 1.27, and checks the committed conformance report. It also checks the vendored contract against the specification commit that `vendor/cwa.lock.json` pins.

Each tag gets a GitHub release once CI passes on the tagged commit ([.github/workflows/release.yml](.github/workflows/release.yml)). The release notes name the specification commit, then list the tag's own commits, written by git-cliff. A tag that isn't `vX.Y.Z`, such as `draft-release`, is a prerelease. A tag pushed before the workflow existed is released with `gh workflow run release.yml -f tag=<tag>`.
