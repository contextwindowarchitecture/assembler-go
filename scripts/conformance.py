#!/usr/bin/env python3
"""Run the vendored conformance cases against an assembler in any language, and write conformance-report.json.

    python3 scripts/conformance.py --command "<adapter>" --name <package> --version <version> --language <Language>

The adapter command is started once per snapshot with the snapshot file's bytes on stdin. It answers by exit code:

    0  assembled, refusals included: stdout holds {"payload": <base64 of the payload bytes, or null when refused>,
       "trace": <the trace>}
    2  rejected before assembly (R-17): stderr lists the problems in the port's words; stdout is not read
    3  a tokenizer or renderer the port does not provide: stderr names it, e.g. "renderer x/v1 is not provided"

Any other exit code, or stdout that is not such an object, fails the case with stderr as the detail. The runner
compares as conformance/README.md, Running a case, says: the payload byte for byte, the trace field for field
without trace_id and timings, and it validates each trace against trace.schema.json when the jsonschema package
is installed. It writes the report in the shape of conformance_report.schema.json (Reporting results), prints a
summary, and exits 1 unless every case passed and every rejection snapshot was rejected.

This is a bootstrap tool: it gets a port a report before it has a runner of its own. A port may keep it or
replace it with a native runner that does the same; scripts/check_report.py checks either's report.
"""
from __future__ import annotations

import argparse
import base64
import json
import shlex
import subprocess
import sys
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parent.parent
LOCK = ROOT / "vendor" / "cwa.lock.json"
SCHEMAS = ROOT / "vendor" / "cwa" / "schema"
MISSING = object()


def utf16(s: str) -> bytes:
    """The sort key the README uses for ids and member names: UTF-16 code units (Ordering)."""
    return s.encode("utf-16-be")


