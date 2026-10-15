package timeutil

import (
	"reflect"
	"testing"
	"time"
)

func TestParseDailyTimes(t *testing.T) {
	tests := []struct {
		input   string
		want    []ClockTime
		wantErr bool
		why     string
	}{
		{"09:00", []ClockTime{{9, 0}}, false, "单个时刻"},
		{"09:00,15:30", []ClockTime{{9, 0}, {15, 30}}, false, "多个时刻"},
		{"15:30,09:00", []ClockTime{{9, 0}, {15, 30}}, false, "输出按时间排序"},
		{"09:00,09:00", []ClockTime{{9, 0}}, false, "重复的时刻去重"},
		{" 09:00 , 15:30 ", []ClockTime{{9, 0}, {15, 30}}, false, "两边的空格不算数"},
		{"9:05", []ClockTime{{9, 5}}, false, "小时可以只写一位"},
		{"00:00", []ClockTime{{0, 0}}, false, "零点"},
		{"23:59", []ClockTime{{23, 59}}, false, "一天的最后一分钟"},
		{"", nil, false, "空表示关闭"},
		{"off", nil, false, "off 表示关闭"},
		{"OFF", nil, false, "大小写不敏感"},
		{"none", nil, false, "none 也是关闭"},
		{"0", nil, false, "0 也是关闭"},
		{"-", nil, false, "横杠也是关闭"},
		{"24:00", nil, true, "小时越界"},
		{"09:60", nil, true, "分钟越界"},
		{"9点", nil, true, "格式不对"},
		{"0900", nil, true, "缺冒号"},
	}
	for _, tt := range tests {
		got, err := ParseDailyTimes(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseDailyTimes(%q) 错误情况不符（%s）: err=%v", tt.input, tt.why, err)
			continue
		}
		if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ParseDailyTimes(%q)（%s）= %v，期望 %v", tt.input, tt.why, got, tt.want)
		}
	}
}

func TestParseWeekdays(t *testing.T) {
	tests := []struct {
		input string
		want  []int
		bad   bool
	}{
		{"Mon", []int{1}, false},
		{"mon", []int{1}, false},
		{"Monday", []int{1}, false},
		{"周一", []int{1}, false},
		{"一", []int{1}, false},
		{"1", []int{1}, false},
		{"Mon,Thu", []int{1, 4}, false},
		{"周一、周四", []int{1, 4}, false},
		{"周日", []int{7}, false},
		{"周天", []int{7}, false},
		{"", nil, false},
		{"8", nil, true},
		{"0", nil, true},
		{"Funday", nil, true},
	}
	for _, tt := range tests {
		got, err := ParseWeekdays(tt.input)
		if (err != nil) != tt.bad {
			t.Errorf("ParseWeekdays(%q): err=%v，期望出错=%v", tt.input, err, tt.bad)
			continue
		}
		if tt.bad {
			continue
		}
		for _, day := range tt.want {
			if !got[day] {
				t.Errorf("ParseWeekdays(%q) 少了星期 %d", tt.input, day)
			}
		}
		if len(got) != len(tt.want) {
			t.Errorf("ParseWeekdays(%q) 得到 %v，期望 %v", tt.input, got, tt.want)
		}
	}
}

func TestParseWeeklySchedule(t *testing.T) {
	weekdays, times, err := ParseWeeklySchedule("Mon 09:00")
	if err != nil || len(weekdays) != 1 || !weekdays[1] || len(times) != 1 || times[0] != (ClockTime{9, 0}) {
		t.Errorf(`ParseWeeklySchedule("Mon 09:00") = %v %v %v`, weekdays, times, err)
	}

	// omitting the weekday means daily
	weekdays, times, err = ParseWeeklySchedule("09:00")
	if err != nil || len(weekdays) != 0 || len(times) != 1 {
		t.Errorf(`ParseWeeklySchedule("09:00") 应为每天 09:00，得到 %v %v %v`, weekdays, times, err)
	}

	// both weekdays and times may list several
	weekdays, times, err = ParseWeeklySchedule("Mon,Thu 09:00,18:00")
	if err != nil || len(weekdays) != 2 || len(times) != 2 {
		t.Errorf(`ParseWeeklySchedule("Mon,Thu 09:00,18:00") 得到 %v %v %v`, weekdays, times, err)
	}

	if _, times, _ = ParseWeeklySchedule("off"); len(times) != 0 {
		t.Error(`ParseWeeklySchedule("off") 应该关闭`)
	}
}

func TestNextOccurrence(t *testing.T) {
	// Wednesday 14:00
	now := time.Date(2026, 9, 16, 14, 0, 0, 0, time.Local)

	tests := []struct {
		times    []ClockTime
		weekdays map[int]bool
		want     string
		why      string
	}{
		{[]ClockTime{{15, 0}}, nil, "2026-09-16 15:00", "今天晚些时候"},
		{[]ClockTime{{9, 0}}, nil, "2026-09-17 09:00", "今天已过，顺延到明天"},
		{[]ClockTime{{9, 0}, {15, 0}}, nil, "2026-09-16 15:00", "多个时刻取最近的未来时刻"},
		{[]ClockTime{{14, 0}}, nil, "2026-09-17 14:00", "正好是此刻也要顺延，避免同一刻重复触发"},
		{[]ClockTime{{9, 0}}, map[int]bool{1: true}, "2026-09-21 09:00", "限定周一"},
		{[]ClockTime{{15, 0}}, map[int]bool{3: true}, "2026-09-16 15:00", "今天就是周三"},
		{[]ClockTime{{9, 0}}, map[int]bool{3: true}, "2026-09-23 09:00", "周三但时刻已过，等下周三"},
	}
	for _, tt := range tests {
		got, ok := NextOccurrence(now, tt.times, tt.weekdays)
		if !ok {
			t.Errorf("%s: 没有算出下次时刻", tt.why)
			continue
		}
		if formatted := got.Format("2006-01-02 15:04"); formatted != tt.want {
			t.Errorf("%s: 期望 %s，实际 %s", tt.why, tt.want, formatted)
		}
	}

	if _, ok := NextOccurrence(now, nil, nil); ok {
		t.Error("没有时刻时应该返回 false")
	}
}

func TestDescribe(t *testing.T) {
	tests := []struct {
		times    []ClockTime
		weekdays map[int]bool
		want     string
	}{
		{nil, nil, "Disabled"},
		{[]ClockTime{{9, 0}}, nil, "Daily 09:00"},
		{[]ClockTime{{9, 0}, {15, 0}}, nil, "Daily 09:00 / 15:00"},
		{[]ClockTime{{9, 0}}, map[int]bool{1: true}, "Weekly Mon 09:00"},
		{[]ClockTime{{9, 0}}, map[int]bool{1: true, 4: true}, "Weekly Mon, Thu 09:00"},
		{[]ClockTime{{9, 0}}, map[int]bool{7: true}, "Weekly Sun 09:00"},
	}
	for _, tt := range tests {
		if got := Describe(tt.times, tt.weekdays); got != tt.want {
			t.Errorf("Describe(%v, %v) = %q，期望 %q", tt.times, tt.weekdays, got, tt.want)
		}
	}
}

func TestISOWeekday(t *testing.T) {
	// 2026-09-14 is a Monday
	for offset, want := range []int{1, 2, 3, 4, 5, 6, 7} {
		day := time.Date(2026, 9, 14+offset, 12, 0, 0, 0, time.Local)
		if got := ISOWeekday(day); got != want {
			t.Errorf("%s 应是星期 %d，实际 %d", day.Format("2006-01-02"), want, got)
		}
	}
}
