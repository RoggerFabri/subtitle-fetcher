package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// nfoActor is one cast/crew entry from an NFO file.
type nfoActor struct {
	Name string `xml:"name" json:"name,omitempty"`
	Role string `xml:"role" json:"role,omitempty"`
	Type string `xml:"type" json:"type,omitempty"`
}

// nfoData is a superset of the Kodi/Jellyfin <movie>, <tvshow>, and
// <episodedetails> schemas. No XMLName is declared, so a single struct
// unmarshals any of the three root elements; fields absent from a given file
// simply stay zero-valued.
type nfoData struct {
	Title         string     `xml:"title" json:"title,omitempty"`
	OriginalTitle string     `xml:"originaltitle" json:"original_title,omitempty"`
	ShowTitle     string     `xml:"showtitle" json:"show_title,omitempty"`
	Year          int        `xml:"year" json:"year,omitempty"`
	Plot          string     `xml:"plot" json:"plot,omitempty"`
	Tagline       string     `xml:"tagline" json:"tagline,omitempty"`
	Rating        float64    `xml:"rating" json:"rating,omitempty"`
	MPAA          string     `xml:"mpaa" json:"mpaa,omitempty"`
	Runtime       int        `xml:"runtime" json:"runtime,omitempty"`
	Premiered     string     `xml:"premiered" json:"premiered,omitempty"`
	Aired         string     `xml:"aired" json:"aired,omitempty"`
	EndDate       string     `xml:"enddate" json:"end_date,omitempty"`
	Status        string     `xml:"status" json:"status,omitempty"`
	Genres        []string   `xml:"genre" json:"genres,omitempty"`
	Studios       []string   `xml:"studio" json:"studios,omitempty"`
	Directors     []string   `xml:"director" json:"directors,omitempty"`
	Actors        []nfoActor `xml:"actor" json:"actors,omitempty"`

	// IMDB id appears under different tags across the three schemas:
	// <imdbid> on movies/episodes, <imdb_id> on shows, and <id> holds a
	// "tt…" value on movies but a numeric TVDB id on shows — so <id> is only
	// trusted when it carries the "tt" prefix (see imdbID).
	IMDBID    string `xml:"imdbid" json:"-"`
	IMDBIDAlt string `xml:"imdb_id" json:"-"`
	GenericID string `xml:"id" json:"-"`

	Collection struct {
		Name string `xml:"name" json:"name,omitempty"`
	} `xml:"set" json:"collection,omitempty"`

	// Root is the XML root element name (movie | tvshow | episodedetails),
	// filled in after decoding so callers can tell the schemas apart.
	Root string `xml:"-" json:"root,omitempty"`
}

// imdbID returns the numeric IMDB id (no "tt" prefix), or "" if the NFO carries
// none. It tolerates the tag-name variance across NFO schemas.
func (n *nfoData) imdbID() string {
	for _, c := range []string{n.IMDBID, n.IMDBIDAlt, n.GenericID} {
		c = strings.TrimSpace(c)
		if strings.HasPrefix(c, "tt") {
			return strings.TrimPrefix(c, "tt")
		}
	}
	return ""
}

// passthroughCharset lets the XML decoder accept declared charsets it doesn't
// natively support (e.g. windows-1252) by reading the bytes as-is rather than
// failing outright — NFO metadata is overwhelmingly ASCII/UTF-8.
func passthroughCharset(_ string, input io.Reader) (io.Reader, error) {
	return input, nil
}

// parseNFOBytes decodes NFO XML and records its root element name.
func parseNFOBytes(data []byte) (*nfoData, error) {
	var n nfoData
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.CharsetReader = passthroughCharset
	if err := dec.Decode(&n); err != nil {
		return nil, err
	}

	d2 := xml.NewDecoder(bytes.NewReader(data))
	d2.CharsetReader = passthroughCharset
	for {
		tok, err := d2.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok {
			n.Root = se.Name.Local
			break
		}
	}
	return &n, nil
}

