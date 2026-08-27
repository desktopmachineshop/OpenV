// User manual chapter: Stakeholder interviews.
const content = `
# Interviews

Interviews let stakeholders talk to an **AI interviewer** through a public
link — no account needed. The agent asks questions based on your brief and
records candidate user needs as it learns them.

Interviews live on the project's **Overview** page, in the **User interviews**
section.

## Creating an interview

Click **+ New interview**:

- **Interview name** — e.g. "Machinist onboarding feedback".
- **Brief for the interviewer agent** — what the interview should find out:
  topics, tone, questions to cover.
- **Persona** (optional) — link the interview to the persona the participant
  represents. Several interviews can share one persona, so you can compare how
  different people in the same role describe their needs. The persona's title
  and description are included in the interviewer agent's context.

## Sharing invite links

Click **Copy invite link** on an interview to create an invite and copy its
URL. The link opens a public, mobile-friendly chat page:

1. The participant enters their name.
2. They chat with the interviewer agent — replies stream in live.
3. When they're done, the session is complete.

Invite links are token-authenticated and can be revoked: **Close** an
interview and its invite links stop working (you'll be asked to confirm).

The interviewer agent needs a runner online to reply — automations-style
ownerless routing applies, so a hosted runner keeps interviews responsive even
when no one is at their desk (see *Runs & runners*).

## Reviewing sessions

Expand an interview to see its **sessions** — one per participant, with name,
status, and start time.

- Open a session to read the full **transcript**.
- Sessions can carry a **summary** produced by the agent, highlighted above
  the transcript.
- Use the **persona filter** above the interview list to focus on interviews
  linked to one persona (or those with none).

Candidate user needs recorded by the interviewer arrive as draft artifacts /
proposals for review — nothing enters the requirement set unreviewed.
`;

export default content;
