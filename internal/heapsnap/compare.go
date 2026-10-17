package heapsnap

import (
	"errors"
	"sort"
)

var ErrRetainedNotParsed = errors.New("head snapshot was parsed without retained sizes")

type Options struct {
	Top      int
	Retained bool
}

type Totals struct {
	ID            string `json:"id,omitempty"`
	NodeCount     int    `json:"nodeCount"`
	EdgeCount     int    `json:"edgeCount"`
	TotalSelfSize int64  `json:"totalSelfSize"`
}

type ConstructorDelta struct {
	Name         string `json:"name"`
	BaseCount    int    `json:"baseCount"`
	HeadCount    int    `json:"headCount"`
	CountDelta   int    `json:"countDelta"`
	BaseSelfSize int64  `json:"baseSelfSize"`
	HeadSelfSize int64  `json:"headSelfSize"`
	SizeDelta    int64  `json:"sizeDelta"`
	RetainedSize *int64 `json:"retainedSize,omitempty"`
}

type Comparison struct {
	Base                Totals             `json:"base"`
	Head                Totals             `json:"head"`
	NodeDelta           int                `json:"nodeDelta"`
	SizeDelta           int64              `json:"sizeDelta"`
	Changed             int                `json:"changed"`
	Constructors        []ConstructorDelta `json:"constructors"`
	NewDuplicateStrings []DuplicateString  `json:"newDuplicateStrings"`
}

func (a *Aggregate) totals() Totals {
	return Totals{NodeCount: a.NodeCount, EdgeCount: a.EdgeCount, TotalSelfSize: a.TotalSelfSize}
}

func Compare(base, head *Aggregate, opts Options) (Comparison, error) {
	if opts.Retained && head.Retained == nil {
		return Comparison{}, ErrRetainedNotParsed
	}
	top := ClampTop(opts.Top)

	rows := map[string]*ConstructorDelta{}
	row := func(name string) *ConstructorDelta {
		r := rows[name]
		if r == nil {
			r = &ConstructorDelta{Name: name}
			rows[name] = r
		}
		return r
	}
	for _, c := range base.Constructors {
		r := row(c.Name)
		r.BaseCount, r.BaseSelfSize = c.Count, c.SelfSize
	}
	for _, c := range head.Constructors {
		r := row(c.Name)
		r.HeadCount, r.HeadSelfSize = c.Count, c.SelfSize
	}
	changed := make([]ConstructorDelta, 0, len(rows))
	for _, r := range rows {
		r.CountDelta = r.HeadCount - r.BaseCount
		r.SizeDelta = r.HeadSelfSize - r.BaseSelfSize
		if r.CountDelta != 0 || r.SizeDelta != 0 {
			changed = append(changed, *r)
		}
	}
	sort.Slice(changed, func(i, j int) bool { return lessDelta(changed[i], changed[j]) })
	constructors := changed
	if len(constructors) > top {
		constructors = constructors[:top:top]
	}
	if opts.Retained {
		for i := range constructors {
			size := head.Retained[constructors[i].Name]
			constructors[i].RetainedSize = &size
		}
	}

	return Comparison{
		Base:                base.totals(),
		Head:                head.totals(),
		NodeDelta:           head.NodeCount - base.NodeCount,
		SizeDelta:           head.TotalSelfSize - base.TotalSelfSize,
		Changed:             len(changed),
		Constructors:        constructors,
		NewDuplicateStrings: newDuplicates(base, head, top),
	}, nil
}

func lessDelta(a, b ConstructorDelta) bool {
	if x, y := abs(a.SizeDelta), abs(b.SizeDelta); x != y {
		return x > y
	}
	if a.SizeDelta != b.SizeDelta {
		return a.SizeDelta > b.SizeDelta
	}
	if x, y := abs(int64(a.CountDelta)), abs(int64(b.CountDelta)); x != y {
		return x > y
	}
	if a.CountDelta != b.CountDelta {
		return a.CountDelta > b.CountDelta
	}
	return a.Name < b.Name
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func newDuplicates(base, head *Aggregate, top int) []DuplicateString {
	type key struct {
		value  string
		length int
	}
	inBase := make(map[key]bool, len(base.DuplicateStrings))
	for _, d := range base.DuplicateStrings {
		inBase[key{d.Value, d.Length}] = true
	}
	out := make([]DuplicateString, 0)
	for _, d := range head.DuplicateStrings {
		if len(out) == top {
			break
		}
		if !inBase[key{d.Value, d.Length}] {
			out = append(out, d)
		}
	}
	return out
}
