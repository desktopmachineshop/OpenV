package plan

import (
	"sort"
	"strings"
	"time"
)

// Calendar says which days a plan works: the days of the week it works,
// less its holidays. A task's duration and float count only those days, so a
// plan that works Monday to Friday does not count its weekends. Dates
// themselves stay dates: a task may start or end on a day off, which then
// simply counts for nothing.
type Calendar struct {
	work [7]bool // by time.Weekday
	// off holds the holidays, as YYYY-MM-DD.
	off map[string]bool
}

// Holiday is a day the plan does not work whatever its weekday.
type Holiday struct {
	Date string `json:"date"`
	Name string `json:"name"`
}

// MaxHolidays bounds a plan's holidays.
const MaxHolidays = 366

// CleanHolidays checks a plan's holidays and returns them in date order,
// each day once (the first name given wins).
func CleanHolidays(in []Holiday) ([]Holiday, error) {
	seen := map[string]bool{}
	out := []Holiday{}
	for _, h := range in {
		d, err := time.Parse(DateLayout, strings.TrimSpace(h.Date))
		if err != nil {
			return nil, ErrHoliday
		}
		day := d.Format(DateLayout)
		if seen[day] {
			continue
		}
		seen[day] = true
		name := strings.TrimSpace(h.Name)
		if len(name) > 80 {
			return nil, ErrHoliday
		}
		out = append(out, Holiday{Date: day, Name: name})
	}
	if len(out) > MaxHolidays {
		return nil, ErrHoliday
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

// WithHolidays is the calendar with those days off as well.
func (c Calendar) WithHolidays(holidays []Holiday) Calendar {
	if len(holidays) == 0 {
		return c
	}
	c.off = make(map[string]bool, len(holidays))
	for _, h := range holidays {
		c.off[h.Date] = true
	}
	return c
}

// Without is the calendar with those days (YYYY-MM-DD) off as well.
func (c Calendar) Without(days map[string]bool) Calendar {
	if len(days) == 0 {
		return c
	}
	off := make(map[string]bool, len(c.off)+len(days))
	for d := range c.off {
		off[d] = true
	}
	for d := range days {
		off[d] = true
	}
	c.off = off
	return c
}

// elapsedCalendar works every day: a task whose duration is elapsed time
// counts weekends and holidays too.
var elapsedCalendar = Calendar{work: [7]bool{true, true, true, true, true, true, true}}

func (c Calendar) works(d time.Time) bool {
	return c.work[d.Weekday()] && !c.off[d.Format(DateLayout)]
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
func (c Calendar) Works(day string) bool { return c.works(parseDay(day)) }

// Between counts the working days after a, up to and including b; negative
// when b is before a. It is how far something at a can slip to reach b.
func (c Calendar) Between(a, b string) int {
	from, to, sign := parseDay(a), parseDay(b), 1
	if to.Before(from) {
		from, to, sign = to, from, -1
	}
	n := 0
	if len(c.off) == 0 {
		// Whole weeks at once: no holiday to look for.
		days := int(to.Sub(from).Hours() / 24)
		n = days / 7 * c.perWeek()
		from = from.AddDate(0, 0, days/7*7)
	}
	for d := from.AddDate(0, 0, 1); !d.After(to); d = d.AddDate(0, 0, 1) {
		if c.works(d) {
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

// calendarHorizon bounds a walk along the calendar, in days: a calendar
// whose holidays leave it no working day for that long is not walked
// further.
const calendarHorizon = 4000

// step walks from d by whole days (by is 1 or -1) to the first working day.
func (c Calendar) step(d time.Time, by int) time.Time {
	for i := 0; i < calendarHorizon && !c.works(d); i++ {
		d = d.AddDate(0, 0, by)
	}
	return d
}

// OnOrAfter is the first working day on or after day; OnOrBefore, the last
// on or before it.
func (c Calendar) OnOrAfter(day string) string {
	return c.step(parseDay(day), 1).Format(DateLayout)
}

func (c Calendar) OnOrBefore(day string) string {
	return c.step(parseDay(day), -1).Format(DateLayout)
}

// End is the due date of a task that starts on start and runs for duration
// working days: the last of them. A start on a day off begins the count at
// the next working day.
func (c Calendar) End(start string, duration int) string {
	return c.Add(c.OnOrAfter(start), duration-1)
}

// Start is the start date of a task that is due on due and runs for
// duration working days: the first of them. A due date on a day off counts
// back from the working day before it.
func (c Calendar) Start(due string, duration int) string {
	return c.Add(c.OnOrBefore(due), 1-duration)
}

// Add is the day n working days after day, or before it when n is negative;
// day itself when n is 0 or less than a day is asked. A calendar that works
// no day answers day.
func (c Calendar) Add(day string, n int) string {
	if c.perWeek() == 0 || n == 0 {
		return day
	}
	d, by := parseDay(day), 1
	if n < 0 {
		by, n = -1, -n
	}
	for i := 0; n > 0 && i < calendarHorizon*8; i++ {
		d = d.AddDate(0, 0, by)
		if c.works(d) {
			n--
		}
	}
	return d.Format(DateLayout)
}
