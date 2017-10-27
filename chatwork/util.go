package chatwork

import (
	"fmt"
	"time"
)

type JSONTime int64

func (t JSONTime) String() string {
	tm := t.Time()
	return fmt.Sprintf("\"%s\"", tm.Format("Mon Jan _2"))
}

func (t JSONTime) Time() time.Time {
	return time.Unix(int64(t), 0)
}
