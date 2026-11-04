# Project TOML

The `build.nfb.toml`(or whatever else it is named) file defines a project's build configuration. It must be located at the root of the project.

## Example

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

[Dependencies](Dependencies.md) lists the `source` types, and [Output paths](Output-Paths.md) explains `from` and `to`.
