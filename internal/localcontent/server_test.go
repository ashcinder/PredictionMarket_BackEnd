package localcontent

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerStoresMultipartFileAndServesByHash(t *testing.T) {
	server := NewServer(t.TempDir())
	mux := http.NewServeMux()
	server.Register(mux)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(`{"condition":"gold above 2400"}`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	upload := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ipfs/add", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	mux.ServeHTTP(upload, req)
	if upload.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", upload.Code, upload.Body.String())
	}

	var response struct {
		Hash string `json:"Hash"`
	}
	if err := json.Unmarshal(upload.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(response.Hash, LocalCIDPrefix) {
		t.Fatalf("hash=%q, want local cid prefix", response.Hash)
	}

	download := httptest.NewRecorder()
	mux.ServeHTTP(download, httptest.NewRequest(http.MethodGet, "/api/v1/ipfs/"+response.Hash, nil))
	if download.Code != http.StatusOK {
		t.Fatalf("download status=%d body=%s", download.Code, download.Body.String())
	}
	got, err := io.ReadAll(download.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"condition":"gold above 2400"}` {
		t.Fatalf("download body=%q", got)
	}
}

func TestServerRejectsMissingMultipartFile(t *testing.T) {
	server := NewServer(t.TempDir())
	mux := http.NewServeMux()
	server.Register(mux)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	upload := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ipfs/add", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	mux.ServeHTTP(upload, req)
	if upload.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", upload.Code)
	}
}

func TestServerRejectsUnsafeCID(t *testing.T) {
	server := NewServer(t.TempDir())
	mux := http.NewServeMux()
	server.Register(mux)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/ipfs/not-a-local-cid", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", response.Code)
	}
}
