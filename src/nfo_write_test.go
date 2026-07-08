package main

import (
	"strings"
	"testing"
)

func TestMovieNFORoundTrip(t *testing.T) {
	m := &tmdbMovie{
		Title:         "Aquaman and the Lost Kingdom",
		OriginalTitle: "Aquaman and the Lost Kingdom",
		Overview:      "The tide is turning.",
		Tagline:       "The tide is turning.",
		ReleaseDate:   "2023-12-20",
		VoteAverage:   6.9,
		Runtime:       124,
		IMDBID:        "tt9663764",
		PosterPath:    "/poster.jpg",
		BackdropPath:  "/backdrop.jpg",
		Genres:        []tmdbGenre{{Name: "Action"}, {Name: "Adventure"}},
		Companies:     []tmdbCompany{{Name: "DC Studios"}},
		Collection: &struct {
			Name string `json:"name"`
		}{Name: "Aquaman Collection"},
		Credits: tmdbCredits{
			Cast: []tmdbCastMember{{Name: "Jason Momoa", Character: "Arthur Curry"}},
			Crew: []tmdbCrewMember{{Name: "James Wan", Job: "Director"}, {Name: "Someone", Job: "Producer"}},
		},
	}

	n := nfoFromMovie(m)
	data, err := marshalNFO(n, "movie")
	if err != nil {
		t.Fatal(err)
	}
	xml := string(data)
	if !strings.Contains(xml, "<movie>") {
		t.Errorf("expected <movie> root, got:\n%s", xml)
	}
	// Only <imdbid> should appear — not the empty <imdb_id>/<id> read-path fields.
	if strings.Contains(xml, "<imdb_id>") || strings.Contains(xml, "<id>") {
		t.Errorf("unexpected empty id tags emitted:\n%s", xml)
	}

	// Round-trip: parse the output back and assert the key fields survive.
	got, err := parseNFOBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != "movie" {
		t.Errorf("root = %q, want movie", got.Root)
	}
	if got.Title != m.Title {
		t.Errorf("title = %q, want %q", got.Title, m.Title)
	}
	if got.Year != 2023 {
		t.Errorf("year = %d, want 2023", got.Year)
	}
	if got.imdbID() != "9663764" {
		t.Errorf("imdbID = %q, want 9663764", got.imdbID())
	}
	if len(got.Directors) != 1 || got.Directors[0] != "James Wan" {
		t.Errorf("directors = %v, want [James Wan]", got.Directors)
	}
	if got.Collection == nil || got.Collection.Name != "Aquaman Collection" {
		t.Errorf("collection = %+v, want Aquaman Collection", got.Collection)
	}
}

func TestTVNFORoundTrip(t *testing.T) {
	tv := &tmdbTV{
		Name:           "Your Friends & Neighbors",
		OriginalName:   "Your Friends & Neighbors",
		Overview:       "A hedge funder steals from his neighbors.",
		FirstAirDate:   "2025-04-11",
		LastAirDate:    "2025-06-01",
		Status:         "Returning Series",
		VoteAverage:    7.5,
		EpisodeRunTime: []int{50},
		Genres:         []tmdbGenre{{Name: "Drama"}},
		Networks:       []tmdbCompany{{Name: "Apple TV+"}},
		ExternalIDs:    tmdbExternalIDs{IMDBID: "tt30459041"},
	}

	n := nfoFromTV(tv)
	if n.Status != "Continuing" {
		t.Errorf("status = %q, want Continuing", n.Status)
	}
	// Running show: enddate must not be filled from last_air_date.
	if n.EndDate != "" {
		t.Errorf("enddate = %q, want empty for a continuing show", n.EndDate)
	}

	data, err := marshalNFO(n, "tv")
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseNFOBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != "tvshow" {
		t.Errorf("root = %q, want tvshow", got.Root)
	}
	if got.Year != 2025 {
		t.Errorf("year = %d, want 2025", got.Year)
	}
	if got.Status != "Continuing" {
		t.Errorf("status = %q, want Continuing", got.Status)
	}
	if got.imdbID() != "30459041" {
		t.Errorf("imdbID = %q, want 30459041", got.imdbID())
	}
	if got.Runtime != 50 {
		t.Errorf("runtime = %d, want 50", got.Runtime)
	}
}

func TestMapTMDBStatus(t *testing.T) {
	cases := map[string]string{
		"Returning Series": "Continuing",
		"In Production":    "Continuing",
		"Ended":            "Ended",
		"Canceled":         "Ended",
		"Weird":            "Weird",
	}
	for in, want := range cases {
		if got := mapTMDBStatus(in); got != want {
			t.Errorf("mapTMDBStatus(%q) = %q, want %q", in, got, want)
		}
	}
}
