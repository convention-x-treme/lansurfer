package main

import "testing"

func TestVerify(t *testing.T) {
	for _, tc := range []struct {
		name                                           string
		max, before, joiners, joined, after, duplicate int
		wantErr                                        bool
	}{
		{name: "exact fill", max: 6, before: 5, joiners: 100, joined: 1, after: 6},
		{name: "multiple places", max: 8, before: 3, joiners: 100, joined: 5, after: 8},
		{name: "fewer joiners than places", max: 8, before: 3, joiners: 2, joined: 2, after: 5},
		{name: "unlimited", max: 0, before: 3, joiners: 10, joined: 10, after: 13},
		{name: "overfilled", max: 6, before: 5, joiners: 100, joined: 2, after: 7, wantErr: true},
		{name: "places left unused", max: 6, before: 3, joiners: 100, joined: 2, after: 5, wantErr: true},
		{name: "joined count differs from rows", max: 6, before: 5, joiners: 100, joined: 1, after: 5, wantErr: true},
		{name: "duplicate membership", max: 6, before: 5, joiners: 100, joined: 1, after: 6, duplicate: 1, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := verify(tc.max, tc.before, tc.joiners, tc.joined, tc.after, tc.duplicate)
			if (err != nil) != tc.wantErr {
				t.Fatalf("verify error = %v, wantErr %t", err, tc.wantErr)
			}
		})
	}
}
