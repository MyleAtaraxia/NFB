# Package store

The package store is a single repository of NFB packages that anyone can add to with a pull request. NFB reads it through a `metadata.json` that is published to GitHub Pages on every merge.

## Using packages

| Command                     | What it does                                                     |
|-----------------------------|------------------------------------------------------------------|
| `nfb search [term]`         | Lists packages, optionally only those matching `term`            |
| `nfb install llvm`          | Adds the latest version to the manifest, or moves to it if it's already there |
| `nfb install llvm@19.1.0`   | Adds an exact version                                            |
| `nfb remove llvm`           | Removes the package from the manifest                            |

`nfb install` adds a dependency and updates `nfb.lock`, it does not build anything. Comments and layout in the manifest are kept.

```toml
[[dependency]]
name = "llvm"
source = "store"
version = "19.1.0"
```

Versions are always exact, there are no ranges. `nfb update` re-resolves pins, but it never changes a version. To move to a newer version, run `nfb install` again.

Once `nfb.lock` has a package, builds no longer need the store to be reachable.

`NFB_STORE` overrides the URL of `metadata.json`, for forks and self-hosting.

## Adding a package

A package is a folder in `packages/` with two files. Nothing else is allowed in it, so sources come from `[source]` or `command`.

```text
packages/llvm/package.nfb.toml
packages/llvm/meta.toml
```

```toml
# meta.toml
description = "A compiler toolchain" # required
license = "Apache-2.0"
homepage = "https://llvm.org"
platforms = ["linux/amd64"] # the default, shown by nfb install when it doesn't match
```

Prefer official prebuilt archives where they exist, as the store holds recipes and not binaries. Everyone who uses a package that compiles from source has to wait for it.

Run `nfb pin` next to `package.nfb.toml` to download `[source]` and write its `sha256`, it is not meant to be written by hand.

The rules for a package are:

- The folder name must match `[package]` name, and can only use lowercase letters, numbers and `-`.
- `[source]` must have a `sha256`. A `git clone` in `command` should use a tag or a commit, which is checked in review.
- Dependencies can only use `cmd` and `store`.

`nfb store check --base origin/main --build` runs the same checks as a pull request.

## Versions

Versions are the upstream version as it is, such as `1.1.1w` or `4.7.2-stable`. A fix to the package itself is a new version such as `19.1.0-2`.

Versions come from git history. To publish a new one, edit `package.nfb.toml` and change `version`. Every commit that adds a version is where that version lives, and the version added last is the latest, even if it is an older release.

A published version cannot be changed, unless the package is dynamic. To hide a broken one, add it to `meta.toml`:

```toml
yanked = ["19.1.0"]
```

A yanked version cannot be installed and is never the latest. Projects already using it keep working, with a warning.

## Dynamic versions

A package that uses `{version}` can be installed at any version, not only the ones published. NFB replaces `{version}` everywhere in the manifest with the version that was asked for. A shell's `${version}` is left alone.

```toml
[package]
name = "cmake"
version = "4.4.5" # used by nfb install cmake, and the only version sha256 covers

[source]
url = "https://github.com/Kitware/CMake/releases/download/v{version}/cmake-{version}-linux-x86_64.tar.gz"
sha256 = "..."

[build]
image = "debian:bookworm-slim"

[[output.artifact]]
from = "cmake-{version}-linux-x86_64/"
to = "."
```

- `nfb install cmake@3.31.12` uses the same manifest. If that version doesn't exist or doesn't build, pick another version or another package.
- Only the version in `[package]` is reviewed and has a `sha256` in the manifest. For any other, the checksum is saved to `nfb.lock` on the first download, so that download is trusted as it is.
- A fix to the manifest doesn't need a new version. `nfb.lock` keeps a project on the manifest it resolved.
- `nfb store check --build` only builds the version in `[package]`.
- Any version can be added to `yanked`, even one that was never published.

The history of `main` must never be rewritten, because `metadata.json` links to its commits.
