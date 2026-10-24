"""发布版本唯一规则：主版本 0–9，次版本 0–99，修订号 0–999。"""
import pathlib
import re
import sys

PATTERN = re.compile(r"^[0-9]\.(0|[1-9][0-9]?)\.(0|[1-9][0-9]{0,2})$")


def parse(raw):
    text = raw.strip().removeprefix("Weir version:").strip().removeprefix("v")
    if not PATTERN.fullmatch(text):
        raise ValueError("invalid version")
    major, minor, patch = (int(part) for part in text.split("."))
    if major > 9 or minor > 99 or patch > 999:
        raise ValueError("version above 9.99.999")
    return major, minor, patch


def format_version(parts):
    return "%d.%d.%d" % parts


def next_version(raw):
    major, minor, patch = parse(raw)
    if (major, minor, patch) == (9, 99, 999):
        raise ValueError("9.99.999 cannot be incremented")
    if patch < 999:
        return format_version((major, minor, patch + 1))
    if minor < 99:
        return format_version((major, minor + 1, 0))
    return format_version((major + 1, 0, 0))


def read_file(path):
    return pathlib.Path(path).read_text(encoding="utf-8")


def main(argv):
    if len(argv) < 3:
        raise SystemExit("usage: version.py check|next|match FILE [TAG]")
    command, path = argv[1], argv[2]
    current = format_version(parse(read_file(path)))
    if command == "check":
        print(current)
        return
    if command == "next":
        print(next_version(current))
        return
    if command == "match":
        tag = argv[3]
        if tag != "v" + current:
            raise SystemExit("tag does not match VERSION")
        print(current)
        return
    raise SystemExit("unknown command")


if __name__ == "__main__":
    main(sys.argv)
