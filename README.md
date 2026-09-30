# github.com/contextwindowarchitecture/assembler-go

A Go assembler for the [Context Window Architecture](https://contextwindowarchitecture.io) (CWA) draft specification. It admits candidate items, resolves declared conflicts, fits them to a token budget, renders the payload and emits the trace.

Status: the current report passes all 58 published assembly cases and rejects all 24 invalid snapshots.

## Install

Use Go 1.26 or newer. Once the maintainer publishes this repository, add the module with `go get github.com/contextwindowarchitecture/assembler-go`.

## Use

`Assemble(snapshotJSON, Options{})` accepts the frozen snapshot as JSON bytes. It returns a `Result` with rendered UTF-8 `Payload` and a `Trace`. A refusal has a nil payload. Invalid snapshots return `SnapshotRejectedError` with no result; unknown component ids return `UnsupportedComponentError`. Callers can supply their own tokenizers in `Options.Tokenizers` for one call, each under an id no published tokenizer uses. A tokenizer supplied under a published id, `fixture-whitespace/v1` or `estimate-utf8/v1`, stops the call before assembly: `Assemble` returns an error with no payload and no trace, whatever tokenizer the snapshot names, so a trace that names a published tokenizer always means its published count (R-16).

With the same snapshot and component implementations, repeated calls produce identical payload bytes and trace fields; `trace_id` and `timings` are the only fields allowed to vary.

Admission applies the published schema, producer permissions, route eligibility, defaults and reason precedence. A producer may send only the slots the route lists for it, narrowed by its kind; an mcp tool specification meets the capability check only where the route lists `governance.capabilities` for its producer, and is `producer_slot_not_allowed` elsewhere. Declared instruction and fact conflicts resolve or escalate through route policy, followed by source supersession, exact deduplication and source diversity caps. Missing required slots, protected unplaced slots, unresolved conflicts and insufficient evidence refuse with a trace; evidence refusals distinguish missing producer context from budget omissions with or without variants. Protected items refuse when their own cap, slot cap or the protected-only payload exceeds budget. Per-item caps and slot caps run before budget pressure, which sheds droppable items, then compresses or omits compressible items in route order, counting the whole rendered payload after each change. During budget pressure, a reduction that would cross a slot's `min_tokens` freezes that slot; a payload still over budget refuses with `slot_floor_over_budget`. Both built-in renderers are available. In `cwa-messages/v1`, a surfaced conflict member placed with `system` or `tools` carries its mark in the entry's text, `<conflict group="...">` around the unescaped body, because that text is all the model receives; the mark counts only in `result.input_tokens`. The current case coverage is recorded in `conformance-report.json`.

## Requirements

Go 1.26 or newer.

## Development

```sh
go mod download
go build ./...
go test ./...
python3 scripts/vendor_contract.py --verify
python3 scripts/check_report.py
```

After a contract update, run `python3 scripts/generate_contract.py` to refresh the embedded schemas and policy tables.

## Cost

Every reduction under budget pressure is its own fit test, and every fit test renders and counts the whole payload (conformance/README.md, Fitting). Keep the cost down at the source, as the spec advises: send no more passages than the route's budget can use, and bound slots with `max_per_source` or `max_tokens`.

## Conformance

```sh
go build -o /tmp/cwa-adapter ./cmd/adapter
python3 scripts/conformance.py --command /tmp/cwa-adapter \
  --name github.com/contextwindowarchitecture/assembler-go --version 0.1.0 --language Go
python3 scripts/check_report.py
```

The bootstrap runner checks every vendored case and rejection snapshot as `conformance/README.md` describes. It writes `conformance-report.json` and exits 1 if any supported case fails. A case passes only when its payload matches byte for byte and its trace matches field for field, except `trace_id`, `timings` and `recovery.detail`. The committed report is the current run: a test fails when it goes stale.

## The contract

`vendor/cwa/` holds the published contract this implementation follows: the schemas, the contract data and the conformance cases, copied from the website repository. `vendor/cwa.lock.json` pins each file by SHA-256 and records the website commit. It is Apache-2.0 licensed; see `vendor/cwa/LICENSE` and `vendor/cwa/NOTICE`.

See [AGENTS.md](AGENTS.md) for the working rules.

## License

Apache License 2.0, the same as the specification: see [LICENSE](LICENSE) and [NOTICE](NOTICE).
