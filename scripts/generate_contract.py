#!/usr/bin/env python3
"""Copy vendored schemas and contract tables into the Go embed directory."""

from pathlib import Path
from shutil import copyfile

ROOT = Path(__file__).resolve().parent.parent
SOURCE = ROOT / "vendor" / "cwa"
TARGET = ROOT / "internal" / "generated"


def main() -> None:
    (TARGET / "schema").mkdir(parents=True, exist_ok=True)
    for source in sorted((SOURCE / "schema").glob("*.schema.json")):
        copyfile(source, TARGET / "schema" / source.name)
    for name in ("reasons.json", "slot-defaults.json"):
        copyfile(SOURCE / "contract" / name, TARGET / name)


if __name__ == "__main__":
    main()
