# Package TOML

The default file that will be used from git etc. is package.nfb.toml, it *can* be changed, but it is not advised for the user-facing packages for convenience reasons

## Example

```toml
[package]
name = "package" # name in the package defenition must stay the same.
version = "4.2.0"

[build] # exact same build step setup as a project
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

## Source

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
