# Bundled fonts

Latin subsets (Basic Latin, Latin-1, common punctuation and arrows) as woff2, all SIL Open Font License 1.1 (texts in `LICENSES/`). Made with fontTools: `varLib.instancer` (weight 400-700; `opsz` and `wdth` pinned) then `pyftsubset --layout-features='*'` (ligatures kept, hinting dropped). Sources: `google/fonts` on GitHub, `ofl/<family>/`.

| File | Font | Role | Bytes |
| --- | --- | --- | --- |
| inter.woff2 | Inter | UI | 43028 |
| ibm-plex-sans.woff2 | IBM Plex Sans | UI | 31032 |
| atkinson-hyperlegible-next.woff2 | Atkinson Hyperlegible Next | UI | 18872 |
| source-sans-3.woff2 | Source Sans 3 | UI | 47020 |
| lexend.woff2 | Lexend | UI | 21504 |
| jetbrains-mono.woff2 | JetBrains Mono | mono, ligatures | 29808 |
| fira-code.woff2 | Fira Code | mono, ligatures | 37084 |
| ibm-plex-mono-400 / -700.woff2 | IBM Plex Mono | mono | 9808 + 9812 |
| source-code-pro.woff2 | Source Code Pro | mono | 23656 |
| cascadia-code.woff2 | Cascadia Code | mono, ligatures | 28908 |

Total 300532 bytes. `css/fonts.css` declares the faces; a browser fetches a file only when the chosen stack uses it. The service worker caches `/fonts/` on first use, not at install. Uploaded fonts live in the state dir (`fonts/`) and are served from `/fonts/custom/<name>`.
