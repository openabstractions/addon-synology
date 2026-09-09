#!/usr/bin/env bash
# Writes AbstractionJobd-<version>-<arch>.spk for Package Center's Manual Install.
# Needs Go, tar, gzip, md5sum. Nothing is sent anywhere.
#
#   bash spk/build.sh                                  jobd from service-jobd@$SERVICE_JOBD_REF
#   JOBD_SRC=<dir> JOBD_PKG=./cmd/jobd bash spk/build.sh   jobd from a checkout
#   APP_WORK=<go.work> bash spk/build.sh                   jobui against local layer modules
#   LICENSE_SRC=<file>                                 when no LICENSE sits beside spk/
set -euo pipefail
cd "$(dirname "$0")"
SERVICE_JOBD_REF="${SERVICE_JOBD_REF:-v0.2.0}"
GOARCH_TARGET="${GOARCH:-amd64}"
PKG_ARCH="${PKG_ARCH:-x86_64}"
OUT_DIR="${SPK_OUT:-$PWD/dist}"
ICON_SIZES="16 24 32 48 64 72 256"

for t in go tar gzip md5sum; do command -v "$t" >/dev/null || { echo "build.sh: needs $t" >&2; exit 1; }; done

VERSION="$(sed -n 's/^version="\(.*\)"$/\1/p' INFO.in)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/target/bin" "$WORK/target/ui/images" "$WORK/scripts" "$WORK/conf" "$OUT_DIR"

build() { CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH_TARGET" go build -trimpath -ldflags="-s -w" "$@"; }
if [ -n "${JOBD_SRC:-}" ]; then
    ( cd "$JOBD_SRC" && build -o "$WORK/target/bin/jobd" "${JOBD_PKG:-.}" )
else
    mkdir -p "$WORK/shim"
    ( cd "$WORK/shim" && GOWORK=off go mod init shim >/dev/null 2>&1 &&
      GOWORK=off go get "github.com/openabstractions/service-jobd@$SERVICE_JOBD_REF" &&
      GOWORK=off build -o "$WORK/target/bin/jobd" github.com/openabstractions/service-jobd )
fi

( cd app && GOWORK="${APP_WORK:-off}" build -o "$WORK/target/bin/jobui" . )

# mkicon needs nothing but the standard library, and where this repository sits
# in a checkout decides whether a module encloses it. Give it one that always
# does rather than depend on the answer.
mkdir -p "$WORK/icon"
cp icon/mkicon.go "$WORK/icon/"
printf 'module icon\n\ngo 1.21\n' >"$WORK/icon/go.mod"
icon() { ( cd "$WORK/icon" && GOWORK=off go run . "$@" ); }
icon -size 64 -out "$WORK/PACKAGE_ICON.PNG"
icon -size 256 -out "$WORK/PACKAGE_ICON_256.PNG"
for n in $ICON_SIZES; do
    icon -size "$n" -out "$WORK/target/ui/images/app_$n.png"
done
cp ui/config.in "$WORK/target/ui/config.in"
cp scripts/* "$WORK/scripts/"
cp conf/privilege "$WORK/conf/"
LICENSE_SRC="${LICENSE_SRC:-../LICENSE}"
[ -f "$LICENSE_SRC" ] || { echo "build.sh: no LICENSE at $LICENSE_SRC; set LICENSE_SRC" >&2; exit 1; }
cp "$LICENSE_SRC" "$WORK/LICENSE"
sed -i 's/\r$//' "$WORK"/scripts/*

# DSM refuses a non-executable start-stop-status, and chmod on an NTFS temp
# directory is not honoured, so the mode is written at tar time.
tar -C "$WORK/target" --mode=0755 --owner=0 --group=0 -czf "$WORK/package.tgz" .
sed "s/@@CHECKSUM@@/$(md5sum "$WORK/package.tgz" | cut -d' ' -f1)/" INFO.in >"$WORK/INFO"

SPK="$OUT_DIR/AbstractionJobd-${VERSION}-${PKG_ARCH}.spk"
tar -C "$WORK" --owner=0 --group=0 -cf "$SPK" INFO package.tgz conf PACKAGE_ICON.PNG PACKAGE_ICON_256.PNG LICENSE
tar -C "$WORK" --mode=0755 --owner=0 --group=0 -rf "$SPK" scripts
echo "wrote $SPK  sha256 $(sha256sum "$SPK" | cut -d' ' -f1)"
echo "Install: Package Center -> Manual Install."
echo "This package carries no signature, so DSM refuses it until Trust Level"
echo "(Settings -> General) allows any publisher. You built it here and nothing"
echo "was downloaded, so the sha256 above is your own. Set Trust Level back after."
