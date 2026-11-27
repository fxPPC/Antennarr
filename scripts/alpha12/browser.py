"""Run the production server on disposable loopback listeners for the browser suite."""
import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile

SCRIPTS = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('compat', SCRIPTS / 'test-patch-compatibility.py')
compat = importlib.util.module_from_spec(spec)
spec.loader.exec_module(compat)

with tempfile.TemporaryDirectory(prefix='antennarr-alpha-ui-') as tmp:
    root = Path(tmp)
    binary = os.environ.get('ANTENNARR_TEST_BINARY')
    if not binary:
        binary = str(root / 'antennarr')
        subprocess.run(['go', 'build', '-o', binary, './cmd/antennarr'], cwd=SCRIPTS.parent, check=True)
    app = compat.Installation(root / 'data')
    with app.running(Path(binary).resolve()):
        subprocess.run([sys.argv[1], str(SCRIPTS / 'test-alpha-ui.mjs'), '--server', app.admin], check=True)

