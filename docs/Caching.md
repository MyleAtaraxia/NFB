# Caching

## Caching and pinning

A build is identified by its manifest hash, dependency hashes, image, environment and architecture.

Branch names in `ref` and image tags such as `debian:bookworm-slim` are allowed. `nfb.lock` records the resolved commits, image digests and checksums.

When a `url` download has no `sha256`, NFB saves its checksum to `nfb.lock` on the first fetch. Later builds fail if the file changes.

`ref` can be a branch, a tag or a commit. To pin something exactly, use a commit in `ref` and a `sha256` for downloads.

| Command              | What it does                                                                        |
|----------------------|-------------------------------------------------------------------------------------|
| `nfb update`         | Re-resolves every ref, checksum and image, and rewrites `nfb.lock`                  |
| `nfb build --locked` | Fails if `nfb.lock` is missing or out of date, and never writes it. Use this in CI. |

## Build cache

Every build can use `/nfb/cache`, also available as `$NFB_CACHE`. Its contents stay between builds of the same package name, even when a build fails, and are never part of the output. `--no-cache` ignores it.

The cache is shared by every build of a package name, one build at a time. `nfb clean --docker` empties it for all projects.

The build step reruns in full whenever its source or manifest changes, so point your compiler's cache here to keep rebuilds fast:

```toml
[build]
image = "python:3.12-slim-bookworm"
command = [
  'scons cache_path="$NFB_CACHE/scons"'
]
```

## Fetching and the local cache

Downloads are retried up to 3 times after network errors, stalls, and `408`, `429` and `5xx` responses. A download stalls after 60 seconds with no data, however long it has been running. Git over `http` stops after 60 seconds below 1 KB/s.

Fetched repositories and downloads are removed after 30 days without use, and fetched again if needed. `nfb clean` removes all of them. Several NFB runs can share the cache at the same time.
