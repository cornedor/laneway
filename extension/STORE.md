# Store listings

Build: `extension/pack.sh` → `dist/extension/` (zip, promo tile, screenshots). One zip for both stores. Bump `version` in `manifest.json` for every upload.

## Chrome: store listing

Category: Developer Tools. Language: English.

Summary: the manifest's `description`.

Description:

> Jira links open in laneway web, the Jira client of laneway (github.com/cornedor/laneway), instead of Jira's own pages.
>
> Issues, boards, backlogs, reports, the roadmap, releases, saved filters and JQL searches each open their laneway view. Anything laneway has no view for stays in Jira, and laneway's own "Open in Jira" links stay in Jira too.
>
> Options: laneway's address (default http://127.0.0.1:8484) and which Jira sites to redirect, each optionally switching laneway to the matching site. The toolbar button turns it off and on.
>
> Needs laneway web running: `laneway web`.

Images: icon from the zip, `promo-440x280.png`, the three `*-1280x800.png`.

## Chrome: privacy practices

Single purpose: open Jira links in laneway web instead of Jira.

Permissions:

- `declarativeNetRequestWithHostAccess`: redirect Jira page loads to the extension's page that maps them to laneway; let Jira pages opened from laneway through.
- `storage`: keep the options (laneway's address, Jira sites, on/off).
- `cookies`: set laneway's `lw_site` cookie so laneway switches to the Jira site of the link.
- Host `*.atlassian.net`: the Jira Cloud pages to redirect.
- Host `127.0.0.1`, `localhost`: laneway web's default address, for the site cookie.
- Optional host `*://*/*`: asked for only when the user enters a laneway address or Jira host elsewhere, for that host alone.

Remote code: no.

Data usage: none of the types. Certify all three statements.

Privacy policy: https://cornedor.github.io/laneway/extension-privacy/

## Firefox (addons.mozilla.org)

Submit a new add-on → On this site → upload the zip. Lint first: `npx web-ext lint -s extension` (one expected warning: Firefox ignores `service_worker`, it runs `background.scripts`).

Source code: no, nothing is minified or bundled.

Name: laneway redirect. Slug: `laneway-redirect`. Summary: the manifest's `description`. Description: as Chrome's.

Categories: Other. Support site: https://github.com/cornedor/laneway/issues. License: MIT.

Privacy policy: paste the text of `docs/extension-privacy.md` (AMO wants the text, not a URL).

Notes to reviewer:

> Plain, unbundled source. Needs laneway web (github.com/cornedor/laneway, `laneway web`) to do anything useful; without it the redirect lands on 127.0.0.1:8484. The pure URL mapping is in map.js, tested by map.test.mjs in the repo.

Screenshots: the three `*-1280x800.png`.
