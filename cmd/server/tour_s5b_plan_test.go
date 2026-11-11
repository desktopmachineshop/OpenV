//go:build unix

package main

import (
	"net/http"
	"testing"
)

// TestTourS5bPlan is the S5b tour's plan area: the Gantt over the board
// (internal/domain/plan). Its golden is testdata/tour/s5b/plan.json.
//
// The owner, in its personal (nightly) workspace, keeps project P with user
// needs N1 and N2, requirement R deriving from N1, release MVP, and three
// board tasks: Build, with sub-tasks Frame (done) and Wiring. Project Q has a
// task and a milestone of its own. The viewer is a project viewer of P. The
// dates are far in the past or the future, so late never depends on the day
// the tour runs.
//
// What the golden shows:
//   - an empty project's plan: no tasks, no milestones, every metric at 0;
//   - a schedule sets a start, a due date (the work item's own), a parent
//     and relationships, each finish to start, start to start or finish to
//     finish, several to a task; an unknown type is a 400, and the earlier
//     depends_on form keeps the type a task has; a start after the due date, a task of Q as parent or
//     dependency, a task as its own parent, a loop of parents or of
//     dependencies and a malformed date are a 400; the viewer's is a 403;
//   - a task keeps its duration: a start sent alone moves the due date, a
//     due date sent alone moves the start, a duration sent alone moves the
//     due date; effort is stored beside it; a negative one is a 400; the
//     plan reports each task's float and marks the critical path;
//   - a relationship is made, retyped, turned round and removed from either
//     task; a task with sub-tasks takes no dates of its own (400);
//   - the plan has holidays and a critical threshold; a task has a
//     deadline and may count its duration in calendar days; a relationship
//     may be start to finish, and its offset has a unit;
//   - a task has a scheduling mode and a date constraint; a plan has a
//     mode, a start and a finish, and may have a critical path per chain;
//     an automatic task is placed after each change;
//   - a task is assigned to people for a share of their time, has a fixed
//     cost and may let its effort decide its duration; a person has a
//     daily rate and a capacity; the plan has a currency and a budget, and
//     is levelled on request;
//   - a relationship carries an offset in working days, and the plan is
//     recalculated: the driven tasks move to what their relationships ask,
//     and a second recalculation moves nothing;
//   - a task is assigned to a milestone or to a release's, and taken off
//     it; the milestone then counts its tasks done among its criteria;
//     another project's milestone or release is a 400;
//   - the plan's working days are set (Monday to Saturday), and durations
//     and float count them; none, or a day 8, is a 400;
//   - a to-do raised on a task is the project's, takes one date (its
//     deadline), a task lists its own, and
//     ticking one moves the task's progress and its parent's;
//   - a milestone takes criteria; an unknown metric, a target of 0 and a
//     missing name are a 400; MVP is a release milestone with the default
//     criteria until its own are set; its name cannot be set here (400);
//   - the plan: the outline with spans and progress, a late task, the
//     dependencies, the milestones in date order with their criteria
//     measured (one of two user needs addressed: 50%);
//   - the export as CSV, JSON, Excel and PDF; ReqIF is a 400;
//   - Q's milestone and Q's release through P are a 404; a deleted milestone
//     is a 404 after it.
func TestTourS5bPlan(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5b",
		key:   "plan",
		about: "The plan: a task's schedule, sub-tasks, dependencies and to-dos, milestones and their criteria, " +
			"release milestones, the computed plan and its export.",
		run: planTour,
	})
}

