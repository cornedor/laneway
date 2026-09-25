# Security

laneway holds a Jira API token and talks to your Jira instance, so security
reports are taken seriously.

## Reporting

Please **do not** open a public issue. Report privately through
[GitHub's vulnerability reporting](https://github.com/cornedor/laneway/security/advisories/new)
or email contact@corne.info. Expect a reply within a week.

## Scope

- Leaking the API token (logs, crash output, terminal escapes, state file)
- Terminal escape injection from issue content
- Anything that makes laneway write to Jira without a user action

## Supported versions

Only the latest release gets fixes.
