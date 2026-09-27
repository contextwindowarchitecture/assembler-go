# github.com/contextwindowarchitecture/assembler-go

A Go assembler for the [Context Window Architecture](https://contextwindowarchitecture.io) (CWA) draft specification. It admits candidate items, resolves declared conflicts, fits them to a token budget, renders the payload and emits the trace.

Status: in development. The current report records 0/52 passing cases and 22/22 rejected invalid snapshots. Valid snapshots currently return an explicit implementation gap.

## Install

TODO: how to add the package to a project.

## Use

TODO: the call and its types, in the language's terms. What it must say:

- `assemble(snapshot)` takes a snapshot in the shape of `schema/snapshot.schema.json`, the frozen assembly input (R-23), and returns the payload, the rendered UTF-8 bytes, or null when the assembly is refused (`trace.refused.reason` says why), together with a trace valid against `schema/trace.schema.json`.
- A snapshot that fails its schemas or the snapshot checks is rejected with its problems in words: no payload and no trace (R-17).
- A snapshot that names a tokenizer or renderer this package does not provide is unsupported, not invalid. The package provides the tokenizers `fixture-whitespace/v1` and `estimate-utf8/v1` and the renderers `fixture-xml/v1` and `cwa-messages/v1`; callers pass their model's tokenizer keyed by the id their snapshots name, and a built-in id cannot be redefined.
- `trace_id` and `timings` may differ between runs of the same snapshot (R-23); everything else, the payload bytes included, is deterministic.

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
go run ./cmd/conformance
```

This runs every vendored case and rejection snapshot as `conformance/README.md` describes. It writes `conformance-report.json`, valid against `schema/conformance_report.schema.json`, and exits 1 unless every case passed and every rejection snapshot was rejected. A case passes only when its payload matches byte for byte and its trace matches field for field, except `trace_id` and `timings`. The committed report is the current run: a test fails when it goes stale. A case is skipped when its snapshot names a tokenizer or renderer this package does not provide.

## The contract

`vendor/cwa/` holds the published contract this implementation follows: the schemas, the contract data and the conformance cases, copied from the website repository. `vendor/cwa.lock.json` pins each file by SHA-256 and records the website commit. It is Apache-2.0 licensed; see `vendor/cwa/LICENSE` and `vendor/cwa/NOTICE`.

See [AGENTS.md](AGENTS.md) for the working rules.

## License

Apache License 2.0, the same as the specification: see [LICENSE](LICENSE) and [NOTICE](NOTICE).
