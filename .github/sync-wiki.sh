#!/bin/sh
# Publishes docs/ to the GitHub wiki. Usage: sync-wiki.sh <wiki git url>
set -e

wiki=$(mktemp -d)
git clone -q "$1" "$wiki"
find "$wiki" -maxdepth 1 -name '*.md' -delete

# The wiki shows the page name as its title and links to pages without the extension.
for file in docs/*.md; do
  sed -E -e '1{/^# /d}' -e 's/\]\(([A-Za-z_-]+)\.md(#[^)]*)?\)/](\1\2)/g' "$file" > "$wiki/$(basename "$file")"
done

cd "$wiki"
git add -A
if git diff --cached --quiet; then
  exit 0
fi
git -c user.name="github-actions[bot]" -c user.email="github-actions[bot]@users.noreply.github.com" commit -q -m "sync docs"
git push -q
