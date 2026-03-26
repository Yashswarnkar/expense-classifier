package categories

import (
	"bufio"
	"os"
	"strings"
)

// Category represents a spending category with optional hint keywords that
// are forwarded to the LLM prompt for better classification accuracy.
type Category struct {
	Name     string
	Keywords []string
}

// Load reads categories from a plain-text file.
//
// Format (one category per line):
//
//	# comment lines are ignored
//	Groceries: supermarket, kirana, bigbasket, zepto, blinkit
//	Dining Out: swiggy, zomato, restaurant, cafe
//	Transport
//
// The keyword list is optional — the LLM will still classify using the
// transaction description alone when no keywords are provided.
func Load(filePath string) ([]Category, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cats []Category
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		cat := Category{}
		if idx := strings.Index(line, ":"); idx != -1 {
			cat.Name = strings.TrimSpace(line[:idx])
			kwStr := strings.TrimSpace(line[idx+1:])
			for _, kw := range strings.Split(kwStr, ",") {
				kw = strings.TrimSpace(kw)
				if kw != "" {
					cat.Keywords = append(cat.Keywords, kw)
				}
			}
		} else {
			cat.Name = line
		}

		if cat.Name != "" {
			cats = append(cats, cat)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Ensure Uncategorized is always available as a fallback.
	hasUncategorized := false
	for _, c := range cats {
		if strings.EqualFold(c.Name, "uncategorized") {
			hasUncategorized = true
			break
		}
	}
	if !hasUncategorized {
		cats = append(cats, Category{Name: "Uncategorized"})
	}

	return cats, nil
}

// Names extracts just the category names — handy for building prompts.
func Names(cats []Category) []string {
	names := make([]string, len(cats))
	for i, c := range cats {
		names[i] = c.Name
	}
	return names
}
