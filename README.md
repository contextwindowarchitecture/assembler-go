# github.com/contextwindowarchitecture/assembler-go

A Go assembler for the [Context Window Architecture](https://contextwindowarchitecture.io) (CWA) draft specification. It admits candidate items, resolves declared conflicts, fits them to a token budget, renders the payload and emits the trace.

Status: in development. The current report records 7/52 passing cases and 22/22 rejected invalid snapshots. Unimplemented assembly features return an explicit gap.

## Install

TODO: how to add the package to a project.

## Use

`Assemble(snapshotJSON, Options{})` accepts the frozen snapshot as JSON bytes. It returns a `Result` with rendered UTF-8 `Payload` and a `Trace`. A refusal has a nil payload. Invalid snapshots return `SnapshotRejectedError` with no result; unknown component ids return `UnsupportedComponentError`. Callers can supply tokenizers in `Options.Tokenizers` for one call; the built-in tokenizer ids cannot be replaced.

Admission applies the published schema, producer permissions, route eligibility, defaults and reason precedence. Missing required slots and protected unplaced slots refuse with a trace. Other refusals, conflict resolution, fitting, and `cwa-messages/v1` remain pending. The current case coverage is recorded in `conformance-report.json`.

## Requirements

Go 1.26 or newer.

## Development

```sh
go mod download
go build ./...
go test ./...
python3 scripts/vendor_contract.py --verify
python3 scripts/check_report.py --allow-failures
```

After a contract update, run `python3 scripts/generate_contract.py` to refresh the embedded schemas and policy tables.

## Cost

Every reduction under budget pressure is its own fit test, and every fit test renders and counts the whole payload (conformance/README.md, Fitting). Keep the cost down at the source, as the spec advises: send no more passages than the route's budget can use, and bound slots with `max_per_source` or `max_tokens`.

## Conformance

```sh
go build -o /tmp/cwa-adapter ./cmd/adapter
python3 scripts/conformance.py --command /tmp/cwa-adapter \
  --name github.com/contextwindowarchitecture/assembler-go --version 0.1.0 --language Go
python3 scripts/check_report.py --allow-failures
```

The bootstrap runner checks every vendored case and rejection snapshot as `conformance/README.md` describes. It writes `conformance-report.json` and exits 1 while ordinary cases remain pending. A case passes only when its payload matches byte for byte and its trace matches field for field, except `trace_id` and `timings`. The committed report is the current run: a test fails when it goes stale.

## The contract

`vendor/cwa/` holds the published contract this implementation follows: the schemas, the contract data and the conformance cases, copied from the website repository. `vendor/cwa.lock.json` pins each file by SHA-256 and records the website commit. It is Apache-2.0 licensed; see `vendor/cwa/LICENSE` and `vendor/cwa/NOTICE`.

See [AGENTS.md](AGENTS.md) for the working rules.

## License

Apache License 2.0, the same as the specification: see [LICENSE](LICENSE) and [NOTICE](NOTICE).
