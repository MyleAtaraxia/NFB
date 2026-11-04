# NFB

NFB For Builds.

Describe your toolchain and build steps in a TOML file. NFB runs them in Docker and copies the results back to your machine, so a build works the same on every machine that has Docker.

```toml
# build.nfb.toml
[project]
name = "hello"

[build]
image = "debian:bookworm-slim"
command = ["gcc hello.c -o hello"]

[[dependency]]
name = "gcc"
source = "cmd"
command = [
  "apt-get update",
  "apt-get install -y gcc libc6-dev"
]

[[output.artifact]]
from = "hello"
to = "."
```

```console
$ nfb build

Well hello there~
building hello
built hello to /path/to/project/dist
```

## Install

You need [Go](https://go.dev/dl/) 1.26 or newer to install NFB, and [Docker](https://docs.docker.com/get-docker/) and git to run it.

```sh
go install github.com/MyleAtaraxia/nfb@latest
```

## Features

- **Dependencies from anywhere:** a shell command, a git repository, a direct link, a local path or the package store.
- **Package store:** add a package with `nfb install llvm-mingw`.
- **Packages:** a dependency can be its own NFB package, built separately and imported through an `[init]` hook.
- **Pinning:** `nfb.lock` records exact git commits, image digests and checksums.
- **Fast rebuilds:** `/nfb/cache` keeps compiler caches between builds.
- **Private sources:** pass in secrets without storing them in image layers, and fetch with your git setup or a token.

## Commands

| Command             | What it does                                            |
|---------------------|---------------------------------------------------------|
| `nfb init`          | Create a starter manifest                               |
| `nfb install <name>`| Add a package from the store                            |
| `nfb remove <name>` | Remove a store package                                  |
| `nfb search [term]` | Search the package store                                |
| `nfb build`         | Resolve dependencies and build the project              |
| `nfb build --locked`| Build without changing `nfb.lock`, for CI               |
| `nfb update`        | Re-resolve every ref, checksum and image                |
| `nfb graph`         | Print the dependency graph in build order               |
| `nfb clean`         | Remove fetched repositories and downloads               |
| `nfb -v`            | Print the version                                       |

## Documentation

The [documentation](docs/Home.md) covers every manifest option and how builds behave.

## License

[MIT](LICENSE)
