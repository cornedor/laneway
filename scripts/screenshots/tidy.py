"""Swap the kit's config path in a capture for the one a reader would have."""
import os
import re
import sys

path = sys.argv[1]
text = open(path, encoding="utf-8").read()
nice = "~/.config/laneway/config.yaml"
here = re.escape(os.path.dirname(os.path.abspath(__file__)))
# The pane truncates long paths with an ellipsis, so match any tail.
text = re.sub(here + r"\S*", lambda m: nice.ljust(len(m.group(0))), text)
open(path, "w", encoding="utf-8").write(text)
