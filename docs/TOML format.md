# TOML Configuration Reference

This document describes the TOML file formats used by NFB.

## Project TOML

The `build.nfb.toml`(or whatever else it is named) file defines a project's build configuration. It must be located at the root of the project.

### Example

```toml
[project]
name = "nfb" # name in the project section cannot be removed and must be present.
version = "4.2.0"

[build]
image="debian:bookworm-slim" # can be any docker image
command = [
  "cmake -B build -G Ninja -DCMAKE_C_COMPILER=zig cc -DCMAKE_CXX_COMPILER=zig c++ -DCMAKE_BUILD_TYPE=Release",
  "cmake --build build"
]

[build.env]
GOOS="windows"

[[dependency]]
name = "go-compiler" # name is an optional field that is there purely for debugging / verbosity and readability reasons, it can be safely removed
source = "cmd" # used for simple command installations
command = [
  "apt install -y golang",
]

[[dependency]]
name = "zig"
source = "git"
url = "https://github.com/ziglang/zig-nfb"
ref = "0.16.0"

[[dependency]]
name = "ninja"
source = "git"
url = "https://github.com/username/ninja-nfb" # will auto install using the package manifest from the repo
ref = "v1.12.1"
# path = "llvm.nfb.toml" # file inside the repo, defaults to package.nfb.toml

[[dependency]]
name = "LLVM"
source = "url"
url = "https://raw.githubusercontent.com/username/2026.0/llvm-nfb/llvm.nfb.toml" # direct link, can be any file name
sha256 = "..." # optional, checksum of the file at url

[[dependency]]
source = "local"
path = "package.nfb.toml" # relative to this manifest

[output]
dir = "dist" # the directory where the artifacts live

[[output.artifact]]
from = "bin/win64/nfb.exe"
to = "build/nfb.exe"

[[output.artifact]]
from = "bin/win64/libs/"     # trailing slash = copy contents only
to = "build/libs/"

```

### Dependency sources

`source` says where a dependency comes from. Everything except `cmd` is an NFB package. A field not listed for a source is an error.

| `source` | Required           | Optional       | Description                                |
|----------|--------------------|----------------|--------------------------------------------|
| `cmd`    | `command`          | `name`         | Runs the commands to install the dependency |
| `git`    | `url`, `ref`       | `name`, `path` | Package in a git repository                |
| `url`    | `url`              | `name`, `sha256` | Package manifest at a direct link        |
| `local`  | `path`             | `name`         | Package manifest on the filesystem         |

## Package TOML
The default file that will be used from git etc. is package.nfb.toml, it *can* be changed, but it is not advised for the user-facing packages for convenience reasons

### Example
```toml
[package]
name = "package" # name in the package defenition must stay the same.
version = "4.2.0"

[build] # exact same build step setup as above
image="debian:bookworm-slim"
command = [
    "cmake build"
]

[[dependency]]
name = "cmake"
source = "cmd"
command = [
  "apt install -y cmake",
]

# no need to define [output] dir as that is handled by NFB.
[[output.artifact]]
from = "bin"
to = "." # will output it to {package-name}/bin/ as the bin didnt have a / at the end

# ran when the package is imported, from the packages root output folder
[init]
command=[
    'export PATH="$PWD/bin:$PATH"', # use absolute paths, the working directory changes after init
    'export DEPENDENCY="$PWD/bin/binary"'
]

```

### Source

The optional `[source]` section tells NFB to download a package's source. It is only valid in a package.

```toml
[package]
name = "LLVM"
version = "19.1.0"

[source]
url = "https://example.com/llvm-project-19.1.0.src.tar.xz" # .tar.gz, .tar.xz or .zip
sha256 = "..." # optional, checksum of the file at url

[build]
image = "debian:bookworm-slim"
command = [
    "cmake -S llvm-project-19.1.0.src/llvm -B build",
    "cmake --build build"
]
```

`url` is required and `sha256` is optional. NFB verifies the checksum, then unpacks the archive as it is into the build's starting directory before any commands run.

## Build behaviour

NFB generates a Dockerfile and runs it with `docker build --output type=local`. Dependencies are built first, each in its own image.

### Build contents

- The project's source is copied into the container.
- A `git` package starts with its repository checked out at `ref`, including submodules.
- A `local` package's source is the folder containing its manifest.
- A `url` package only has its manifest, so it gets its source through `[source]` or fetches it in `command`.
- Package outputs are available at `/nfb/deps/{package-name}/`, using the name from `[package]`.

### Commands

All commands in a build run in one shell with `set -e`, so the first failing command stops the build. They run in this order:

1. `cmd` dependencies
2. `[init]` of each package dependency, from that package's root output folder. NFB returns to the starting directory afterwards.
3. `[build]`

