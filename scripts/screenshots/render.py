"""Turn `tmux capture-pane -e` output into a PNG, via HTML and headless Chromium.

From Koen Hendriks' screenshot kit."""
import html
import os
import re
import subprocess
import sys

# Tokyo Night, for the 16 ANSI slots the TUI actually paints with.
BASE = [
    "#15161e", "#f7768e", "#9ece6a", "#e0af68", "#7aa2f7", "#bb9af7", "#7dcfff", "#a9b1d6",
    "#414868", "#ff7a93", "#b9f27c", "#ff9e64", "#7da6ff", "#c8a2f8", "#0db9d7", "#c0caf5",
]
BG, FG = "#1a1b26", "#c0caf5"

# Cell metrics are exact: JetBrains Mono advances 0.6em, so 15px type is a 9px cell.
FONT_PX, CELL_W, LINE_H, PAD = 15, 9, 20, 22


def xterm256(n):
    if n < 16:
        return BASE[n]
    if n < 232:
        n -= 16
        r, g, b = n // 36, (n % 36) // 6, n % 6
        lv = [0, 95, 135, 175, 215, 255]
        return "#%02x%02x%02x" % (lv[r], lv[g], lv[b])
    v = 8 + (n - 232) * 10
    return "#%02x%02x%02x" % (v, v, v)


class Style:
    def __init__(self):
        self.reset()

    def reset(self):
        self.fg = self.bg = None
        self.bold = self.dim = self.italic = self.underline = self.reverse = False

    def copy(self):
        s = Style()
        s.__dict__.update(self.__dict__)
        return s

    def css(self):
        fg, bg = self.fg or FG, self.bg
        if self.reverse:
            fg, bg = bg or BG, fg
        out = []
        if fg != FG:
            out.append("color:%s" % fg)
        if bg:
            out.append("background:%s" % bg)
        if self.bold:
            out.append("font-weight:700")
        if self.dim:
            out.append("opacity:.62")
        if self.italic:
            out.append("font-style:italic")
        if self.underline:
            out.append("text-decoration:underline")
        return ";".join(out)


def apply_sgr(st, params):
    i = 0
    while i < len(params):
        p = params[i]
        if p in (0, None):
            st.reset()
        elif p == 1:
            st.bold = True
        elif p == 2:
            st.dim = True
        elif p == 3:
            st.italic = True
        elif p == 4:
            st.underline = True
        elif p == 7:
            st.reverse = True
        elif p == 22:
            st.bold = st.dim = False
        elif p == 23:
            st.italic = False
        elif p == 24:
            st.underline = False
        elif p == 27:
            st.reverse = False
        elif 30 <= p <= 37:
            st.fg = BASE[p - 30]
        elif p == 39:
            st.fg = None
        elif 40 <= p <= 47:
            st.bg = BASE[p - 40]
        elif p == 49:
            st.bg = None
        elif 90 <= p <= 97:
            st.fg = BASE[p - 90 + 8]
        elif 100 <= p <= 107:
            st.bg = BASE[p - 100 + 8]
        elif p in (38, 48):
            target = "fg" if p == 38 else "bg"
            if i + 1 < len(params) and params[i + 1] == 5:
                setattr(st, target, xterm256(params[i + 2]))
                i += 2
            elif i + 1 < len(params) and params[i + 1] == 2:
                setattr(st, target, "#%02x%02x%02x" % tuple(params[i + 2:i + 5]))
                i += 4
        i += 1


SGR = re.compile(r"\x1b\[([0-9;]*)m")
OTHER_ESC = re.compile(r"\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[\[\(][0-9;?]*[A-Za-z]")


