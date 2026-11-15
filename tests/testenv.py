"""Shared test isolation (rule G23): tests never touch the live legion/data folder.

Import this before any tool module (the tools read LEGION_DATA when they are imported, too) and call isolate().
isolate() points LEGION_DATA at one temp folder for the whole test process and removes it at exit. Tests that
need their own folder still set LEGION_DATA in setUp; isolate() is what they restore in tearDown.
"""
import atexit
import os
import shutil
import tempfile

_DIR = []


def isolate():
    if not _DIR or not os.path.isdir(_DIR[0]):
        d = tempfile.mkdtemp(prefix="legion-test-data-")
        _DIR[:] = [d]
        atexit.register(shutil.rmtree, d, True)
    os.environ["LEGION_DATA"] = _DIR[0]
    os.environ["LEGION_TEST"] = "1"   # common.data_dir() refuses the live folder while this is set (rule G23)
    return _DIR[0]
