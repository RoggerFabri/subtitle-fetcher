package main

import "testing"

const movieNFO = `<?xml version="1.0" encoding="utf-8" standalone="yes"?>
<movie>
  <title>Aquaman and the Lost Kingdom</title>
  <year>2023</year>
  <genre>Action</genre>
  <genre>Adventure</genre>
  <imdbid>tt9663764</imdbid>
  <id>tt9663764</id>
  <tmdbid>572802</tmdbid>
  <set><name>Aquaman Collection</name></set>
</movie>`

const tvshowNFO = `<?xml version="1.0" encoding="utf-8" standalone="yes"?>
<tvshow>
  <title>Your Friends &amp; Neighbors</title>
  <year>2025</year>
  <imdb_id>tt30459041</imdb_id>
  <tmdbid>241609</tmdbid>
  <id>443433</id>
  <status>Continuing</status>
</tvshow>`

const episodeNFO = `<?xml version="1.0" encoding="utf-8" standalone="yes"?>
<episodedetails>
  <title>This Is What Happens</title>
  <imdbid>tt32323060</imdbid>
</episodedetails>`

func TestParseMovieNFO(t *testing.T) {
	n, err := parseNFOBytes([]byte(movieNFO))
	if err != nil {
		t.Fatal(err)
	}
	if n.Root != "movie" {
		t.Errorf("root = %q, want movie", n.Root)
	}
	if n.Year != 2023 {
		t.Errorf("year = %d, want 2023", n.Year)
	}
	if got := n.imdbID(); got != "9663764" {
		t.Errorf("imdbID = %q, want 9663764", got)
	}
	if len(n.Genres) != 2 {
		t.Errorf("genres = %v, want 2", n.Genres)
	}
	if n.Collection.Name != "Aquaman Collection" {
		t.Errorf("collection = %q", n.Collection.Name)
	}
}

func TestParseTVShowNFO(t *testing.T) {
	n, err := parseNFOBytes([]byte(tvshowNFO))
	if err != nil {
		t.Fatal(err)
	}
	if n.Root != "tvshow" {
		t.Errorf("root = %q, want tvshow", n.Root)
	}
	if n.Year != 2025 {
		t.Errorf("year = %d, want 2025", n.Year)
	}
	// <imdb_id> carries the id; <id> is the numeric TVDB id and must be ignored.
	if got := n.imdbID(); got != "30459041" {
		t.Errorf("imdbID = %q, want 30459041 (must ignore numeric <id>)", got)
	}
	if n.Status != "Continuing" {
		t.Errorf("status = %q, want Continuing", n.Status)
	}
}

func TestParseEpisodeNFO(t *testing.T) {
	n, err := parseNFOBytes([]byte(episodeNFO))
	if err != nil {
		t.Fatal(err)
	}
	if n.Root != "episodedetails" {
		t.Errorf("root = %q, want episodedetails", n.Root)
	}
	if got := n.imdbID(); got != "32323060" {
		t.Errorf("imdbID = %q, want 32323060", got)
	}
}

// A numeric-only <id> with no imdbid/imdb_id must not be mistaken for an IMDB id.
func TestIMDBIDIgnoresNumericID(t *testing.T) {
	n, err := parseNFOBytes([]byte(`<tvshow><id>443433</id></tvshow>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := n.imdbID(); got != "" {
		t.Errorf("imdbID = %q, want empty", got)
	}
}
