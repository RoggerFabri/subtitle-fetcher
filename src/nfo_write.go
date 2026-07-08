package main

import (
	"encoding/xml"
	"io"
	"os"
	"strconv"
	"strings"
)

// movieNFODoc / tvshowNFODoc wrap nfoData purely to stamp the correct XML root
// element (<movie> / <tvshow>) on output; the embedded fields are promoted and
// marshaled inline. nfoData itself declares no XMLName so it can still be
// unmarshaled from any of the three NFO schemas on the read path.
type movieNFODoc struct {
	XMLName xml.Name `xml:"movie"`
	*nfoData
}

type tvshowNFODoc struct {
	XMLName xml.Name `xml:"tvshow"`
	*nfoData
}

// marshalNFO renders an nfoData as a Kodi/Jellyfin NFO document. kind is "tv"
// for a <tvshow>, anything else for a <movie>.
func marshalNFO(n *nfoData, kind string) ([]byte, error) {
	var doc any
	if kind == "tv" {
		doc = tvshowNFODoc{nfoData: n}
	} else {
		doc = movieNFODoc{nfoData: n}
	}
	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	out := append([]byte(xml.Header), body...)
	out = append(out, '\n')
	return out, nil
}

// nfoFromMovie maps a TMDB movie payload into the shared nfoData schema.
func nfoFromMovie(m *tmdbMovie) *nfoData {
	n := &nfoData{
		Title:         strings.TrimSpace(m.Title),
		OriginalTitle: strings.TrimSpace(m.OriginalTitle),
		Plot:          strings.TrimSpace(m.Overview),
		Tagline:       strings.TrimSpace(m.Tagline),
		Year:          yearFromDate(m.ReleaseDate),
		Premiered:     m.ReleaseDate,
		Rating:        m.VoteAverage,
		Runtime:       m.Runtime,
		Genres:        genreNames(m.Genres),
		Studios:       companyNames(m.Companies),
		Directors:     directorNames(m.Credits.Crew),
		Actors:        castActors(m.Credits.Cast),
		IMDBID:        pickIMDB(m.IMDBID, m.ExternalIDs.IMDBID),
	}
	if m.Collection != nil {
		if name := strings.TrimSpace(m.Collection.Name); name != "" {
			n.Collection = &nfoSet{Name: name}
		}
	}
	return n
}

// nfoFromTV maps a TMDB TV payload into the shared nfoData schema.
func nfoFromTV(t *tmdbTV) *nfoData {
	status := mapTMDBStatus(t.Status)
	n := &nfoData{
		Title:         strings.TrimSpace(t.Name),
		OriginalTitle: strings.TrimSpace(t.OriginalName),
		ShowTitle:     strings.TrimSpace(t.Name),
		Plot:          strings.TrimSpace(t.Overview),
		Year:          yearFromDate(t.FirstAirDate),
		Premiered:     t.FirstAirDate,
		Status:        status,
		Rating:        t.VoteAverage,
		Genres:        genreNames(t.Genres),
		Studios:       companyNames(t.Networks),
		Directors:     directorNames(t.Credits.Crew),
		Actors:        castActors(t.Credits.Cast),
		IMDBID:        pickIMDB(t.ExternalIDs.IMDBID),
	}
	if len(t.EpisodeRunTime) > 0 {
		n.Runtime = t.EpisodeRunTime[0]
	}
	// last_air_date is set even for running shows (their latest episode), so
	// only record it as the series end date once the show has actually ended.
	if status == "Ended" {
		n.EndDate = t.LastAirDate
	}
	return n
}

// mapTMDBStatus normalizes TMDB's series status to the Kodi Continuing/Ended
// values the scanner stores in media.air_status. Unknown values pass through.
func mapTMDBStatus(s string) string {
	switch strings.TrimSpace(s) {
	case "Returning Series", "In Production", "Planned", "Pilot":
		return "Continuing"
	case "Ended", "Canceled", "Cancelled":
		return "Ended"
	default:
		return strings.TrimSpace(s)
	}
}

func yearFromDate(s string) int {
	if len(s) >= 4 {
		if y, err := strconv.Atoi(s[:4]); err == nil {
			return y
		}
	}
	return 0
}

func genreNames(g []tmdbGenre) []string {
	out := make([]string, 0, len(g))
	for _, x := range g {
		if n := strings.TrimSpace(x.Name); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func companyNames(c []tmdbCompany) []string {
	out := make([]string, 0, len(c))
	for _, x := range c {
		if n := strings.TrimSpace(x.Name); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func directorNames(crew []tmdbCrewMember) []string {
	var out []string
	for _, m := range crew {
		if m.Job == "Director" {
			if n := strings.TrimSpace(m.Name); n != "" {
				out = append(out, n)
			}
		}
	}
	return out
}

// castActors maps TMDB cast entries into nfoActor, capped so the file stays a
// reasonable size for large ensemble casts.
func castActors(cast []tmdbCastMember) []nfoActor {
	const maxCast = 30
	out := make([]nfoActor, 0, len(cast))
	for _, c := range cast {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		out = append(out, nfoActor{Name: name, Role: strings.TrimSpace(c.Character)})
		if len(out) >= maxCast {
			break
		}
	}
	return out
}

func pickIMDB(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// writeFileFrom streams r into a newly created file at dest.
func writeFileFrom(dest string, r io.Reader) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}
