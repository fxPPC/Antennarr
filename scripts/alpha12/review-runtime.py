"""Verify the ARM64 runtime to which the alpha's glibc review applies.

Read a flattened Docker export on stdin and the signed Wolfi update APK as argv[1].
"""
import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import sys
import tarfile

APK_HASH = '80c3e195b50c63ea5bb4f4bf70bcbb7ddb2eef6940e5c2bc0c5271d2d447a8bb'
UPSTREAM = 'c7f65355ebd788c705ef6dddd6030bab679f7791'
LIBC = 'usr/lib/libc.so.6'


def check_runtime(apk, rootfs):
    if hashlib.sha256(apk).hexdigest() != APK_HASH:
        raise ValueError('The ARM64 glibc package differs from the reviewed Wolfi artifact')
    with tarfile.open(fileobj=io.BytesIO(apk), mode='r:gz', ignore_zeros=True) as package:
        recipe = package.extractfile('.melange.yaml').read().decode().splitlines()
        if f'expected-commit: {UPSTREAM}' not in (line.strip() for line in recipe):
            raise ValueError('Wolfi package recipe differs from reviewed glibc commit')
        expected = hashlib.sha256(package.extractfile(LIBC).read()).hexdigest()
    actual = None
    with tarfile.open(fileobj=rootfs, mode='r|*') as image:
        for entry in image:
            path = PurePosixPath(entry.name)
            if 'gconv' in path.parts or path.name.upper().startswith(('IBM1390', 'IBM1399')):
                raise ValueError(f'Runtime contains a glibc converter or converter directory: {path}')
            if path.as_posix() == LIBC and entry.isfile():
                actual = hashlib.sha256(image.extractfile(entry).read()).hexdigest()
    if actual != expected:
        raise ValueError('Installed libc differs from the signed, reviewed ARM64 APK')
    return {'libc_sha256': actual, 'apk_sha256': APK_HASH, 'source_commit': UPSTREAM,
            'contains_gconv_modules': False}


if __name__ == '__main__':
    print(json.dumps(check_runtime(Path(sys.argv[1]).read_bytes(), sys.stdin.buffer), indent=2))
