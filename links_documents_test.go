package topolab

// Download writes the build's documents beside the file: the link route's 200
// names signed links to its columns.json and each notice file, fetched like the
// file (no API key), written as path+".columns.json" and
// path+".notices/<name>". A notice whose name is not a plain file name is
// skipped; a document that cannot be fetched fails the call. Archive writes the
// zip only: it holds the documents itself.

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	lkDocPrefix   = "/platform-staging-datasets/exports/" + lkTable + "/2026-12/standard/20261202T020000Z-a1b2c3d4-w1"
	lkColumnsPath = lkDocPrefix + "/columns.json"
	lkNoticePath  = lkDocPrefix + "/notices/osm.txt"
	lkColumns     = `{"tier":"standard","columns":[{"name":"location_name"}]}`
	lkNotice      = "OpenStreetMap contributors, ODbL 1.0\n"
)

func lkReadyWithDocuments(storage *lkServer, month, noticeName string, columns bool) lkReply {
	reply := lkReady(storage, month)
	documents := map[string]any{"columns": nil, "notices": []map[string]any{{"name": noticeName, "url": storage.URL + lkNoticePath + "?" + lkQuery}}}
	if columns {
		documents["columns"] = storage.URL + lkColumnsPath + "?" + lkQuery
	}
	reply.json.(map[string]any)["documents"] = documents
	return reply
}

func TestLinkDownloadWritesTheDocumentsBesideTheFile(t *testing.T) {
	storage := newLkServer(t, map[string][]lkReply{
		lkSigned:               one(lkReply{status: 200, body: lkLinkCSV}),
		"GET " + lkColumnsPath: one(lkReply{status: 200, body: lkColumns}),
		"GET " + lkNoticePath:  one(lkReply{status: 200, body: lkNotice}),
	})
	api := newLkServer(t, map[string][]lkReply{lkLink: one(lkReadyWithDocuments(storage, "2026-12", "osm.txt", true))})
	ds, _ := lkDataset(t, api)
	out := filepath.Join(t.TempDir(), "mcdonalds.csv")

	if err := ds.Download(context.Background(), out, "csv"); err != nil {
		t.Fatal(err)
	}

	if got := lkRead(t, out+".columns.json"); got != lkColumns {
		t.Fatalf("columns.json = %q", got)
	}
	if got := lkRead(t, filepath.Join(out+".notices", "osm.txt")); got != lkNotice {
		t.Fatalf("notice = %q", got)
	}
	for _, route := range []string{"GET " + lkColumnsPath, "GET " + lkNoticePath} {
		seen := storage.first(route)
		if seen.header.Get("X-API-Key") != "" {
			t.Fatalf("%s carried the API key", route)
		}
		if !strings.HasSuffix(seen.uri, "?"+lkQuery) {
			t.Fatalf("%s lost its signed query: %s", route, seen.uri)
		}
	}
}

func TestLinkDownloadSkipsANoticeNameThatIsNotAPlainFileName(t *testing.T) {
	storage := newLkServer(t, map[string][]lkReply{lkSigned: one(lkReply{status: 200, body: lkLinkCSV})})
	api := newLkServer(t, map[string][]lkReply{lkLink: one(lkReadyWithDocuments(storage, "2026-12", "../escape.txt", false))})
	ds, _ := lkDataset(t, api)
	dir := t.TempDir()

	if err := ds.Download(context.Background(), filepath.Join(dir, "mcdonalds.csv"), "csv"); err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(dir)
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "mcdonalds.csv" {
		t.Fatalf("files = %v", names)
	}
	if storage.count("GET "+lkNoticePath) != 0 {
		t.Fatal("an unsafe notice name was fetched")
	}
}

func TestLinkDownloadFailsWhenADocumentCannotBeFetched(t *testing.T) {
	storage := newLkServer(t, map[string][]lkReply{
		lkSigned:               one(lkReply{status: 200, body: lkLinkCSV}),
		"GET " + lkColumnsPath: one(lkReply{status: 200, body: lkColumns}),
		"GET " + lkNoticePath:  one(lkReply{status: 403, body: "<Error>SignatureDoesNotMatch</Error>"}),
	})
	api := newLkServer(t, map[string][]lkReply{lkLink: one(lkReadyWithDocuments(storage, "2026-12", "osm.txt", true))})
	ds, _ := lkDataset(t, api)

	err := ds.Download(context.Background(), filepath.Join(t.TempDir(), "m.csv"), "csv")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "X-Goog-Signature") {
		t.Fatalf("the error quotes the signed query: %v", err)
	}
}

func TestLinkArchiveWritesTheZipOnly(t *testing.T) {
	storage := newLkServer(t, map[string][]lkReply{lkSigned: one(lkReply{status: 200, body: lkZip})})
	api := newLkServer(t, map[string][]lkReply{lkLink: one(lkReadyWithDocuments(storage, "2026-11", "osm.txt", true))})
	ds, _ := lkDataset(t, api)
	dir := t.TempDir()

	if err := ds.Archive(context.Background(), filepath.Join(dir, "november.zip"), "2026-11", "csv"); err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "november.zip" {
		t.Fatalf("files = %v", entries)
	}
	if storage.count("GET "+lkColumnsPath) != 0 || storage.count("GET "+lkNoticePath) != 0 {
		t.Fatal("archive fetched the documents")
	}
}
