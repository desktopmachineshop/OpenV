// User manual chapter: Crews (agent org charts).
const content = `
# Crews

A **crew** is a group of agents (and optionally people) arranged in an
org-chart graph. Use a crew when a task naturally splits — one agent drafts
requirements, another reviews for testability, a person signs off.

Open **Crew** in the project sidebar.

## Managing crews

The header holds a crew selector plus:

- **New crew** — create an empty crew for this project.
- **Start from default crew** — clone the workspace's default crew as a
  starting point.
- **Delete** — available for non-default crews.
- **▶ Run crew** — launch the crew with a prompt (what should the crew work
  on?). You're taken to the Runs page to follow along.

## The canvas

Two view modes:

- **Org Chart** — hierarchical layout.
- **Network** — free-form layout.

Node positions are saved per view — drag nodes to arrange them. The filter
toggle shows **Employees**, **Agents**, or **All** nodes. Nodes with a live
run pulse so you can see who is working.

## Adding nodes

**+ Add node** adds either:

- an **Agent** — pick from the workspace's agent definitions, or
- a **Person** — pick a workspace member.

Both take an optional label and department (departments group nodes on the
chart).

Click a node to open its configuration panel (label, department, bound agent
or person); click an edge to configure or delete it.

## Connecting nodes

Click **Connect**, then click the source node and the target node. Pick the
connection type:

| Edge type | Meaning |
| --- | --- |
| Delegates to | The source agent can hand work down to the target agent |
| Hands off to | Work passes to the target when the source finishes |
| Reviews | The target reviews the source's output |

**Human targets are special**: you can't delegate to a person. "Hands off to"
and "Reviews" edges pointing at a person instead create a **Board card
assigned to that person** when the work reaches them — so human review steps
show up on the kanban board.

## Crews elsewhere in the app

- Board cards can be **assigned to a crew** (👥) — moving the card launches
  the crew on it.
- **Automations** can target a crew instead of a single agent.
- Runs launched through a crew are tagged *(crew)* on the Runs page, and
  delegation between agents shows up as follow-up runs.
`;

export default content;
