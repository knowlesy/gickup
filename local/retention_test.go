package local

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/cooperspencer/gickup/types"
)

func utc(year int, month time.Month, day, hour int) int64 {
	return time.Date(year, month, day, hour, 0, 0, 0, time.UTC).Unix()
}

// newestFirst sorts stamps the way pruneOldBackups passes them to selectTieredBackups.
func newestFirst(stamps []int64) []int64 {
	sort.Slice(stamps, func(i, j int) bool { return stamps[i] > stamps[j] })
	return stamps
}

func keptDates(keep map[int64]bool) []string {
	dates := []string{}
	for ts := range keep {
		dates = append(dates, time.Unix(ts, 0).UTC().Format("2006-01-02 15h"))
	}
	sort.Strings(dates)
	return dates
}

// twoBackupsADay returns a 06:00 and a 18:00 backup for every day from 2025-01-01 to 2026-11-27.
func twoBackupsADay() []int64 {
	stamps := []int64{}
	for d := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC); !d.After(time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)); d = d.AddDate(0, 0, 1) {
		stamps = append(stamps, d.Add(6*time.Hour).Unix(), d.Add(18*time.Hour).Unix())
	}
	return newestFirst(stamps)
}

func TestTieredKeepsDailyWeeklyMonthlyYearly(t *testing.T) {
	t.Parallel()

	keep := selectTieredBackups(twoBackupsADay(), time.UTC,
		types.Local{KeepDaily: 30, KeepWeekly: 8, KeepMonthly: 6, KeepYearly: 1})

	want := []string{}
	// daily: 2026-10-29 to 2026-11-27, also covers the current week, month and year
	for d := time.Date(2026, 10, 29, 18, 0, 0, 0, time.UTC); d.Day() != 28; d = d.AddDate(0, 0, 1) {
		want = append(want, d.Format("2006-01-02 15h"))
	}
	// weekly: the Sundays ending the 7 previous ISO weeks that fall before the daily window
	want = append(want, "2026-10-11 18h", "2026-10-18 18h", "2026-10-25 18h")
	// monthly: last day of the 6 most recent months that fall before the daily window
	want = append(want, "2026-06-30 18h", "2026-07-31 18h", "2026-08-31 18h", "2026-09-30 18h")
	sort.Strings(want)

	if got := keptDates(keep); !reflect.DeepEqual(got, want) {
		t.Fatalf("kept %d backups %v\nwant %d %v", len(got), got, len(want), want)
	}
}

func TestTieredDailyKeepsNewestOfEachDay(t *testing.T) {
	t.Parallel()

	keep := selectTieredBackups(twoBackupsADay(), time.UTC, types.Local{KeepDaily: 3})

	want := []string{"2026-11-25 18h", "2026-11-26 18h", "2026-11-27 18h"}
	if got := keptDates(keep); !reflect.DeepEqual(got, want) {
		t.Fatalf("kept %v, want %v", got, want)
	}
}

func TestTieredMonthlyAndYearly(t *testing.T) {
	t.Parallel()

	keep := selectTieredBackups(twoBackupsADay(), time.UTC, types.Local{KeepMonthly: 3, KeepYearly: 3})

	// only two years of backups exist, so yearly keeps both
	want := []string{"2025-12-31 18h", "2026-09-30 18h", "2026-10-31 18h", "2026-11-27 18h"}
	if got := keptDates(keep); !reflect.DeepEqual(got, want) {
		t.Fatalf("kept %v, want %v", got, want)
	}
}

func TestTieredWeeklyUsesISOYear(t *testing.T) {
	t.Parallel()

	// 2025-12-28 is the Sunday of 2025-W52; 2025-12-29 and 2026-01-01 are both in 2026-W01
	stamps := newestFirst([]int64{utc(2025, 12, 28, 12), utc(2025, 12, 29, 12), utc(2026, 1, 1, 12)})

	keep := selectTieredBackups(stamps, time.UTC, types.Local{KeepWeekly: 2})

	want := []string{"2025-12-28 12h", "2026-01-01 12h"}
	if got := keptDates(keep); !reflect.DeepEqual(got, want) {
		t.Fatalf("kept %v, want %v", got, want)
	}
}

func TestTieredOverlappingTiersCountOnce(t *testing.T) {
	t.Parallel()

	stamps := []int64{utc(2026, 11, 27, 12)}

	keep := selectTieredBackups(stamps, time.UTC,
		types.Local{KeepDaily: 30, KeepWeekly: 8, KeepMonthly: 6, KeepYearly: 1})

	want := []string{"2026-11-27 12h"}
	if got := keptDates(keep); !reflect.DeepEqual(got, want) {
		t.Fatalf("kept %v, want %v", got, want)
	}
}

