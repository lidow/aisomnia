# Releasing

Releases are automated. A maintainer tags, GitHub Actions does the rest.

1. Update `CHANGELOG.md`: move the `Unreleased` items under a new `## [X.Y.Z] - YYYY-MM-DD` heading.
2. Tag and push:

   ```sh
   git tag -a vX.Y.Z -m "aisomnia vX.Y.Z"
   git push origin vX.Y.Z
   ```

3. The `release` workflow runs GoReleaser: it builds `darwin/arm64` and `darwin/amd64`, writes `checksums.txt`, creates the GitHub release with the changelog section, and opens a commit on `lidow/homebrew-tap` that updates `Formula/aisomnia.rb`.

The tap commit needs a token with write access to the tap repository, stored as the `HOMEBREW_TAP_TOKEN` repository secret (a fine-grained personal access token scoped to `homebrew-tap`, contents read and write). Everything else uses the workflow's own `GITHUB_TOKEN`. Without the secret the release is still published and only the formula step is skipped (`--skip=homebrew`); add the secret and re-run the workflow for that tag to publish the formula.

Versioning follows [Semantic Versioning](https://semver.org/). Breaking changes to the profile format or the CLI bump the major version.

## Homebrew tap layout

```
homebrew-tap/
  Formula/
    aisomnia.rb      # written by GoReleaser on every release
```

Users install with `brew install lidow/tap/aisomnia`. The formula installs the binary and registers `brew services start aisomnia` as an alternative way to run the lid watcher.
