package handlers

import (
	"io/ioutil"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"duq-gateway/internal/config"
)

// GET /api/docs/:name/content - Returns raw markdown content
func DocsContent(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get doc name from path
		docName := r.PathValue("name")
		if docName == "" {
			http.Error(w, "Document name required", http.StatusBadRequest)
			return
		}

		// Security: prevent path traversal
		if strings.Contains(docName, "..") || strings.Contains(docName, "/") {
			http.Error(w, "Invalid document name", http.StatusBadRequest)
			return
		}

		// Add .md extension if not present
		if !strings.HasSuffix(docName, ".md") {
			docName += ".md"
		}

		// Build full path using configured docs path
		docsPath := cfg.DocsPath
		if docsPath == "" {
			docsPath = "/opt/obsidian-vault/Coding/duq" // fallback for backwards compatibility
		}
		fullPath := filepath.Join(docsPath, docName)

		// Read file
		content, err := ioutil.ReadFile(fullPath)
		if err != nil {
			log.Printf("[docs-content] File not found: %s", fullPath)
			http.Error(w, "Document not found", http.StatusNotFound)
			return
		}

		// Return as plain text
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(content)

		log.Printf("[docs-content] Served: %s (%d bytes)", docName, len(content))
	}
}
