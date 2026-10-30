# Issue tracker: GitHub

Issues and specs for this repo live as GitHub Issues in `darkrei08/cpa-usage-keeper`. Use the `gh` CLI for all operations.

## Conventions

- Create an issue: `gh issue create --repo darkrei08/cpa-usage-keeper --title "..." --body "..."`.
- Read an issue: `gh issue view <number> --repo darkrei08/cpa-usage-keeper --json number,title,body,labels,comments`.
- List issues: `gh issue list --repo darkrei08/cpa-usage-keeper --state open --json number,title,body,labels,comments`.
- Comment: `gh issue comment <number> --repo darkrei08/cpa-usage-keeper --body "..."`.
- Apply labels: `gh issue edit <number> --repo darkrei08/cpa-usage-keeper --add-label "..."`.
- Close: `gh issue close <number> --repo darkrei08/cpa-usage-keeper --comment "..."`.

## Pull requests as a triage surface

No. Pull requests are delivery artifacts and require normal human review.

## Wayfinding operations

Wayfinder maps are GitHub issues labelled `wayfinder:map`; decision tickets are child issues labelled `wayfinder:research`, `wayfinder:prototype`, `wayfinder:grilling`, or `wayfinder:task`. Use native GitHub sub-issues and issue dependencies where supported. If native dependencies are unavailable, record `Blocked by: #<issue>` at the top of the child issue.
