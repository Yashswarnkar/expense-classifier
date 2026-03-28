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
	// StreamIdx records the element's original position in the PDF content
	// stream. Preserving this order is critical for PDFs that use RTL-encoded
	// fonts where X positions are reversed per character (AU Bank uses this).
	StreamIdx int
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
		for idx, t := range p.Content().Text {
			if strings.TrimSpace(t.S) == "" {
				continue
			}
			elems = append(elems, TextElement{
				X:         t.X,
				Y:         t.Y,
				Content:   t.S,
				FontSize:  t.FontSize,
				Page:      pageNum,
				StreamIdx: idx,
			})
		}
		pages = append(pages, PageContent{PageNum: pageNum, Elements: elems})
	}
	return pages, nil
}

// GroupByRows clusters text elements that share approximately the same Y
// coordinate into rows, then sorts each row left-to-right by X.
// Use this for PDFs with standard LTR fonts (HDFC, Amazon Pay ICICI).
// tolerance is in PDF points (1 pt ≈ 0.35 mm); 3–5 works well in practice.
func GroupByRows(elements []TextElement, tolerance float64) [][]TextElement {
	if len(elements) == 0 {
		return nil
	}

	sorted := make([]TextElement, len(elements))
	copy(sorted, elements)
	sort.Slice(sorted, func(i, j int) bool {
		dy := sorted[i].Y - sorted[j].Y
		if math.Abs(dy) > tolerance {
			return dy > 0
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

// GroupByRowsNaturalOrder groups elements by Y position but preserves the
// original PDF content stream order within each row.
//
// AU Bank PDFs use an RTL-encoded font where every character is a separate
// text element and X positions run right-to-left within each word. Sorting
// by X ascending produces reversed/garbled text. Using the content stream
// order (the order the PDF renderer draws characters) restores correct
// left-to-right reading order.
func GroupByRowsNaturalOrder(elements []TextElement, tolerance float64) [][]TextElement {
	if len(elements) == 0 {
		return nil
	}

	// Assign each element to a row bucket based on Y proximity.
	// We scan in stream order so each element joins the first row bucket
	// whose Y is within tolerance.
	type rowBucket struct {
		refY     float64
		elements []TextElement
	}
	var buckets []rowBucket

	for _, el := range elements {
		placed := false
		for i := range buckets {
			if math.Abs(el.Y-buckets[i].refY) <= tolerance {
				buckets[i].elements = append(buckets[i].elements, el)
				placed = true
				break
			}
		}
		if !placed {
			buckets = append(buckets, rowBucket{refY: el.Y, elements: []TextElement{el}})
		}
	}

	// Sort buckets top-to-bottom (highest Y = top of page in PDF space).
	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i].refY > buckets[j].refY
	})

	rows := make([][]TextElement, len(buckets))
	for i, b := range buckets {
		rows[i] = b.elements // natural stream order preserved
	}
	return rows
}

// ReconstructText rebuilds human-readable text from a row whose elements are
// in natural content stream order (e.g. from GroupByRowsNaturalOrder).
//
// For RTL-encoded fonts (AU Bank), characters within a word have decreasing X
// positions in stream order. A word boundary is signalled by either:
//   - X jumping UP (moving to the next column to the right), or
//   - X dropping by more than ~1.5× the normal inter-character step.
//
// Within a word the X step between adjacent chars is ~1×charW. At a word
// boundary the drop is ~charW (prev char) + space width (~0.3–0.5×charW),
// so word gaps land around 1.3–1.5×charW. A threshold of 1.5 catches those
// gaps without splitting tight ligatures inside words.
func ReconstructText(row []TextElement) string {
	if len(row) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(row[0].Content)

	for i := 1; i < len(row); i++ {
		prev := row[i-1]
		curr := row[i]

		charW := prev.FontSize * 0.55
		if charW < 3 {
			charW = 5
		}

		xDiff := curr.X - prev.X

		switch {
		case xDiff > charW:
			// X jumped right → moved to a new column or a wide gap → space
			sb.WriteByte(' ')
		case xDiff < -(charW * 1.5):
			// X dropped > 1.5× char width → word space within RTL column
			sb.WriteByte(' ')
		}
		sb.WriteString(curr.Content)
	}
	return sb.String()
}

func sortByX(row []TextElement) []TextElement {
	sort.Slice(row, func(i, j int) bool { return row[i].X < row[j].X })
	return row
}

// RowText joins all content in a row with a single space.
// Useful for keyword detection where exact spacing doesn't matter.
func RowText(row []TextElement) string {
	parts := make([]string, len(row))
	for i, el := range row {
		parts[i] = el.Content
	}
	return strings.Join(parts, " ")
}

// AllElements flattens all pages into a single slice, preserving per-page
// stream order.
func AllElements(pages []PageContent) []TextElement {
	var all []TextElement
	for _, p := range pages {
		all = append(all, p.Elements...)
	}
	return all
}