`[init]` is a package's import hook, like an `on_import()` function. It runs in the shell of the project or package importing it, and does not run when the package itself is built. Packages add to `PATH` and other environment variables by exporting them in `[init]`. A project cannot be imported, so `[init]` in a project is an error.

### Dependency rules

- Only direct dependencies are visible. If A depends on B and B depends on C, A does not see C's outputs or `[init]`.
- Each package builds against its own dependency versions.
- Two versions of the same package in one consumer is an error.
- `[build.env]` is not inherited. A package only gets the environment it declares.
- `source = "local"` is rejected in packages fetched through `git` or `url`.
- A `local` path can use `../` to reach folders outside the manifest's own.
- A `git` path must stay inside the repository, so `../` and absolute paths are errors.
- A dependency must be a package. A project manifest cannot be used as one.
- `version` is required in a package. It does not have to match `ref`.

### Caching and pinning

A build is identified by its manifest hash, dependency hashes, image, environment and architecture.

Branch names in `ref` and image tags such as `debian:bookworm-slim` are allowed. `nfb.lock` records the resolved commits, image digests and checksums.

When a `url` download has no `sha256`, NFB saves its checksum to `nfb.lock` on the first fetch. Later builds fail if the file changes.

`ref` can be a branch, a tag or a commit. To pin something exactly, use a commit in `ref` and a `sha256` for downloads.

| Command              | What it does                                                                        |
|----------------------|-------------------------------------------------------------------------------------|
| `nfb update`         | Re-resolves every ref, checksum and image, and rewrites `nfb.lock`                  |
| `nfb build --locked` | Fails if `nfb.lock` is missing or out of date, and never writes it. Use this in CI. |

### Build cache

Every build can use `/nfb/cache`, also available as `$NFB_CACHE`. Its contents stay between builds of the same package name, even when a build fails, and are never part of the output. `--no-cache` ignores it.

The build step reruns in full whenever its source or manifest changes, so point your compiler's cache here to keep rebuilds fast:

```toml
[build]
image = "python:3.12-slim-bookworm"
command = [
  'scons cache_path="$NFB_CACHE/scons"'
]
```

### Secrets

`secrets` lists host environment variables to pass into a build. They are available as normal environment variables, and are not stored in image layers.

```toml
[build]
image = "debian:bookworm-slim"
secrets = ["GITHUB_TOKEN"]
command = [
  'curl -H "Authorization: Bearer $GITHUB_TOKEN" https://example.com/private.tar.gz -o private.tar.gz'
]
```

- A build fails if a listed variable is not set on the host.
- A package fetched through `git` or `url` cannot ask for secrets.
- Changing a secret's value does not trigger a rebuild.
- Do not put secrets in `[build.env]`, because it is stored in image layers.

### Private sources

`git` dependencies use the host's git setup, so SSH keys and credential helpers work. Git never prompts for a password during a build.

`url` dependencies and `[source]` downloads send a bearer token if `NFB_TOKEN_{HOST}` is set, with the host in capitals and `.` and `-` replaced by `_`. For example, `NFB_TOKEN_EXAMPLE_COM` is used for `https://example.com/`. Tokens are only sent over `https`.

## Output paths

| Path               | Relative to                                                     |
|--------------------|-----------------------------------------------------------------|
| `from`             | The build's starting directory, even after a `cd`. Absolute paths are allowed. |
| `to` in a project  | `output.dir`, which defaults to `dist`                          |
| `to` in a package  | `{package-name}/`                                               |

A trailing slash on `from` copies the folder's contents. A `to` that ends in `/` or is `.` is a folder to copy into. Any other `to` is the exact final path.

| `from`                 | `to`      | Result                         |
|------------------------|-----------|--------------------------------|
| `a/tool` (file)        | `bin/app` | `bin/app`                      |
| `a/tool` (file)        | `bin/`    | `bin/tool`                     |
| `a/tool` (file)        | `.`       | `tool`                         |
| `repo/bin` (folder)    | `.`       | `bin/...`                      |
| `repo/bin` (folder)    | `libs/`   | `libs/bin/...`                 |
| `repo/bin` (folder)    | `libs`    | `libs/...`                     |
| `repo/bin/` (contents) | `.`       | Contents in the root           |
| `repo/bin/` (contents) | `libs/`   | `libs/...`                     |
| `repo/bin/` (contents) | `libs`    | `libs/...`                     |

These are errors:

- `from` does not exist, or has a trailing slash but is a file
- Two artifacts write to the same path
- `to` is absolute or escapes with `../`
- `[output] dir` is set in a package
- A project has no artifacts

A package with no artifacts is allowed, for packages that only provide `[init]`.

Wildcards are not supported. Symlinks and file permissions are kept. Declared artifacts are overwritten, and nothing else in `output.dir` is deleted.