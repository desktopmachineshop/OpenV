# Contributing to OpenV

Thanks for considering a contribution — issues, docs, and code are all
welcome.

## Ground rules

- Open an issue before large changes so the approach can be agreed first.
- Match the surrounding code's style; run the checks CI runs
  (`go vet`, `gofmt`, backend tests, `tsc` + frontend build — see
  `.github/workflows/ci.yml`) before opening a PR.
- Keep PRs focused: one change per PR.

## Release notes

`RELEASE_NOTES.md` at the repository root is read by the people who use
OpenV, so every pull request adds at least one bullet under `## Unreleased`
saying what they will notice: what they can now do, what looks different,
what they no longer have to do. Write for a workspace member, not a
developer; the implementation belongs in the pull request. CI refuses a
pull request that adds no bullet; a change nobody can see (CI, refactors,
internal docs) carries the `no-release-notes` label instead. Start a
bullet with `fix:` when it repairs something: fixes reach every workspace
at the next nightly, while every other bullet is a change that stable-
channel workspaces receive at the monthly release. A user-visible change
also registers itself in `internal/domain/release/features.go` with the
nightly it ships in and gates its code path and UI on that key (server:
`featureEnabled`; client: `useFeature`), so stable-channel workspaces see
it only once their monthly release carries it. The promotion to `release`
turns the Unreleased bullets into the dated section the app announces
(see `docs/release-policy.md` and `docs/railway.md`, "Release pipeline").

## Licensing of contributions

OpenV is licensed under the [GNU AGPL-3.0](LICENSE). By contributing, you
agree that your contribution is licensed under the same terms.

All commits must carry a **Developer Certificate of Origin (DCO)**
sign-off, certifying you have the right to submit the work under the
project's license (the full text is at [developercertificate.org](https://developercertificate.org)):

```
Signed-off-by: Your Name <your.email@example.com>
```

`git commit -s` adds this automatically. PRs with unsigned commits will be
asked to rebase with sign-offs before merging.

The DCO also preserves the project's ability to offer the same code under
additional licenses (e.g. commercial licensing for OEM embedding). Your
contribution always remains available under the AGPL.

## Enterprise code

Any future enterprise-only code will live in a clearly separated `ee/`
directory under its own license, so the boundary between the open core and
commercial extensions stays visible in the tree. Everything outside `ee/`
is and remains AGPL.
