## What this changes

<!-- One or two sentences. Link the issue if there is one. -->

## How you tested it

- [ ] `make check` passes locally
- [ ] Tested on macOS version: <!-- for example 15.2 on a MacBook Pro M4 -->
- [ ] `aisomnia apply` and `aisomnia reset` still round-trip (`status` shows no drift after each)

## Checklist

- [ ] Every new system command goes through the `Runner` interface and is covered by a test
- [ ] Any new setting is reversible by `aisomnia reset`
- [ ] `CHANGELOG.md` has a line under `Unreleased`
- [ ] Documentation updated (README, `docs/profile.md`, `docs/states.md`) if behavior changed
