# Sharing a project: links, the reviewer role and open-source publishing

How a project reaches people who are not members (OpenV Platform project:
REQ-149 share links, REQ-150 the reviewer role, REQ-151 the open-source
tier).

## Three ways in, three levels of trust

| Who | How they get in | What they can do |
|---|---|---|
| A customer, an assessor, a reader | A **public share link** | Read the live project: tree, every artifact, attributes, traceability. No account. Nothing else. |
| A reviewer | A **reviewer share link**, opened signed in | Everything a viewer can, plus notes, comments and mentions on any artifact. Never a change to the text, a link, a status or a setting. |
| A contributor | A **membership** an owner grants by name (editor or owner), or an invitation | Edit. |

Contributor access is deliberately not a link: a link is a credential that
can be forwarded, and forwarding the right to change a specification is the
one thing a project owner must decide person by person. Read access and
comment access forward harmlessly, so those are links.

## Share links (REQ-149)

An owner mints links under Project settings → Access → Share links: a role
(`public` or `reviewer`), a label saying who the link is for, and an
optional expiry. The link is
`${FRONTEND_URL}/share/<token>`. The token is 32 random bytes, shown once
in the answer that minted it and stored as a SHA-256 hash; the list of
links never carries tokens again. Revoking a link closes it at once for
everyone holding it; an expired link closes itself. Every unusable link,
whatever the reason, answers the same 404, and the public lookups are
throttled per address like invitation previews, so a guessed token learns
nothing.

A **public link** opens `SharedProjectView`: the live project as a
read-only export, rendered without a session. The API path
`/api/v1/public/share/{token}` is under `/api/v1/public/`, which the auth
middleware leaves open; the frontend's 401 interceptor leaves `/share`,
`/s` and `/open-source` alone, so a visitor is never bounced to the sign-in
form. Attachments and comments are not part of the shared view: files need
a session, and comments are a member conversation.

A **reviewer link** opens the same page with an invitation: sign in, or
create an account, to review. Once there is a session, `POST
/api/v1/auth/share/accept` grants the `reviewer` role on the project (an
account that already holds editor or owner keeps it) and the app opens on
the project. The decision to join is always the person's: a signed-in
browser that lands on a reviewer link is shown an "Open as reviewer"
button, not joined on arrival, so a link opened in somebody else's browser
cannot pull their account into a project unseen.

Feature key `share-links` (release 0.4.0): a stable-channel workspace whose
release lacks it cannot mint links, and the settings section is hidden;
links already minted keep opening, since the person holding one is not the
workspace.

## The reviewer role (REQ-150)

`owner` > `editor` > `reviewer` > `viewer`. A reviewer passes every viewer
check and `POST /api/v1/chatter`, and is refused everything an editor may
do; the API enforces this, whatever the client shows. The role can also be
granted by name or to a team, like any other.

## Social previews

A share link pasted into Slack, Discord, LinkedIn, Teams, a Mastodon post or
an Instagram bio gets a proper card, not a blank. Unfurlers fetch the URL
and read its Open Graph tags without running any JavaScript, so the app's
`index.html` would preview as nothing. The frontend's nginx therefore
serves `/share/<token>` (and `/open-source/p/<id>`) from the API's own
page for the link: a small HTML document carrying `og:title` (project and
workspace), `og:description`, `og:image` (the card below), `og:url` and the
Twitter `summary_large_image` tags, plus a `meta refresh` into the app at
`/s/<token>` for a browser. The card is `/preview.png`: a 1200×630 PNG the
API draws with the same Go fonts the PDF uses — project name, workspace,
artifact counts and description — so it needs nothing beyond the API.

`FRONTEND_URL` (falling back to `PUBLIC_URL`, then `http://localhost:3000`)
is the origin the links and the refresh point at. The image tag points at
`PUBLIC_URL` — on a deployment like Railway's that is the frontend domain,
whose nginx proxies `/api/` to the API — or, when unset, at the request's
own host.

## Open-source projects (REQ-151)

A workspace on the `open_source` plan is hosted free, on one condition:
every one of its projects is public **as of its latest baseline**. Live work
stays the workspace's own, and is shared only by a member or a share link,
so an open-source team can draft in private and publish by capturing a
baseline. The site's open-source page lists every such project with a
snapshot, newest first, and opens each at `/open-source/<id>` in the same
read-only view a public share link uses, marked with the baseline it
shows. A project with no baseline is not listed at all.

The plan is granted by a platform admin, from the Platform admin page
(account menu) or with `PUT /api/v1/orgs/{id}/plan` and
`{"plan": "open_source"}` (REQ-154, REQ-155), and the listing is the
plan's condition made visible: there is no per-project switch.
