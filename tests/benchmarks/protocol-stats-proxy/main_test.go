package main

import "testing"

func TestUniqueBytes(t *testing.T) {
	files := map[uint16]*fileStats{
		1: {
			intervals: []readInterval{
				{start: 0, end: 10},
				{start: 5, end: 15},
				{start: 30, end: 40},
			},
		},
		2: {
			intervals: []readInterval{
				{start: 10, end: 20},
				{start: 20, end: 20},
			},
		},
	}

	if got, want := uniqueBytes(files), uint64(30); got != want {
		t.Fatalf("uniqueBytes() = %d, want %d", got, want)
	}
}
