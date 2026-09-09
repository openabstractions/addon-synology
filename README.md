# addon-synology

**In development.** No tagged release; the DSM package here has not been
through a release yet.

**Abstraction jobd is a Synology DSM package that downloads files on your NAS.**
Hand it a link from your PC and it keeps fetching while your machine sleeps, is
closed, or is switched off; the file is waiting on the NAS when you come back.

This repository is the native DSM package, installed through Package Center.
For a container instead — on a Synology or on any other NAS that runs Docker —
use [docker-jobd](https://github.com/openabstractions/docker-jobd). It is the
same program with the same behaviour; only the way it is installed differs.

## The problem this solves

A download started on a laptop dies with the laptop. Moving it to the NAS
usually means a second download manager with its own web page, its own queue
and its own idea of where files go.

This package runs a supervisor on the NAS that watches one folder. Your PC
writes a request into that folder over SMB; the NAS notices it, fetches the
file, resumes after an interruption instead of starting again, checks the bytes
against a sha256, and leaves the file on the share. Neither machine opens a
network connection to the other — the shared folder is the whole interface, so
a NAS that is switched off looks exactly like one that has not got to the job
yet, and the PC can take the work back.

## Install

Building needs Go 1.26 or later and nothing else: no Synology toolchain, no
cross-compiler, no Docker. Nothing is uploaded anywhere.

```
git clone https://github.com/openabstractions/addon-synology.git
cd addon-synology
bash spk/build.sh
```

This writes `spk/dist/AbstractionJobd-0.2.0-1-x86_64.spk` and prints its
sha256. There is no published release to download yet; building it is the only
route.

In DSM: **Package Center → Manual Install**, choose that file, finish the
wizard, and leave "Run this package after installation" ticked.

### The package is not signed

Synology signs its own packages and DSM checks that signature. This one carries
no signature at all, so Package Center refuses it until **Settings → General →
Trust Level** is set to "Any publisher", and that setting applies to every
package you install afterwards, not just this one.

We are not asking you to leave it there. What the setting is protecting you
from is installing a package whose origin you cannot check — so check the
origin yourself instead:

- If you built the `.spk` with the commands above, nothing was downloaded and
  the sha256 the build printed is your own. There is nothing further to verify.
- If somebody handed you a `.spk`, run `sha256sum` on it and compare against
  the value published beside the file you were told to expect. A `.spk` is an
  ordinary tar archive: `tar -tf` lists it, and `tar -xf` extracts the scripts
  DSM will run as root before you install it.

Then set Trust Level back.

### What the install does

`postinst` creates a shared folder called `abstraction` on the package's volume
and the store inside it, and DSM registers the daemon to start at boot. From
your PC the store is `\\<nas>\abstraction\store`. Creating the share needs
`synoshare`, whose argument list is undocumented and moves between DSM
releases; if it fails the install still succeeds and the folder is there to
share by hand from Control Panel.

`x86_64` covers every Intel and AMD model — the binaries are static and depend
on no libc. For ARM, `GOARCH=arm64 PKG_ARCH=arm64 bash spk/build.sh`, untested.

**Uninstalling** stops the daemon and leaves the job store alone: records and
downloaded files stay in the `abstraction` shared folder. `postuninst` prints
the path; delete them yourself when nothing needs them.

## Asking it for a file

Put a text file in `wanted/` on the share — `\\<nas>\abstraction\store\wanted\`
— with one URL per line:

```
https://huggingface.co/HuggingFaceTB/SmolLM2-135M-Instruct-GGUF/resolve/main/smollm2-135m-instruct-q8_0.gguf
```

Within a sweep the file is renamed `.accepted`; when the download has landed it
becomes `.done`, and inside it says where the file went and how big it is. A
URL may be followed, in either order, by `sha256:<64 hex>` to have the bytes
verified and by a folder such as `models/` to choose where under the store it
lands; without one it lands in `files/`. A request the NAS will not act on is
renamed `.refused` and one that broke is renamed `.failed`, with the reason
inside either. Nothing is installed on your PC to do this.

## The Abstraction Downloads window

The package adds an **Abstraction Downloads** icon to the DSM desktop: what the
NAS is fetching, a box to start a download, and two settings — where finished
files land inside the share, and how many days they stay.

**That window is plain HTTP on port 8734 of the NAS, reachable by anything on
your LAN, and a random 48-character key is the only thing in front of it.** DSM
does not proxy a package's own port: the desktop icon opens
`http://<host>:8734/?k=<key>` directly, so the reverse proxy, the HTTPS
certificate and the DSM session that protect Package Center do not apply. The
key is generated at install and kept `0600` under the package account at
`/var/packages/AbstractionJobd/etc/ui.key`. Anyone holding that key, or able to
read that URL, can start and cancel downloads and change where files land.
Treat it as a tool for a LAN you trust; there is no TLS and no DSM login in
front of it.

## Using it from a program

A program built on
[abstraction-download](https://github.com/openabstractions/abstraction-download)
hands work to the NAS on its own once `ABSTRACTION_NAS_STORE` points at the
store — it never names a NAS in its own code. That library, its `nas` package
and the command-line `dl` are documented there.

## Status

Experimental.

Run on a DS218+ (DSM 6.2.4-25556) with the package payload extracted by hand:
the daemon started, reported and stopped; a 386 MB download submitted from a
Windows machine was fetched and digest-verified by the NAS with no process left
running on the PC, and was delivered to the PC when it came back. The window was
driven the same way with `curl` — a real download started, a setting that
survived a restart, a refusal shown. Both packaged binaries are static x86-64
ELF and execute on that device's 4.4 kernel.

**A Package Center install has not been verified**: that `postinst` creates the
shared folder, that the package account can write the store, that the desktop
icon appears, and that DSM starts the package at boot. Installing needs root,
which SSH on DSM does not give. Do it from Package Center and say what it said.

Known gaps: a partly fetched download restarts from zero on the NAS rather than
resuming, and bytes already fetched on the PC are not handed over. Credentials
for gated sources (a Hugging Face token, say) must be set in the daemon's
environment separately, because the request only ever names the credential.
Reads of a job record across an SMB share can lag writes by tens of seconds on
an otherwise idle share, which does not affect correctness but can make
progress look stalled.

## Requirements

Go 1.26 or later to build. DSM 6.2-24922 or later on an x86_64 model to
install.

## Licence

Apache License 2.0. See [LICENSE](LICENSE).
