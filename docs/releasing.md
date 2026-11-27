# Releasing

Releases are fully automated. Nobody tags by hand.

## How a change becomes a release

1. Every commit on `main` follows [Conventional Commits](https://www.conventionalcommits.org/): `feat:` adds a feature (minor version), `fix:` fixes a bug (patch), `feat!:` or a `BREAKING CHANGE:` footer is a breaking change (major, minor while below 1.0). `docs:`, `chore:`, `ci:` and `test:` do not cause a release on their own.
2. When CI passes on `main`, the **Release** workflow runs release-please. It opens or updates a pull request titled `chore(main): release X.Y.Z` with the new version and the `CHANGELOG.md` entry.
3. With the repository variable `AUTO_RELEASE` set to `true`, the workflow merges that pull request itself in the same run. Without it, a maintainer merges it.
4. release-please then creates the tag `vX.Y.Z` and the GitHub release with the changelog.
5. GoReleaser runs the tests again on macOS, builds `darwin/arm64` and `darwin/amd64`, attaches the archives and `checksums.txt` to the release, and commits the updated formula to [lidow/homebrew-tap](https://github.com/lidow/homebrew-tap).

Users get the new version with `brew upgrade aisomnia`.

The workflow also runs every Monday, so dependency fixes merged by Dependabot (`fix(deps)`) are released even when nothing else changes. It can be started by hand from the Actions tab (**Release** → **Run workflow**).

## Secrets and settings

| Name | Kind | Purpose |
|---|---|---|
| `HOMEBREW_TAP_DEPLOY_KEY` | repository secret | private half of a deploy key with write access to `lidow/homebrew-tap` only. Without it, the release is still published and only the formula step is skipped |
| `AUTO_RELEASE` | repository variable | `true` merges the release pull request automatically |
| Actions may create pull requests | repository setting | needed by release-please |

To rotate the deploy key: create a new ed25519 key pair, add the public key to `homebrew-tap` → Settings → Deploy keys with write access, store the private key as `HOMEBREW_TAP_DEPLOY_KEY`, delete the old deploy key.

## Homebrew tap layout

```
homebrew-tap/
  Formula/
    aisomnia.rb      # written by GoReleaser on every release
```

Users install with `brew install lidow/tap/aisomnia`. The formula also offers `brew services start aisomnia` to run the lid watcher.
