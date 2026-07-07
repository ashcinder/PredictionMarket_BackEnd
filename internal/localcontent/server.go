package localcontent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const LocalCIDPrefix = "local-v1-"

var localCIDPattern = regexp.MustCompile(`^local-v1-[0-9a-f]{64}$`)

type Server struct {
	root string
}

func NewServer(root string) *Server {
	return &Server{root: root}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/ipfs/add", s.handleAdd)
	mux.HandleFunc("GET /api/v1/ipfs/{cid}", s.handleGet)
}

func (s *Server) handleAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		http.Error(w, "invalid multipart upload", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "multipart field file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, 16<<20))
	if err != nil {
		http.Error(w, "read upload failed", http.StatusBadRequest)
		return
	}
	if len(data) == 0 {
		http.Error(w, "uploaded file is empty", http.StatusBadRequest)
		return
	}

	cid := contentCID(data)
	if err := s.writeContent(cid, data); err != nil {
		http.Error(w, "store upload failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"Name": safeUploadName(header.Filename),
		"Hash": cid,
		"Size": fmt.Sprintf("%d", len(data)),
	}); err != nil {
		http.Error(w, "encode response failed", http.StatusInternalServerError)
	}
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	cid := strings.TrimSpace(r.PathValue("cid"))
	path, err := s.contentPath(cid)
	if err != nil {
		http.Error(w, "invalid cid", http.StatusBadRequest)
		return
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		http.Error(w, "cid not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "read cid failed", http.StatusInternalServerError)
		return
	}

	if contentType := http.DetectContentType(data); contentType != "application/octet-stream" {
		w.Header().Set("Content-Type", contentType)
	} else if ext := filepath.Ext(cid); ext != "" {
		w.Header().Set("Content-Type", mime.TypeByExtension(ext))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) writeContent(cid string, data []byte) error {
	path, err := s.contentPath(cid)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (s *Server) contentPath(cid string) (string, error) {
	if !localCIDPattern.MatchString(cid) {
		return "", fmt.Errorf("invalid local cid %q", cid)
	}
	if strings.TrimSpace(s.root) == "" {
		return "", errors.New("local content root is empty")
	}
	return filepath.Join(s.root, cid), nil
}

func contentCID(data []byte) string {
	sum := sha256.Sum256(data)
	return LocalCIDPrefix + hex.EncodeToString(sum[:])
}

func safeUploadName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == string(filepath.Separator) {
		return "file"
	}
	if name == "" {
		return "file"
	}
	return name
}
