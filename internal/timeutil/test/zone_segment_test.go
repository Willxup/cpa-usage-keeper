package test

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"cpa-usage-keeper/internal/timeutil"
)

// A minimal TZif v2 file with its last explicit transition at the Unix epoch.
// This exercises POSIX extension rules without depending on host zic/tzdata.
func posixLocation(t *testing.T, rule string) *time.Location {
	t.Helper()
	var data bytes.Buffer
	for _, width := range []int{4, 8} {
		data.WriteString("TZif2")
		data.Write(make([]byte, 15))
		for _, count := range []uint32{0, 0, 0, 1, 1, 4} {
			if err := binary.Write(&data, binary.BigEndian, count); err != nil {
				t.Fatal(err)
			}
		}
		data.Write(make([]byte, width+1+6)) // transition, type index, UTC type
		data.WriteString("UTC\x00")
	}
	data.WriteString("\n" + rule + "\n")
	location, err := time.LoadLocationFromTZData("test/posix", data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return location
}

func TestZoneSegmentEndLeapYearPOSIXRules(t *testing.T) {
	for _, rule := range []string{
		"CET-1CEST,M3.5.0,M10.5.0/3",
		"PST8PDT,M3.2.0,M11.1.0",
		"<-04>4<-03>,M9.1.6/24,M4.1.6/24",
		// In 2028 the last Sunday is December 31: real transitions that day
		// must remain intact before the expired synthetic year-end boundary.
		"STD0DST,M12.5.0/1,M12.5.0/3",
	} {
		t.Run(rule, func(t *testing.T) {
			location := posixLocation(t, rule)
			for _, year := range []int{2026, 2028, 2040} {
				start := time.Date(year, 12, 28, 0, 0, 0, 0, time.UTC)
				for at := start; at.Before(start.AddDate(0, 0, 7)); at = at.Add(30 * time.Minute) {
					local := at.In(location)
					end := timeutil.ZoneSegmentEnd(local)
					if !end.IsZero() && !end.After(local) {
						t.Fatalf("non-advancing boundary: at=%s end=%s", local, end)
					}
					_, original := local.ZoneBounds()
					if (original.IsZero() || original.After(local)) && !end.Equal(original) {
						t.Fatal("changed a valid boundary")
					}
				}
				for day := 29; day <= 31; day++ {
					date := timeutil.CivilDate{Year: year, Month: time.December, Day: day}
					bounds, ok := timeutil.DayBounds(date, location)
					wantStart := time.Date(year, 12, day, 0, 0, 0, 0, location)
					wantEnd := wantStart.AddDate(0, 0, 1)
					if !ok || !bounds.Start.Equal(wantStart) || !bounds.End.Equal(wantEnd) {
						t.Fatalf("wrong year-end day: %s %+v", date, bounds)
					}
				}
			}
		})
	}
}

func TestZoneSegmentEndKeepsNormalBoundaries(t *testing.T) {
	for _, name := range []string{"UTC", "Asia/Shanghai", "America/Los_Angeles", "Europe/Berlin", "Australia/Lord_Howe"} {
		location, err := time.LoadLocation(name)
		if err != nil {
			t.Fatal(err)
		}
		for month := time.January; month <= time.December; month++ {
			for day := 1; day <= 31; day++ {
				at := time.Date(2026, month, day, 0, 0, 0, 0, location)
				_, want := at.ZoneBounds()
				if got := timeutil.ZoneSegmentEnd(at); !got.Equal(want) {
					t.Fatalf("changed %s boundary at %s: %s != %s", name, at, got, want)
				}
			}
		}
	}
}
