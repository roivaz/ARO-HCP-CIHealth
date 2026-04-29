package window

import "time"

type DefaultMode string

const (
	DefaultNone         DefaultMode = ""
	DefaultLatestWeek   DefaultMode = "latest_week"
	DefaultRolling      DefaultMode = "rolling"
	DefaultLatestSprint DefaultMode = "latest_sprint"
)

type Request struct {
	Date        string
	StartDate   string
	EndDate     string
	Week        string
	Now         time.Time
	DefaultMode DefaultMode
	RollingDays int
}

type Scope struct {
	StartDate  string
	EndDate    string
	StartTime  time.Time
	EndTime    time.Time
	DateLabels []string
	AnchorWeek string
}

type WeekWindow struct {
	Weeks        []string `json:"weeks,omitempty"`
	CurrentWeek  string   `json:"current_week"`
	PreviousWeek string   `json:"previous_week,omitempty"`
	NextWeek     string   `json:"next_week,omitempty"`
	Index        int      `json:"-"`
}