def to_html(text):
    """One span per cell, on an exact grid: icon glyphs from fallback fonts do
    not advance by one cell, and would otherwise shift the rest of the line."""
    classes, order = {}, []

    def cls(css):
        if css not in classes:
            classes[css] = "c%d" % len(classes)
            order.append(css)
        return classes[css]

    out = []
    for line in text.split("\n"):
        line = OTHER_ESC.sub(lambda m: m.group(0) if SGR.fullmatch(m.group(0)) else "", line)
        st, parts, pos = Style(), [], 0

        def emit(chunk, style):
            css = style.css()
            for ch in chunk:
                if ch == " " and not css:
                    parts.append('<i> </i>')
                elif css:
                    parts.append('<i class="%s">%s</i>' % (cls(css), html.escape(ch)))
                else:
                    parts.append('<i>%s</i>' % html.escape(ch))

        for m in SGR.finditer(line):
            emit(line[pos:m.start()], st)
            raw = m.group(1)
            params = [int(x) if x else 0 for x in raw.split(";")] if raw else [0]
            apply_sgr(st, params)
            pos = m.end()
        emit(line[pos:], st)
        out.append("".join(parts))

    css_rules = "\n".join(".%s{%s}" % (classes[c], c) for c in order)
    return "\n".join(out), len(out), css_rules


PAGE = """<!doctype html><meta charset="utf-8"><style>
@font-face {{ font-family: 'JB'; src: local('JetBrainsMono Nerd Font'), local('JetBrains Mono'); }}
html,body {{ margin:0; padding:0; background:{bg}; }}
pre {{
  margin:0; padding:{pad}px;
  font-family:'JetBrainsMono Nerd Font','JetBrains Mono','Symbols Nerd Font Mono',
              'Noto Sans Symbols 2','DejaVu Sans Mono',monospace;
  font-size:{fs}px; line-height:{lh}px; letter-spacing:0;
  color:{fg}; background:{bg};
  font-variant-ligatures:none; -webkit-font-smoothing:antialiased;
  white-space:pre; tab-size:8;
}}
i {{ display:inline-block; width:{cw}px; height:{lh}px; line-height:{lh}px;
     vertical-align:top; font-style:normal; overflow:visible; }}
{rules}
</style><pre>{body}</pre>"""


def visible_width(line):
    """Columns the line occupies once escapes are gone and wide glyphs counted."""
    import unicodedata
    plain = SGR.sub("", OTHER_ESC.sub("", line))
    w = 0
    for ch in plain:
        if unicodedata.combining(ch):
            continue
        w += 2 if unicodedata.east_asian_width(ch) in ("W", "F") else 1
    return w


def render(ansi_path, png_path, cols=0):
    text = open(ansi_path, encoding="utf-8", errors="replace").read()
    text = text.rstrip("\n")
    if not cols:
        cols = max((visible_width(l) for l in text.split("\n")), default=80)
    body, rows, rules = to_html(text)
    page = PAGE.format(bg=BG, fg=FG, fs=FONT_PX, lh=LINE_H, pad=PAD,
                       cw=CELL_W, rules=rules, body=body)
    html_path = png_path + ".html"
    with open(html_path, "w", encoding="utf-8") as f:
        f.write(page)
    w = cols * CELL_W + 2 * PAD
    h = rows * LINE_H + 2 * PAD
    cmd = [
        os.environ.get("CHROMIUM", "chromium"), "--headless", "--disable-gpu", "--hide-scrollbars",
        "--force-device-scale-factor=2", "--default-background-color=00000000",
        "--window-size=%d,%d" % (w, h),
        "--screenshot=" + png_path,
        "file://" + os.path.abspath(html_path),
    ]
    r = subprocess.run(cmd, capture_output=True, text=True)
    if not os.path.exists(png_path):
        print(r.stderr[-2000:], file=sys.stderr)
        raise SystemExit("chromium produced no screenshot")
    try:
        # A terminal has few colours: a palette PNG is under half the size.
        from PIL import Image
        im = Image.open(png_path).convert("RGB")
        im.quantize(colors=256, method=Image.Quantize.MEDIANCUT,
                    dither=Image.Dither.NONE).save(png_path, optimize=True)
    except ImportError:
        pass
    print("%s  %dx%d css px, %d cols x %d rows" % (png_path, w, h, cols, rows))


if __name__ == "__main__":
    render(sys.argv[1], sys.argv[2], int(sys.argv[3]) if len(sys.argv) > 3 else 0)
