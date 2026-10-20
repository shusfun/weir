"""Small atomic evidence writer, shared without importing the runner entrypoint."""
import json
import os


def atomic_evidence(path, data):
    temporary = path.with_suffix('.tmp')
    temporary.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding='utf-8')
    os.replace(temporary, path)
