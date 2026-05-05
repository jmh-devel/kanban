package repo

import "testing"

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		owner  string
		repo   string
		wantOK bool
	}{
		{name: "ssh", input: "git@github.com:jmh-devel/solarops.us.git", owner: "jmh-devel", repo: "solarops.us", wantOK: true},
		{name: "https", input: "https://github.com/jmh-devel/solarops.us.git", owner: "jmh-devel", repo: "solarops.us", wantOK: true},
		{name: "ssh url", input: "ssh://git@github.com/jmh-devel/solarops.us.git", owner: "jmh-devel", repo: "solarops.us", wantOK: true},
		{name: "non github", input: "https://gitlab.com/jmh-devel/solarops.us.git", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRemoteURL(tc.input)
			if tc.wantOK && err != nil {
				t.Fatalf("expected success, got error: %v", err)
			}
			if !tc.wantOK && err == nil {
				t.Fatalf("expected error, got success: %+v", got)
			}
			if !tc.wantOK {
				return
			}
			if got.Owner != tc.owner || got.Name != tc.repo {
				t.Fatalf("unexpected repo parse: %+v", got)
			}
		})
	}
}
