// The MCP tools over the project plan, the Gantt over the board
// (internal/domain/plan). Tools, in tools.go, concatenates each area's
// constructor in the table's order.

package mcp

import (
	"fmt"
	"strconv"
	"strings"
)

// planTools returns the tools that read a project's plan and place a task
// on it, in Tools() order.
func planTools() []Tool {
	return []Tool{
		{
			Name:     "get_plan",
			ReadOnly: true,
			Description: "Read a project's plan: every board task in outline order with its start and due dates, parent task, progress " +
				"(from its to-dos and sub-tasks), its duration and float in working days, its effort in days, whether it is on the critical path (the chain of tasks that decides the plan's end, which may still have float) and the day its end can slip to before it pushes the next task, its relationships to the tasks that drive it (FS, SS or FF) and whether it is late; the milestones in date order, each target " +
				"release among them, with their completion criteria measured against the project's artifacts; and every metric a " +
				"criterion can name with its current value.",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id": str("Project ID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("GET", "/api/v1/projects/"+strArg(args, "project_id")+"/plan", nil, nil)
				return out, err
			},
		},
		{
			Name: "schedule_work_item",
			Description: "Place a board task on the plan: its start date and due date (YYYY-MM-DD), its duration in working days and effort in days " +
				"(a task with sub-tasks takes all four from them and refuses them), " +
				"its parent task (making it a sub-task) and its relationships to the tasks that drive it. A relationship is written \"<work item id>:<type>\", " +
				"where the type is FS (finish to start: this task starts after that one finishes), SS (start to start: it starts no " +
				"earlier than that one starts) or FF (finish to finish: it finishes no earlier than that one finishes); with no type it " +
				"is FS. A task may have several, so two tasks can together drive when it starts. Only the arguments you pass change; " +
				"pass an empty string to clear a date or the parent, and an empty relationships list to clear them. Answers the " +
				"task's row of the plan.",
			InputSchema: schema([]string{"id"}, map[string]interface{}{
				"id":               str("Work item ID"),
				"start_date":       str("Start date, YYYY-MM-DD; an empty string clears it"),
				"due_date":         str("Due date, YYYY-MM-DD; an empty string clears it"),
				"duration_days":    str("How long the task runs, in whole working days counting its first and last; the task keeps it, so a start date sent alone moves the due date and a due date sent alone moves the start. \"0\" clears it"),
				"effort_days":      str("The work in the task, in person-days (may be a decimal); \"0\" clears it"),
				"parent_id":        str("Work item ID of the parent task; an empty string makes it a top-level task"),
				"deadline":         str("The day the task should be finished by, YYYY-MM-DD; it moves nothing, and the plan flags a task that misses it. An empty string clears it"),
				"elapsed":          str("\"true\" makes the duration count calendar days, weekends and holidays included (a cure, a delivery); \"false\" counts working days"),
				"mode":             str("The task's own scheduling mode: \"manual\" (it stays where it is put), \"auto\" (the platform places it from its relationships and constraint), or an empty string to take the plan's"),
				"constraint_type":  str("A date constraint: \"asap\" (none), \"alap\" (as late as possible), \"snet\"/\"snlt\" (start no earlier/later than), \"fnet\"/\"fnlt\" (finish no earlier/later than), \"mso\"/\"mfo\" (must start/finish on). Every one but asap and alap needs constraint_date"),
				"constraint_date":  str("The constraint's date, YYYY-MM-DD"),
				"assignments":      strList("Who the task is assigned to on the plan, each \"user:<id>\" or \"agent:<id>\", with an optional share of their time after it as in \"user:<id>:50\" (percent; 100 when left out); replaces the task's assignments, and an empty list leaves it to the board's assignee"),
				"fixed_cost":       str("What the task costs beyond the time of the people on it; \"0\" clears it"),
				"effort_driven":    str("\"true\" lets the task's effort and the people on it decide its duration"),
				"actual_start":     str("The day work really started, YYYY-MM-DD; an empty string clears it"),
				"actual_finish":    str("The day work really finished, YYYY-MM-DD; an empty string clears it"),
				"percent_complete": str("How complete the task is, \"0\" to \"100\", in place of the progress counted from its to-dos and sub-tasks; \"-1\" goes back to the counted progress"),
				"actual_cost":      str("What the task has really cost so far; \"-1\" goes back to its earned share of the planned cost"),
				"after_id":         str("Work item ID of the sibling task this one goes straight after in the outline; an empty string makes it the first of its siblings. Siblings are otherwise listed by start date"),
				"milestone":        str("Key of the milestone the task is assigned to, as get_plan lists it (a milestone's id, or \"release:<release id>\"); an empty string takes it off. The milestone then counts the task among its tasks done"),
				"relationships":    strList("The tasks that drive this one, each \"<work item id>:FS\", \":SS\", \":FF\" or \":SF\", with an optional offset after it as in \"<id>:SS:5\" (working days; negative leads), \"<id>:FS:3ed\" (elapsed days) or \"<id>:SS:50%\" (of the driving task's duration); replaces the task's relationships"),
				"depends_on":       strList("Earlier form of relationships: work item IDs, each finish to start unless it already has a type"),
			}),
			Handler: scheduleWorkItem,
		},
		{
			Name: "recalculate_plan",
			Description: "Recalculate a project's plan: move each task that another task drives, or that a constraint holds to a " +
				"date, to the dates those ask for. A task keeps its duration; a task nothing places, and a task that is done, stay " +
				"where they are. Answers how many tasks moved. A manual plan's tasks move only on this call; an automatic " +
				"plan's are placed after every change.",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id": str("Project ID"),
				"only_later": str("\"true\" treats relationships as minimums: a task already on or after the day asked is left where it is"),
			}),
			Handler: recalculatePlan,
		},
		planCalendarTool(),
		planResourceTool(),
		planLevelTool(),
		{
			Name: "set_plan_working_days",
			Description: "Set which days of the week a project's plan works, as ISO weekday numbers from \"1\" (Monday) to \"7\" " +
				"(Sunday). A task's duration and float count only working days, so a plan that works Monday to Friday (the " +
				"default) leaves weekends out. Answers the working days as stored.",
			InputSchema: schema([]string{"project_id", "working_days"}, map[string]interface{}{
				"project_id":   str("Project ID"),
				"working_days": strList("ISO weekday numbers, \"1\" (Monday) to \"7\" (Sunday); at least one"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				days := []int{}
				for _, d := range strListArg(args, "working_days") {
					n, err := strconv.Atoi(strings.TrimSpace(d))
					if err != nil {
						return "", fmt.Errorf("working_days are weekday numbers from 1 (Monday) to 7 (Sunday)")
					}
					days = append(days, n)
				}
				out, _, err := c.request("PUT", "/api/v1/projects/"+strArg(args, "project_id")+"/plan/settings", nil,
					map[string]interface{}{"working_days": days})
				return out, err
			},
		},
		planBaselineTool(),
		planRescheduleTool(),
	}
}

