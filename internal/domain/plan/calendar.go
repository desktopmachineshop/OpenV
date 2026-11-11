package plan

import (
	"sort"
	"time"
)

// Calendar says which days of the week a plan works. A task's duration and
// float count only those days, so a plan that works Monday to Friday does
// not count its weekends. Dates themselves stay dates: a task may start or
// end on a day off, which then simply counts for nothing.
type Calendar struct {
	work [7]bool // by time.Weekday
}

// DefaultWorkingDays are a plan's working days until it sets its own:
// Monday to Friday, as ISO weekday numbers (Monday is 1, Sunday 7).
var DefaultWorkingDays = []int{1, 2, 3, 4, 5}

// CleanWorkingDays checks a list of ISO weekday numbers and returns it
// sorted, each day once. A plan must work at least one day.
func CleanWorkingDays(days []int) ([]int, error) {
	seen := map[int]bool{}
	out := []int{}
	for _, d := range days {
		if d < 1 || d > 7 {
			return nil, ErrWorkingDays
		}
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return nil, ErrWorkingDays
	}
	sort.Ints(out)
	return out, nil
}

// NewCalendar builds the calendar of the given ISO weekdays; nil or an
// unusable list gives the default.
func NewCalendar(days []int) Calendar {
	clean, err := CleanWorkingDays(days)
	if err != nil {
		clean = DefaultWorkingDays
	}
	var c Calendar
	for _, d := range clean {
		c.work[d%7] = true // ISO 7 (Sunday) is time.Sunday, 0
	}
	return c
}

func parseDay(d string) time.Time {
	t, _ := time.Parse(DateLayout, d)
	return t
}

// Works reports whether the day is a working day.
func (c Calendar) Works(day string) bool { return c.work[parseDay(day).Weekday()] }

// Between counts the working days after a, up to and including b; negative
// when b is before a. It is how far something at a can slip to reach b.
func (c Calendar) Between(a, b string) int {
	from, to, sign := parseDay(a), parseDay(b), 1
	if to.Before(from) {
		from, to, sign = to, from, -1
	}
	days := int(to.Sub(from).Hours() / 24)
	n := days / 7 * c.perWeek()
	for d := from.AddDate(0, 0, days/7*7+1); !d.After(to); d = d.AddDate(0, 0, 1) {
		if c.work[d.Weekday()] {
			n++
		}
	}
	return sign * n
}

func (c Calendar) perWeek() int {
	n := 0
	for _, w := range c.work {
		if w {
			n++
		}
	}
	return n
}

// Span is a task's duration from start to end: the working days it covers,
// counting both ends, and never less than 1 for a task that has dates.
func (c Calendar) Span(start, end string) int {
	n := c.Between(start, end)
	if c.Works(start) {
		n++
	}
	if n < 1 {
		n = 1
	}
	return n
}

// End is the due date of a task that starts on start and runs for duration
// working days: the last of them. A start on a day off begins the count at
// the next working day.
func (c Calendar) End(start string, duration int) string {
	d := parseDay(start)
	for !c.work[d.Weekday()] {
		d = d.AddDate(0, 0, 1)
	}
	for left := duration - 1; left > 0; {
		d = d.AddDate(0, 0, 1)
		if c.work[d.Weekday()] {
			left--
		}
	}
	return d.Format(DateLayout)
}

// Start is the start date of a task that is due on due and runs for
// duration working days: the first of them. A due date on a day off counts
// back from the working day before it.
func (c Calendar) Start(due string, duration int) string {
	d := parseDay(due)
	for !c.work[d.Weekday()] {
		d = d.AddDate(0, 0, -1)
	}
	for left := duration - 1; left > 0; {
		d = d.AddDate(0, 0, -1)
		if c.work[d.Weekday()] {
			left--
		}
	}
	return d.Format(DateLayout)
}

// Add is the day n working days after day, or before it when n is negative;
// day itself when n is 0. A calendar that works no day answers day.
func (c Calendar) Add(day string, n int) string {
	if c.perWeek() == 0 || n == 0 {
		return day
	}
	d, step := parseDay(day), 1
	if n < 0 {
		step, n = -1, -n
	}
	for n > 0 {
		d = d.AddDate(0, 0, step)
		if c.work[d.Weekday()] {
			n--
		}
	}
	return d.Format(DateLayout)
}