def read_json(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8"))


def validator_for(name: str) -> Any:
    """A jsonschema validator for one published schema, resolving $refs among the vendored ones; None without jsonschema."""
    try:
        import jsonschema
    except ImportError:
        return None
    schemas = [read_json(p) for p in sorted(SCHEMAS.glob("*.schema.json"))]
    schema = next(s for s in schemas if s["$id"].endswith("/" + name))
    try:
        from referencing import Registry, Resource

        registry = Registry().with_resources((s["$id"], Resource.from_contents(s)) for s in schemas)
        return jsonschema.Draft202012Validator(schema, registry=registry)
    except ImportError:  # jsonschema before 4.18
        resolver = jsonschema.RefResolver.from_schema(schema, store={s["$id"]: s for s in schemas})
        return jsonschema.Draft202012Validator(schema, resolver=resolver)


def schema_problem(validator: Any, instance: Any) -> str | None:
    """The most relevant schema error for an instance, or None when it validates or nothing can validate it."""
    if validator is None:
        return None
    from jsonschema.exceptions import best_match

    errors = list(validator.iter_errors(instance))
    if not errors:
        return None
    error = best_match(errors)
    where = "/" + "/".join(str(p) for p in error.absolute_path) if error.absolute_path else "/"
    return f"{where}: {error.message}"


def same_kind(a: Any, b: Any) -> bool:
    """JSON kinds: true is not 1, but 12 and 12.0 are the same number."""
    if isinstance(a, bool) or isinstance(b, bool):
        return isinstance(a, bool) and isinstance(b, bool)
    if isinstance(a, (int, float)) and isinstance(b, (int, float)):
        return True
    return type(a) is type(b)


def pointer(key: str) -> str:
    return key.replace("~", "~0").replace("/", "~1")


def first_difference(expected: Any, actual: Any, path: str = "") -> str | None:
    """The JSON Pointer of the first place two JSON values differ, with both values; None when they are equal."""

    def show(value: Any) -> str:
        return "nothing" if value is MISSING else json.dumps(value, ensure_ascii=False)

    if isinstance(expected, list) and isinstance(actual, list):
        for i in range(max(len(expected), len(actual))):
            e = expected[i] if i < len(expected) else MISSING
            a = actual[i] if i < len(actual) else MISSING
            found = first_difference(e, a, f"{path}/{i}")
            if found:
                return found
        return None
    if isinstance(expected, dict) and isinstance(actual, dict):
        for key in sorted(set(expected) | set(actual), key=utf16):
            found = first_difference(expected.get(key, MISSING), actual.get(key, MISSING), f"{path}/{pointer(key)}")
            if found:
                return found
        return None
    if expected is not MISSING and actual is not MISSING and same_kind(expected, actual) and expected == actual:
        return None
    return f"{path or '/'}: expected {show(expected)}, got {show(actual)}"


def comparable(trace: Any) -> Any:
    """Trace ids and timings may differ between runs (R-23); every other member is compared."""
    if not isinstance(trace, dict):
        return trace
    return {k: v for k, v in trace.items() if k not in ("trace_id", "timings")}


class Adapter:
    def __init__(self, command: list[str], timeout: float):
        self.command = command
        self.timeout = timeout

    def run(self, snapshot: bytes) -> tuple[int | None, bytes, str]:
        """Exit code (None on timeout), stdout bytes, stderr text."""
        try:
            proc = subprocess.run(self.command, input=snapshot, capture_output=True, timeout=self.timeout)
        except subprocess.TimeoutExpired:
            return None, b"", f"timed out after {self.timeout:g} s"
        except OSError as error:
            return 127, b"", f"could not start {shlex.join(self.command)}: {error}"
        return proc.returncode, proc.stdout, proc.stderr.decode("utf-8", "replace").strip()


def parse_result(stdout: bytes) -> tuple[Any, str | None]:
    """The adapter's {"payload", "trace"} object, or a detail saying why it is unusable."""
    try:
        result = json.loads(stdout.decode("utf-8"))
    except (UnicodeDecodeError, ValueError):
        return None, "stdout is not a JSON object with payload and trace"
    if not isinstance(result, dict) or "payload" not in result or "trace" not in result:
        return None, "stdout is not a JSON object with payload and trace"
    if result["payload"] is not None:
        try:
            result["payload"] = base64.b64decode(result["payload"], validate=True)
        except (TypeError, ValueError):
            return None, "payload is not base64 or null"
    return result, None


def refusal(trace: Any) -> str:
    refused = trace.get("refused") if isinstance(trace, dict) else None
    reason = refused.get("reason") if isinstance(refused, dict) else None
    return f"refused with {reason}" if reason else "refused"


def run_case(adapter: Adapter, trace_validator: Any, case: Path) -> dict[str, str]:
    code, stdout, stderr = adapter.run((case / "snapshot.json").read_bytes())
    if code == 3:
        return {"outcome": "skipped", "detail": stderr or "a tokenizer or renderer is not provided"}
    if code == 2:
        return {"outcome": "failed", "detail": f"rejected the snapshot where the case expects an assembly: {stderr}"}
    if code != 0:
        return {"outcome": "failed", "detail": f"exited {code}: {stderr}" if code is not None else stderr}
    result, problem = parse_result(stdout)
    if problem:
        return {"outcome": "failed", "detail": problem}
    trace, payload = result["trace"], result["payload"]
    problem = schema_problem(trace_validator, trace)
    if problem:
        return {"outcome": "failed", "detail": f"the trace fails trace.schema.json at {problem}"}
    payload_path = case / "expected.payload.txt"
    expected_payload = payload_path.read_bytes() if payload_path.exists() else None
    if expected_payload is None and payload is not None:
        return {"outcome": "failed", "detail": "rendered a payload where the case expects a refusal"}
    if expected_payload is not None and payload is None:
        return {"outcome": "failed", "detail": f"{refusal(trace)} where the case expects a payload"}
    if expected_payload is not None and payload != expected_payload:
        return {"outcome": "failed", "detail": "the payload bytes differ from expected.payload.txt"}
    difference = first_difference(comparable(read_json(case / "expected.trace.json")), comparable(trace))
    if difference:
        return {"outcome": "failed", "detail": f"the trace differs at {difference}"}
    return {"outcome": "passed"}


def run_rejection(adapter: Adapter, case: Path) -> dict[str, str]:
    code, stdout, stderr = adapter.run((case / "snapshot.json").read_bytes())
    if code == 2:
        return {"outcome": "rejected"}
    if code == 3:
        return {"outcome": "skipped", "detail": stderr or "a tokenizer or renderer is not provided"}
    if code == 0:
        result, problem = parse_result(stdout)
        if problem:
            return {"outcome": "failed", "detail": f"exited 0 instead of rejecting, and {problem}"}
        if result["payload"] is None:
            return {"outcome": "failed", "detail": f"{refusal(result['trace'])} instead of rejecting"}
        return {"outcome": "failed", "detail": "assembled a payload instead of rejecting"}
    return {"outcome": "failed", "detail": f"exited {code}: {stderr}" if code is not None else stderr}


def ids(directory: Path) -> list[str]:
    return sorted((p.name for p in directory.iterdir() if p.is_dir()), key=utf16) if directory.exists() else []


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--command", required=True, help="the adapter command, started once per snapshot")
    parser.add_argument("--name", required=True, help="the implementation's package name, as the report names it")
    parser.add_argument("--version", required=True, help="the implementation's version")
    parser.add_argument("--language", help="the implementation's language, e.g. Go")
    parser.add_argument("--conformance", type=Path, default=ROOT / "vendor" / "cwa" / "conformance", help="the cases directory")
    parser.add_argument("--out", type=Path, default=ROOT / "conformance-report.json", help="where to write the report")
    parser.add_argument("--timeout", type=float, default=60, help="seconds allowed per snapshot")
    args = parser.parse_args()

    if not LOCK.exists():
        sys.exit("vendor/cwa.lock.json does not exist; run scripts/vendor_contract.py --website <checkout> first")
    lock = read_json(LOCK)
    adapter = Adapter(shlex.split(args.command), args.timeout)
    trace_validator = validator_for("trace.schema.json")
    if trace_validator is None:
        print("note: jsonschema is not installed, so traces are compared but not validated against trace.schema.json", file=sys.stderr)

    implementation = {"name": args.name, "version": args.version}
    if args.language:
        implementation["language"] = args.language
    cases_dir, rejections_dir = args.conformance / "cases", args.conformance / "rejections"
    report = {
        "implementation": implementation,
        "contract": {"website_commit": lock["website_commit"], "dirty": lock["dirty"]},
        "cases": [{"id": id, "rules": read_json(cases_dir / id / "case.json")["rules"], **run_case(adapter, trace_validator, cases_dir / id)}
                  for id in ids(cases_dir)],
        "rejections": [{"id": id, "rules": read_json(rejections_dir / id / "case.json")["rules"], **run_rejection(adapter, rejections_dir / id)}
                       for id in ids(rejections_dir)],
    }
    args.out.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")

    passed = sum(row["outcome"] == "passed" for row in report["cases"])
    rejected = sum(row["outcome"] == "rejected" for row in report["rejections"])
    print(f"{args.out}: {passed}/{len(report['cases'])} cases passed, {rejected}/{len(report['rejections'])} rejection snapshots rejected")
    for row in report["cases"] + report["rejections"]:
        if "detail" in row:
            print(f"  {row['id']}: {row['outcome']}: {row['detail']}")
    return 0 if passed == len(report["cases"]) and rejected == len(report["rejections"]) else 1


if __name__ == "__main__":
    sys.exit(main())
