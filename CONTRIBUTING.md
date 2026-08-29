# Contributing to OpenV

Thanks for considering a contribution — issues, docs, and code are all
welcome.

## Ground rules

- Open an issue before large changes so the approach can be agreed first.
- Match the surrounding code's style; run the checks CI runs
  (`go vet`, `gofmt`, backend tests, `tsc` + frontend build — see
  `.github/workflows/ci.yml`) before opening a PR.
- Keep PRs focused: one change per PR.

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
