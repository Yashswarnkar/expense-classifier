package pdf

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/ledongthuc/pdf"
	pdfapi "github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// TextElement is a single piece of text extracted from a PDF page together
// with its position in the PDF coordinate system (origin at bottom-left,
// Y increases upward).
type TextElement struct {
	X, Y     float64
	Content  string
	FontSize float64
	Page     int
}

// PageContent holds all text elements for one page.
type PageContent struct {
	PageNum  int
	Elements []TextElement
}

// DecryptAndExtract decrypts a PDF when a password is provided, then
// extracts all text elements with their positions.
// When password is empty the PDF is read directly.
func DecryptAndExtract(inputPath, password string) ([]PageContent, error) {
	pdfPath := inputPath

	if password != "" {
		tmp, err := os.CreateTemp("", "expense-decrypted-*.pdf")
		if err != nil {
			return nil, fmt.Errorf("create temp file: %w", err)
		}
		tmpPath := tmp.Name()
		tmp.Close()
		defer os.Remove(tmpPath)

		conf := model.NewDefaultConfiguration()
		conf.UserPW = password
		conf.OwnerPW = password

		if err := pdfapi.DecryptFile(inputPath, tmpPath, conf); err != nil {
			return nil, fmt.Errorf("decrypt PDF %q: %w", inputPath, err)
		}
		pdfPath = tmpPath
	}

	return extractText(pdfPath)
}

func extractText(path string) ([]PageContent, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open PDF: %w", err)
	}
	defer f.Close()

	var pages []PageContent
	for pageNum := 1; pageNum <= r.NumPage(); pageNum++ {
		p := r.Page(pageNum)
		if p.V.IsNull() {
			continue
		}

		var elems []TextElement
		for _, t := range p.Content().Text {
			if strings.TrimSpace(t.S) == "" {
				continue
			}
			elems = append(elems, TextElement{
				X:        t.X,
				Y:        t.Y,
				Content:  t.S,
				FontSize: t.FontSize,
				Page:     pageNum,
			})
		}
		pages = append(pages, PageContent{PageNum: pageNum, Elements: elems})
	}
	return pages, nil
}

// GroupByRows clusters text elements that share approximately the same Y
// coordinate into rows, then sorts each row left-to-right by X.
// tolerance is in PDF points (1 pt ≈ 0.35 mm); 3–5 works well in practice.
func GroupByRows(elements []TextElement, tolerance float64) [][]TextElement {
	if len(elements) == 0 {
		return nil
	}

	// Sort top-to-bottom (highest Y first in PDF space) then left-to-right.
	sorted := make([]TextElement, len(elements))
	copy(sorted, elements)
	sort.Slice(sorted, func(i, j int) bool {
		dy := sorted[i].Y - sorted[j].Y
		if math.Abs(dy) > tolerance {
			return dy > 0 // higher Y = earlier in reading order
		}
		return sorted[i].X < sorted[j].X
	})

	var rows [][]TextElement
	var cur []TextElement
	refY := sorted[0].Y

	for _, el := range sorted {
		if math.Abs(el.Y-refY) <= tolerance {
			cur = append(cur, el)
		} else {
			if len(cur) > 0 {
				rows = append(rows, sortByX(cur))
			}
			cur = []TextElement{el}
			refY = el.Y
		}
	}
	if len(cur) > 0 {
		rows = append(rows, sortByX(cur))
	}
	return rows
}

func sortByX(row []TextElement) []TextElement {
	sort.Slice(row, func(i, j int) bool { return row[i].X < row[j].X })
	return row
}

// RowText joins all content in a row with a single space.
func RowText(row []TextElement) string {
	parts := make([]string, len(row))
	for i, el := range row {
		parts[i] = el.Content
	}
	return strings.Join(parts, " ")
}

// AllElements flattens all pages into a single slice.
func AllElements(pages []PageContent) []TextElement {
	var all []TextElement
	for _, p := range pages {
		all = append(all, p.Elements...)
	}
	return all
}
