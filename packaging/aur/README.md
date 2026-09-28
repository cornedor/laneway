# AUR packages

Two packages on the AUR, both built from source by the user:

- [`laneway`](https://aur.archlinux.org/packages/laneway) — the latest release, from its source tarball.
- [`laneway-git`](https://aur.archlinux.org/packages/laneway-git) — `main`, versioned `<last tag>.r<commits>.g<hash>`.

Each directory holds that package's `PKGBUILD`; this repository is where they are
edited. The AUR repositories only ever receive what `publish.sh` pushes, so no
change belongs there directly.

Both install the binary, the bash/zsh/fish completions `laneway completion`
prints, the guide under `/usr/share/doc`, and run `go test ./...` at build time.

## Publishing

`.github/workflows/aur.yml` runs `publish.sh` in an Arch container: it clones
the AUR repository, copies the `PKGBUILD` from here, stamps the version, builds
and tests the package, regenerates `.SRCINFO`, and pushes if anything changed.
A package that would not build never reaches the AUR.

- **A release** (`v*` tag) publishes both, `laneway` at the tag's version.
- **A packaging change** publishes on demand: run the workflow from the Actions
  tab and pick which package. Bump that `PKGBUILD`'s `pkgrel` first when the
  version itself has not changed, since the AUR sorts by `pkgver-pkgrel`.

It needs one secret, `AUR_SSH_PRIVATE_KEY`: the private half of an SSH key
whose public half is on the maintainer's AUR account. The AUR host key is
pinned in the workflow.

## Running it by hand

On Arch, with that key loaded:

```
MAKEPKG_OPTS=--nodeps ./packaging/aur/publish.sh laneway-git
./packaging/aur/publish.sh laneway 1.2.3
```

`MAKEPKG_OPTS=--nodeps` skips the dependency check for a Go toolchain pacman
did not install. `AUR_REMOTE` points the push at another repository, which is
how to rehearse one against a local bare clone.