// scheduleWorkItem is schedule_work_item's call: the arguments that are set,
// as the schedule route reads them.
func scheduleWorkItem(c *Client, args map[string]interface{}) (string, error) {
	body := map[string]interface{}{}
	for _, key := range []string{"start_date", "due_date", "parent_id", "milestone", "deadline", "mode", "constraint_type", "constraint_date", "actual_start", "actual_finish", "after_id"} {
		if _, set := args[key]; set {
			body[key] = strArg(args, key)
		}
	}
	if _, set := args["assignments"]; set {
		list := []map[string]interface{}{}
		for _, a := range strListArg(args, "assignments") {
			parts := strings.SplitN(a, ":", 3)
			if len(parts) < 2 {
				return "", fmt.Errorf("an assignment is user:<id> or agent:<id>, optionally followed by :<percent>")
			}
			one := map[string]interface{}{"type": strings.TrimSpace(parts[0]), "id": strings.TrimSpace(parts[1])}
			if len(parts) == 3 {
				n, err := strconv.Atoi(strings.TrimSpace(parts[2]))
				if err != nil {
					return "", fmt.Errorf("an assignment's share is a whole percentage")
				}
				one["units"] = n
			}
			list = append(list, one)
		}
		body["assignments"] = list
	}
	if _, set := args["effort_driven"]; set {
		body["effort_driven"] = strings.EqualFold(strings.TrimSpace(strArg(args, "effort_driven")), "true")
	}
	if _, set := args["elapsed"]; set {
		body["elapsed"] = strings.EqualFold(strings.TrimSpace(strArg(args, "elapsed")), "true")
	}
	for _, key := range []string{"duration_days", "effort_days", "fixed_cost", "actual_cost", "percent_complete"} {
		if _, set := args[key]; !set {
			continue
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(strArg(args, key)), 64)
		if err != nil {
			return "", fmt.Errorf("%s must be a number", key)
		}
		body[key] = n
	}
	if _, set := args["relationships"]; set {
		rels := []map[string]interface{}{}
		for _, r := range strListArg(args, "relationships") {
			id, rest, _ := strings.Cut(r, ":")
			kind, lag, _ := strings.Cut(rest, ":")
			rel := map[string]interface{}{"task_id": strings.TrimSpace(id), "type": strings.TrimSpace(kind)}
			if lag = strings.TrimSpace(lag); lag != "" {
				unit := "d"
				for _, u := range []string{"ed", "%", "d"} {
					if strings.HasSuffix(lag, u) {
						unit, lag = u, strings.TrimSuffix(lag, u)
						break
					}
				}
				n, err := strconv.Atoi(lag)
				if err != nil {
					return "", fmt.Errorf("a relationship's offset is a whole number, optionally followed by ed (elapsed days) or %% (of the driving task's duration)")
				}
				rel["lag_days"], rel["lag_unit"] = n, unit
			}
			rels = append(rels, rel)
		}
		body["relationships"] = rels
	}
	if _, set := args["depends_on"]; set {
		deps := strListArg(args, "depends_on")
		if deps == nil {
			deps = []string{}
		}
		body["depends_on"] = deps
	}
	out, _, err := c.request("PUT", "/api/v1/work-items/"+strArg(args, "id")+"/schedule", nil, body)
	return out, err
}

