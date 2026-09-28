#!/usr/bin/env python3
"""Check local Markdown links, changed-file formatting, and shell syntax."""

import argparse
import re
import subprocess
from pathlib import Path
from urllib.parse import unquote, urlsplit

LINK = re.compile(r"(?<!!)\[[^\]]+\]\(([^)]+)\)|!\[[^\]]*\]\(([^)]+)\)")
FENCE = re.compile(r"^\s*(`{3,}|~{3,})([^`]*)$")
PLACEHOLDER = re.compile(r"<[^>\n]+>")
OLD_COMMAND = re.compile(r"^\s*(?:\$\s*)?wso2(?:\s|$)")


def check_file(page: Path, root: Path, check_format: bool = False) -> list[str]:
    errors = []
    lines = page.read_text(encoding="utf-8").splitlines()
    if check_format:
        for number, line in enumerate(lines, 1):
            # Two spaces are an intentional Markdown hard line break.
            hard_break = line.endswith("  ") and not line.endswith("   ")
            if line.rstrip() != line and not hard_break:
                errors.append(f"{page}:{number}: trailing whitespace")
        if not page.read_bytes().endswith(b"\n"):
            errors.append(f"{page}: missing final newline")

    fence_start = None
    language = ""
    body = []
    for number, line in enumerate(lines, 1):
        match = FENCE.match(line)
        if match:
            if fence_start is None:
                fence_start = number
                language = match.group(2).strip()
                body = []
            elif match.group(1)[0] == lines[fence_start - 1].lstrip()[0]:
                if language in {"sh", "bash", "shell"}:
                    if page == root / "README.md" or (root / "docs" / "guides") in page.parents or (root / "docs" / "reference") in page.parents:
                        for offset, command in enumerate(body, fence_start + 1):
                            if OLD_COMMAND.match(command):
                                errors.append(f"{page}:{offset}: use the released command name ws")
                    # Placeholders denote values, not shell redirections.
                    syntax = PLACEHOLDER.sub("example", "\n".join(body))
                    result = subprocess.run(["bash", "-n"], input=syntax, text=True, capture_output=True)
                    if result.returncode:
                        errors.append(f"{page}:{fence_start}: shell example has invalid syntax: {result.stderr.strip()}")
                fence_start = None
            continue
        if fence_start is not None:
            body.append(line)
            continue
        for link in LINK.finditer(line):
            destination = (link.group(1) or link.group(2)).split()[0].strip("<>")
            parsed = urlsplit(destination)
            if parsed.scheme or destination.startswith(("#", "//")):
                continue
            target = (root / unquote(parsed.path).lstrip("/")) if parsed.path.startswith("/") else (page.parent / unquote(parsed.path))
            if not target.exists():
                errors.append(f"{page}:{number}: local link target does not exist: {destination}")
    if fence_start is not None:
        errors.append(f"{page}:{fence_start}: unclosed code fence")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--changed-from", help="Git base revision for formatting checks")
    args = parser.parse_args()
    root = Path(__file__).resolve().parent.parent
    pages = [*root.glob("*.md"), *root.joinpath("docs").rglob("*.md")]
    if args.changed_from:
        changed = subprocess.check_output(
            ["git", "diff", "--name-only", "--diff-filter=ACMR", f"{args.changed_from}...HEAD", "--", "*.md"],
            cwd=root, text=True,
        ).splitlines()
        formatted = {root / path for path in changed}
    else:
        formatted = set(pages)
    errors = [error for page in pages for error in check_file(page, root, page in formatted)]
    for error in errors:
        print(error)
    print(f"Checked {len(pages)} Markdown files; {len(errors)} issue(s).")
    return bool(errors)


if __name__ == "__main__":
    raise SystemExit(main())
