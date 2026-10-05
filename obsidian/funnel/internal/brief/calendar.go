package brief

import (
	"errors"
	"fmt"
	"time"
)

func SelectWeeks(name string, now time.Time) ([]Week, error) {
	if name != "" {
		w, err := ParseWeek(name)
		return []Week{w}, err
	}
	local := now.In(Madrid)
	result := make([]Week, 0, 3)
	for n := 0; n < 3; n++ {
		y, w := local.AddDate(0, 0, -7*n).ISOWeek()
		week, err := ParseWeek(fmt.Sprintf("%04d-W%02d", y, w))
		if err != nil {
			return nil, err
		}
		result = append(result, week)
	}
	return result, nil
}
func (w Week) Envelope(now time.Time) (time.Time, time.Time, error) {
	start, end := w.Start.AddDate(0, 0, -1), w.Start.AddDate(0, 0, 8)
	if end.After(now) {
		end = now.UTC()
	}
	if w.Start.Format("2006-01-02") > now.In(Madrid).Format("2006-01-02") || !start.Before(end) {
		return start, end, errors.New("unelapsed week")
	}
	return start, end, nil
}
func (w Week) Days() []string {
	result := make([]string, 7)
	for n := range result {
		result[n] = w.Start.AddDate(0, 0, n).Format("2006-01-02")
	}
	return result
}
