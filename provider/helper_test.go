package provider

import (
	"math"
	"testing"
)

func TestTruish(t *testing.T) {
	cases := []struct {
		k string
		v bool
	}{
		{"", false},
		{"false", false},
		{"0", false},
		{"off", false},
		{"true", true},
		{"1", true},
		{"on", true},
	}

	for _, c := range cases {
		b := truish(c.k)
		if b != c.v {
			t.Errorf("expected %v got %v", c.v, b)
		}
	}
}

func TestReplace(t *testing.T) {
	cases := []struct {
		k             string
		v             interface{}
		fmt, expected string
	}{
		{"foo", true, "${foo}", "true"},
		{"foo", "1", "abc${foo}${foo}", "abc11"},
		{"foo", math.Pi, "${foo:%.2f}", "3.14"},
	}

	for _, c := range cases {
		s, err := replaceFormatted(c.fmt, map[string]interface{}{
			c.k: c.v,
		})

		if s != c.expected || err != nil {
			t.Error(s, err)
		}
	}
}

func TestReplaceMulti(t *testing.T) {
	s, err := replaceFormatted("${foo}-${bar}", map[string]interface{}{
		"foo": "bar",
		"bar": "baz",
	})

	if s != "bar-baz" || err != nil {
		t.Error(s, err)
	}
}

func TestReplaceNoMatch(t *testing.T) {
	s, err := replaceFormatted("${foo}", map[string]interface{}{
		"bar": "baz",
	})

	if s != "" || err == nil {
		t.Error(s, err)
	}
}
