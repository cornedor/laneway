#!/usr/bin/env bash
# usage: publish.sh <laneway|laneway-git> [version]
#
# Pushes one package to the AUR: clone its AUR repo, take the PKGBUILD next to
# this script, stamp the version, build and test it, regenerate .SRCINFO and
# push whatever changed. Needs Arch (makepkg, updpkgsums) and an SSH key for
# the AUR account. Set MAKEPKG_OPTS=--nodeps to build against a Go toolchain
# pacman does not know about, or AUR_REMOTE to push somewhere else.
set -euo pipefail

pkgname=${1:?usage: publish.sh <laneway|laneway-git> [version]}
version=${2-}
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
template=$here/$pkgname/PKGBUILD

if [[ ! -f $template ]]; then
	echo "publish.sh: no PKGBUILD for $pkgname" >&2
	exit 1
fi

# A pkgver() function means the package tracks the branch and versions itself;
# everything else is built from a release tarball and needs one to exist.
if ! grep -q '^pkgver()' "$template" && [[ -z $version ]]; then
	echo "publish.sh: no release tag yet, nothing to publish for $pkgname"
	exit 0
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

git clone "${AUR_REMOTE:-ssh://aur@aur.archlinux.org/$pkgname.git}" "$work/$pkgname"
cd "$work/$pkgname"
# An unregistered package clones empty, leaving HEAD unborn on whatever the
# local default branch is; the AUR only takes master.
git checkout -q -B master

cp "$template" PKGBUILD
if [[ -n $version ]]; then
	sed -i "s/^pkgver=.*/pkgver=$version/" PKGBUILD
	updpkgsums
fi

# Building proves the recipe before the AUR sees it, and lets makepkg write
# back the pkgver that a pkgver() computes.
read -ra makepkg_opts <<<"${MAKEPKG_OPTS:-}"
makepkg --check --force --cleanbuild --noconfirm "${makepkg_opts[@]}"
makepkg --printsrcinfo >.SRCINFO

git add PKGBUILD .SRCINFO
if git diff --cached --quiet; then
	echo "publish.sh: $pkgname is unchanged"
	exit 0
fi

read -r pkgver pkgrel < <(awk '$1 == "pkgver" {v = $3} $1 == "pkgrel" {r = $3} END {print v, r}' .SRCINFO)
git -c "user.name=${AUR_COMMIT_NAME:-Koen Hendriks}" -c "user.email=${AUR_COMMIT_EMAIL:-aur@koenhendriks.nl}" \
	commit -q -m "upgpkg: $pkgname $pkgver-$pkgrel"
git push origin master
