# Build behaviour

NFB generates a Dockerfile and runs it with `docker build --output type=local`. Dependencies are built first, each in its own image. NFB is tested with Docker 29.

## Build contents

- The project's source is copied into the container.
- A `git` package starts with its repository checked out at `ref`, including submodules.
- A `local` package's source is the folder containing its manifest.
- A `url` package only has its manifest, so it gets its source through `[source]` or fetches it in `command`.
- Package outputs are available at `/nfb/deps/{package-name}/`, using the name from `[package]`.

## Commands

All commands in a build run in one shell with `set -e`, so the first failing command stops the build. They run in this order:

1. `cmd` dependencies
2. `[init]` of each package dependency, from that package's root output folder. NFB returns to the starting directory afterwards.
3. `[build]`

`[init]` is a package's import hook, like an `on_import()` function. It runs in the shell of the project or package importing it, and does not run when the package itself is built. Packages add to `PATH` and other environment variables by exporting them in `[init]`. A project cannot be imported, so `[init]` in a project is an error.
