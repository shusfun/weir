"""Keep prior evidence before live checks replace the latest convenience files."""
import datetime,shutil
from pathlib import Path
root=Path('docs/evidence')
target=root/'snapshots'/datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
target.mkdir(parents=True)
for item in root.iterdir():
 if item.is_file():shutil.copyfile(item,target/item.name)
print('Prior evidence retained:',target.as_posix())
