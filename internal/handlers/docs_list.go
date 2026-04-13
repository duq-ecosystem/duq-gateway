package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"

	"duq-gateway/internal/config"
)

// DocFile represents a single documentation file
type DocFile struct {
	Name  string `json:"name"`  // Filename with extension
	Path  string `json:"path"`  // Web path (e.g., "/docs/api")
	Title string `json:"title"` // Display title
}

// DocCategory represents a group of related docs
type DocCategory struct {
	Category string    `json:"category"`
	Files    []DocFile `json:"files"`
}

// DocsList returns structured list of available documentation files
func DocsList(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Println("[docs-list] Scanning documentation directory")

		// Docs are in /opt/obsidian-vault/Coding/duq/
		docsPath := "/opt/obsidian-vault/Coding/duq"

		// Check if directory exists
		if _, err := os.Stat(docsPath); os.IsNotExist(err) {
			log.Printf("[docs-list] Directory not found: %s", docsPath)
			http.Error(w, "Documentation directory not found", http.StatusNotFound)
			return
		}

		// Scan directory
		entries, err := os.ReadDir(docsPath)
		if err != nil {
			log.Printf("[docs-list] Failed to read directory: %v", err)
			http.Error(w, "Failed to read documentation", http.StatusInternalServerError)
			return
		}

		// Categorize files
		categories := make(map[string][]DocFile)

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}

			filename := entry.Name()

			// Skip archive files (starting with _) and backups
			if strings.HasPrefix(filename, "_") || strings.HasSuffix(filename, ".backup") {
				log.Printf("[docs-list] Skipping: %s", filename)
				continue
			}

			basename := strings.TrimSuffix(filename, ".md")

			// Determine category from filename prefix or content
			category := categorizeDoc(basename)

			// Generate title from filename
			title := generateTitle(basename)

			// Web path for frontend routing
			webPath := "/docs/" + strings.ToLower(strings.ReplaceAll(basename, " ", "-"))

			categories[category] = append(categories[category], DocFile{
				Name:  filename,
				Path:  webPath,
				Title: title,
			})
		}

		// Convert map to sorted slice
		result := make([]DocCategory, 0, len(categories))
		for category, files := range categories {
			// Sort files alphabetically
			sort.Slice(files, func(i, j int) bool {
				return files[i].Title < files[j].Title
			})

			result = append(result, DocCategory{
				Category: category,
				Files:    files,
			})
		}

		// Sort categories
		sort.Slice(result, func(i, j int) bool {
			// Priority order
			order := map[string]int{
				"Getting Started": 1,
				"API":             2,
				"Services":        3,
				"Architecture":    4,
				"Guides":          5,
				"Reference":       6,
			}

			iOrder := order[result[i].Category]
			jOrder := order[result[j].Category]

			if iOrder == 0 {
				iOrder = 99
			}
			if jOrder == 0 {
				jOrder = 99
			}

			if iOrder != jOrder {
				return iOrder < jOrder
			}

			return result[i].Category < result[j].Category
		})

		log.Printf("[docs-list] Found %d categories, %d total files", len(result), countFiles(result))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

// categorizeDoc determines category based on filename patterns
func categorizeDoc(basename string) string {
	lower := strings.ToLower(basename)

	// API docs
	if strings.Contains(lower, "api") || strings.Contains(lower, "endpoint") {
		return "API"
	}

	// Services
	if strings.Contains(lower, "cortex") || strings.Contains(lower, "gws") ||
		strings.Contains(lower, "telegram") || strings.Contains(lower, "obsidian") ||
		strings.Contains(lower, "workflow") || strings.Contains(lower, "heartbeat") {
		return "Services"
	}

	// Architecture
	if strings.Contains(lower, "architecture") || strings.Contains(lower, "design") ||
		strings.Contains(lower, "database") || strings.Contains(lower, "deployment") {
		return "Architecture"
	}

	// Guides
	if strings.Contains(lower, "guide") || strings.Contains(lower, "tutorial") ||
		strings.Contains(lower, "howto") || strings.Contains(lower, "setup") {
		return "Guides"
	}

	// Getting Started
	if strings.Contains(lower, "readme") || strings.Contains(lower, "intro") ||
		strings.Contains(lower, "quickstart") || strings.Contains(lower, "getting") {
		return "Getting Started"
	}

	// MCP Tools
	if strings.Contains(lower, "mcp") || strings.Contains(lower, "tool") {
		return "Reference"
	}

	// Default
	return "Reference"
}

// generateTitle converts filename to display title
func generateTitle(basename string) string {
	// Remove common prefixes
	title := strings.TrimPrefix(basename, "duq-")
	title = strings.TrimPrefix(title, "not-that-duq-")

	// Replace dashes/underscores with spaces
	title = strings.ReplaceAll(title, "-", " ")
	title = strings.ReplaceAll(title, "_", " ")

	// Capitalize words
	words := strings.Fields(title)
	for i, word := range words {
		// Keep acronyms uppercase
		if len(word) <= 3 && strings.ToUpper(word) == word {
			continue
		}
		words[i] = strings.Title(strings.ToLower(word))
	}

	return strings.Join(words, " ")
}

// countFiles counts total files across all categories
func countFiles(categories []DocCategory) int {
	count := 0
	for _, cat := range categories {
		count += len(cat.Files)
	}
	return count
}
