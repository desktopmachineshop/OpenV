# OpenV release notes

What changed for the people who use OpenV, newest release first. Each
release is a version number and three groups: what you can now do, what
quietly improved, and what stopped being broken.

Contributors: how to add to this file, and what the promotion does with it,
is in [CONTRIBUTING.md](CONTRIBUTING.md#release-notes).

## Unreleased

### Bug fixes

- **A failed agent run is retried only when retrying can help.** When an
  agent's CLI fails, OpenV reads its error text to tell a sign-in problem
  (not retried) and a provider outage or rate limit (retried) from the
  agent's own error (not retried), and numbers and words that only looked
  like those could fool it. A run whose CLI said it "wrote 403 lines" was
  failed as a sign-in problem, and one that "processed 1500 files" or
  mentioned "the network tool" was retried as a provider outage, failing
  the same way again. A status code now counts only where it is one, as in
  "HTTP 403" or "Error: 429", and a word only as a whole word, so these
  read as the agent's own error. A rate limit or overload that mentions an
  API key, such as "rate limit exceeded; check your API key", is now
  retried as the rate limit it is, rather than failed as a sign-in problem.

- **A run that cannot read its project's repositories fails instead of
  running without them.** When an agent with repository access ran and its
  runner could not read the project's repository connections, because the
  request was refused or the server failed, the run went ahead in an empty
  workspace and could finish as succeeded without ever seeing the code. It
  now fails before the agent starts, saying "could not read the project's
  repository connections" and why, and it is not retried.

- **A run whose agent CLI exits with an error code fails.** A run whose
  agent CLI exited with a non-zero code, without the runner reporting an
  error with it, showed as succeeded with the exit code beside it. It now
  fails, saying "the agent CLI exited with code" and the code, and is not
  retried. The answer and token counts the run produced are kept, as for
  any failed run.

- **A runner no longer crashes on an answer to its claim that names no
  run.** When a runner asked for work and the answer named no run, which
  OpenV itself never sends but a proxy or a mismatched server can, the
  runner crashed. Every run it was working on stopped reporting with it,
  and was failed once its heartbeat lapsed. The runner now treats such an
  answer as no work, logs it, and asks again on its next poll. As before, a
  run that crashes inside the runner is reported as a worker error and the
  runner carries on.

- **A failed Codex sign-in shows the CLI's last lines.** When `codex login`
  failed on your own runner or on a cloud runner, the sign-in card quoted
  the end of the CLI's output, but the runner could lose the last lines,
  where the CLI says why it failed, if it exited right after printing them.
  The card now always ends with the CLI's own last lines.

- **A runner pool node puts `HOME` back after every lease.** A pool node
  points `HOME` at a directory of the lease's own while it serves a member,
  so the vendor CLIs keep that member's sign-ins there, and deletes the
  directory when the lease ends. When a lease could not start because its
  workspace directory could not be made, the node left `HOME` on the
  lease's directory, and the directory on disk, until the next lease
  started. A node started with `HOME` unset or empty kept `HOME` on the
  deleted directory after every lease. The node now puts `HOME` back
  exactly as it found it, set, empty or unset, whenever a lease ends or
  fails to start, and removes the directory a failed lease made. This
  matters only if you run your own runner pool.

- **A notification's email and phone alert open the page the bell does.**
  The link in a notification email, and a tap on its phone alert, took you
  to your projects list, or to the project's overview, for a change to your
  workspace or project access, someone joining or leaving the workspace, a
  release, and a dedicated instance's support window. They now open the
  same page as the notification in the bell: the workspace's or the
  project's members, What's new, or the workspace settings. A cloud runner
  minutes alert, which tells an admin to raise the allowance from the
  Billing tab, opened your projects list everywhere, the bell included; it
  now opens the workspace's Billing tab.

- **Notifications read as written.** A review request for an artifact
  whose title has quotes or a backslash in it showed extra backslashes, as
  in `"Brake \"pedal\" force"`; the title now reads exactly as written. A
  release announcement showed the release notes' formatting marks, such as
  the `**` around each headline; it now shows plain text. A phone alert too
  long for the screen could end in the middle of a word, and a release
  announcement's on a bare bullet; it now ends after a whole word, with an
  ellipsis. On a dedicated instance, the support window warning read
  "Upgrade OpenV within 1 days", and "within 0 days" on the last day; it
  now counts the days left, rounding up, as "within 2 days" or "within 1
  day", says "Upgrade OpenV today" on the last day, and the notice that the
  window has closed arrives when it closes rather than a day later.

- **Notification emails show a workspace's name correctly in the subject.**
  A subject naming a workspace whose name has accented or other non-English
  letters, such as Zürich Labs, could show garbled in some mail programs;
  it is now encoded so that every mail program shows it as written. A line
  break in a workspace's name no longer reaches the email's headers: the
  subject shows it as a space.

- **An artifact in a baseline is read-only.** Reading an artifact in a
  baseline still offered Edit, Delete, Submit for review and History, and
  they acted on the live artifact, not on the baseline: Delete removed it
  from the live project, a status change moved the live artifact along, and
  Edit left the document pane blank. A baseline now shows its artifacts
  without those buttons. To change an artifact, switch back to Live
  Project.

- **Project settings keep your quality-rule changes, and screen readers
  can tell which tab is open.** An unsaved change on the Quality rules tab
  was lost when you switched to another tab, and the rules loaded again on
  every visit; the change is now still there when you come back, as on the
  other tabs, and the rules load once. A screen reader now announces the
  settings tabs as tabs and says which one is selected. The Add party and
  Remove buttons under Reference parties looked like plain browser buttons;
  they now match the other buttons in settings. And in a workspace that
  does not have share links yet, project settings no longer load them.

- **An assistant card added to the project from the guided wizard says it
  was added.** When you added a card that creates an artifact in the
  project from the V&V Assistant beside the guided wizard, the artifact was
  created but the card said "The change could not be applied." and kept
  its "+ Add to project" button, so a second click created a duplicate.
  The card now shows "✓ Added to project", stays marked as added when you
  come back to the wizard, and offers no second add.

## 0.15.1 — 2026-09-30

### Maintenance updates

- **Runners open a run's repositories with the run's own access.** A runner
  now looks up the repositories of the project an agent works in with the
  access OpenV gives that run, not with the runner's own key, which reads
  only the projects its owner can open. Your personal runner keeps working
  from your own copy of each repository, the local path you set under
  Project settings → Repositories, and a run taken by a workspace runner
  clones the repository, as before.

- **The manual says what an agent can read on the machine it runs on.** An
  agent runs as the same user of the machine as the runner that runs it,
  so it can read whatever that user can: on your own machine, your files
  and the Agent Connector's key file, and on Windows and macOS the runner's
  environment, which holds its key. The *Runs & runners* page of the
  manual, and the runner setup guide, now say so, and recommend a cloud
  runner or the hosted runner, which run on Linux with nothing of yours on
  them, or a separate user on your machine, for agents you do not fully
  trust.

- **The web app's connection library carries today's security fixes.** The
  app now uses axios 1.20.0. Security advisories published on 30 September
  2026 cover every earlier 1.x version: requests that could be redirected or
  altered by polluted objects, and inputs that could stall the app. Nothing
  you do in the app changes.

### Bug fixes

- **A runner no longer prints its keys in its help or on a flag error.**
  `agentd` showed the worker key and the runner pool key, from
  `WORKER_API_KEY` and `RUNNER_POOL_KEY`, as the defaults of `--worker-key`
  and `--pool-key`. It printed them in `agentd -h`, and in the usage it
  prints on any flag error, such as a mistyped flag, so both keys could
  land in a container log or in help output someone shared. The runner now
  names those variables and never prints their values, and `--pool` and
  `--node-name` no longer show two defaults when `RUNNER_POOL` or
  `RUNNER_NODE_NAME` is set. A runner still reads all four from the
  environment when the flag is not given. If a runner's help or flag-error
  output may have reached anyone, or a log others can read, replace the
  keys it showed. *Rotate* a personal runner key under *My personal runner*
  in your settings, which stops the old key at once. For a workspace key,
  use *Create key* on the workspace's *Runners* tab, move the runner to the
  new key, then *Revoke* the old one, which keeps working until you do. If
  you run your own OpenV, also set a new `RUNNER_POOL_KEY` on the server and
  its pool nodes, and a new `WORKER_API_KEY` on the server if the runner
  used the deployment's own key, then restart them.

- **A runner no longer hands its own keys to the agents it runs.** Every
  program a runner started for a run (the agent's CLI, its sign-in and
  version checks, and git in the run's workspace) inherited
  `WORKER_API_KEY` and `RUNNER_POOL_KEY` from the runner, and on Linux any
  of them could also read both from the runner process itself. So an agent,
  or a command or git hook it ran, could read the key its runner signs in
  with, and on a pool node the deployment's shared pool key, whichever
  member's run it was. The runner now keeps both keys from everything it
  starts, and on Linux, hosted runners and the runner image included, it
  stops programs running as its own user from reading its environment or
  memory. On Windows and macOS a program running as the same user can still
  read a runner's environment through the operating system, so run agents
  you do not trust on Linux or as another user. Agents still reach OpenV
  with their run's own token, and every other setting, a provider's API key
  included, reaches them as before. If agents you do not fully trust have
  run on a runner, replace the key it used. *Rotate* a personal runner key
  under *My personal runner* in your settings. For a workspace key, use
  *Create key* on the workspace's *Runners* tab, move the runner to the new
  key, then *Revoke* the old one. If you run your own OpenV, also set a new
  `RUNNER_POOL_KEY` on the server and its pool nodes, and a new
  `WORKER_API_KEY` on the server if a runner used the server's own key.

- **Committing a guided definition approves its drafts.** The wizard's last
  step said every draft was now live, but the personas, needs, requirements,
  hazards and test stubs it had created stayed drafts: Requirements showed
  them as drafts, and the next *Modify guided definition* listed them for
  review all over again. *Commit* now approves each one as an approval by
  hand does: its history shows it going through review to approved, with a
  note for each step, and the project's *Activity* page records who approved
  it. A reopened definition that adds nothing new can still be committed,
  with *Commit definition*. Drafts left by a definition committed before this
  release stay drafts, and committing a later *Modify guided definition* does
  not approve them: submit each for review and approve it from its status in
  Requirements, or send the project for review and approve them from the
  Review Queue.

- **Open-source projects now show on the open-source page.** A workspace on
  the open-source tier found none of its projects listed there, and each
  project's public address answered *not found*, even after a baseline was
  taken. Every project with a baseline is now listed, and its address opens
  the latest baseline for anyone, no account needed, with a preview card
  when the link is shared. Work since the last baseline stays private, a
  new name or description included, and so do the other projects its
  requirements are linked to.

- **The V&V report now counts verification done in child projects, as the
  V&V dashboard does.** A requirement with no test case of its own, verified
  through the requirements of a child project that refine it, was counted as
  uncovered in the PDF from *Download V&V report* and listed there under
  *Requirements without a test case*, although the dashboard showed the
  result its refinements earned. The report, and the V&V status in a
  downloaded PDF or Word document, now give a requirement refined in a child
  project the same result as the dashboard: the worst of its refinements'
  results, or, for a requirement with evidence of its own (its own test
  cases, or marked verified), the worse of its own result and theirs. A
  requirement without a verification method is still listed as missing one.

- **A demonstration, analysis or inspection requirement refined in a child
  project is no longer listed as *Unverified*.** The V&V gaps, and the V&V
  report, listed it there while its refinements were still uncovered, even
  once it was marked verified itself, although a test requirement in the
  same place was never listed under *Requirements without a test case*.
  Neither is listed now: the gap is in its refinements, and the dashboard
  shows each of them with its own result.

- **Verification that reaches a project by two routes now counts on both.**
  When one project's requirements refined requirements of two projects of a
  programme, say both the aircraft's and the landing gear's, and were
  refined in turn by a project below, the V&V dashboard, gaps and report
  could count requirements verified through it as uncovered, and which ones
  could change from one reload to the next. Each now gets the result its
  refinements earned.

- **Citing the same evidence twice from one test result through the API no
  longer answers with a citation that does not exist.** The repeat was
  accepted, but its answer carried a new citation id, time and note that were
  never saved, so that id could not be found afterwards. It now answers with
  the citation already on record, its original note and time intact; to give
  a citation a different note, remove it and cite the evidence again with the
  new note.

- **A workspace update refused through the API no longer saves part of the
  request.** A `PUT /api/v1/orgs/{id}` refused for one of its settings, such
  as a monthly budget the workspace's plan does not include or a release
  channel it cannot choose, could still save other settings sent with it, a
  new name among them. A refused update now changes nothing. Workspace
  settings in the app save one setting at a time and were not affected.

- **Uploading a logo for a workspace that does not exist answers *workspace
  not found*.** A platform admin's logo upload through the API for an
  unknown workspace id answered with a server error and left the file on
  the server. It now answers 404 and stores nothing.

- **Switching to a workspace that does not exist is refused.** A platform
  admin's `POST /api/v1/orgs/{id}/activate` for an unknown workspace id
  answered as though the session had switched to it. It now answers 404
  *workspace not found*, and the session stays in the workspace it was in.

- **Previewing the next stable release is refused in a workspace you are
  not a member of.** A platform admin who turned the preview on in a
  workspace they do not belong to was answered as though it had worked,
  with the preview still off. The preview belongs to a member's own place in
  the workspace, so the request is now refused with *you are not a member of
  this organization*.

- **Restoring a deleted workspace answers with the workspace as restored.**
  The answer gave the workspace's last-updated time as the time it was
  deleted; it is now the time of the restore, as reading the workspace
  afterwards shows.

- **Profile pictures and workspace logos must be the image type they claim
  to be.** A picture of one image type named as another, such as a GIF
  saved as `.png`, or a BMP or icon file renamed to `.png`, was accepted and
  then served as the type its name claimed. It is now refused with *File
  content does not match an image of the declared type*: save the picture
  as a real PNG, JPEG, GIF or WebP file and upload it again. Pictures and
  logos uploaded before this release are left as they are.

- **Demoting a workspace's last admin now says why it cannot be done.**
  Changing the only admin's role to member failed with *failed to update
  workspace member*, an error that looked like a fault on our side. It is
  now refused with the reason, that the last admin of a workspace cannot be
  demoted, as removing that admin already was: make another member an admin
  first.

- **Removing someone who is no longer a member of the workspace is
  refused.** Removing a person who had already left, or whom another admin
  had removed a moment before, answered as if it had worked, and the
  workspace's admins were told of a removal that did not happen. The same
  went for an account named through the API that was never a member. Such a
  removal is now refused, as a role change for that person already was, and
  only removals that happen are announced. In the Members tab, either
  refusal now refreshes the list and says the person is no longer a member,
  instead of saying that you are not one.

- **A platform admin granting a plan to a workspace that pays by
  subscription is told why it is refused.** Moving a workspace with a live
  subscription onto the enterprise or open-source plan from the platform
  admin page failed with *failed to set the workspace plan*. It is now
  refused with the reason, that the workspace has a live subscription to
  cancel before a plan can be granted, and an API client gets `409` with the
  code `already_subscribed`, as a checkout for that workspace does.

- **Membership notifications name your role in plain English.** The
  notification and email for joining a workspace said you had "the a member
  role", and those for project access said "a editor access" or that your
  role was now "a editor" or "a owner". They now read "with a member role",
  "with an admin role", "editor access", "an editor" and "an owner".

- **A workspace's admins are told whenever someone joins by invitation.**
  Signing up from an invitation link added the account to the workspace
  without telling its admins, although joining through single sign-on did;
  and taking a link up while signed in told the person who joined, in the
  app and by email, what they had just done. Every way of taking up an
  invitation now tells the workspace's admins who joined and with which
  role, and does not notify the person who joined. For API clients, the
  workspace's events now carry `org.invitation_accepted` for a sign-up with
  an invitation link too, and name the person who joined as its actor where
  a signed-in acceptance named `system`.

- **The password reset email gives the link's full hour.** It said the link
  was valid for 59 minutes; the link has always lasted an hour, and the
  email now says 60 minutes.

- **A workspace's automations fire only on events in that workspace.** An
  automation set to run when something happens anywhere in its workspace,
  such as a new artifact, also ran when that happened in another workspace
  on the same server: the run was placed in the other workspace's project,
  with that workspace's artifact title in the agent's instructions and the
  run's card on that project's board. An automation now runs only for
  events in its own workspace, and, when it is pinned to a project, only
  for that project's.

- **An automation records the person who created it.** An automation
  created through the API was recorded as created by whichever account the
  request named, and one created in the app recorded no creator at all. It
  is now always recorded as created by the person who created it; an
  account named in the request is ignored.

- **An agent can no longer approve or reject its own proposals.** An agent
  that proposes its changes for review could approve those proposals itself
  through the API, with the credentials its run is given, so its changes
  reached the project without anyone reviewing them. A run's credentials
  are now refused whenever they are used to approve or reject a proposal,
  and every change an agent proposes waits for a person to review it.

- **A personal runner key can no longer approve proposals or change
  artifacts beyond its owner's role.** A member's personal runner key could
  approve or reject an agent's proposals, and create or change artifacts, in
  every project of the workspace, even one where that member could only
  view. Used through the API, it now needs the
  same access its owner needs in the app: an editor's role in the project,
  or admin of the workspace. Workspace runner keys are unchanged.

- **A workspace admin's list of proposals through the API holds all of the
  workspace's proposals.** Asking for the proposals of a whole workspace,
  with no project named, could leave out some or all of that workspace's
  proposals once other workspaces on the same server had hundreds of newer
  ones. The list now holds the workspace's own proposals, the newest 500 of
  them; when there are none it is `null`, as a project's empty list already
  was.

- **A run that fails once its changes are reviewed keeps the reason.** When
  an approved change an agent proposed could not be applied, the run was
  marked failed, but the reason, *one or more approved proposals failed to
  apply*, showed only to someone watching the run at that moment: opened
  later, or read through the API, the failed run gave no reason at all. The
  run now keeps it.

- **A run handed back to the queue no longer lets its agent go on working.**
  When a runner shutting down handed an unfinished run back, the agent it had
  started could still read and write the project with the run's access until
  another runner picked the run up. That access now ends the moment the run is
  handed back (its token answers `401`), and the runner that picks the run up
  gets access of its own.

- **A run still waiting in the queue can no longer be reported finished.** A
  runner's key could mark such a run, one no runner had picked up, as
  finished, with a result no agent had produced. Such a report is now refused
  with `409`, as a second report for a finished run already was, and the run
  stays in the queue for a runner to take.

- **An agent whose changes need review can no longer use the
  *Draft test cases* request.** An agent in proposal mode could call it
  through the API, which started another agent run and put its card on the
  board with no person asking for it and no record of who had. That request
  is now refused for such an agent, as a status change by such an agent
  already was. People, and agents that write directly, draft test cases as
  before.

- **Deleting an agent that is already gone, and two runner requests about
  something that no longer exists, answer *not found*.** Through the API,
  deleting an agent a moment after someone else did (or one that never
  existed), a runner pool node releasing itself after the pool lost its
  record, and a crew agent delegating after its place in the crew was
  removed all failed with a server error. They now answer `404`: *agent not
  found*; *pool node is not registered*, as the node's heartbeat already did,
  so the node no longer retries the report (its heartbeat, answered the same
  way, is what has it register again); and *team node not found*.

- **A runner's report of its AI tools is recorded in full.** A runner reports
  every AI tool it has in one go, and when that report named one this server
  does not know, as a newer runner's can, the tools the server does know were
  recorded or not at random, so the workspace's *AI providers* settings could
  show a tool the runner has as never detected. Every tool the server knows
  is now recorded, and the report is still refused for the one it does not.

- **Copying a crew checks the project it is copied into.** A crew copied
  through the API with a `project_id` that named no project, or a project of
  another workspace, was accepted, and the copy then could not be renamed,
  changed or deleted, even by the workspace's admins. A copy now goes where
  a new crew may: into a project of the crew's own workspace that you can
  edit, or, for a workspace admin, the whole workspace; anything else is
  refused with the reason. *Start from default crew* in the crew builder
  always named the project you were in and was not affected. A crew whose
  project has since been deleted, or that was pinned to another workspace's
  project, is now looked after by its own workspace's admins, and a crew's
  run always stays in the crew's workspace.

- **A project's crew runs in that project when launched without naming
  one.** Launching a crew made for a project through the API, with no
  project in the request, started a run that belonged to no project: it got
  no card on the board, and only the person who launched it and the
  workspace's admins could follow it. It now runs in the crew's project,
  with its card on that project's board. Launching from the crew builder
  always named the project and was not affected.

- **Saving an AI provider's settings again answers with their saved id.**
  `PUT /api/v1/provider-settings` for a provider whose settings were already
  saved answered with a new id that was never stored, so it did not match
  the id the provider settings list shows. It now answers with the saved
  id. The settings themselves were always saved correctly.

- **A completed CLI sign-in stays completed.** When a runner reported a
  vendor CLI sign-in as failed after it had already reported it complete,
  the sign-in showed as failed although the CLI was signed in. A completed
  sign-in now keeps its status, as a cancelled one already did.

- **Only a workspace admin can cancel a sign-in on the workspace's shared
  runners, or paste its code.** Any member of a workspace could cancel a
  vendor CLI sign-in an admin had started on the workspace's shared
  runners, or paste an authorization code into it, which could sign those
  runners in to the member's own account, although only an admin can start
  one. Both now take a workspace admin, as starting one does. A sign-in on
  your own runner is unchanged: you, and the workspace's admins, can still
  cancel it or paste its code.

- **A workspace runner key can no longer do what only a project's owner
  can.** A workspace runner key carries an editor's rights in every project
  of its workspace, but used through the API it could also do what only a
  project's owner or a workspace admin can: see, create and revoke the
  project's share links, add, change and remove its members and team
  access, connect, change and remove its repositories, delete baselines,
  rebuild the project's search index, and delete the project itself. It is
  now refused these, as a project editor is. Everything an editor can do it
  still does, and its changes still land without review. Personal runner
  keys are unchanged.

- **An agent can no longer create projects.** With the credentials its run
  is given, an agent could create a new project in its workspace through
  the API, a project with no owner, and an agent whose changes need review
  did so without anyone reviewing it. An agent works only in its run's
  project, so its credentials are now refused (`403`) when they are used to
  create a project, and creating one from a template or by import, which was
  already refused, now gives the same reason.

- **An agent refused an owner's action in its own project is told why.**
  When an agent asked for something only the project's owner may do, such
  as creating a share link or adding a member, the refusal said the agent's
  run was not scoped to the project, although the run belonged to it. It
  now says that an agent run acts at most as a project editor. A request
  for another project is still refused as not scoped to it.

- **A platform admin asking for a project or workspace that does not exist
  is told it is not found.** Through the API, a platform admin who named a
  project that does not exist in a download, the export, a V&V view or
  report, the product profile, the quality report or a delete got a server
  error, and so did a connector pairing for a workspace that does not
  exist; many other such requests answered with an empty list or as if
  they had worked. Wherever a platform admin's access to a project or
  workspace is checked, OpenV now first checks that it exists, and one
  that does not answers `404` *project not found* or *workspace not
  found*.

- **Removing someone from a project that does not exist is refused, and
  nobody is told of it.** A platform admin's removal of a member or of a
  team's access from a project that does not exist, the revocation of their
  own runner key in a workspace that does not exist, and a search reindex of
  such a project answered through the API as if they had worked, and the
  member's removal told the admin's own workspace that someone had left a
  project that was never there. They now answer `404`, and nothing is
  announced.

- **Nothing can be added to a project that does not exist.** A platform
  admin creating an artifact, an attribute definition or a guided session
  through the API for a project that does not exist had it saved where no
  project could show it, and the new artifact was announced in the admin's
  own workspace; saving such a project as a template failed with a server
  error. Each is now refused with `404` *project not found*, and nothing is
  saved or announced.

- **Creating a project from a template that is not there answers *template
  not found*.** Through the API, a well-formed template id that no template
  has failed with a server error; it now answers `404` *template not
  found*. So does a template saved in another workspace, which yours cannot
  see, where a project used to be created from it, copying that template's
  content into your workspace. Built-in templates and your own workspace's
  templates work as before.

- **Importing a JSON file that cannot be read says why.** Importing a JSON
  file that is not valid JSON, such as one cut short, failed with *failed
  to import project*, an error that looked like a fault on our side. It is
  now refused with the reason the file could not be read, as a malformed
  ReqIF file already was (`400` for an API client).

- **A read-only workspace on a self-hosted deployment is told which setting
  to change.** On a deployment the operator runs themselves, a workspace
  over the deployment's limits is read-only, and every change refused there
  told a workspace admin to subscribe from the Billing tab, which such a
  deployment does not have, and called those limits the workspace's plan.
  The refusal now speaks of the deployment's limits and names the setting
  to raise in `OPENV_LIMITS`, as a refusal at one of those limits already
  did. Hosted workspaces are pointed to the Billing tab as before.

- **An agent whose changes need review can no longer start other runs.**
  With its run's credentials, an agent in proposal mode could launch another
  agent, a crew, or an agent on a test run through the API, send the V&V
  Assistant a message, or set up an interview with any agent as its
  interviewer, invite a participant and answer as one, and so start work
  whose changes could land with no one reviewing them. Each is now refused
  (`403`), as *Draft test cases* already was, and so are retrying a run and
  running an automation now. An agent that writes directly can still launch
  runs, and a run it launches now shows under it in the run tree of its
  run's details, where it had stood alone with no record of what started it;
  the tree shows only the runs you could open yourself. Like a run a crew
  agent delegates, such a run is followed in that tree rather than on a card
  of its own on the board, and it is not retried automatically if it fails.

- **Every way of creating a project counts toward the workspace's project
  limit and respects a read-only workspace.** Creating a project from a
  template or by importing one did not count toward the most projects the
  workspace's plan allows (on a self-hosted deployment, `OPENV_LIMITS`), so
  either could take a workspace past its limit and make it read-only; and
  on a workspace already read-only because it is over its plan, a new
  project and one from a template were still created. Each now counts: at
  the limit it is refused with the limit and how to raise it, and on a
  read-only workspace a new project and one from a template are refused like
  any other change. Importing stays available on a read-only workspace
  while the workspace is under its project limit. Launching an agent with no
  project through the API now also needs you to be a member of the agent's
  workspace, and is refused while that workspace is read-only.

- **A runner key can no longer create projects.** Through the API, a
  workspace runner key, or a member's own runner key, could create a
  project that no one owned, and creating one from a template or by import
  with it answered as if nobody had signed in. Each is now refused (`403`,
  *runner keys cannot create projects*). Any member of a workspace still
  creates projects there and becomes their owner.

- **An agent listing projects sees only its own.** Through the API, an
  agent's run credentials listed every project of its workspace, including
  projects the agent cannot open. The list now holds only the project the
  run works in, and is empty for a run that has none.

- **A personal runner key reads only the projects its owner can open.** A
  member's personal runner key could read every project of its workspace
  through the API, and list them all, even projects that member could not
  open in the app. It now reads and lists only the projects its owner can
  open, as a workspace admin or with a role in the project, and anything
  else is refused as it is for the member. Workspace runner keys are
  unchanged.

- **A personal runner takes only the work its owner could see.** A member's
  personal runner picked up automation and board runs from every project of
  the workspace, so its agent worked in projects that member cannot open. It
  now takes such a run only in a project its owner has a role in, or, for a
  workspace admin, anywhere in the workspace. A run that belongs to no
  project, which only a workspace's admins can open, is left to an admin's
  runner or to the workspace's shared runners. Runs you launch still go to
  your own runner, and workspace and hosted runners take any run as before.

- **Runner keys and share links can be revoked in a read-only workspace.**
  In a workspace that holds more than its plan allows, and so is read-only,
  revoking a workspace runner key, your own personal runner key or a
  project's share link was refused like any change to the workspace
  (`403 plan_read_only` for an API client), so the key or link went on
  working until the workspace was back under its plan. Revoking them is now
  always allowed there, as removing someone from the workspace or revoking
  a workspace invitation already was.

- **You can switch to a read-only workspace, set your own stable-release
  preview and end your cloud runner there.** In a workspace that holds more
  than its plan allows, turning the next stable release's preview on or off
  for yourself and ending your leased cloud runner were refused, although
  neither changes the workspace, and so was making it your session's active
  workspace (`POST /api/v1/orgs/{id}/activate`, which the app sends when you
  switch to it). These three now work there as in any other workspace;
  starting or extending a cloud runner lease, like changes to the
  workspace's own content, stays refused until it is back under its plan.

- **A run can be cancelled in a read-only workspace.** In a workspace that
  holds more than its plan allows, a project's editor could not cancel a
  run someone else had launched in the project, nor a workspace admin a
  run launched outside any project: the cancel was refused as a change to
  the workspace, although the person who launched the run could cancel it.
  Everyone who may cancel a run can now cancel it there.

- **Something you cannot reach answers exactly as something that does not
  exist.** Through the API, a project, a workspace, or anything in them
  that your account cannot open answered `403` saying you had no access,
  and some of them told another workspace's runner key or agent that it
  needed to sign in, while one that did not exist answered `404`, so the
  answer told anyone who guessed an id that it existed. Both now get the
  same `404` and the same message: *project not found*, *workspace not
  found*, or the item's own, such as *baseline not found*, *team not found*
  or *agent run not found*. This holds for people outside the workspace,
  for another workspace's runner keys and agents, for an agent working in
  another project, and for a workspace member with no role in the project.
  A crew is launched only by someone who may see it: a member of its
  workspace, that workspace's runner keys and agents, or someone who can
  open the project it is pinned to; an agent sees a crew pinned to a
  project only if it works in that project. An editor of a project who is
  not a member of its workspace, launching one of that workspace's crews
  there, is now told *team not found*. Something a request names in a
  project or workspace you cannot open is refused as if it did not exist
  too: a link from or to an artifact there answers `400` *source artifact
  not found* or *target artifact not found*, a crew pinned to such a
  project `400` *project not found*, a parent project `400` *parent project
  not found*, an interview's persona `400` *persona artifact not found*,
  and giving a people-team of such a workspace access to your project `404`
  *team not found*. A project you can open but may not change still
  answers `403`. A deleted workspace now
  answers *workspace not found* to its own members too. Its admins can
  still restore it, a member who is not its admin is still told that only
  admins can, and restoring a workspace you are not a member of answers
  *workspace not found*. In the app, a link to a project you cannot open
  still takes you back to your projects.

- **A platform admin's event and run lists for a project show that
  project's.** Through the API, `GET /api/v1/events?project_id=` and
  `GET /api/v1/agent-runs?project_id=` answered a platform admin an empty
  list unless the admin's own active workspace was the project's. They now
  list that project's events and runs, as every other read of it does, and
  so they do for a member of the project working in another workspace.

- **Listing proposals without naming a project says what is missing.**
  Through the API, `GET /api/v1/proposals` with no `project_id` refused a
  workspace member who is not its admin with `403`, as if forbidden. It now
  answers `400` *project_id is required*. Workspace admins still get the
  proposals of their whole workspace.

- **An id that is not an id is answered as one that does not exist.**
  Through the API, a request that named something by a malformed id, one
  that is not a UUID, failed with a server error (`500`) in many places:
  reading or managing a workspace or a project, choosing your default
  workspace, marking notifications read or flagging one, a platform admin's
  password reset link or admin standing, a template, evidence, a shared
  product, the agent and run filters of the run and proposal lists, and the
  runner pool's nodes; an id holding a byte that is not text, such as `%FF`
  or `%00`, failed so on every project and workspace. Each now answers as
  for an id nobody has: `404` with the usual *not found*, an empty list, or
  nothing changed. Updating or restoring an artifact, recording a test
  result for a test case, and downloading a project from a baseline failed
  with a server error for an id nobody has, and so for a malformed one;
  they now answer `404` *artifact not found* or *baseline not found*.
  Giving a project role to an account that does not exist now answers `404`
  *user not found*, where it failed with a server error.

- **A profile picture is shown only to people who share a workspace with its
  owner.** Anyone signed in could fetch any account's uploaded profile
  picture by the account's id, including someone who shares no workspace
  with it. A picture is now served to the account itself, to members of the
  workspaces it belongs to, and to platform admins. Anyone else is answered
  `404` *user has no uploaded picture*, exactly as for an account with no
  picture or no account at all, so the answer no longer tells them the
  account exists. Where OpenV lists someone whose picture you may not see,
  such as a project member from outside your workspace, it shows their
  initial in its place.

- **Returning from a checkout the billing provider has no record of says
  so.** Through the API, `POST /api/v1/orgs/{id}/billing/refresh` with a
  `session_id` the billing provider does not know answered `503` *the
  billing provider did not answer*, with a `Retry-After`, as if the provider
  were down and trying again could help. It now answers `404` *checkout not
  found*, with no `Retry-After`, and still leaves the workspace as it was.
  When the provider really does not answer, the refresh still answers `503`
  with a `Retry-After`, as before.

- **Signing up with a password that is too short gives the same error code
  as changing or resetting one.** Through the API,
  `POST /api/v1/auth/register` refused a password under 8 characters with
  *password must be at least 8 characters* but no error code, where
  changing or resetting a password gives that refusal the code
  `weak_password`. Signing up now gives it the same code and the same
  message. Signing in is unchanged: a wrong password, however short, is
  refused with one message that does not say whether the account exists.

- **A closed test run no longer takes new results, and a run takes results
  only for its own project's test cases.** A result recorded in a test run
  that had been completed or aborted was accepted, so a closed run's record
  could still change. It is now refused with `409` and the message *this
  test run is completed; only in-progress runs accept new results*. A
  result for a test case of another project was accepted into a run too;
  it is now answered as for a test case that does not exist, `404`
  *artifact not found*, whether or not you can open that project. An
  approved agent proposal that records a result follows the same rules.

- **Recording a test result again keeps the earlier one.** Recording a
  result for a test case a run already had one for overwrote it, so only
  the automatic note on the test case said what it had been. Each result is
  now kept: the newest is the case's result wherever OpenV shows one (the
  run's grid, V&V coverage and gaps, reports and documents), evidence cited
  for the case moves with it, and API clients list every result a run holds
  with `GET /api/v1/test-runs/{id}/results?history=true`, newest first. A
  result recorded again has an id of its own. Deleting a test run that
  holds results, which deleted them with it, is now refused with `409`,
  and the run is kept: complete or abort a run still in progress instead.
  A run with no results can still be deleted.

- **A time sent with a time zone offset keeps its moment.** An evidence
  capture's date, a to-do's due date and an interview invite's expiry were
  stored without their offset, so a time sent as `2026-01-15T09:30:00+01:00`
  read back as `2026-01-15T09:30:00Z`, an hour late, and an invite with such
  an expiry expired at the wrong time. They are now stored as the moment
  sent and read back in UTC, `2026-01-15T08:30:00Z` for that example, as a
  workspace invitation's expiry is too. Times stored before this release
  read as the UTC times OpenV wrote. The app already sends these dates in
  UTC, so what it shows is unchanged; API and MCP clients that send an
  offset now get the moment they meant.

- **Restoring an earlier version of an artifact keeps its reference, and the
  activity log records it.** Restoring a version from an artifact's History
  gave the artifact a new reference, so REQ-3 might become REQ-9 and every
  citation of REQ-3, in documents, notes and other artifacts, stopped
  pointing at it; figures added afterwards were numbered under the new
  reference. A restore now keeps the artifact's reference, as every other
  edit does. Only a restore that brings back a different type of artifact,
  such as a heading over what is now a requirement, takes a new reference
  of the matching kind, as changing the type does. Each restore now also
  appears in the project's activity log as *artifact.restored*, with the
  version it brought back, and the log's filter offers it. An artifact a
  restore already renumbered keeps the reference it has now.

- **Restoring a version an artifact never had says so.** Through the API,
  restoring an artifact to a version it never had failed with a server
  error (`500`). It now answers `404` *artifact version not found*, and
  nothing is written.

- **A deleted artifact's history can still be read.** Through the API, the
  version history of a deleted artifact answered `404` even to the
  project's members, although OpenV keeps every version. Anyone who can
  view the project can now read a deleted artifact's versions, and the
  links of any one of them. Someone with no access to the project still
  gets `404`, as for an artifact that does not exist.

- **Adding a figure to an artifact creates a new version of it.** Uploading
  a new file for a figure, or renaming one, already took the artifact to a
  new version, but adding a figure did not, so the History did not show
  when a drawing first appeared. Adding a figure now creates a version too.
  As with the other figure changes, that version does not send an approved
  artifact back to draft or mark its links suspect.

- **Deleting a baseline is recorded in the activity log.** A project owner
  could delete a baseline and nothing recorded that it had existed. The
  deletion now appears in the project's activity log as
  *baseline.deleted*, with the baseline's name and who deleted it, and the
  log's filter offers it. Deleting a baseline is still for the project's
  owners alone.

- **A baseline keeps the project's attribute definitions.** A baseline kept
  a project's artifacts, links and the details of its attachments, but not
  the custom attributes defined for it, so what a baseline's values meant
  could change after it was taken. A new baseline also keeps the attribute
  definitions in effect when it is captured, the workspace's and the
  project's, and its ReqIF download types list attributes by them. Its
  PDF and Word downloads, and the fields the download wizard offers for
  it, name each custom attribute as it was defined then, where the live
  project's downloads name it after its key. Attachment files are still
  not copied into a baseline, and each attachment's name, type and size
  are still kept. Baselines captured before this release are unchanged and
  read as before.

- **PDF and Word downloads include V&V status unless you turn it off.** A
  downloaded specification carried each requirement's verification status,
  the coverage summary and the gaps only when *V&V status* was ticked or
  the Verification & Validation template chosen. Every PDF and Word
  download, and the Specification template, now carries them by default;
  untick *V&V status* under *Document* in the download wizard to leave them
  out. The Requirements review and Test planning templates still leave
  them out, and test results are still included only when chosen. A box
  you tick or untick under *Document*, or the fields you choose, after
  choosing a template now always reaches the document: before, one ticked
  to match what a download has without a template, such as *Figures* on
  the Test planning template or every field on Requirements review, was
  lost, and the document followed the template instead.

- **A ReqIF download types list attributes as the ReqIF export does.** A
  project downloaded as ReqIF wrote an attribute with a fixed list of
  values, such as a *risk* of low or high, as free text, while the ReqIF
  export wrote it as a list of values, so DOORS, Polarion and an OpenV
  import saw two different documents. The download now writes it as the
  export does, from the same attribute definitions, and the two files
  match. As in the export, a value that is not in its attribute's list,
  such as one left behind when the list was edited, is left out of the
  file. A download of a baseline uses the definitions the baseline kept;
  one captured before this release still writes such attributes as text.

- **A test run can only name a baseline of its own project.** Through the
  API, creating a test run with a `baseline_id` no baseline has, or one of
  another project, stored that reference anyway, and an id that is not a
  UUID failed with a database error (`400`). Each now answers `404`
  *baseline not found*, as every other request that names a baseline
  does, and no run is created. A run on one of the project's own
  baselines, or on none, is created as before.

- **A card on the board is assigned only to a person, an agent or an
  existing crew, and the board's cards list in the order its columns
  flow.** Through the API, creating or editing a work item with an
  `assignee_type` other than `user`, `agent` or `team` saved it as sent,
  and assigning a card to a crew (`assignee_type` `team`) that does not
  exist saved that too, so the card named an assignee nobody could find.
  The first now answers `400` *invalid assignee_type: must be user, agent
  or team*, the second `404` *team not found*, and neither saves anything.
  A crew you may not see is refused as one that does not exist, and one
  you can see in another workspace than the card's project answers `400`
  *team belongs to a different workspace*. An edit that sends no
  `assignee_type` keeps the card's, and the assignee it sends is checked
  against that; an edit that sends the card's own assignee back unchanged
  is not checked again, so it saves as before. A person's or an agent's id
  is not looked up. Cards already assigned so are left as they are. The
  list of a project's cards, which agents read through the
  `list_work_items` tool, gave the columns in alphabetical order, with Done
  before In Progress and To Do; it now gives them as the board shows them,
  Backlog, To Do, In Progress, Review and Done, each column's cards in
  their order on the board. The board itself looks as it did.

- **The quality report accepts a citation of a linked artifact in another
  project.** A requirement that cites an artifact of another project it is
  linked to, such as the requirement it refines, was flagged in the
  project's quality report as citing something it has no traceability link
  to, and its score in the requirements list was lowered for it, while the
  requirement's own quality check found nothing wrong. The report now
  judges every citation as that check does, so a citation of any artifact
  the requirement is linked to, in whichever project, counts as linked in
  both. A citation of an artifact it is not linked to is still flagged in
  both.

- **A run whose approved changes could not be applied is marked an agent
  error.** When you approved an agent's proposed changes and one of them
  could not be applied, the run was marked failed with no failure class, so
  the Runs page and the run's panel showed no *agent error* beside it, and
  an API client read no `error_class`. Such a run now fails as an
  `agent_error`, as other failures of the agent's own work do, and is not
  retried automatically, since the same changes would fail the same way.
  You can still retry it yourself.

- **A crew no longer hands work to someone who cannot open the project.**
  When a crew run finished and its crew handed work to a person, or asked a
  person for a review, the card it put on the project's board was assigned
  to that person even when they had no role in the project, so they could
  not open the work they were given. Such a hand-off is now refused: no
  card is made, nobody is given access, and the run's log and its card on
  the board say who the hand-off was for and why it was refused. Give the
  person a role in the project, or make them an admin of the workspace, to
  hand work to them. Someone who has left the workspace is refused even if
  they kept a role in the project, and the reason says they are no longer a
  member of the workspace: add them back to it, with a role in the
  project, to hand work to them. Hand-offs to members of the workspace who
  can open the project, and to agents, work as before.

- **A crew run stopped by the budget now says which agents it did not
  start.** Where a workspace's monthly AI budget is enforced, a crew run
  that finished after the budget was reached launched none of the agents
  it hands work to or is reviewed by, and only the server's own log said
  so. The run's log now ends with a note naming those agents and the
  budget that stopped them, and the project's *Activity* page records an
  `agentrun.successors_skipped` event with the same, which API clients can
  follow. Hand-offs to people still go ahead, since they start no run.

- **Revoking a server's own runner key on the Runners tab now stops it.**
  On a deployment you run yourself, the runner key the server is given in
  `WORKER_API_KEY` is listed on the workspace's *Runners* tab as
  *env-bootstrap*. Revoking it there changed nothing: the server went on
  accepting the same key from its own settings. It is now refused like any
  revoked key, even while the server still has that value, and a restart
  with the same value keeps it revoked, adds no key, and logs a warning
  saying so. To use the server's own key again, set a new `WORKER_API_KEY`
  and restart the server, which registers the new value as a new key.

- **A runner keeps more OpenV credentials from the agents it runs.** A
  runner already kept its own worker and pool keys from every program it
  starts for a run. It now also keeps `OPENV_API_TOKEN`, `OPENV_EMAIL` and
  `OPENV_PASSWORD` from them, which a machine that also runs the OpenV
  tools or scripts may have set, so an agent's CLI, the commands and git
  hooks it runs, and its MCP servers no longer see a runner key or an
  account's password that way. Agents still reach OpenV with their run's
  own token, and every other setting, a provider's API key included,
  reaches them as before.

- **Runner keys stay off the command line.** The setup command shown when
  you create a runner key, and the runner setup guide, passed the key to
  `agentd` as `--worker-key`, where `ps` shows it to every user of the
  machine. Both now set the key in the runner's environment as
  `WORKER_API_KEY`, and the guide starts a pool node with
  `RUNNER_POOL_KEY` there too. `agentd` still accepts `--worker-key` and
  `--pool-key`, so existing scripts keep working, but it now logs a warning
  when either is given, naming the flag and the variable to use instead,
  never the key. If a runner was started with its key on the command line
  of a machine other people use, replace the key: *Rotate* a personal
  runner key under *My personal runner* in your settings, or create a new
  workspace key on the *Runners* tab, move the runner to it and revoke the
  old one.

- **Asking again for a cloud runner you hold answers 200.** Through the
  API, `POST /api/v1/orgs/{id}/runner-session` returned the cloud runner
  you already hold with `201 Created`, as if it had leased you another. It
  now returns that same lease with `200`, and answers `201` only when it
  leases you a new runner. The body is the same either way, and the app
  works as before.

- **A workspace a platform admin moves onto Business or Enterprise keeps the
  nightly channel.** Moving a workspace from Single User, Business Lite,
  Self-hosted or Open source onto Business or Enterprise from the platform
  admin page, or with `PUT /api/v1/orgs/{id}/plan`, put it on the stable
  channel before any stable release had turned on for it, so every newer
  feature its members were using disappeared at once. It now stays on
  nightly, as a workspace that moves onto Business by checkout already does,
  and its admins can choose stable in workspace settings whenever they want
  to. A workspace whose admins had already chosen a channel keeps their
  choice, and a move between Business and Enterprise changes nothing.
  Workspaces moved before this release stay on the channel they are on: an
  admin can choose nightly in workspace settings.

- **A share link closes at the moment its expiry names.** A share link
  created through the API with an expiry carrying a time zone offset, such
  as `2026-10-01T12:00:00+02:00`, was stored without its offset, so it
  closed at 12:00 UTC: a link given a `+02:00` expiry stayed open to anyone
  holding it for two hours after it should have closed, and one given a
  `-05:00` expiry closed five hours early. Links now close at the moment
  sent, and the list of a project's links shows each expiry in UTC,
  `2026-10-01T10:00:00Z` for that example; an expiry that falls outside
  the years 1 to 9999 in UTC is refused. The app already sends a link's
  expiry in UTC, so links made in Project settings close when they did;
  links already created close when they did before this release, so
  revoke and re-create any link an API client gave an expiry with a
  positive offset if it should close sooner.

- **Settings are read one way, and a mistyped one is named in the log.** A
  self-hosted server or runner treated the same slip differently from one
  setting to the next: spaces round a value, as in `AGENT_CONCURRENCY=" 3"`,
  made it count as nonsense; `SECURE_COOKIES=TRUE` counted as off, since
  only a lower-case `true` was on; a runner took `0` or a negative number
  for its concurrency, and `-1h` for how long it keeps finished workspaces;
  and a rate limit, such as sign-in's, given a refill of `Inf` stopped
  throttling altogether. The API and the runner now ignore spaces round a
  setting's value, read a count as a whole number above 0, a duration as
  positive and a rate as a positive, finite number, and take `true` or
  `false` in any case, or `1` or `0`, for an on/off setting. A value that
  breaks this keeps the setting's default, and the log says so once, as the
  server or runner starts, naming the variable but never its value.
  `HOSTED_RUNNER_PIDS_LIMIT=0`, or a negative number, still means no cap on
  a hosted runner's processes. Credentials are the exception to the spaces:
  a key, token, password or private key, such as `WORKER_API_KEY`,
  `RUNNER_POOL_KEY`, `DATABASE_URL`, `DB_PASSWORD` or `STRIPE_SECRET_KEY`,
  is used exactly as set, and one with spaces or a line break around it is
  named in the log once, as the server or runner starts, and never printed.
  A `DATABASE_URL` that does not parse, or that has spaces or a line break
  in front of it, no longer puts the whole URL, password included, in the
  log: the server stops, before it dials anything, with a message saying
  what is wrong. Billing still refuses to start on a malformed setting,
  and now also when anything follows the number, so
  `OPENV_BILLING_TRIAL_DAYS=30d`, which gave a 30-day trial by luck, stops
  the server with a message that it must be a whole number of days; a
  malformed `OPENV_STRIPE_PRICES` is now explained in its own terms rather
  than by an internal type name. If you run your own OpenV, check your
  settings before you upgrade: `SECURE_COOKIES=TRUE`,
  `OPENV_TRUST_PROXY=true` and `OPENV_RUN_AUTO_RETRY=0` now take effect, so
  set `OPENV_TRUST_PROXY` only when a proxy sits in front of the API; a
  billing count such as `30d` keeps the server from starting until it is
  written as a number; a runner's `AGENT_CHILD_CONCURRENCY=0`, or a
  negative number, which reserved no extra slots for child and interview
  runs, now keeps the default of 2, so pass `-child-concurrency=0` to
  reserve none; and `STRIPE_SECRET_KEY`, `OPENV_EMBEDDING_API_KEY`,
  `OPENV_VAPID_PRIVATE_KEY` and `DB_PASSWORD` no longer lose the spaces or
  line break around them, and `DB_PASSWORD` keeps the spaces, quotes and
  backslashes in it that used to cut it short or change it, so remove
  whatever the log names, or billing, embeddings, web push or the database
  will refuse the credential; a Stripe, embeddings or VAPID key that is only
  spaces still leaves its feature off.

- **A test run whose baseline was deleted says so.** A test run can be
  pinned to a baseline, and a project owner can still delete that baseline
  later. The run keeps the baseline as part of its record, but the runs
  table on the V&V dashboard then showed a long id where the baseline's
  name had been. It now says *Baseline deleted*. Through the API, a run
  read with `GET /api/v1/test-runs/{id}`, or in a project's list of test
  runs, carries `baseline_deleted: true` when the baseline it names has been
  deleted, and leaves the field out otherwise. A new run still cannot be
  pinned to a deleted baseline.

- **Starting an agent on a closed test run answers 409, as recording a
  result there does.** Through the API, starting an agent on a completed or
  aborted run with `POST /api/v1/test-runs/{id}/agent-run` answered `400`,
  while recording a result in the same run answers `409` with the same
  message. The launch now answers `409` with the same body, *this test run
  is completed; only in-progress runs accept new results* (or *aborted*),
  so a client can treat both refusals alike. No agent starts, as before,
  and the app offers the launch only on a run in progress.

- **Ticking every artifact type after choosing a download template now
  gets every type.** In the download wizard, a template such as
  *Requirements review* ticks only its own types, user needs and
  requirements. Ticking the other types as well still gave a document with
  the template's types alone, while the wizard said it would hold the whole
  project. The download now holds every type you tick. Through the API,
  `types=all` beside a `template` keeps every type whatever the template
  keeps; a request that leaves `types` out still gets the template's types,
  and a download with no template is unchanged.

## 0.15.0 — 2026-09-22

Stable channel release since 2026-10-01.

### New features

- **A workspace admin can subscribe to Business Lite or Business from the
  new Billing tab in workspace settings.** Pick the plan, monthly or yearly,
  in GBP, USD or EUR, and pay on Stripe's checkout page; the workspace is on
  its plan by the time the tab reloads. A first subscription starts with a
  14-day trial, promotion codes work at checkout, VAT is worked out there
  and a business VAT number is accepted. The same tab changes plan in place
  (prorated), and *Manage billing* opens Stripe's portal for invoices, the
  card, the billing address and cancellation. Nothing about a card ever
  reaches OpenV. Workspaces created before the date announced with the first
  live price keep every tier's features free, for good; the tiers' limits
  apply only to workspaces created after it, and only from the release that
  turns them on. Feature key `workspace-billing` (0.15.0): the plan picker
  waits for a stable release on stable-channel workspaces; a workspace that
  already holds a subscription always sees its Billing tab.

- **The tiers are now real, and every workspace from the alpha keeps
  everything.** On the date announced with the first live price, a
  workspace created before it is marked as keeping the alpha terms — every
  feature, no member or workspace caps, unmetered cloud runners — for good,
  whatever plan it is on; the Limits tab says so. A workspace created after
  it is on its tier: the free tier seats two people in one shared
  workspace and 300 minutes of leased cloud runner a month; Business Lite
  adds an always-on hosted runner and unmetered cloud runners; Business
  adds teams, per-project team access, the workspace budget and usage
  rollup, and as many members and shared workspaces as it bills for. A
  refusal names the Billing tab. A workspace that holds more than its plan
  allows — after a lapsed subscription, say — becomes read-only rather
  than losing anything: everything stays readable and exportable, and
  removing members or deleting projects, or subscribing, makes it writable
  again. Admins are told when leased cloud-runner minutes reach 80% and
  100% of the month's allowance. No feature key: the values are plan
  defaults with no channel in scope, and the plans they cap are always on
  the nightly channel, so a key would gate nothing.

### Maintenance updates

- **A Business subscription's seat count follows the Members tab on its
  own.** Adding, inviting, removing or revoking updates the billed quantity
  within a few seconds, prorated on the next invoice, and the Billing tab
  shows the seats billed beside the members counted. A membership change
  never waits on, or fails because of, the billing provider; a missed
  update is caught within minutes.
- **Workspace plans now carry a billing status, and the pricing page can
  read live prices.** A workspace's limits resolve from the plan it is
  entitled to, which is its plan while a subscription (there is none yet) is
  in good standing and the free tier once one lapses; the limits panel names
  the entitled plan and the status. The pricing page reads confirmed prices
  from the platform when a billing provider is configured and shows its
  usual copy otherwise. Nothing is for sale yet, no workspace's limits
  change, and a self-hosted deployment is unaffected.

## 0.14.3 — 2026-09-21

### Maintenance updates

- **Per-address rate limits read the real client more carefully behind a
  proxy.** A self-hosted deployment can now name the header its CDN sets to the
  real client (`OPENV_CLIENT_IP_HEADER`, e.g. Cloudflare's `CF-Connecting-IP`)
  or declare how many proxies it sits behind (`OPENV_TRUSTED_PROXY_HOPS`); the
  forwarded-for chain is read from the right so a caller cannot pick its own
  rate-limit bucket by prepending a header. The existing `OPENV_TRUST_PROXY=1`
  keeps working as a single-hop setting. Hosted workspaces are unaffected.

## 0.14.2 — 2026-09-20

### Bug fixes

- **The browser tab shows the OpenV mark again.** `favicon.ico` was never an
  image — it was a placeholder text file saying a real one should go there —
  so every tab, bookmark and history entry fell back to the browser's blank
  page icon. It is now the round check mark, at the four sizes browsers ask
  for. A staging deployment gets the amber version, matching its app icon.

### Maintenance updates

- **A test copy of OpenV installs as its own app.** Installing the app from a
  staging or preview deployment put a second white *OpenV* tile on the home
  screen, identical to the live one, so which you opened was a guess. A
  non-production deployment can now carry its own installed name, icon and
  theme colour — staging installs as amber *OpenV (Staging)*. The live
  service is unchanged.

## 0.14.1 — 2026-09-18

### Maintenance updates

- **Every deployment can now say which build it is running.** The service
  health check reports the commit it was built from, and the web app serves
  the same at `/build.json`. Nothing changes in the app itself; it means a
  deployment can be matched to an exact revision when something needs
  chasing down, and it is what lets a test run prove which build it tested.
- **Agents can clear a suspect link.** Editing an artifact marks the links
  touching it suspect, and until now an agent could see the flag but not act
  on it — someone had to open the app and confirm each one by hand, even
  where the agent had just re-read both ends. The connector's tools now
  include confirming a link, the same action the link panel offers. Agents
  held to proposal review are still refused it: vouching that a trace holds
  is a sign-off, and it stays with the people who do the signing off.

## 0.14.0 — 2026-09-18

### New features

- **Read a project straight through.** Getting from one requirement to the next
  meant going back to the tree, finding your place in it, and clicking the row
  below — on a project of any size the tree became somewhere you lived rather
  than somewhere you looked. An artifact now carries **‹** and **›** and says
  where you are: *12 of 148*. They walk the project in the order the tree shows
  it — a heading, then what is under it, then the next heading — across whatever
  your search and filters have left in view, so a narrowed tree reads as its own
  short document.
- **J and K on a keyboard, a swipe on a phone.** **J** moves to the next
  artifact and **K** to the previous one, from anywhere except a field, an open
  editor or a dialog. On a phone, swipe the document sideways instead: left for
  the next artifact, right for the one before. A table or code block wide enough
  to scroll keeps its own sideways swipe, scrolling up and down is never
  interrupted, and the Tree / Document / Notes buttons still switch panes.
- **The tree opens itself to wherever you are.** Arriving at an artifact from a
  link, a citation, the review queue or the new ‹ / › controls could leave its
  row hidden inside a section you had collapsed: the artifact was on screen and
  the tree gave no sign of where it sat. The path from the artifact up to the
  top is now opened for you — and only that path, so a section you closed on
  purpose stays closed.

## 0.13.0 — 2026-09-18

### New features

- **Approve or send back from the review queue itself.** The In review list
  was a list of links: every decision meant opening the artifact, changing its
  status and coming back. Each row is now something you can act on. It shows
  the type, the reference, the title, the start of the description and small
  previews of the artifact's figures — pictures as thumbnails, PDFs and CAD
  files as chips — so you can judge most things without opening anything, and
  carries **Approve** and **Send back** buttons of its own.
- **A rejection now has to say why.** *Send back* asks what needs to change
  before it moves anything, and the comment goes onto the artifact's feed as an
  ordinary note — `@name` reaches a person, `@@name` raises them a to-do,
  `#REQ-12` cites, exactly as in the notes panel. The reason is posted before
  the artifact moves, so nobody ever finds work back in their drafts with no
  word on what was wrong with it.
- **Decide a batch at once.** Tick rows, or the box in the header to take the
  whole table, and approve everything selected or send it all back with one
  shared reason. Anything that could not be decided is reported and stays in
  the queue.

### Bug fixes

- **Sending a project for review no longer skips its headings.** A review
  round covered requirements, needs, test cases and the rest but deliberately
  left headings and descriptions out, which meant they sat in draft for ever
  and a project that had been through review still had unreviewed artifacts in
  it. A round now covers the whole document.

## 0.12.0 — 2026-09-18

### New features

- **Send a whole project for review in one action.** Putting a project through
  review meant opening each requirement and submitting it by hand, which is a
  chore on a project of ten and not a process at all on a project of two
  hundred. The Review Queue now has **Send project for review**: every
  artifact still in draft goes into review at once, and headings and
  descriptions — which have nothing to sign off — are left alone. It is built
  to be run again each cycle rather than once: an approved requirement nobody
  has touched stays approved, so nobody is asked to sign the same words twice,
  and one that was edited since it was approved comes back for a fresh
  sign-off. What comes back says how many were sent and how many stayed
  approved, so a quiet cycle looks quiet instead of looking like a button that
  did nothing.

### Bug fixes

- **Large figures and evidence files upload again.** An upload was given the
  same 60-second deadline as an ordinary click, and because that deadline
  covers sending the file as well as waiting for the answer, it quietly became
  a limit on how fast your connection was rather than on how big the file was:
  anything that took over a minute to send failed with a timeout before OpenV
  had seen it, however far inside your plan's limit it was. A separate 32 MB
  ceiling in front of the API turned away bigger files on top of that, so the
  128 MB / 512 MB / 1 GB figure limits your plan advertises were unreachable.
  Uploads now run to completion, and what refuses a file is your workspace's
  own limit, which says what it is. Adding a figure or an evidence file also
  shows a percentage as it goes, so a long upload no longer looks like a
  frozen screen.

## 0.11.1 — 2026-09-17

### Bug fixes

- **Attach the CAD file you actually work from.** A figure was capped at
  25 MB, which turned away most real geometry and made the CAD formats the
  uploader offers a promise it could not keep. The ceiling is now a workspace
  limit that follows your plan — 128 MB free, 512 MB on Business Lite, 1 GB on
  Business, and nothing imposed on a self-hosted OpenV. Workspace settings →
  **Limits** shows your number under *Largest figure*, and a refusal now names
  it instead of saying only that the file is too big.
- **Business workspaces get the bigger cloud runner they were sold.** Every
  runner figure on the Business tier was identical to Business Lite's, so the
  tier bought company features and not one minute of extra runner. Business
  now leases 8 GB and 4 CPUs for four hours, reclaimed after 30 idle minutes,
  and the pricing page states each tier's numbers rather than leaving
  Business's to be guessed from the tier below it.
- **Choosing how a side panel behaves no longer closes it mid-choice.**
  Clicking *Pinned → Auto-hide → Hidden* on the project menu or the notes
  panel shut the panel the moment the mode changed, taking the button that
  changes it off the screen; getting to the third option meant finding the
  edge strip first. The panel now stays put while you cycle, and closes when
  you click away or press Escape.
- **The strip that brings a hidden panel back is now something you can see and
  hit.** It was ten pixels wide with a chevron most people never found. It is
  wider, carries a chevron that looks like the button it is, and reaches the
  keyboard.
- **A Gemini CLI sign-in says what it will run into before you start it.**
  Google no longer serves Gemini CLI to free, Google One, AI Pro or AI Ultra
  accounts, so a sign-in from one of those lands on a deprecation page. The
  sign-in card now says so up front, and points at the workspace Gemini API
  key that still works.

## 0.11.0 — 2026-09-16

### New features

- **Tag people and artifacts inside a note.** Typing `@` in the notes panel
  offers the people on the project and writes the name the mention will
  actually resolve to, so a comment reaches the person you meant instead of
  quietly naming nobody. Typing `@@` does the same and **raises a to-do for
  them as the note is posted** — the note becomes the card, assigned to whoever
  it named. Raising one afterwards from the note still works exactly as before;
  `@@` is the shortcut for when you already know it is work.
- **Cite a figure or an artifact from a note**, the way you already can from a
  description. `#` offers this artifact's own figures and the artifacts it is
  linked to; `##` reaches the whole project. Citations in a note are links: a
  reader follows `#REQ-12` to the requirement, or `##REQ-17-FIG-1` to the
  drawing, without going to look for it.
- **`##` now offers any artifact in the project**, not only figures. Citing
  something this artifact is not linked to used to mean typing the reference by
  hand, where nothing marked it as a citation at all. The doubled marker is
  what says the citation reaches outside what this artifact is connected to, in
  the text as well as in the menu, so a single `#` still means a link the
  traceability matrix can see.
- **The quality linter flags a citation with no link behind it**, as an
  **error** — the same weight as unfinished placeholder text. A description
  that cites REQ-99 without a traceability link to it claims a connection the
  matrix does not hold, so a coverage or impact analysis reading the links will
  quietly disagree with the requirement as written. The finding names the
  artifact and asks you to link the two. Figure citations are not flagged:
  pointing at a drawing asserts nothing about how two artifacts relate. Set
  **Citation with no link** to a lower severity, or off, in the quality rules
  if your project wants it quieter.

### Maintenance updates

- The notes panel opens on **Comments** rather than the whole history, and the
  filters read **Comments, Changes, All** — the panel is where people talk to
  each other, and the recorded changes are the backdrop to that rather than the
  reason to open it.

## 0.10.1 — 2026-09-16

### Maintenance updates

- The notes panel's **Comments** tab is now called **History**, which is what it
  has always held: every version, status move, link, figure and test result
  OpenV recorded against the artifact, alongside the notes people wrote. Three
  buttons above the list narrow it — **All**, **Changes** for the audit trail
  alone, **Comments** for the discussion alone — so you can follow what happened
  to a requirement without reading past the conversation about it, or the other
  way round. An empty list now says what would appear in it instead of showing
  nothing at all.

## 0.10.0 — 2026-09-16

### New features

- **Search finds an artifact by its ref.** Typing `REQ-30` into the search box
  now brings back REQ-30 itself, at the top, instead of whatever happens to
  mention it — and `req-30` works just as well, so a ref pasted out of a
  report or a chat message finds its artifact without being tidied up first.
  Every result now shows its ref beside the title, so a list of similar
  titles is finally possible to tell apart. A longer ref that merely contains
  what you typed still appears, below the exact match: searching `REQ-3` puts
  REQ-3 first and leaves REQ-30 further down.

## 0.9.2 — 2026-09-16

### Maintenance updates

- OpenV is now at **openv.app**. The old `*.up.railway.app` addresses keep
  working, so nothing you have bookmarked or configured breaks, but the new
  one is the address to use and to share. Agent connectors, the MCP server
  and `sync.py` reach the API at `https://api.openv.app`.
- The web app is built with a current, maintained toolchain. The previous one
  had its last release in 2022 and has since been retired by its authors,
  which meant a slowly growing list of security advisories that nobody could
  act on, because no fixed version was ever going to be published. Nothing
  about the app looks or behaves differently — the same pages load the same
  way — but the ground it is built on is supported again.

### Bug fixes

- Retyping an artifact now gives it a matching reference. A heading created
  by accident and switched to a requirement (or any other type) picks up a
  fresh reference in that type's own numbering instead of keeping the
  heading's old one.

## 0.9.1 — 2026-09-15

### Maintenance updates

- Releases are no longer held up by the dependency robot failing to write its
  own update. It reports a failure when a flagged package has no upgrade path
  available, which is a fact about that package rather than anything wrong
  with the release — so fixes and features reach you on time instead of
  waiting behind it. Every test OpenV runs against itself still has to pass
  before a release goes out, as does the separate scan of the packages OpenV
  depends on and the scan of OpenV's own code.

## 0.9.0 — 2026-09-15

### New features

- Attach more than pictures to an artifact. A figure can now be a **PDF** —
  a supplier datasheet, a standard, a test report — or a **CAD file**: STEP,
  IGES, STL, 3MF, OBJ, PLY, glTF, DXF, DWG and the common native part
  formats. Clicking one opens it: a PDF in a reader, an STL as a 3D preview
  you can drag to turn, and every other format in a panel naming it with a
  Download button. Figures keep one numbering sequence whatever the format,
  so `REQ-17-FIG-2` may be a drawing today and the STEP model tomorrow.
- Cite a figure on **any** artifact in the project by typing `##` in a
  description. A single `#` still offers this artifact's own figures and the
  artifacts it is linked to; `##` offers every figure in the project, naming
  the artifact each belongs to, and writes the citation as
  `##REQ-99-FIG-2` so a reader can see it reaches outside what they are
  reading.

### Maintenance updates

- OpenV's own source code is now scanned by CodeQL on every change, on every
  release, and weekly against an updated set of rules. It looks for bugs we
  wrote — injection, path traversal and similar — which is the half that the
  existing dependency scanning cannot see.

### Bug fixes

- Clicking a figure or artifact citation in a description works again. Every
  citation had been rendering as a link with no destination, so following one
  opened a new tab showing the page you were already on instead of the figure
  or artifact it named.

## 0.8.2 — 2026-09-15

### Maintenance updates

- Every release from now on is tagged in the source repository as
  `v<version>`, so the exact code behind a version number can be found,
  compared against another release, or checked out. That matters when you
  are self-hosting, and when pinning down which release a problem started
  in. Releases up to 0.8.1 predate this and are untagged.

## 0.8.1 — 2026-09-15

### Bug fixes

- 0.8.0 could not start against an existing database, taking the service
  down until this release. Nothing was lost and no data was touched — the
  server refused to start rather than run a schema change it could not
  complete — but OpenV was unreachable in the meantime.

## 0.8.0 — 2026-09-15

### New features

- **To-dos**, under *Plan*, lists the project's work under the person it
  belongs to rather than the column it sits in — everyone with access gets a
  section, including those who owe nothing, so the page answers who owes
  what. It is the board's work rearranged, not a second list: move a card
  and this page says so. *Just mine* narrows it to you, and finished work is
  hidden until you ask for it.
- **Raise a to-do from a note.** A note that mentions someone with @name now
  offers *Add to-do*, which fills in the person it named and the note's
  first line, links the artifact and carries the whole note across as the
  description. Afterwards the note shows a small link to the to-do with its
  current status, so an old thread tells you whether the thing being
  discussed ever got done. Mentioning someone still just tells them: nothing
  is added to the board unless you ask for it.
- **Restore an older version of a figure.** Its history now offers *Restore*
  on any earlier version: the figure goes back to the image and the name it
  had then. Like restoring an artifact, it is recorded as a new version and
  nothing is deleted — the superseded drawing stays in the history and stays
  openable, which is what you need when a requirement was reviewed against
  what the figure used to show. The history entry says which version it
  brought back.

### Bug fixes

- Antigravity agents no longer stop five minutes in. The CLI applies its own
  five-minute cap to a single-prompt run and ends it there regardless of the
  timeout set on the agent — which defaults to thirty minutes — so a longer
  piece of work was cut off and whatever had been produced so far was
  reported as the answer. The agent's own timeout is now the only one that
  applies.

### Maintenance updates

- Cloud runners now build with a fixed, checksum-verified copy of the
  Antigravity CLI rather than fetching whichever version is current at build
  time. Two rebuilds of the same commit now give runners the same CLI, and a
  version change is a visible edit to this repository instead of something
  that happens quietly between builds.

## 0.7.1 — 2026-09-15

### Maintenance updates

- The **?** help button now sits beside the notification bell instead of
  floating over the bottom-right corner of the page. It used to cover
  whatever the page put in that corner — on the V&V dashboard, the *Complete*
  and *Abort* buttons of the last test run — and the first attempt at fixing
  that reserved the corner instead, which left an empty strip along the
  bottom of every page. Neither now: it is in the bar with the other controls
  that are about you rather than the page, which is where phones have had it
  all along.

## 0.7.0 — 2026-09-15

### New features

- **Antigravity CLI** is available as an agent provider. Google moved the
  free, Google One, AI Pro and AI Ultra tiers off the Gemini CLI and onto
  Antigravity on 18 June 2026, so agents can now run on it. It runs from a
  Gemini API key set on the workspace rather than a personal sign-in, because
  the CLI keeps its sign-in in the computer's keyring and a cloud runner has
  none. Agents that edit a connected repository still need Claude Code.

### Maintenance updates

- The floating **?** help button no longer sits on top of the buttons in the
  bottom-right corner of a page. On the V&V dashboard it covered the
  *Complete* and *Abort* buttons of the last test run, which could not be
  clicked at all once the table reached the bottom of the window.
- A failed **Gemini CLI** sign-in now explains the likeliest reason: since
  18 June 2026 the Gemini CLI signs in only Google accounts on a Gemini Code
  Assist Standard or Enterprise licence, and other tiers need an API key
  instead. The message used to be the CLI's raw output with no hint that no
  amount of retrying would help.

## 0.6.3 — 2026-09-14

### Bug fixes

- Signing the Gemini CLI in from a cloud runner works again. The CLI will
  only complete a sign-in from a session it considers interactive, and it
  counted the runner's as automated on three separate grounds — so it stopped
  with "Manual authorization is required but the current session is
  non-interactive" before showing the link. It now gets a real terminal and
  the sign-in link appears as it should. It also no longer refuses the
  runner's workspace as untrusted.

## 0.6.2 — 2026-09-14

### Maintenance updates

- The **Cloud runner** card now shows how busy the shared pool is as a
  traffic light — *Runners available*, *Runners busy*, *All runners in use* —
  instead of printing how many runners are free. You can hold one runner at a
  time, so the count was never something you could act on, and the deployment's
  capacity is no longer published to every account.

## 0.6.1 — 2026-09-14

### Bug fixes

- Signing the Gemini CLI in from a runner works again, on a phone or a
  desktop. The CLI now refuses to start unless it is told which kind of
  Google account to use, so the sign-in failed before it could show you a
  link — and headless Gemini runs failed the same way. OpenV names the mode
  for it. Workspaces that run Gemini on an API key or on Vertex AI keep the
  account they configured.

## 0.6.0 — 2026-09-14

### New features

- Choose the workspace OpenV opens in when you sign in. Personal settings →
  *Default workspace* lists your personal workspace and every company
  workspace you belong to; pick the one you work in and each sign-in starts
  there instead of in your personal space. Switching workspaces during a
  session is unchanged. Stable-channel workspaces get this with their next
  stable release.
- Figures can be renamed. A screenshot arrives called something like
  `Screenshot 2026-09-14 at 09.12.33.png`; use ✎ on the figure while editing
  to give it a name that says what it shows. The name appears under the
  figure and in PDF and Word documents, and every rename is a figure
  version, with who changed it and when, alongside the image history.
- Forgot your password? The sign-in screen can now email you a link to set
  a new one: it works once and lasts an hour, and setting the password signs
  the account out everywhere. On a server that sends no mail, or when the
  email does not arrive, a platform admin can make a reset link for your
  account from the Platform admin page and pass it to you.

### Bug fixes

- A duplicated or pasted artifact now says where it came from. Its history
  starts with one note, *Copied from REQ-12 (version 3)*, instead of looking
  like an artifact that was typed in from scratch.

- Uploading a new version of a figure works again. Pressing ⬆ (or the
  history, rename or delete buttons) on a figure used to save the artifact
  and leave the editor before the file was chosen, so the image never
  changed; the buttons no longer submit the editing form.
- PDF and Word downloads no longer print each requirement's description
  twice. It sits once, in the Description row of the fields table, and
  keeps its formatting there: lists, tables, code, links and emphasis all
  survive, and a description longer than a page carries its row across
  the break.

## 0.5.1 — 2026-09-13

### Maintenance updates

- OpenV's licence is now the Elastic License 2.0. Self-hosting stays free
  for anyone at any scale, the source stays public, and paid installation
  or support stays allowed; what is no longer allowed is offering OpenV to
  others as a hosted or managed service. The README, the site's licence
  section and the manual carry a plain-English summary.

## 0.5.0 — 2026-09-13

### New features

- The V&V Assistant can now change the project, not just add to it — from
  the notes panel and from the guided definition wizard alike. Ask it to
  add a test case, a design item, a heading or any other kind of artifact
  and it offers a card that files it where you said; ask it to rewrite or
  restructure and it offers *Apply change* and *Move* cards that edit an
  artifact's text or move it under another heading. It sees the whole
  project's outline while you talk and can read any artifact in full, so
  when you resume a definition it can work on what is already there, name
  things by their reference, and tell a locked wizard entry from a new
  one. Stable-channel workspaces get this with their next stable release.

## 0.4.2 — 2026-09-13

### Maintenance updates

- Platform admins have a page of their own, under the account menu: every
  workspace with its plan, changeable in place (which is how a workspace
  is granted the open-source tier), and every account, where platform-admin
  standing is granted or removed. Until now only the first account ever
  registered could be a platform admin.

## 0.4.1 — 2026-09-13

### Maintenance updates

- A platform admin can move a workspace to another plan through the API,
  which is how a workspace is granted the open-source tier; before, that
  took a hand edit of the database.

## 0.4.0 — 2026-09-13

### New features

- Share a project from one link. Under Project settings → Access, an owner
  mints a *public* link that opens the live project read only for anyone,
  no account needed, or a *reviewer* link that asks the holder to sign in
  and makes them a reviewer. Links can carry a label and an expiry, and can
  be revoked. Pasted into Slack, Discord, LinkedIn and the like, a link
  shows a preview card naming the project.
- A new project role, *reviewer*: reads everything, adds notes, comments
  and mentions, and changes nothing. It can be granted by name or team like
  the other roles.
- Workspaces on the open-source tier have every project's latest baseline
  published on the site's open-source page, where anyone can read it; live
  work stays private until the next baseline.

### Maintenance updates

- PDF and Word downloads put each requirement's description in its table,
  under the reference and type, so it no longer gets lost between figures.
- The site has grown from one page into a small storefront: how OpenV works
  with diagrams, five narrated demo videos (two of them on a phone), a
  security and subscription FAQ, an open-source projects page, and places
  for customer stories and white papers.

## 0.3.0 — 2026-09-13

### New features

- A project can name the project it refines, so a subsystem or a supplier
  works in its own project under the system it belongs to. Set it under
  Project settings → General.
- A requirement can *refine* a requirement of the parent project. The link
  is offered when editing a requirement, is seen from both projects, and the
  parent's V&V shows each refined requirement's own result and rolls the
  worst one up, so verification flows back up the tree.
- Every artifact can have an owner: a member, or one of the reference
  parties a project lists in its settings (your workspace is always one).
  The owner shows on the artifact, and the module view, the API and the
  agent tools can filter by it.
- A download can be narrowed to one or more owners, in every format, so a
  supplier's share of a project can be handed over on its own.

## 0.2.0 — 2026-09-13

### New features

- Workspace settings show the workspace's release channel: Business and
  Enterprise workspaces are on the monthly stable channel and their admins
  can switch to nightly; Personal and Lite workspaces run nightly.
- Stable-channel workspaces receive new features at a monthly stable
  release instead of with every nightly; maintenance updates and bug fixes
  still arrive with each nightly. Workspace admins choose the day and hour
  the monthly release turns on, up to 14 days after it is designated, and
  are told when it is designated and the day before it turns on.
- Any member of a Business or Enterprise workspace can try the next stable
  release early for their own account from workspace settings.
- What's new opens with your workspace's own channel: the stable release it
  runs and the one scheduled next, or the nightly it runs; stable releases
  are marked in the history.
- Release notifications follow the channel: nightly-channel members hear
  about each release, stable-channel members when their monthly release
  turns on.
- Dedicated OpenV instances are warned 30 and 7 days before their support
  window closes after a newer stable release, and again once it has.

## 0.1.0 — 2026-09-13

### New features

- OpenV releases are numbered now. What's new names the version you were
  upgraded to and keeps new features, maintenance updates and bug fixes
  apart instead of running them together in one list, and the notification
  that announces a release is grouped the same way.

### Bug fixes

- What's new shows the releases and nothing else. It was also showing the
  notes file's instructions to contributors, and changes that had not
  shipped yet.

## 2026-09-12.3

- The V&V Assistant's answers now read as they were written — lists as lists,
  emphasis as emphasis — instead of showing the raw markup around them. The
  same goes for the assistant in an interview.
- Add what the assistant suggests from wherever you are talking to it. A
  persona, need, requirement, NFR or hazard it proposes can be added to the
  project straight from the notes panel, filed under the usual heading and
  left as a draft for you to review; before, only the guided definition
  wizard could take them.

## 2026-09-12.2

- Get a notification whenever OpenV is updated, with a summary of what
  changed, and read the full history under What's new in the account menu.
- Open tabs learn about an update while they are open and offer a reload.

## 2026-09-12

- Upload your own profile picture from Personal settings. It replaces the
  picture your sign-in provider supplied and stays put across sign-ins.
- Member avatars keep their round shape beside long names on a phone.
- PDF and Word downloads are rebuilt on a document model: templates, field
  selection, evidence appendices and the workspace logo on the cover page.
- Baselines record who captured them, and large baselines are stored
  compressed.
- Workspace admins are notified when somebody joins, leaves or changes role,
  and members are told when their own access changes.
- Workspace limits are shown in workspace settings.