func TestTieredAlwaysKeepsNewest(t *testing.T) {
	t.Parallel()

	keep := selectTieredBackups(twoBackupsADay(), time.UTC, types.Local{KeepYearly: 1})

	want := []string{"2026-11-27 18h"}
	if got := keptDates(keep); !reflect.DeepEqual(got, want) {
		t.Fatalf("kept %v, want %v", got, want)
	}
}

func TestTieredUsesLocation(t *testing.T) {
	t.Parallel()

	// 2026-11-27 23:00 UTC is 2026-11-28 in UTC+2, so both backups are the newest of their day there
	stamps := newestFirst([]int64{utc(2026, 11, 27, 23), utc(2026, 11, 27, 12)})

	keep := selectTieredBackups(stamps, time.FixedZone("UTC+2", 2*60*60), types.Local{KeepDaily: 2})
	if len(keep) != 2 {
		t.Fatalf("kept %v, want both", keptDates(keep))
	}

	keep = selectTieredBackups(stamps, time.UTC, types.Local{KeepDaily: 2})
	if want := []string{"2026-11-27 23h"}; !reflect.DeepEqual(keptDates(keep), want) {
		t.Fatalf("kept %v, want %v", keptDates(keep), want)
	}
}

func TestTieredDailyAcrossDaylightSaving(t *testing.T) {
	t.Parallel()

	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Skipf("no time zone data: %v", err)
	}

	cases := []struct {
		name   string
		stamps []int64
		want   []string
	}{
		{
			// clocks go back on 2026-10-25, so that day lasts 25 hours: 00:30 BST and 23:30 GMT are the same day
			name:   "autumn",
			stamps: []int64{utc(2026, 10, 24, 11), utc(2026, 10, 24, 23) + 30*60, utc(2026, 10, 25, 23) + 30*60},
			want:   []string{"2026-10-24 11h", "2026-10-25 23h"},
		},
		{
			// clocks go forward on 2026-03-29, so that day lasts 23 hours: 00:30 GMT and 23:30 BST are the same day
			name:   "spring",
			stamps: []int64{utc(2026, 3, 28, 12), utc(2026, 3, 29, 0) + 30*60, utc(2026, 3, 29, 22) + 30*60},
			want:   []string{"2026-03-28 12h", "2026-03-29 22h"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			keep := selectTieredBackups(newestFirst(c.stamps), london, types.Local{KeepDaily: 2})
			if got := keptDates(keep); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("kept %v, want %v", got, c.want)
			}
		})
	}
}

func TestPruneTieredRemovesGenerations(t *testing.T) {
	t.Parallel()

	// 3 days apart, so each backup is on its own day in any time zone
	day := int64(3 * 24 * 60 * 60)
	newest := utc(2026, 11, 27, 12)
	backups := []string{}
	for i := int64(0); i < 4; i++ {
		ts := fmt.Sprint(newest - i*day)
		backups = append(backups, ts, ts+".issues")
	}

	remaining := runPruneWith(t, append(backups, "dummy"), types.Local{KeepDaily: 2})

	want := []string{backups[2], backups[3], backups[0], backups[1], "dummy"}
	if !reflect.DeepEqual(remaining, want) {
		t.Fatalf("remaining = %v, want %v", remaining, want)
	}
}

func TestPruneTieredOverridesKeep(t *testing.T) {
	t.Parallel()

	remaining := runPruneWith(t, []string{"1785000000", "1785061241", "1785115111"},
		types.Local{Keep: 10, KeepYearly: 1})

	want := []string{"1785115111"}
	if !reflect.DeepEqual(remaining, want) {
		t.Fatalf("remaining = %v, want %v", remaining, want)
	}
}

func TestLocalVersioned(t *testing.T) {
	t.Parallel()

	cases := []struct {
		l    types.Local
		want bool
	}{
		{types.Local{}, false},
		{types.Local{Keep: 5}, true},
		{types.Local{KeepDaily: 1}, true},
		{types.Local{KeepWeekly: 1}, true},
		{types.Local{KeepMonthly: 1}, true},
		{types.Local{KeepYearly: 1}, true},
	}
	for _, c := range cases {
		if got := c.l.Versioned(); got != c.want {
			t.Errorf("%+v.Versioned() = %v, want %v", c.l, got, c.want)
		}
	}
}
