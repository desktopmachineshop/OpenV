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
internal docs) carries the `no-release-notes` label instead.

Every bullet goes under one of three group headings, because that is how a
reader tells them apart:

```markdown
## Unreleased

### New features

- Export a traceability matrix to Excel from the V&V tab.

### Maintenance updates

- Large baselines load faster.

### Bug fixes

- Member avatars keep their shape beside a long name on a phone.
```

`### Maintenance updates` is for something a member can still notice — it is
faster, clearer, better documented — not for work with no visible effect;
that is what the `no-release-notes` label is for. A bullet under no group is
refused.

The group also decides the version. Promotion cuts the Unreleased bullets
into a new section headed by a semantic version and the date: anything under
*New features* makes it a minor release, maintenance and fixes alone make it
a patch, and a major release is asked for explicitly when the workflow is
run. The app announces that version to every account and shows it under
What's new (see `docs/railway.md`, "Release pipeline").

The group also decides who sees the change when (`docs/release-policy.md`).
Maintenance updates and bug fixes reach every workspace with the release
that carries them. So does a new feature on the nightly channel, but a
stable-channel workspace sees it only once the monthly stable release it
has turned on is that release or a later one. A new feature therefore
registers a key in `internal/domain/release/features.go` with the version
it ships in (`scripts/release_notes.py next` prints it) and gates its code
path and UI on that key (server: `featureEnabled`; client: `useFeature`).

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