// setPlanCalendar is set_plan_calendar's call: the settings that are set.
func setPlanCalendar(c *Client, args map[string]interface{}) (string, error) {
	body := map[string]interface{}{}
	if _, set := args["holidays"]; set {
		holidays := []map[string]string{}
		for _, h := range strListArg(args, "holidays") {
			date, name, _ := strings.Cut(h, ":")
			holidays = append(holidays, map[string]string{"date": strings.TrimSpace(date), "name": strings.TrimSpace(name)})
		}
		body["holidays"] = holidays
	}
	if _, set := args["budget"]; set {
		n, err := strconv.ParseFloat(strings.TrimSpace(strArg(args, "budget")), 64)
		if err != nil {
			return "", fmt.Errorf("budget must be a number")
		}
		body["budget"] = n
	}
	for _, key := range []string{"scheduling_mode", "project_start", "project_finish", "schedule_from", "currency", "status_date", "active_baseline_id"} {
		if _, set := args[key]; set {
			body[key] = strArg(args, key)
		}
	}
	if _, set := args["multiple_critical_paths"]; set {
		body["multiple_critical_paths"] = strings.EqualFold(strings.TrimSpace(strArg(args, "multiple_critical_paths")), "true")
	}
	if _, set := args["critical_slack_days"]; set {
		n, err := strconv.Atoi(strings.TrimSpace(strArg(args, "critical_slack_days")))
		if err != nil {
			return "", fmt.Errorf("critical_slack_days is a whole number of working days")
		}
		body["critical_slack_days"] = n
	}
	out, _, err := c.request("PUT", "/api/v1/projects/"+strArg(args, "project_id")+"/plan/settings", nil, body)
	return out, err
}

// recalculatePlan is recalculate_plan's call.
func recalculatePlan(c *Client, args map[string]interface{}) (string, error) {
	body := map[string]interface{}{"only_later": strings.EqualFold(strings.TrimSpace(strArg(args, "only_later")), "true")}
	out, _, err := c.request("POST", "/api/v1/projects/"+strArg(args, "project_id")+"/plan/recalculate", nil, body)
	return out, err
}

// planCalendarTool is set_plan_calendar: the plan's holidays, critical
// threshold and scheduling settings.
func planCalendarTool() Tool {
	return Tool{
		Name: "set_plan_calendar",
		Description: "Set a project's plan holidays, its critical threshold and how it is scheduled. holidays replaces the " +
			"plan's days off (days it does not work whatever the weekday), each \"YYYY-MM-DD\" or \"YYYY-MM-DD:Name\"; " +
			"critical_slack_days is the float, in working days, at or below which a task counts as critical (\"0\" by default); " +
			"scheduling_mode \"auto\" has the platform place every task from its relationships and constraints after each " +
			"change, \"manual\" (the default) leaves tasks where they are put. An argument left out stays as it is. " +
			"Answers every setting as stored.",
		InputSchema: schema([]string{"project_id"}, map[string]interface{}{
			"project_id":              str("Project ID"),
			"holidays":                strList("The plan's holidays, each \"YYYY-MM-DD\" or \"YYYY-MM-DD:Name\"; an empty list clears them"),
			"critical_slack_days":     str("Float in working days at or below which a task is critical, \"0\" to \"365\""),
			"scheduling_mode":         str("\"manual\" or \"auto\": the mode of every task that has none of its own"),
			"project_start":           str("The day the plan starts, YYYY-MM-DD: an automatic task starts no earlier. An empty string clears it"),
			"project_finish":          str("The day the plan must finish by, YYYY-MM-DD: float is counted to it. An empty string clears it"),
			"schedule_from":           str("\"start\" (tasks as soon as possible) or \"finish\" (automatic tasks as late as possible)"),
			"currency":                str("The money costs are in, as up to three letters, e.g. \"GBP\""),
			"budget":                  str("What the plan may cost; \"0\" clears it"),
			"status_date":             str("The day progress is reported as of, YYYY-MM-DD: earned value and behind-schedule marks are measured at it. An empty string means today"),
			"active_baseline_id":      str("ID of the baseline variance and earned value are measured against, as get_plan lists under baselines; an empty string means the newest"),
			"multiple_critical_paths": str("\"true\" gives each chain of tasks a critical path of its own, to its own last task"),
		}),
		Handler: setPlanCalendar,
	}
}

