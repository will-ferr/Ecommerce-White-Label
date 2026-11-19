package domain

import (
	"time"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// MaxReportDays bounds a report period.
const MaxReportDays = 366

const dateLayout = "2006-01-02"

// Period is a range of whole days in the timezone of a store.
type Period struct {
	From  string    // first day, inclusive
	To    string    // last day, inclusive
	Start time.Time // start of the first day
	End   time.Time // start of the day after the last one
}

// Days lists every day of the period.
func (p Period) Days() []string {
	var days []string
	for day := p.Start; day.Before(p.End); day = day.AddDate(0, 0, 1) {
		days = append(days, day.Format(dateLayout))
	}
	return days
}

// ParsePeriod reads a period of days written as YYYY-MM-DD in a timezone.
// Empty values mean the last 30 days, ending today.
func ParsePeriod(from, to string, location *time.Location, now time.Time) (Period, error) {
	invalid := func(field, message string) error {
		return apperr.Validation(apperr.FieldError{Field: field, Code: "invalid", Message: message})
	}
	today := now.In(location)
	end := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, location)
	if to != "" {
		parsed, err := time.ParseInLocation(dateLayout, to, location)
		if err != nil {
			return Period{}, invalid("to", "Use a date such as 2026-11-30.")
		}
		end = parsed
	}
	start := end.AddDate(0, 0, -29)
	if from != "" {
		parsed, err := time.ParseInLocation(dateLayout, from, location)
		if err != nil {
			return Period{}, invalid("from", "Use a date such as 2026-11-01.")
		}
		start = parsed
	}
	if start.After(end) {
		return Period{}, invalid("from", "The start must not be after the end.")
	}
	exclusive := end.AddDate(0, 0, 1)
	if exclusive.Sub(start) > MaxReportDays*24*time.Hour+time.Hour {
		return Period{}, invalid("from", "Choose a period of up to 366 days.")
	}
	return Period{From: start.Format(dateLayout), To: end.Format(dateLayout), Start: start, End: exclusive}, nil
}
