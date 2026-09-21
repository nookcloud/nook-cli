package manifest

import "testing"

func fires(t *testing.T, spec string, bits func(*Schedule) uint64, want ...int) {
	t.Helper()
	s, err := ParseSchedule(spec)
	if err != nil {
		t.Fatalf("%q: %v", spec, err)
	}
	var got uint64
	for _, v := range want {
		got |= 1 << uint(v)
	}
	if bits(s) != got {
		t.Errorf("%q: field is %b, want %b", spec, bits(s), got)
	}
}

func TestParseSchedule(t *testing.T) {
	min := func(s *Schedule) uint64 { return s.Min }
	dow := func(s *Schedule) uint64 { return s.Dow }
	hour := func(s *Schedule) uint64 { return s.Hour }

	fires(t, "0 9 * * 1-5", min, 0)
	fires(t, "0 9 * * 1-5", hour, 9)
	fires(t, "0 9 * * 1-5", dow, 1, 2, 3, 4, 5)
	fires(t, "*/15 * * * *", min, 0, 15, 30, 45)
	fires(t, "5/15 * * * *", min, 5, 20, 35, 50)
	fires(t, "0,30 * * * *", min, 0, 30)
	fires(t, "0 0 * * 7", dow, 0) // 7 and 0 are both Sunday
	fires(t, "0 0 * * 0,6", dow, 0, 6)

	for _, bad := range []string{"", "0 9 * *", "0 9 * * * *", "60 * * * *", "* 24 * * *", "0 0 32 * *",
		"0 0 * 13 *", "0 0 * * 8", "a * * * *", "10-5 * * * *", "*/0 * * * *", "*/x * * * *"} {
		if _, err := ParseSchedule(bad); err == nil {
			t.Errorf("expected an error for %q", bad)
		}
	}
}
