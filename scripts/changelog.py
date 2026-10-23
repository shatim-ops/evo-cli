#!/usr/bin/env python3
"""Changelog fragments: each PR writes its entry to changelog.d/<topic>.md,
not to CHANGELOG.md, so PRs of one version do not conflict in the
«[Не выпущено]» section (changelog.d/README.md).

  python3 scripts/changelog.py check   fragments and «[Не выпущено]» are well formed
  python3 scripts/changelog.py fold    move the fragments into «[Не выпущено]»
                                       of CHANGELOG.md and delete them

fold runs in the version PR before a tag, rc or stable (docs/RELEASING.md):
the CHANGELOG.md of the release archive then lists the whole version.
Tests: python3 scripts/changelog-test.py
"""
import os
import re
import sys

# Keep a Changelog order; «Миграция» goes first in a version
# (docs/RELEASING.md, breaking changes, condition 5).
SECTIONS = ["Миграция", "Добавлено", "Изменено", "Устарело", "Удалено", "Исправлено", "Безопасность"]
UNRELEASED = "## [Не выпущено]"
FRAGMENTS = "changelog.d"


def parse(text, where):
    """Sections of a fragment or of «[Не выпущено]»: {section: [item, ...]}
    and problems. An item is a «- » line with the lines that follow it."""
    sections, problems = {}, []
    current, item = None, None
    for n, line in enumerate(text.split("\n"), 1):
        m = re.match(r"^### (.+?)\s*$", line)
        if m:
            current = m.group(1)
            item = None
            if current not in SECTIONS:
                problems.append(f"{where}:{n}: section «{current}», want one of: {', '.join(SECTIONS)}")
            elif current in sections:
                problems.append(f"{where}:{n}: section «{current}» twice")
            sections.setdefault(current, [])
            continue
        if line.startswith("#"):
            problems.append(f"{where}:{n}: heading {line.split()[0]}: only ### sections")
            continue
        if line.startswith("- "):
            if current is None:
                problems.append(f"{where}:{n}: item before a ### section")
                continue
            item = [line]
            sections[current].append(item)
            continue
        if not line.strip():
            if item is not None:
                item.append(line)
            continue
        if item is None:
            problems.append(f"{where}:{n}: text outside an item («- …»)")
            continue
        item.append(line)
    for name, items in sections.items():
        if not items:
            problems.append(f"{where}: section «{name}» has no items")
    out = {name: ["\n".join(i).rstrip() for i in items] for name, items in sections.items()}
    return out, problems


def fragment_paths(root):
    d = os.path.join(root, FRAGMENTS)
    if not os.path.isdir(d):
        return []
    return sorted(os.path.join(d, n) for n in os.listdir(d) if n != "README.md")


def read(path):
    with open(path, encoding="utf-8") as f:
        return f.read()


def split_changelog(text):
    """(before «[Не выпущено]» with its heading, its body, the rest)."""
    start = text.find(UNRELEASED + "\n")
    if start < 0:
        raise SystemExit(f"CHANGELOG.md: no «{UNRELEASED}»")
    body_start = start + len(UNRELEASED) + 1
    end = text.find("\n## [", body_start)
    end = len(text) if end < 0 else end + 1
    return text[:body_start], text[body_start:end], text[end:]


def check(root):
    problems = []
    fragments = []
    for path in fragment_paths(root):
        rel = os.path.relpath(path, root)
        if not path.endswith(".md"):
            problems.append(f"{rel}: not a .md file")
            continue
        sections, p = parse(read(path), rel)
        problems.extend(p)
        if not sections and not p:
            problems.append(f"{rel}: no ### section")
        fragments.append((path, sections))
    changelog = os.path.join(root, "CHANGELOG.md")
    if os.path.exists(changelog):
        _, body, _ = split_changelog(read(changelog))
        problems.extend(parse(body, "CHANGELOG.md, «[Не выпущено]»")[1])
    return problems


def fold(root):
    """Move the fragments into «[Не выпущено]»; the number folded."""
    problems = check(root)
    if problems:
        raise SystemExit("\n".join(problems))
    paths = fragment_paths(root)
    if not paths:
        return 0
    changelog = os.path.join(root, "CHANGELOG.md")
    head, body, rest = split_changelog(read(changelog))
    merged, _ = parse(body, "CHANGELOG.md")
    for path in paths:
        for name, items in parse(read(path), path)[0].items():
            merged.setdefault(name, []).extend(items)
    out = "\n"
    for name in SECTIONS:
        if merged.get(name):
            out += f"### {name}\n\n" + "\n\n".join(merged[name]) + "\n\n"
    with open(changelog, "w", encoding="utf-8") as f:
        f.write(head + out + rest)
    for path in paths:
        os.remove(path)
    return len(paths)


def main():
    root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    cmd = sys.argv[1] if len(sys.argv) > 1 else ""
    if cmd == "check":
        problems = check(root)
        print("\n".join(problems) or "ok")
        return 1 if problems else 0
    if cmd == "fold":
        print(f"folded {fold(root)} fragment(s) into «[Не выпущено]»")
        return 0
    print(__doc__, file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main())
