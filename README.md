# github.com/contextwindowarchitecture/assembler-go

A Go assembler for the [Context Window Architecture](https://contextwindowarchitecture.io) (CWA) draft specification. It admits candidate items, resolves declared conflicts, fits them to a token budget, renders the payload and emits the trace.

Status: the current report passes all 61 published assembly cases and rejects all 25 invalid snapshots, the ones that use the optional renderer `cwa-message-blocks/v1` included.

## Install

Use Go 1.26 or newer. Once the maintainer publishes this repository, add the module with `go get github.com/contextwindowarchitecture/assembler-go`.

## Use

`Assemble(snapshotJSON, Options{})` accepts the frozen snapshot as JSON bytes. It returns a `Result` with rendered UTF-8 `Payload` and a `Trace`. A refusal has a nil payload. Invalid snapshots return `SnapshotRejectedError` with no result; unknown component ids return `UnsupportedComponentError`. Callers can supply their own tokenizers in `Options.Tokenizers` for one call, each under an id no published tokenizer uses. A tokenizer supplied under a published id, `fixture-whitespace/v1` or `estimate-utf8/v1`, stops the call before assembly: `Assemble` returns an error with no payload and no trace, whatever tokenizer the snapshot names, so a trace that names a published tokenizer always means its published count (R-16). Callers cannot supply a renderer: `Options` has no field for one, and the only renderers are the published `fixture-xml/v1` and `cwa-messages/v1` and the optional `cwa-message-blocks/v1`, so R-16's stop on an application renderer under a published id cannot arise, and a trace that names a published renderer always means its published rendering.

With the same snapshot and component implementations, repeated calls produce identical payload bytes and trace fields; `trace_id` and `timings` are the only fields allowed to vary.

Admission applies the published schema, producer permissions, route eligibility, defaults and reason precedence. A producer may send only the slots the route lists for it, narrowed by its kind; an mcp tool specification meets the capability check only where the route lists `governance.capabilities` for its producer, and is `producer_slot_not_allowed` elsewhere. Declared instruction and fact conflicts resolve or escalate through route policy, followed by source supersession, exact deduplication and source diversity caps. Missing required slots, protected unplaced slots, unresolved conflicts and insufficient evidence refuse with a trace; evidence refusals distinguish missing producer context from budget omissions with or without variants. Protected items refuse when their own cap, slot cap or the protected-only payload exceeds budget. Per-item caps and slot caps run before budget pressure, which sheds droppable items, then compresses or omits compressible items in route order, counting the whole rendered payload after each change. During budget pressure, a reduction that would cross a slot's `min_tokens` freezes that slot; a payload still over budget refuses with `slot_floor_over_budget`. Both required renderers are available, and so is the optional `cwa-message-blocks/v1`. Each orders the items of a placement by id, except `interaction.history`, whose turns render in the order they were said: by `freshness` compared as instants at full precision, and by id only among turns said at the same instant (R-7); `included[]` follows the same order. In `cwa-messages/v1`, a surfaced conflict member placed with `system` or `tools` carries its mark in the entry's text, `<conflict group="...">` around the unescaped body, because that text is all the model receives; the mark counts only in `result.input_tokens`. `cwa-message-blocks/v1` renders the request `cwa-messages/v1` renders, except that the user message's content is an array with one entry per `xml:` occurrence, holding its `id` and its `text` exactly as `cwa-messages/v1` writes that occurrence, and a surfaced member's entry also names its group in `conflict`. Its count, in `result.input_tokens` and in every fit test, sums the tokenizer's count of every system, tools and message entry's text, each on its own, so a tokenizer that rounds each text up, as `estimate-utf8/v1` does, can fit fewer items than under `cwa-messages/v1`. The current case coverage is recorded in `conformance-report.json`.

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

CI (`.github/workflows/ci.yml`) runs the build and tests on Go 1.26 and 1.27, checks the committed conformance report, and checks the vendored contract against the specification repository at the commit `vendor/cwa.lock.json` pins. Each tag gets a GitHub release once CI passes on the tagged commit (`.github/workflows/release.yml`); its notes name that specification commit and list the tag's own commits, written by git-cliff. A tag that is not `vX.Y.Z` is a prerelease, and a tag pushed before the workflow existed is released with `gh workflow run release.yml -f tag=<tag>`.

## Cost

Every reduction under budget pressure is its own fit test, and every fit test renders and counts the whole payload (conformance/README.md, Fitting). Keep the cost down at the source, as the spec advises: send no more passages than the route's budget can use, and bound slots with `max_per_source` or `max_tokens`.

## Conformance

```sh
go build -o /tmp/cwa-adapter ./cmd/adapter
python3 scripts/conformance.py --command /tmp/cwa-adapter \
  --name github.com/contextwindowarchitecture/assembler-go --version 0.0.1 --language Go
python3 scripts/check_report.py
```

The bootstrap runner checks every vendored case and rejection snapshot as `conformance/README.md` describes. It writes `conformance-report.json` and exits 1 unless every case passed and every rejection snapshot was rejected. A case passes only when its payload matches byte for byte and its trace matches field for field, except `trace_id`, `timings` and `recovery.detail`. `conformance/README.md` requires every implementation to provide the four components its Tokenizers and renderers section lists before its Optional heading, `fixture-whitespace/v1`, `estimate-utf8/v1`, `fixture-xml/v1` and `cwa-messages/v1`, and the runner reads that list from there. The optional components follow under Optional: `cwa-message-blocks/v1`, which this port provides too, so it skips no case. A case the adapter reports as unsupported is skipped only when its snapshot names a tokenizer or renderer outside that list, and a rejection only when its renderer is outside it; one that uses only required components has failed (Reporting results), and `check_report.py` treats a skip of such a case as a problem. The native conformance tests in `go test ./...` hold each case and rejection to the same rule. Its `contract` names the repository and commit the cases came from, the lock's `repository` and `spec_commit`, and whether that checkout was dirty, as `{"repository", "commit", "dirty"}`; `check_report.py` rejects any other contract, the earlier `{"website_commit", "dirty"}` included, and `TestReportContract` and `TestCheckReportContract` hold the report and the checker to that shape. The committed report is the current run: a test fails when it goes stale.

## The contract

`vendor/cwa/` holds the published contract this implementation follows: the schemas, the contract data and the conformance cases, copied from the specification repository, [contextwindowarchitecture/contextwindowarchitecture](https://github.com/contextwindowarchitecture/contextwindowarchitecture), where every file sits at the same path. `vendor/cwa.lock.json` pins each file by SHA-256 and records that repository, as `repository`, and the commit the files came from, as `spec_commit`, with whether they were dirty there; `TestContractLockSource` holds the lock to those members. `python3 scripts/vendor_contract.py --spec ../contextwindowarchitecture` re-vendors from a checkout of it, taking the repository from its `origin` remote, and `--check` compares `vendor/cwa/` with the checkout file for file. It is Apache-2.0 licensed; see `vendor/cwa/LICENSE` and `vendor/cwa/NOTICE`.

See [AGENTS.md](AGENTS.md) for the working rules.

## License

Apache License 2.0, the same as the specification: see [LICENSE](LICENSE) and [NOTICE](NOTICE).
