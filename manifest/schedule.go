package manifest

import (
	"fmt"
	"strconv"
	"strings"
)

// Five-field cron, minute granularity. No seconds and no @weekly aliases: what crontab(5)
// accepts, minus the parts agents get wrong. nook-init runs the entries off these bitmasks.

// A Schedule is one bitmask per field. Bit n of Min means "fires at minute n".
type Schedule struct {
	Min, Hour, Dom, Mon, Dow uint64
	Spec                     string
}

type cronField struct {
	name     string
	min, max int
}

var cronFields = []cronField{{"minute", 0, 59}, {"hour", 0, 23}, {"day of month", 1, 31}, {"month", 1, 12}, {"day of week", 0, 6}}

func ParseSchedule(spec string) (*Schedule, error) {
	parts := strings.Fields(spec)
	if len(parts) != 5 {
		return nil, fmt.Errorf("schedule %q needs 5 fields (minute hour day-of-month month day-of-week), got %d", spec, len(parts))
	}
	s := &Schedule{Spec: strings.Join(parts, " ")}
	into := []*uint64{&s.Min, &s.Hour, &s.Dom, &s.Mon, &s.Dow}
	for i, p := range parts {
		bits, err := parseCronField(p, cronFields[i])
		if err != nil {
			return nil, fmt.Errorf("schedule %q: %w", spec, err)
		}
		*into[i] = bits
	}
	return s, nil
}

// parseCronField reads a comma list of *, a, a-b, and any of those with a /step.
func parseCronField(expr string, f cronField) (uint64, error) {
	var bits uint64
	for _, term := range strings.Split(expr, ",") {
		body, stepStr, hasStep := strings.Cut(term, "/")
		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepStr)
			if err != nil || n < 1 {
				return 0, fmt.Errorf("%s: step in %q must be a positive number", f.name, term)
			}
			step = n
		}
		lo, hi := f.min, f.max
		if body != "*" {
			a, b, isRange := strings.Cut(body, "-")
			n, err := cronValue(a, f)
			if err != nil {
				return 0, err
			}
			lo, hi = n, n
			switch {
			case isRange:
				if hi, err = cronValue(b, f); err != nil {
					return 0, err
				}
			case hasStep:
				hi = f.max // "5/15" means 5, 20, 35, 50, the way cron reads it
			}
			if hi < lo {
				return 0, fmt.Errorf("%s: range %q runs backwards", f.name, body)
			}
		}
		for v := lo; v <= hi; v += step {
			bits |= 1 << uint(v)
		}
	}
	if bits == 0 {
		return 0, fmt.Errorf("%s: %q never fires", f.name, expr)
	}
	return bits, nil
}

func cronValue(s string, f cronField) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a number", f.name, s)
	}
	if f.name == "day of week" && n == 7 {
		n = 0 // both 0 and 7 mean Sunday
	}
	if n < f.min || n > f.max {
		return 0, fmt.Errorf("%s: %d is outside %d-%d", f.name, n, f.min, f.max)
	}
	return n, nil
}
