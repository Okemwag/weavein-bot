package memberos

import (
	"errors"
	"net/http"
	"time"
)

type reportPeriod struct{ From, To time.Time }

func parseReportPeriod(r *http.Request) (reportPeriod, error) {
	now := time.Now().UTC()
	period := reportPeriod{From: now.AddDate(0, 0, -30), To: now}
	for _, field := range []struct {
		value  string
		target *time.Time
	}{
		{r.URL.Query().Get("from"), &period.From}, {r.URL.Query().Get("to"), &period.To},
	} {
		if field.value == "" {
			continue
		}
		value, err := time.Parse("2006-01-02", field.value)
		if err != nil {
			return reportPeriod{}, errors.New("dates must use YYYY-MM-DD")
		}
		*field.target = value.UTC()
	}
	if !period.From.Before(period.To) || period.To.After(now.Add(24*time.Hour)) || period.To.Sub(period.From) > 366*24*time.Hour {
		return reportPeriod{}, errors.New("date range must be 1 to 366 days and cannot be in the future")
	}
	return period, nil
}
