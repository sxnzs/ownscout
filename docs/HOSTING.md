# Hosting OwnScout

OwnScout is a local-first CLI. It needs no server, only a public repository, CI,
and somewhere to serve the static documentation. This note records the hosting
options and the student programs that offset their cost.

## Continuous integration

Two pipelines are provided and should stay in sync:

| File | Forge |
|---|---|
| `.github/workflows/ci.yml` | GitHub Actions |
| `.gitlab-ci.yml` | GitLab CI |

Both build the Go reference, build all three ports, and run
`spec/parity/verify-all.sh`.

## Where to host

| Forge | Cost | Notes |
|---|---|---|
| GitHub | Free | Public repos get unlimited Actions minutes; the CI here is written and validated for it. |
| GitLab | Free | Unlimited public repos, Pages and CI included. *GitLab for Open Source* grants free Ultimate to qualifying OSS projects. |
| Codeberg | Free | Non-profit Forgejo + Woodpecker CI. FOSS-licensed content only; mirroring from other forges is not permitted. |
| Sourcehut | €4-12/month | Free account when only contributing to existing projects. |
| Self-hosted Forgejo | Free | Full control, no third party. |

## Student programs

Sign up with an `.edu` address. Georgia Tech is on Microsoft 365, which makes
**Azure for Students** a plain sign-in with no card and no document.

- GitHub Student Developer Pack — https://education.github.com/pack
- Azure for Students — https://azure.microsoft.com/en-us/free/students/
- JetBrains Student Pack — https://www.jetbrains.com/community/education/#students
- GitLab for Open Source — https://about.gitlab.com/solutions/open-source/
- Google Cloud Free Trial — https://cloud.google.com/free
- AWS Educate — https://aws.amazon.com/education/awseducate/

## Notes

- Georgia Tech already publishes Atlassian and Adobe SSO domain verifications,
  so Bitbucket/Jira and Adobe Creative Cloud may already be available.
- These signups require interactive verification (CAPTCHA, SSO, email
  confirmation, and sometimes an enrollment document). They cannot be completed
  unattended.
