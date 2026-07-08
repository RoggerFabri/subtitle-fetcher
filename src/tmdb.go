package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	tmdbBaseURL   = "https://api.themoviedb.org/3"
	tmdbImageBase = "https://image.tmdb.org/t/p/original"
)

// tmdbClient is a minimal TMDB v3 REST client. The API key is passed as an
// api_key query parameter (v3-style), mirroring the lightweight HTTP-client
// conventions used by the other providers (imdb.go, subdl.go).
type tmdbClient struct {
	apiKey string
	http   *http.Client
}

func newTMDBClient(apiKey string) *tmdbClient {
	return &tmdbClient{apiKey: apiKey, http: &http.Client{Timeout: 30 * time.Second}}
}

// get performs a GET against the TMDB API and decodes the JSON body into out.
func (c *tmdbClient) get(path string, params url.Values, out any) error {
	if c.apiKey == "" {
		return fmt.Errorf("no TMDB API key configured")
	}
	if params == nil {
		params = url.Values{}
	}
	params.Set("api_key", c.apiKey)
	u := tmdbBaseURL + path + "?" + params.Encode()

	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", appName)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("tmdb %s: %s: %s", path, resp.Status, strings.TrimSpace(string(snippet)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Shared sub-structures across movie/tv detail payloads.
type (
	tmdbGenre struct {
		Name string `json:"name"`
	}
	tmdbCompany struct {
		Name string `json:"name"`
	}
	tmdbCastMember struct {
		Name      string `json:"name"`
		Character string `json:"character"`
	}
	tmdbCrewMember struct {
		Name string `json:"name"`
		Job  string `json:"job"`
	}
	tmdbCredits struct {
		Cast []tmdbCastMember `json:"cast"`
		Crew []tmdbCrewMember `json:"crew"`
	}
	tmdbExternalIDs struct {
		IMDBID string `json:"imdb_id"`
	}
)

// tmdbMovie is the subset of GET /movie/{id} fields we map into an NFO.
type tmdbMovie struct {
	ID            int           `json:"id"`
	Title         string        `json:"title"`
	OriginalTitle string        `json:"original_title"`
	Overview      string        `json:"overview"`
	Tagline       string        `json:"tagline"`
	ReleaseDate   string        `json:"release_date"`
	VoteAverage   float64       `json:"vote_average"`
	Runtime       int           `json:"runtime"`
	IMDBID        string        `json:"imdb_id"`
	PosterPath    string        `json:"poster_path"`
	BackdropPath  string        `json:"backdrop_path"`
	Genres        []tmdbGenre   `json:"genres"`
	Companies     []tmdbCompany `json:"production_companies"`
	Collection    *struct {
		Name string `json:"name"`
	} `json:"belongs_to_collection"`
	Credits     tmdbCredits     `json:"credits"`
	ExternalIDs tmdbExternalIDs `json:"external_ids"`
}

// tmdbTV is the subset of GET /tv/{id} fields we map into an NFO.
type tmdbTV struct {
	ID             int             `json:"id"`
	Name           string          `json:"name"`
	OriginalName   string          `json:"original_name"`
	Overview       string          `json:"overview"`
	FirstAirDate   string          `json:"first_air_date"`
	LastAirDate    string          `json:"last_air_date"`
	Status         string          `json:"status"`
	VoteAverage    float64         `json:"vote_average"`
	EpisodeRunTime []int           `json:"episode_run_time"`
	PosterPath     string          `json:"poster_path"`
	BackdropPath   string          `json:"backdrop_path"`
	Genres         []tmdbGenre     `json:"genres"`
	Networks       []tmdbCompany   `json:"networks"`
	Credits        tmdbCredits     `json:"credits"`
	ExternalIDs    tmdbExternalIDs `json:"external_ids"`
}

// resolve maps a media entry to a TMDB record, returning the kind ("movie" or
// "tv") and TMDB id. It prefers an exact IMDB-id lookup (imdbID is numeric,
// without the "tt" prefix, as stored in media.imdb_id) and falls back to a
// name+year search. mediaType is this app's "movie"/"series" value and only
// nudges which result list is preferred when both are present.
func (c *tmdbClient) resolve(imdbID, name string, year int, mediaType string) (string, int, error) {
	if imdbID != "" {
		var res struct {
			MovieResults []struct {
				ID int `json:"id"`
			} `json:"movie_results"`
			TVResults []struct {
				ID int `json:"id"`
			} `json:"tv_results"`
		}
		if err := c.get("/find/tt"+imdbID, url.Values{"external_source": {"imdb_id"}}, &res); err != nil {
			return "", 0, err
		}
		preferTV := mediaType == "series"
		if preferTV && len(res.TVResults) > 0 {
			return "tv", res.TVResults[0].ID, nil
		}
		if !preferTV && len(res.MovieResults) > 0 {
			return "movie", res.MovieResults[0].ID, nil
		}
		if len(res.TVResults) > 0 {
			return "tv", res.TVResults[0].ID, nil
		}
		if len(res.MovieResults) > 0 {
			return "movie", res.MovieResults[0].ID, nil
		}
		// IMDB find returned nothing usable — fall through to name search.
	}

	if mediaType == "series" {
		params := url.Values{"query": {name}}
		if year > 0 {
			params.Set("first_air_date_year", strconv.Itoa(year))
		}
		var res struct {
			Results []struct {
				ID int `json:"id"`
			} `json:"results"`
		}
		if err := c.get("/search/tv", params, &res); err != nil {
			return "", 0, err
		}
		if len(res.Results) > 0 {
			return "tv", res.Results[0].ID, nil
		}
	} else {
		params := url.Values{"query": {name}}
		if year > 0 {
			params.Set("year", strconv.Itoa(year))
		}
		var res struct {
			Results []struct {
				ID int `json:"id"`
			} `json:"results"`
		}
		if err := c.get("/search/movie", params, &res); err != nil {
			return "", 0, err
		}
		if len(res.Results) > 0 {
			return "movie", res.Results[0].ID, nil
		}
	}
	return "", 0, fmt.Errorf("no TMDB match for %q", name)
}

func (c *tmdbClient) movieDetails(id int) (*tmdbMovie, error) {
	var m tmdbMovie
	if err := c.get(fmt.Sprintf("/movie/%d", id), url.Values{"append_to_response": {"credits,external_ids"}}, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (c *tmdbClient) tvDetails(id int) (*tmdbTV, error) {
	var t tmdbTV
	if err := c.get(fmt.Sprintf("/tv/%d", id), url.Values{"append_to_response": {"credits,external_ids"}}, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// downloadImage fetches a TMDB image (given its "/abc.jpg" path) to dest. A
// blank imgPath is a no-op.
func (c *tmdbClient) downloadImage(imgPath, dest string) error {
	if imgPath == "" {
		return nil
	}
	req, _ := http.NewRequest("GET", tmdbImageBase+imgPath, nil)
	req.Header.Set("User-Agent", appName)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tmdb image %s: %s", imgPath, resp.Status)
	}
	return writeFileFrom(dest, resp.Body)
}
