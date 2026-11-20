"""Regenerate the stored Finder layout with ds-store 1.3.1. Release builds copy it."""
from pathlib import Path
from ds_store import DSStore

with DSStore.open(str(Path(__file__).with_name("finder-layout.bin")), "w+") as db:
    db["."]["bwsp"] = {"ShowToolbar": False, "ShowSidebar": False, "ShowStatusBar": False,
                       "ShowPathbar": False, "ContainerShowSidebar": False,
                       "WindowBounds": "{{200, 150}, {580, 340}}"}
    db["."]["icvp"] = {"viewOptionsVersion": 1, "iconSize": 100.0, "textSize": 14.0,
                       "arrangeBy": "none", "backgroundType": 0, "showIconPreview": True,
                       "labelOnBottom": True, "gridSpacing": 100.0}
    db["."]["vstl"] = ("type", b"icnv")
    db["Antennarr.app"]["Iloc"] = (135, 100)
    db["Applications"]["Iloc"] = (440, 100)
    db["Read Me.txt"]["Iloc"] = (285, 232)