// planResourceTool is set_plan_resource: a person's or an agent's daily
// rate and capacity on a project.
func planResourceTool() Tool {
	return Tool{
		Name: "set_plan_resource",
		Description: "Set what a day of a person's or an agent's time costs on a project, and the share of a working day they " +
			"can give it. The plan costs each task from its people's rates, and reports anyone asked for more than their " +
			"capacity. get_plan lists who can be assigned under people.",
		InputSchema: schema([]string{"project_id", "type", "id", "daily_rate"}, map[string]interface{}{
			"project_id": str("Project ID"),
			"type":       str("\"user\" or \"agent\""),
			"id":         str("The user's or the agent's ID"),
			"daily_rate": str("What a day of their time costs, in the plan's currency"),
			"capacity":   str("The percentage of a working day they can give the project; \"100\" when left out"),
		}),
		Handler: func(c *Client, args map[string]interface{}) (string, error) {
			body := map[string]interface{}{"type": strArg(args, "type")}
			if _, set := args["daily_rate"]; set {
				rate, err := strconv.ParseFloat(strings.TrimSpace(strArg(args, "daily_rate")), 64)
				if err != nil {
					return "", fmt.Errorf("daily_rate must be a number")
				}
				body["daily_rate"] = rate
			}
			if _, set := args["capacity"]; set {
				n, err := strconv.Atoi(strings.TrimSpace(strArg(args, "capacity")))
				if err != nil {
					return "", fmt.Errorf("capacity is a whole percentage")
				}
				body["capacity"] = n
			}
			out, _, err := c.request("PUT", "/api/v1/projects/"+strArg(args, "project_id")+"/plan/resources/"+strArg(args, "id"), nil, body)
			return out, err
		},
	}
}

// planBaselineTool is save_plan_baseline: the plan as it stands, kept to
// measure against.
func planBaselineTool() Tool {
	return Tool{
		Name: "save_plan_baseline",
		Description: "Save a baseline of a project's plan: every task's dates, duration, effort and cost as they stand, kept so " +
			"get_plan can report how far each task has moved since (its variance) and the plan's earned value. A plan keeps " +
			"at most 11. Answers the baseline's id and name.",
		InputSchema: schema([]string{"project_id"}, map[string]interface{}{
			"project_id": str("Project ID"),
			"name":       str("What to call it; \"Baseline <today>\" when left out"),
		}),
		Handler: func(c *Client, args map[string]interface{}) (string, error) {
			out, _, err := c.request("POST", "/api/v1/projects/"+strArg(args, "project_id")+"/plan/baselines", nil,
				map[string]interface{}{"name": strArg(args, "name")})
			return out, err
		},
	}
}

// planRescheduleTool is reschedule_remaining_work: unstarted work that is
// behind the status date moves to it.
func planRescheduleTool() Tool {
	return Tool{
		Name: "reschedule_remaining_work",
		Description: "Move the work that should have started by the plan's status date and has not to that date, and the tasks it " +
			"drives after it. A task with progress or an actual start, and a task that is done, stay where they are. Answers how many tasks moved.",
		InputSchema: schema([]string{"project_id"}, map[string]interface{}{
			"project_id": str("Project ID"),
		}),
		Handler: func(c *Client, args map[string]interface{}) (string, error) {
			out, _, err := c.request("POST", "/api/v1/projects/"+strArg(args, "project_id")+"/plan/reschedule-remaining", nil, nil)
			return out, err
		},
	}
}

// planLevelTool is level_plan: delays tasks until nobody is over-allocated.
func planLevelTool() Tool {
	return Tool{
		Name: "level_plan",
		Description: "Level a project's plan: delay tasks until no person or agent is asked for more than their capacity on any day. " +
			"Where someone is over-allocated the task with the most float is delayed until after the first of the others ends, and the tasks it drives follow. Answers how many tasks moved.",
		InputSchema: schema([]string{"project_id"}, map[string]interface{}{
			"project_id": str("Project ID"),
		}),
		Handler: func(c *Client, args map[string]interface{}) (string, error) {
			out, _, err := c.request("POST", "/api/v1/projects/"+strArg(args, "project_id")+"/plan/level", nil, nil)
			return out, err
		},
	}
}
