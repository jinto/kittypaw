#!/usr/bin/env python3
"""Strip ANSI escapes, spinner glyphs, and REPL prompts from `kittypaw chat
<text>` output, leaving only the model's reply on stdout.

The `kittypaw chat` 1-shot mode renders progress with a paw> spinner and
ANSI cursor controls; the actual response lands on a fresh line at the very
end. This filter keeps anything that is not a control sequence, header, or
spinner-only line.
"""
import re
import sys


def main() -> None:
    raw = sys.stdin.read()
    # Drop ANSI escape sequences.
    raw = re.sub(r"\x1b\[[0-9;?]*[a-zA-Z]", "", raw)
    # Promote carriage returns to newlines so spinner overwrites become
    # individual lines we can filter out.
    raw = raw.replace("\r", "\n")

    out: list[str] = []
    for line in raw.split("\n"):
        line = line.strip()
        if not line:
            continue
        if line.startswith("KittyPaw chat"):
            continue
        if line == "you>" or line.startswith("you>"):
            continue
        # Strip any leading "paw> " plus a run of braille spinner glyphs.
        line = re.sub(r"^(paw>\s*)?[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏]+", "", line)
        line = re.sub(r"^paw>\s*", "", line)
        if not line:
            continue
        # The CLI emits a "⚠ versions differ" warning sometimes; drop it.
        if line.startswith("⚠"):
            continue
        out.append(line)

    print("\n".join(out))


if __name__ == "__main__":
    main()
