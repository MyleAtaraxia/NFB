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

- A build fails if a listed variable is not set on the host, unless it has a default.
- `secret_defaults` is used when a variable is not set, which makes that secret optional. It is plain text in the manifest, so it can't hold anything sensitive.
- A fetched package needs your approval, see below.
- Changing a secret's value does not trigger a rebuild, but moving between a default and a real value does.
- Do not put secrets in `[build.env]`, because it is stored in image layers.

```toml
[build]
image = "debian:bookworm-slim"
secrets = ["GITHUB_TOKEN"]
secret_defaults = { GITHUB_TOKEN = "" }
```

## Approving secrets

Packages fetched through `git`, `url` or `store` run commands you did not write, so they can only read a secret you approved. Secrets in your own manifest and `local` packages need no approval.

`nfb install` lists the secrets a package asks for and asks about each one. `nfb build` asks about any that have no default, such as when a new version adds one. Approvals are saved in `nfb.lock`, so `nfb build --locked` never asks.

- A package without approval gets its default. If it has none, the build fails.
- Only direct dependencies can be approved. A deeper package always gets its default.
- Approving a secret for one package does not give it to another that asks for the same variable.

## Private sources

`git` dependencies use the host's git setup, so SSH keys and credential helpers work. Git never prompts for a password during a build.

`url` dependencies and `[source]` downloads send a bearer token if `NFB_TOKEN_{HOST}` is set, with the host in capitals and `.` and `-` replaced by `_`. For example, `NFB_TOKEN_EXAMPLE_COM` is used for `https://example.com/`. Tokens are only sent over `https`.
