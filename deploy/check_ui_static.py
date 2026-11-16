"""Invoke the installed global frontend-static-scan skill with this project's config."""
import os
from pathlib import Path
import subprocess
import sys


def main():
    codex_dir = Path(os.environ.get("CODEX_HOME") or Path.home() / ".codex")
    locations = [Path.home() / ".agents/skills", codex_dir / "skills", Path.home() / ".claude/skills"]
    script = next((folder / "frontend-static-scan/scripts/check_ui_static.py" for folder in locations
                   if (folder / "frontend-static-scan/scripts/check_ui_static.py").is_file()), None)
    if script is None:
        print("未安装全局 frontend-static-scan 技能。请从 shusfun/claude 的 skills/frontend-static-scan 安装后重试。", file=sys.stderr)
        return 2
    args = sys.argv[1:]
    if not any(arg == "--root" or arg.startswith("--root=") for arg in args):
        args = ["--root", str(Path(__file__).resolve().parent.parent), *args]
    return subprocess.run([sys.executable, str(script), *args]).returncode


if __name__ == "__main__":
    sys.exit(main())
