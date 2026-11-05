#!/usr/bin/env python3
# Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
"""Record an already completed public website export. Never pushes to a remote."""
import argparse
import fcntl
import hashlib
from pathlib import Path
import shutil
import subprocess
import tempfile

REPO = Path(__file__).resolve().parent.parent


def git(*args):
    return subprocess.check_output(['git', '-C', str(REPO), *args])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('source', type=Path, help='Completed, reviewed public web export directory')
    args = parser.parse_args()
    source = args.source.resolve(strict=True)
    if not source.is_dir() or source == REPO or REPO in source.parents or source in REPO.parents:
        parser.error('Source must be a separate directory outside this worktree')
    lock = Path(git('rev-parse', '--git-path', 'export-record.lock').decode().strip())
    if not lock.is_absolute():
        lock = REPO / lock
    with lock.open('a') as guard:
        fcntl.flock(guard, fcntl.LOCK_EX)
        if git('status', '--porcelain', '--untracked-files=all').strip():
            parser.error('Worktree has uncommitted changes; inspect and preserve them first')
        # Build a complete replacement before touching tracked files. The producer
        # must hand off a finished directory and leave it unchanged during copying.
        with tempfile.TemporaryDirectory(prefix='export-record-', dir=REPO.parent) as tmp:
            stage = Path(tmp) / 'next'
            stage.mkdir()
            for entry in source.rglob('*'):
                relative = entry.relative_to(source)
                if entry.is_symlink() or any(p.lower() == '.git' for p in relative.parts):
                    parser.error(f'Symlinks and nested Git metadata are not accepted: {relative}')
                target = stage / relative
                if entry.is_dir():
                    target.mkdir(parents=True, exist_ok=True)
                elif entry.is_file():
                    target.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copyfile(entry, target)
                    target.chmod(0o644)
                else:
                    parser.error(f'Unsupported file type: {relative}')
            if not (stage / 'index.html').is_file():
                parser.error('A completed website export must contain index.html')
            # Include names and file bytes in the identifier; no per-run timestamp
            # is written into the payload, so identical exports remain a no-op.
            digest = hashlib.sha256()
            for entry in sorted(stage.rglob('*')):
                if entry.is_file():
                    name = entry.relative_to(stage).as_posix().encode()
                    digest.update(len(name).to_bytes(8, 'big'))
                    digest.update(name)
                    digest.update(hashlib.sha256(entry.read_bytes()).digest())
            current = REPO / 'content'
            previous = Path(tmp) / 'previous'
            if current.exists():
                current.rename(previous)
            try:
                stage.rename(current)
            except BaseException:
                if previous.exists():
                    previous.rename(current)
                raise
            git('add', '-A', '--', 'content')
            if not git('diff', '--cached', '--name-only', '--', 'content').strip():
                print('No content changes; no commit created')
                return
            git('commit', '-m', f'Record web export {digest.hexdigest()[:16]}', '--', 'content')
            print('Recorded export commit:', git('rev-parse', 'HEAD').decode().strip())


if __name__ == '__main__':
    main()
