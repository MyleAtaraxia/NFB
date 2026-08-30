# TOML Configuration Reference

This document describes the TOML file formats used by NFB.

## Project TOML

The `build.nfb.toml`(or whatever else it is named) file defines a project's build configuration. It must be located at the root of the project.

### Example

```toml
[project]
name = "nfb" # name in the project section cannot be removed and must be present.
version = "4.2.0"
output = "dist/nfb"

[build]
image="debian:bookworm-slim" # can be any docker image
[build.env]
GOOS="windows"

command = [
  "cmake -B build -G Ninja -DCMAKE_C_COMPILER=zig cc -DCMAKE_CXX_COMPILER=zig c++ -DCMAKE_BUILD_TYPE=Release",
  "cmake --build build"
]

[[dependency]]
name = "go-compiler" # name is an optional field that is there purely for debugging / verbosity and readability reasons, it can be safely removed
source = "cmd" # used for simple command installations
command = [
  "apt install -y golang",
]

[[dependency]]
name = "zig"
source = "git"
url = "https://github.com/ziglang/zig"
ref = "0.16.0"

# commands are executed inside of the root repo folder
command = [
  "cmake -B build -G Ninja",
  "cmake --build build --target install"
]

[[dependency]]
name = "ninja"
source = "package"
ref="v1.12.1"
url="https://github.com/username/ninja-nfb/" # will auto install using the package manifest from the toml
#for git urls it assumes that the package is at /package.nfb.toml

[[dependency]]
name = "LLVM"
source = "package"
url="https://raw.githubusercontent.com/username/2026.0/llvm-nfb/llvm.nfb.toml"
#can be any package name for the direct links

[[dependency]]
source = "package"
path = "package.nfb.toml" # also works with filesystem paths

[output]
dir = "dist" # the directory where the artifacts live

[[output.artifact]]
from = "bin/win64/nfb.exe"
to = "build/nfb.exe"

[[output.artifact]]
from = "bin/win64/libs/"     # trailing slash = copy contents only
to = "build/libs/"

```

## Package TOML
The default convention that will be pulled from gihub etc for packages is package.nfb.toml, it *can* be changed but it is not advised for the user-facing packages for convenience reasons

### Example
```toml
[package]
name = "package" # name in the package defenition must stay the same.
version = "4.2.0"

[build] # exact same build step setup as above
image="debian:bookworm-slim"
command = [
    "git clone https://github.com/user/repo",
    "cd repo",
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
from = "repo/bin"
to = "." # will output it to {package-name}/bin/ as the bin didnt have a / at the end

# ran before the build step of a project from the packages root output folder
[init]
command=[
    "export DEPENDENCY=bin/binary"
]

```