func planTour(tr *tour) {
	o := tr.owner
	tr.setup("project P", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour plan"}`)).capture("p", "/id")
	tr.setup("project Q", o, "POST /api/v1/projects", jsonBody(`{"name":"Tour plan elsewhere"}`)).capture("q", "/id")
	viewer := tr.register("viewer", "Tour Viewer", "a project viewer of P")
	tr.setup("the viewer joins P", o, "POST /api/v1/projects/{id}/members", at("id", "{{p}}"),
		jsonBody(`{"email":"tour-viewer@example.com","role":"viewer"}`), expect(http.StatusCreated))

	plan := "GET /api/v1/projects/{id}/plan"
	tr.step("P's plan before anything: no tasks, no milestones", o, plan, at("id", "{{p}}"))

	art := "POST /api/v1/artifacts"
	tr.setup("user need N1", o, art, jsonBody(`{"project_id":"{{p}}","type":"user-need","title":"Stop in time"}`)).capture("n1", "/id")
	tr.setup("user need N2", o, art, jsonBody(`{"project_id":"{{p}}","type":"user-need","title":"Stay quiet"}`)).capture("n2", "/id")
	tr.setup("requirement R", o, art, jsonBody(`{"project_id":"{{p}}","type":"requirement","title":"Brake within 2 m",`+
		`"body":"The system shall brake within 2 m."}`)).capture("r", "/id")
	tr.setup("R derives from N1", o, "POST /api/v1/links", jsonBody(`{"from_id":"{{r}}","to_id":"{{n1}}","type":"derives-from"}`))
	tr.setup("release MVP", o, "POST /api/v1/projects/{id}/releases", at("id", "{{p}}"),
		jsonBody(`{"name":"MVP","target_date":"2099-06-30"}`)).capture("mvp", "/id")
	tr.setup("Q's release", o, "POST /api/v1/projects/{id}/releases", at("id", "{{q}}"), jsonBody(`{"name":"MVP"}`)).capture("q_mvp", "/id")
	card := "POST /api/v1/projects/{id}/work-items"
	tr.setup("task Build", o, card, at("id", "{{p}}"), jsonBody(`{"title":"Build the rig","column":"in-progress"}`)).capture("build", "/id")
	tr.setup("task Frame, done", o, card, at("id", "{{p}}"), jsonBody(`{"title":"Weld the frame","column":"done"}`)).capture("frame", "/id")
	tr.setup("task Wiring", o, card, at("id", "{{p}}"), jsonBody(`{"title":"Wire the rig","column":"todo"}`)).capture("wiring", "/id")
	tr.setup("task Order, long overdue", o, card, at("id", "{{p}}"), jsonBody(`{"title":"Order the parts","column":"todo"}`)).capture("order", "/id")
	tr.setup("Q's task", o, card, at("id", "{{q}}"), jsonBody(`{"title":"Elsewhere"}`)).capture("q_task", "/id")

	schedule := "PUT /api/v1/work-items/{id}/schedule"
	tr.step("Frame: 1 to 10 March 2099, under Build", o, schedule, at("id", "{{frame}}"),
		jsonBody(`{"start_date":"2099-03-01","due_date":"2099-03-10","parent_id":"{{build}}"}`))
	tr.step("Wiring: starts the day Frame ends, under Build, after Frame", o, schedule, at("id", "{{wiring}}"),
		jsonBody(`{"start_date":"2099-03-10","due_date":"2099-03-20","parent_id":"{{build}}","depends_on":["{{frame}}"]}`))
	tr.step("Order: a due date alone, in 2020, so it is late", o, schedule, at("id", "{{order}}"),
		jsonBody(`{"due_date":"2020-01-15"}`))
	tr.step("Frame waits on Order; its parent and dates stay as they were", o, schedule, at("id", "{{frame}}"),
		jsonBody(`{"depends_on":["{{order}}"]}`))
	tr.step("a start after the due date, both sent: 400", o, schedule, at("id", "{{frame}}"),
		jsonBody(`{"start_date":"2099-04-01","due_date":"2099-03-10"}`))
	tr.step("Frame's start moved alone: its due date moves with it, the 10 days kept", o, schedule, at("id", "{{frame}}"),
		jsonBody(`{"start_date":"2099-03-02"}`))
	tr.step("Frame's due date moved back alone: its start moves with it", o, schedule, at("id", "{{frame}}"),
		jsonBody(`{"due_date":"2099-03-10"}`))
	tr.step("Wiring's duration set to 5 days, with 7.5 days of effort: its due date moves", o, schedule, at("id", "{{wiring}}"),
		jsonBody(`{"duration_days":5,"effort_days":7.5}`))
	tr.step("Wiring back to 11 days", o, schedule, at("id", "{{wiring}}"), jsonBody(`{"duration_days":11}`))
	tr.step("a negative duration: 400", o, schedule, at("id", "{{wiring}}"), jsonBody(`{"duration_days":-1}`))
	tr.step("negative effort: 400", o, schedule, at("id", "{{wiring}}"), jsonBody(`{"effort_days":-1}`))
	tr.step("a date not written YYYY-MM-DD: 400", o, schedule, at("id", "{{frame}}"), jsonBody(`{"start_date":"01/03/2099"}`))
	tr.step("its own parent: 400", o, schedule, at("id", "{{build}}"), jsonBody(`{"parent_id":"{{build}}"}`))
	tr.step("a parent that is its sub-task: 400", o, schedule, at("id", "{{build}}"), jsonBody(`{"parent_id":"{{frame}}"}`))
	tr.step("Q's task as parent: 400", o, schedule, at("id", "{{build}}"), jsonBody(`{"parent_id":"{{q_task}}"}`))
	tr.step("Q's task as dependency: 400", o, schedule, at("id", "{{build}}"), jsonBody(`{"depends_on":["{{q_task}}"]}`))
	tr.step("a loop of dependencies: 400", o, schedule, at("id", "{{order}}"), jsonBody(`{"depends_on":["{{wiring}}"]}`))
	tr.step("Wiring is driven by two tasks: it finishes no earlier than Frame (FF) and starts no earlier than Order (SS)", o, schedule,
		at("id", "{{wiring}}"), jsonBody(`{"relationships":[{"task_id":"{{frame}}","type":"FF"},{"task_id":"{{order}}","type":"SS"}]}`))
	tr.step("an unknown relationship type: 400", o, schedule, at("id", "{{wiring}}"),
		jsonBody(`{"relationships":[{"task_id":"{{frame}}","type":"SF"}]}`))
	tr.step("depends_on, the earlier form: Frame keeps FF, and Order is dropped", o, schedule, at("id", "{{wiring}}"),
		jsonBody(`{"depends_on":["{{frame}}"]}`))
	tr.step("Wiring back to finish to start on Frame", o, schedule, at("id", "{{wiring}}"),
		jsonBody(`{"relationships":[{"task_id":"{{frame}}"}]}`))
	rel := "PUT /api/v1/work-items/{id}/relationships/{otherId}"
	tr.step("a relationship made from the driving end: Order drives Wiring, start to start", o, rel,
		at("id", "{{order}}", "otherId", "{{wiring}}"), jsonBody(`{"type":"SS","direction":"drives"}`))
	tr.step("the same pair turned round from Wiring's end: Wiring drives Order, finish to finish", o, rel,
		at("id", "{{wiring}}", "otherId", "{{order}}"), jsonBody(`{"type":"FF","direction":"drives"}`))
	tr.step("a relationship that would close a loop: 400", o, rel, at("id", "{{frame}}", "otherId", "{{wiring}}"),
		jsonBody(`{"type":"FS","direction":"driven_by"}`))
	tr.step("a direction that is neither: 400", o, rel, at("id", "{{order}}", "otherId", "{{wiring}}"),
		jsonBody(`{"type":"FS","direction":"sideways"}`))
	tr.step("Q's task: 400", o, rel, at("id", "{{order}}", "otherId", "{{q_task}}"), jsonBody(`{"type":"FS"}`))
	tr.step("the viewer may not relate tasks: 403", viewer, rel, at("id", "{{order}}", "otherId", "{{wiring}}"), jsonBody(`{"type":"FS"}`))
	tr.step("the relationship removed from Order's end: 204", o, "DELETE /api/v1/work-items/{id}/relationships/{otherId}",
		at("id", "{{order}}", "otherId", "{{wiring}}"))
	tr.step("an offset: Order drives Wiring start to start, 5 working days later", o, rel,
		at("id", "{{order}}", "otherId", "{{wiring}}"), jsonBody(`{"type":"SS","direction":"drives","lag_days":5}`))
	tr.step("an offset in an unknown unit: 400", o, rel, at("id", "{{order}}", "otherId", "{{wiring}}"),
		jsonBody(`{"type":"SS","direction":"drives","lag_days":5,"lag_unit":"weeks"}`))
	tr.step("start to finish, half the driver's duration later", o, rel, at("id", "{{order}}", "otherId", "{{wiring}}"),
		jsonBody(`{"type":"SF","direction":"drives","lag_days":50,"lag_unit":"%"}`))
	tr.setup("back to start to start, 5 working days later", o, rel,
		at("id", "{{order}}", "otherId", "{{wiring}}"), jsonBody(`{"type":"SS","direction":"drives","lag_days":5}`))
	tr.step("Wiring gets a deadline, and its duration counts calendar days", o, schedule, at("id", "{{wiring}}"),
		jsonBody(`{"deadline":"2099-12-31","elapsed":true}`))
	tr.step("a deadline that is not a date: 400", o, schedule, at("id", "{{wiring}}"), jsonBody(`{"deadline":"soon"}`))
	tr.setup("Wiring counts working days again, with no deadline", o, schedule, at("id", "{{wiring}}"), jsonBody(`{"deadline":"","elapsed":false}`))
	tr.step("an offset beyond ten years: 400", o, rel, at("id", "{{order}}", "otherId", "{{wiring}}"),
		jsonBody(`{"type":"SS","direction":"drives","lag_days":99999}`))
	recalculate := "POST /api/v1/projects/{id}/plan/recalculate"
	tr.step("the plan recalculated: the driven tasks move to what their relationships ask", o, recalculate, at("id", "{{p}}"))
	tr.step("recalculated with relationships as minimums: nothing is pulled earlier", o, recalculate, at("id", "{{p}}"), jsonBody(`{"only_later":true}`))
	tr.step("Wiring must start on a date, in its own automatic mode: it is placed at once", o, schedule, at("id", "{{wiring}}"),
		jsonBody(`{"mode":"auto","constraint_type":"mso","constraint_date":"2099-06-01"}`))
	tr.step("a constraint with no date: 400", o, schedule, at("id", "{{wiring}}"), jsonBody(`{"constraint_type":"snet","constraint_date":""}`))
	tr.step("an unknown mode: 400", o, schedule, at("id", "{{wiring}}"), jsonBody(`{"mode":"sometimes"}`))
	tr.setup("Wiring back to the plan's mode, with no constraint", o, schedule, at("id", "{{wiring}}"), jsonBody(`{"mode":"","constraint_type":"asap"}`))
	tr.step("recalculated again: nothing moves", o, recalculate, at("id", "{{p}}"))
	tr.step("the viewer may not recalculate: 403", viewer, recalculate, at("id", "{{p}}"))
	tr.setup("Order and Wiring unrelated again", o, "DELETE /api/v1/work-items/{id}/relationships/{otherId}",
		at("id", "{{order}}", "otherId", "{{wiring}}"))
	ann := "11111111-1111-4111-8111-111111111111"
	resource := "PUT /api/v1/projects/{id}/plan/resources/{assigneeId}"
	tr.step("a person's daily rate and capacity", o, resource, at("id", "{{p}}", "assigneeId", ann),
		jsonBody(`{"type":"user","daily_rate":400,"capacity":80}`))
	tr.step("a negative rate: 400", o, resource, at("id", "{{p}}", "assigneeId", ann), jsonBody(`{"type":"user","daily_rate":-1}`))
	tr.step("a resource that is neither a user nor an agent: 400", o, resource, at("id", "{{p}}", "assigneeId", ann),
		jsonBody(`{"type":"team","daily_rate":1}`))
	tr.step("the viewer may not set a rate: 403", viewer, resource, at("id", "{{p}}", "assigneeId", ann), jsonBody(`{"type":"user","daily_rate":1}`))
	tr.step("Wiring assigned to her at full time, with a fixed cost, its effort deciding its duration", o, schedule, at("id", "{{wiring}}"),
		jsonBody(`{"assignments":[{"type":"user","id":"`+ann+`","units":100}],"fixed_cost":250,"effort_driven":true}`))
	tr.step("an assignment of 0 percent: 400", o, schedule, at("id", "{{wiring}}"),
		jsonBody(`{"assignments":[{"type":"user","id":"`+ann+`","units":-5}]}`))
	tr.step("a negative fixed cost: 400", o, schedule, at("id", "{{wiring}}"), jsonBody(`{"fixed_cost":-1}`))
	level := "POST /api/v1/projects/{id}/plan/level"
	tr.step("the plan levelled: she is over her 80% on Wiring alone, which no delay can cure", o, level, at("id", "{{p}}"))
	tr.step("the viewer may not level: 403", viewer, level, at("id", "{{p}}"))
	tr.setup("Wiring unassigned, with no fixed cost, its duration its own again", o, schedule, at("id", "{{wiring}}"),
		jsonBody(`{"assignments":[],"fixed_cost":0,"effort_driven":false}`))
	tr.step("Wiring moved to the top of Build's sub-tasks", o, schedule, at("id", "{{wiring}}"), jsonBody(`{"after_id":""}`))
	tr.step("after a task that is not its sibling: 400", o, schedule, at("id", "{{wiring}}"), jsonBody(`{"after_id":"{{order}}"}`))
	tr.setup("Wiring after Frame again", o, schedule, at("id", "{{wiring}}"), jsonBody(`{"after_id":"{{frame}}"}`))
	baselines := "POST /api/v1/projects/{id}/plan/baselines"
	tr.step("a baseline of the plan as it stands", o, baselines, at("id", "{{p}}"), jsonBody(`{"name":"As approved"}`)).capture("bl", "/id")
	tr.step("the viewer may not save one: 403", viewer, baselines, at("id", "{{p}}"), jsonBody(`{}`))
	tr.step("Wiring really started, is 40% complete by hand and has cost 120", o, schedule, at("id", "{{wiring}}"),
		jsonBody(`{"actual_start":"2099-03-02","percent_complete":40,"actual_cost":120}`))
	tr.step("140 percent complete: 400", o, schedule, at("id", "{{wiring}}"), jsonBody(`{"percent_complete":140}`))
	tr.step("finished before it started: 400", o, schedule, at("id", "{{wiring}}"), jsonBody(`{"actual_finish":"2099-01-01"}`))
	tr.step("progress is reported as of a status date, against that baseline", o, "PUT /api/v1/projects/{id}/plan/settings", at("id", "{{p}}"),
		jsonBody(`{"status_date":"2099-03-10","active_baseline_id":"{{bl}}"}`))
	tr.step("a status date that is not a date: 400", o, "PUT /api/v1/projects/{id}/plan/settings", at("id", "{{p}}"), jsonBody(`{"status_date":"now"}`))
	remaining := "POST /api/v1/projects/{id}/plan/reschedule-remaining"
	tr.step("the work that has not started moves to the status date", o, remaining, at("id", "{{p}}"))
	tr.step("the viewer may not reschedule: 403", viewer, remaining, at("id", "{{p}}"))
	tr.setup("Wiring's actuals cleared, the status date today again", o, schedule, at("id", "{{wiring}}"),
		jsonBody(`{"actual_start":"","percent_complete":-1,"actual_cost":-1}`))
	tr.setup("no status date or chosen baseline", o, "PUT /api/v1/projects/{id}/plan/settings", at("id", "{{p}}"),
		jsonBody(`{"status_date":"","active_baseline_id":""}`))
	baseline := "DELETE /api/v1/projects/{id}/plan/baselines/{baselineId}"
	tr.step("the viewer may not delete the baseline: 403", viewer, baseline, at("id", "{{p}}", "baselineId", "{{bl}}"))
	tr.step("the baseline deleted", o, baseline, at("id", "{{p}}", "baselineId", "{{bl}}"))
	tr.step("deleted again: 404", o, baseline, at("id", "{{p}}", "baselineId", "{{bl}}"))
	tr.step("dates on Build, which has sub-tasks: 400", o, schedule, at("id", "{{build}}"), jsonBody(`{"start_date":"2099-03-01"}`))

	settings := "PUT /api/v1/projects/{id}/plan/settings"
	tr.step("the plan works Monday to Saturday", o, settings, at("id", "{{p}}"), jsonBody(`{"working_days":[6,1,2,3,4,5]}`))
	tr.step("holidays, and a critical threshold of two days", o, settings, at("id", "{{p}}"),
		jsonBody(`{"holidays":[{"date":"2099-12-25","name":"Christmas"},{"date":"2099-01-01"}],"critical_slack_days":2}`))
	tr.step("an automatic plan from 1 January 2099, a path per chain, to finish by 2100", o, settings, at("id", "{{p}}"),
		jsonBody(`{"scheduling_mode":"auto","project_start":"2099-01-01","project_finish":"2100-01-01","multiple_critical_paths":true}`))
	tr.step("a mode that is neither: 400", o, settings, at("id", "{{p}}"), jsonBody(`{"scheduling_mode":"magic"}`))
	tr.setup("the plan manual again, with no start or finish", o, settings, at("id", "{{p}}"),
		jsonBody(`{"scheduling_mode":"manual","project_start":"","project_finish":"","multiple_critical_paths":false}`))
	tr.step("costs in pounds against a budget", o, settings, at("id", "{{p}}"), jsonBody(`{"currency":"gbp","budget":25000}`))
	tr.step("a currency of four letters: 400", o, settings, at("id", "{{p}}"), jsonBody(`{"currency":"EURO"}`))
	tr.step("a holiday that is not a date: 400", o, settings, at("id", "{{p}}"), jsonBody(`{"holidays":[{"date":"Christmas"}]}`))
	tr.step("a threshold over a year: 400", o, settings, at("id", "{{p}}"), jsonBody(`{"critical_slack_days":999}`))
	tr.setup("the holidays and the threshold cleared", o, settings, at("id", "{{p}}"), jsonBody(`{"holidays":[],"critical_slack_days":0}`))
	tr.step("no working days: 400", o, settings, at("id", "{{p}}"), jsonBody(`{"working_days":[]}`))
	tr.step("a day 8: 400", o, settings, at("id", "{{p}}"), jsonBody(`{"working_days":[1,8]}`))
	tr.step("the viewer may not set them: 403", viewer, settings, at("id", "{{p}}"), jsonBody(`{"working_days":[1]}`))
	tr.step("the viewer may not schedule: 403", viewer, schedule, at("id", "{{frame}}"), jsonBody(`{"start_date":"2099-03-02"}`))

	todo := "POST /api/v1/work-items/{id}/todos"
	tr.step("a to-do on Wiring", o, todo, at("id", "{{wiring}}"), jsonBody(`{"text":"Crimp the connectors"}`)).capture("t1", "/id")
	tr.step("a second to-do on Wiring, with its deadline", o, todo, at("id", "{{wiring}}"),
		jsonBody(`{"text":"Label the loom","due_date":"2099-03-15"}`)).capture("t2", "/id")
	tr.step("a deadline not written YYYY-MM-DD: 400", o, todo, at("id", "{{wiring}}"), jsonBody(`{"text":"x","due_date":"15/03/2099"}`))
	tr.step("no text: 400", o, todo, at("id", "{{wiring}}"), jsonBody(`{"text":" "}`))
	tr.step("the viewer may not add one: 403", viewer, todo, at("id", "{{wiring}}"), jsonBody(`{"text":"x"}`))
	tr.step("Wiring's to-dos, to the viewer: both open", viewer, "GET /api/v1/work-items/{id}/todos", at("id", "{{wiring}}"))
	tr.step("Frame has none: []", o, "GET /api/v1/work-items/{id}/todos", at("id", "{{frame}}"))
	tr.setup("the first to-do ticked off", o, "POST /api/v1/todos/{id}/done", at("id", "{{t1}}"), jsonBody(`{"done":true}`))

	milestone := "POST /api/v1/projects/{id}/plan/milestones"
	tr.step("a milestone: every user need addressed by 1 February 2020", o, milestone, at("id", "{{p}}"),
		jsonBody(`{"name":"Definition complete","target_date":"2020-02-01","criteria":[{"metric":"needs_addressed","target":100},{"metric":"tasks_done","target":100}]}`)).
		capture("m1", "/id")
	tr.step("a milestone with no date and no criteria", o, milestone, at("id", "{{p}}"), jsonBody(`{"name":"Kick-off"}`)).capture("m2", "/id")
	tr.step("Q's milestone", o, milestone, at("id", "{{q}}"), jsonBody(`{"name":"Elsewhere"}`)).capture("q_m", "/id")
	tr.step("no name: 400", o, milestone, at("id", "{{p}}"), jsonBody(`{"name":" "}`))
	tr.step("an unknown metric: 400", o, milestone, at("id", "{{p}}"), jsonBody(`{"name":"M","criteria":[{"metric":"velocity","target":100}]}`))
	tr.step("a target of 0: 400", o, milestone, at("id", "{{p}}"), jsonBody(`{"name":"M","criteria":[{"metric":"tests_passing","target":0}]}`))
	tr.step("the viewer may not add one: 403", viewer, milestone, at("id", "{{p}}"), jsonBody(`{"name":"M"}`))

	update := "PUT /api/v1/projects/{id}/plan/milestones/{milestoneId}"
	tr.step("Kick-off gets a date; its name stays", o, update, at("id", "{{p}}", "milestoneId", "{{m2}}"), jsonBody(`{"target_date":"2099-01-05"}`))
	tr.step("Q's milestone through P: 404", o, update, at("id", "{{p}}", "milestoneId", "{{q_m}}"), jsonBody(`{"name":"x"}`))
	tr.step("Wiring assigned to Definition complete", o, schedule, at("id", "{{wiring}}"), jsonBody(`{"milestone":"{{m1}}"}`))
	tr.step("Order assigned to MVP, a release's milestone", o, schedule, at("id", "{{order}}"), jsonBody(`{"milestone":"release:{{mvp}}"}`))
	tr.step("Q's milestone: 400", o, schedule, at("id", "{{order}}"), jsonBody(`{"milestone":"{{q_m}}"}`))
	tr.step("Q's release: 400", o, schedule, at("id", "{{order}}"), jsonBody(`{"milestone":"release:{{q_mvp}}"}`))
	tr.step("Frame assigned and taken off again", o, schedule, at("id", "{{frame}}"), jsonBody(`{"milestone":"{{m2}}"}`))
	tr.setup("Frame off its milestone", o, schedule, at("id", "{{frame}}"), jsonBody(`{"milestone":""}`))
	tr.step("the plan with MVP on its default criteria, and each milestone counting its tasks", viewer, plan, at("id", "{{p}}"))

	criteria := "PUT /api/v1/projects/{id}/plan/release-milestones/{releaseId}"
	tr.step("MVP's own criteria: requirements approved, 80%", o, criteria, at("id", "{{p}}", "releaseId", "{{mvp}}"),
		jsonBody(`{"criteria":[{"metric":"requirements_approved","target":80}]}`)).capture("mvp_m", "/id")
	tr.step("Q's release through P: 404", o, criteria, at("id", "{{p}}", "releaseId", "{{q_mvp}}"), jsonBody(`{"criteria":[]}`))
	tr.step("a release milestone's name is the release's: 400", o, update, at("id", "{{p}}", "milestoneId", "{{mvp_m}}"),
		jsonBody(`{"name":"Renamed"}`))
	tr.step("the plan: MVP now carries its own criterion", o, plan, at("id", "{{p}}"))

	export := "GET /api/v1/projects/{id}/plan/export"
	tr.step("the plan as CSV", viewer, export, at("id", "{{p}}"), query("format=csv"))
	tr.step("the plan as JSON", o, export, at("id", "{{p}}"), query("format=json"))
	tr.step("the plan as Excel", o, export, at("id", "{{p}}"), query("format=excel"))
	tr.step("the plan as PDF, the default", o, export, at("id", "{{p}}"))
	tr.step("the plan as Microsoft Project XML", o, export, at("id", "{{p}}"), query("format=msproject"))
	tr.step("ReqIF is not a plan format: 400", o, export, at("id", "{{p}}"), query("format=reqif"))

	del := "DELETE /api/v1/projects/{id}/plan/milestones/{milestoneId}"
	tr.step("the viewer may not delete one: 403", viewer, del, at("id", "{{p}}", "milestoneId", "{{m2}}"))
	tr.step("Kick-off deleted: 204", o, del, at("id", "{{p}}", "milestoneId", "{{m2}}"))
	tr.step("Kick-off after the delete: 404", o, update, at("id", "{{p}}", "milestoneId", "{{m2}}"), jsonBody(`{"name":"x"}`))
	tr.step("MVP's stored milestone deleted: its criteria are the defaults again", o, del, at("id", "{{p}}", "milestoneId", "{{mvp_m}}"))
	tr.step("the plan after the deletes", o, plan, at("id", "{{p}}"))

	ext := "PUT /api/v1/work-items/{id}/external-links/{otherId}"
	tr.step("Order waits on Q's task, of another project", o, ext, at("id", "{{order}}", "otherId", "{{q_task}}"), jsonBody(`{"type":"FS","lag_days":2}`))
	tr.step("two tasks of one project: 400", o, ext, at("id", "{{order}}", "otherId", "{{frame}}"), jsonBody(`{}`))
	tr.step("the viewer may not link them: 403", viewer, ext, at("id", "{{order}}", "otherId", "{{q_task}}"), jsonBody(`{}`))
	tr.step("the master plan: P and Q, and the link from Q to P", o, "GET /api/v1/plan/master")
	unext := "DELETE /api/v1/work-items/{id}/external-links/{otherId}"
	tr.step("the link removed", o, unext, at("id", "{{order}}", "otherId", "{{q_task}}"))
	tr.step("removed again: 404", o, unext, at("id", "{{order}}", "otherId", "{{q_task}}"))

	imp := "POST /api/v1/projects/{id}/plan/import"
	msp := `<Project xmlns="http://schemas.microsoft.com/project"><Tasks>` +
		`<Task><UID>1</UID><Name>Commission</Name><OutlineLevel>1</OutlineLevel><Start>2099-07-01T08:00:00</Start><Finish>2099-07-03T17:00:00</Finish></Task>` +
		`<Task><UID>2</UID><Name>Hand over</Name><OutlineLevel>1</OutlineLevel><Start>2099-07-06T08:00:00</Start><Finish>2099-07-07T17:00:00</Finish>` +
		`<PredecessorLink><PredecessorUID>1</PredecessorUID><Type>1</Type><LinkLag>0</LinkLag><LagFormat>7</LagFormat></PredecessorLink></Task>` +
		`<Task><UID>3</UID><Name>Accepted</Name><OutlineLevel>1</OutlineLevel><Milestone>1</Milestone><Finish>2099-07-08T17:00:00</Finish></Task>` +
		`</Tasks></Project>`
	tr.step("a Microsoft Project file imported: two tasks, a milestone and a relationship", o, imp, at("id", "{{p}}"), rawBody("application/xml", []byte(msp)))
	tr.step("a file that is not one: 400", o, imp, at("id", "{{p}}"), rawBody("application/xml", []byte("plan")))
	tr.step("the viewer may not import: 403", viewer, imp, at("id", "{{p}}"), rawBody("application/xml", []byte(msp)))
}
