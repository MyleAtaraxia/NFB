# Output paths

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