// parseNFO reads and decodes an NFO file from disk.
func parseNFO(path string) (*nfoData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseNFOBytes(data)
}

// findNFO returns the path to the metadata NFO for a media folder, or "" if
// none exists. Series use tvshow.nfo; movies prefer movie.nfo and fall back to
// any "<video>.nfo" sidecar. entries is the already-read directory listing.
func findNFO(dir, typ string, entries []os.DirEntry) string {
	names := make(map[string]string, len(entries)) // lowercased -> actual
	for _, e := range entries {
		if !e.IsDir() {
			names[strings.ToLower(e.Name())] = e.Name()
		}
	}
	if typ == "series" {
		if a, ok := names["tvshow.nfo"]; ok {
			return filepath.Join(dir, a)
		}
		return ""
	}
	if a, ok := names["movie.nfo"]; ok {
		return filepath.Join(dir, a)
	}
	for lower, actual := range names {
		if strings.HasSuffix(lower, ".nfo") {
			return filepath.Join(dir, actual)
		}
	}
	return ""
}

// artItem is one image found in a media folder, ready for the viewer carousel.
type artItem struct {
	Name  string `json:"name"`  // bare filename, e.g. "folder.jpg"
	Label string `json:"label"` // friendly label, e.g. "Poster"
}

var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".bmp": true, ".gif": true,
}

var seasonPosterRe = regexp.MustCompile(`^season(\d+)-poster$`)

// artLabelAndRank maps a known Kodi/Jellyfin art filename to a friendly label
// and a sort priority (lower = shown first). Unknown images keep their filename
// and sort last.
func artLabelAndRank(name string) (string, int) {
	stem := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	switch stem {
	case "poster", "folder", "cover":
		return "Poster", 0
	case "keyart":
		return "Key Art", 1
	case "backdrop", "fanart", "background":
		return "Backdrop", 2
	case "landscape":
		return "Landscape", 3
	case "banner":
		return "Banner", 4
	case "thumb":
		return "Thumbnail", 5
	case "logo", "clearlogo":
		return "Logo", 6
	case "clearart":
		return "Clear Art", 7
	case "disc", "discart":
		return "Disc", 8
	}
	if m := seasonPosterRe.FindStringSubmatch(stem); m != nil {
		n, _ := strconv.Atoi(m[1])
		return fmt.Sprintf("Season %d Poster", n), 9
	}
	return name, 10
}

// discoverArt lists the image files sitting directly in a media folder, ordered
// poster-first for the carousel. Season/episode subfolders are ignored.
func discoverArt(dir string) []artItem {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type ranked struct {
		item artItem
		rank int
	}
	var items []ranked
	for _, e := range entries {
		if e.IsDir() || !imageExts[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		label, rank := artLabelAndRank(e.Name())
		items = append(items, ranked{artItem{Name: e.Name(), Label: label}, rank})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].rank != items[j].rank {
			return items[i].rank < items[j].rank
		}
		return items[i].item.Name < items[j].item.Name
	})
	out := make([]artItem, len(items))
	for i, r := range items {
		out[i] = r.item
	}
	return out
}

// nfoInfo holds the scan-time fields extracted from a media NFO.
type nfoInfo struct {
	path   string
	year   int
	status string
	imdb   string // numeric, no "tt" prefix
	title  string
}

// parseMediaNFO locates and parses the metadata NFO for a media folder,
// returning nil when the folder has no NFO. It reads the directory itself so it
// can run independently of the file-scan pass.
func parseMediaNFO(dir, typ string) *nfoInfo {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	path := findNFO(dir, typ, entries)
	if path == "" {
		return nil
	}
	n, err := parseNFO(path)
	if err != nil {
		// Record the path anyway so the row still offers an NFO button; the
		// viewer surfaces the parse error on open.
		return &nfoInfo{path: path}
	}
	return &nfoInfo{
		path:   path,
		year:   n.Year,
		status: strings.TrimSpace(n.Status),
		imdb:   n.imdbID(),
		title:  strings.TrimSpace(n.Title),
	}
}
