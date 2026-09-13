# Requirement flow-down, ownership and supplier subsets

How a programme with subsystems and suppliers is modelled (OpenV Platform
project: NEED-16, REQ-144 to REQ-148, DSC-8).

## The shape

An aircraft has a set of requirements. The landing gear has its own, and
each of those refines one or more of the aircraft's. A brake assembly under
the landing gear has its own again. Each level is a **project**; a project
names the one above it as its **parent project** (Project settings →
General). A parent must be in the same workspace and may not be a
descendant, so the hierarchy is a tree.

A supplier building the landing gear works **in the landing-gear project**
with editor rights there and viewer rights on the aircraft project. Per-
project access grants (REQ-16) make that a normal membership; nothing about
the parent is exposed beyond what the supplier is granted.

## Flow-down: the `refines` link

A requirement in a child project **refines** a requirement of its parent.
It is an ordinary traceability link (requirement → requirement) that is
allowed to cross the project boundary: the API accepts it with editor rights
on the child and viewer rights on the parent, since the supplier can only
read what it is refining. Within one project `refines` may still be used
between two requirements, but `decomposes-to` is the usual choice there.

A link belongs to both projects it touches. The parent's export, baselines,
traceability, V&V and AI map all see the refinement, named with its project:
*Landing gear / REQ-17*. The `linked-artifacts` endpoint carries those names
so the module view shows a foreign requirement's title without rights on its
project.

The refines picker in the artifact editor lists the parent project's
requirements alongside the local ones.

## Flow-up: verification

Verification flows the other way. A parent requirement's coverage carries
its **refinements**, each with its own rollup computed in its own project
(recursively, so a brake requirement under the landing gear rolls all the
way up). The **flow-down** rollup is the worst of them: fail over blocked
over unrun over uncovered over pass.

- A requirement with no evidence of its own takes the flow-down as its
  rollup and is marked *via refinements*; it is not a gap.
- A requirement with its own tests or attestation keeps the worse of its own
  result and the flow-down.
- A requirement without a verification method stays *method-missing*: the
  method is still the parent's to state.

## Owners and reference parties

Every artifact may carry an **owner**: the party or member responsible. It
is the `owner` attribute, a standard key beside priority and status, shown
as a chip on the artifact and filterable in the module view, the list API
and the MCP tools.

Who counts as a party is the project's own list, **Reference parties**
(Project settings → General): the organisations, teams and suppliers the
project works with. The workspace's own company is always first and cannot
be removed. The owner field suggests these parties and the project's
members; any name is accepted.

## Subsets for a supplier

Every download format takes an **owners** filter: the artifacts owned by
the parties named, plus the headings that organise them, and the links
among them (a link to another project's artifact is kept, since the reader
can still see it named). The download wizard offers the owners present in
the project; `scripts/openv/sync.py export --owner "Landing gear supplier"
--format docx` does the same from a terminal.

## How this compares

| | OpenV | IBM DOORS / DOORS Next | Jama Connect | Polarion | Codebeamer |
|---|---|---|---|---|---|
| Hierarchy | Project tree via `parent_project_id` | Folders and modules; DOORS Next project areas | Projects and components | Project groups | Project trees |
| Flow-down | `refines` link across projects, both sides see it | Link modules across modules; cross-project links in DOORS Next | Relationships across projects, "Reuse & sync" | Cross-project work-item links | Cross-project references |
| Verification roll-up | Coverage carries refinements, worst-of flow-down | Reports and views per module | Coverage per item; cross-project coverage via views | Traceability reports | Coverage reports |
| Owner | `owner` attribute, parties + members | User attributes; DOORS partitions name the partner | Assignee and organisation fields | Assignee | Assigned to |
| Supplier subset | Owner-filtered download in every format | DOORS partitions: export a subset to a partner and sync back | Baselines and exports per view | Exports per query | Exports per filter |

Partitions in DOORS are the closest to what a supplier needs and they include
a synchronisation on return; OpenV's owner-filtered download is the outbound
half. Importing a supplier's changes back is the next step (a re-import that
matches on `ref`), not yet built.
