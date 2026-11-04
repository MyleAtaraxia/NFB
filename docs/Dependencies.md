# Dependencies

## Dependency sources

`source` says where a dependency comes from. Everything except `cmd` is an NFB package. A field not listed for a source is an error.

| `source` | Required           | Optional       | Description                                |
|----------|--------------------|----------------|--------------------------------------------|
| `cmd`    | `command`          | `name`         | Runs the commands to install the dependency |
| `git`    | `url`, `ref`       | `name`, `path` | Package in a git repository                |
| `url`    | `url`              | `name`, `sha256` | Package manifest at a direct link        |
| `local`  | `path`             | `name`         | Package manifest on the filesystem         |
| `store`  | `name`, `version`  |                | Package from the [package store](Package-Store.md) |

## Dependency rules

- Only direct dependencies are visible. If A depends on B and B depends on C, A does not see C's outputs or `[init]`.
- Each package builds against its own dependency versions.
- Two versions of the same package in one consumer is an error.
- `[build.env]` is not inherited. A package only gets the environment it declares.
- `source = "local"` is rejected in packages fetched through `git`, `url` or `store`.
- A `local` path can use `../` to reach folders outside the manifest's own.
- A `git` path must stay inside the repository, so `../` and absolute paths are errors.
- A dependency must be a package. A project manifest cannot be used as one.
- `version` is required in a package. It does not have to match `ref`.
