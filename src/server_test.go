package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// newTestServer creates a minimal server backed by a fresh in-memory DB.
func newTestServer(t *testing.T) *server {
	t.Helper()
	db := newTestDB(t)
	s := &server{db: db, listeners: make(map[chan bool]bool)}
	ctx, cancel := context.WithCancel(context.Background())
	s.ctx = ctx
	s.cancel = cancel
	s.workers.Store(2)
	t.Cleanup(cancel)
	return s
}

func insertTestMedia(t *testing.T, s *server, name, typ string) int64 {
	t.Helper()
	id, err := upsertMedia(s.db, "/media/"+name, name, typ)
	if err != nil {
		t.Fatalf("upsertMedia: %v", err)
	}
	return id
}

func insertTestFile(t *testing.T, s *server, mediaID int64, path string, hasSub bool) {
	t.Helper()
	subName := ""
	if hasSub {
		subName = "sub.srt"
	}
	if err := upsertFile(s.db, mediaID, path, nil, nil, hasSub, subName); err != nil {
		t.Fatalf("upsertFile: %v", err)
	}
}

// ── /api/report ───────────────────────────────────────────────────────────────

func TestHandleReport_ReturnsSummariesOnly(t *testing.T) {
	s := newTestServer(t)
	mid := insertTestMedia(t, s, "Inception (2010)", "movie")
	insertTestFile(t, s, mid, "/media/Inception (2010)/Inception.mkv", true)
	insertTestFile(t, s, mid, "/media/Inception (2010)/Extras.mkv", false)

	req := httptest.NewRequest("GET", "/api/report", nil)
	w := httptest.NewRecorder()
	s.handleReport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}

	var result []apiMedia
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("want 1 media item, got %d", len(result))
	}

	m := result[0]
	if m.TotalCount != 2 {
		t.Errorf("want total_count=2, got %d", m.TotalCount)
	}
	if m.SubtitlesCount != 1 {
		t.Errorf("want subtitles_count=1, got %d", m.SubtitlesCount)
	}
	if len(m.Files) != 0 {
		t.Errorf("report should not include file details, got %d files", len(m.Files))
	}
}

func TestHandleReport_Empty(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/report", nil)
	w := httptest.NewRecorder()
	s.handleReport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var result []apiMedia
	json.NewDecoder(w.Body).Decode(&result)
	if len(result) != 0 {
		t.Errorf("want empty slice, got %d items", len(result))
	}
}

// ── /api/media/{id}/files ─────────────────────────────────────────────────────

func TestHandleMediaFiles_ReturnsFiles(t *testing.T) {
	s := newTestServer(t)
	mid := insertTestMedia(t, s, "Breaking Bad (2008)", "series")
	insertTestFile(t, s, mid, "/media/Breaking Bad (2008)/S01E01.mkv", true)
	insertTestFile(t, s, mid, "/media/Breaking Bad (2008)/S01E02.mkv", false)

	req := httptest.NewRequest("GET", "/api/media/1/files", nil)
	req.SetPathValue("id", itoa(mid))
	w := httptest.NewRecorder()
	s.handleMediaFiles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}

	var files []apiFile
	if err := json.NewDecoder(w.Body).Decode(&files); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("want 2 files, got %d", len(files))
	}
	if !files[0].HasSubtitle {
		t.Errorf("first file should have subtitle")
	}
	if files[1].HasSubtitle {
		t.Errorf("second file should not have subtitle")
	}
}

func TestHandleMediaFiles_UnknownID_ReturnsEmpty(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/media/999/files", nil)
	req.SetPathValue("id", "999")
	w := httptest.NewRecorder()
	s.handleMediaFiles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var files []apiFile
	json.NewDecoder(w.Body).Decode(&files)
	if len(files) != 0 {
		t.Errorf("want empty slice for unknown id, got %d", len(files))
	}
}

func TestHandleMediaFiles_InvalidID_Returns400(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/media/abc/files", nil)
	req.SetPathValue("id", "abc")
	w := httptest.NewRecorder()
	s.handleMediaFiles(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestHandleReport_CountsMatchFiles(t *testing.T) {
	s := newTestServer(t)
	// Two media items: one complete, one partial.
	m1 := insertTestMedia(t, s, "Movie A (2020)", "movie")
	insertTestFile(t, s, m1, "/media/Movie A (2020)/a.mkv", true)

	m2 := insertTestMedia(t, s, "Movie B (2021)", "movie")
	insertTestFile(t, s, m2, "/media/Movie B (2021)/b1.mkv", true)
	insertTestFile(t, s, m2, "/media/Movie B (2021)/b2.mkv", false)

	req := httptest.NewRequest("GET", "/api/report", nil)
	w := httptest.NewRecorder()
	s.handleReport(w, req)

	var result []apiMedia
	json.NewDecoder(w.Body).Decode(&result)
	if len(result) != 2 {
		t.Fatalf("want 2 items, got %d", len(result))
	}
	byName := map[string]apiMedia{}
	for _, m := range result {
		byName[m.Name] = m
	}
	if a := byName["Movie A (2020)"]; a.TotalCount != 1 || a.SubtitlesCount != 1 {
		t.Errorf("Movie A: want 1/1, got %d/%d", a.SubtitlesCount, a.TotalCount)
	}
	if b := byName["Movie B (2021)"]; b.TotalCount != 2 || b.SubtitlesCount != 1 {
		t.Errorf("Movie B: want 1/2, got %d/%d", b.SubtitlesCount, b.TotalCount)
	}
}

// itoa is a small helper to avoid importing strconv in tests.
func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
