# Secrets and private sources

## Secrets

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

## Private sources

`git` dependencies use the host's git setup, so SSH keys and credential helpers work. Git never prompts for a password during a build.

`url` dependencies and `[source]` downloads send a bearer token if `NFB_TOKEN_{HOST}` is set, with the host in capitals and `.` and `-` replaced by `_`. For example, `NFB_TOKEN_EXAMPLE_COM` is used for `https://example.com/`. Tokens are only sent over `https`.
