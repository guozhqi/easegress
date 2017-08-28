package common

import (
	"fmt"
	"strconv"
)

type Uint16Value uint16

func NewUint16Value(val uint16, p *uint16) *Uint16Value {
	if p == nil {
		p = new(uint16)
	}
	*p = val
	return (*Uint16Value)(p)
}

func (i *Uint16Value) Set(s string) error {
	v, err := strconv.ParseUint(s, 0, 16)
	if err != nil {
		return err
	}

	*i = Uint16Value(v)
	return nil
}

func (i *Uint16Value) Get() interface{} { return uint16(*i) }

func (i *Uint16Value) String() string { return strconv.FormatUint(uint64(*i), 10) }

type Uint64RangeValue struct {
	v        *uint64
	min, max uint64
}

func NewUint64RangeValue(val uint64, p *uint64, min, max uint64) *Uint64RangeValue {
	if p == nil {
		p = new(uint64)
	}
	*p = val

	return &Uint64RangeValue{
		v:   p,
		min: min,
		max: max,
	}
}

func (i *Uint64RangeValue) Set(s string) error {
	v, err := strconv.ParseUint(s, 0, 64)
	if err != nil {
		return err
	}

	if v < i.min || v > i.max {
		return fmt.Errorf("value out of range [%d, %d]", i.min, i.max)
	}

	*i.v = v
	return nil
}

func (i *Uint64RangeValue) Get() interface{} { return *i.v }

func (i *Uint64RangeValue) String() string {
	if i.v == nil {
		return strconv.FormatUint(0, 10) // zero value
	} else {
		return strconv.FormatUint(*i.v, 10)
	}
}
