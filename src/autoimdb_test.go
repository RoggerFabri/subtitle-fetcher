package main

import "testing"

func TestImdbTypeMatches(t *testing.T) {
	cases := []struct {
		mediaType string
		imdbType  string
		want      bool
	}{
		// movies
		{"movie", "feature", true},
		{"movie", "Feature", true}, // case-insensitive
		{"movie", "tv movie", true},
		{"movie", "TV Movie", true},
		{"movie", "video", true},
		{"movie", "Video", true},
		{"movie", "tv series", false},
		{"movie", "tv mini-series", false},
		{"movie", "short", false},
		{"movie", "", false},
		// series
		{"series", "tv series", true},
		{"series", "TV Series", true},
		{"series", "tv mini series", true},
		{"series", "tv mini-series", true},
		{"series", "TV Mini-Series", true},
		{"series", "feature", false},
		{"series", "video", false},
		{"series", "", false},
	}

	for _, c := range cases {
		got := imdbTypeMatches(c.mediaType, c.imdbType)
		if got != c.want {
			t.Errorf("imdbTypeMatches(%q, %q) = %v, want %v", c.mediaType, c.imdbType, got, c.want)
		}
	}
}

func TestNormalizeTitle(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Futurama: The Beast with a Billion Backs", "futurama the beast with a billion backs"},
		{"Futurama The Beast with a Billion Backs", "futurama the beast with a billion backs"},
		{"It's Always Sunny in Philadelphia", "its always sunny in philadelphia"},
		{"  extra   spaces  ", "extra spaces"},
		{"Marvel's Agents of S.H.I.E.L.D.", "marvels agents of shield"},
	}

	for _, c := range cases {
		got := normalizeTitle(c.input)
		if got != c.want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestParseNameAndYear(t *testing.T) {
	cases := []struct {
		input    string
		wantName string
		wantYear int
		wantOk   bool
	}{
		{"The Godfather (1972)", "The Godfather", 1972, true},
		{"Futurama The Beast with a Billion Backs (2008)", "Futurama The Beast with a Billion Backs", 2008, true},
		{"Breaking Bad", "", 0, false},
		{"Movie (not a year)", "", 0, false},
		{"Spaced (2001) extra", "", 0, false},
	}

	for _, c := range cases {
		name, year, ok := parseNameAndYear(c.input)
		if ok != c.wantOk || name != c.wantName || year != c.wantYear {
			t.Errorf("parseNameAndYear(%q) = (%q, %d, %v), want (%q, %d, %v)",
				c.input, name, year, ok, c.wantName, c.wantYear, c.wantOk)
		}
	}
}
