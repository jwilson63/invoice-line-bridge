package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// LineItem is the in-memory representation shared by both formats. Money is
// kept as integer cents throughout so conversions never round-trip through
// float64 for a value someone is going to bill a customer for.
type LineItem struct {
	InvoiceID      string
	LineNo         int
	SKU            string
	Description    string
	Quantity       float64
	UnitPriceCents int64
	TaxRateBps     int64
	Currency       string
}

// TotalCents is quantity * unit price, rounded to the nearest cent, half
// away from zero. Quantity is a float (hours, weight, partial units), so the
// product can land on a fractional cent even though UnitPriceCents can't.
func (li LineItem) TotalCents() int64 {
	return int64(math.Round(float64(li.UnitPriceCents) * li.Quantity))
}

var csvHeader = []string{"invoice_id", "line_no", "sku", "description", "qty", "unit_price", "tax_rate", "currency"}

// ParseCSV reads the flat CSV export format described in the README.
func ParseCSV(r io.Reader) ([]LineItem, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // checked manually below so the error names the bad row
	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading csv: %w", err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	if !equalHeader(records[0], csvHeader) {
		return nil, fmt.Errorf("unexpected csv header: got %v, want %v", records[0], csvHeader)
	}

	items := make([]LineItem, 0, len(records)-1)
	for i, row := range records[1:] {
		rowNum := i + 2 // 1-indexed, plus the header row
		if len(row) != len(csvHeader) {
			return nil, fmt.Errorf("row %d: expected %d fields, got %d", rowNum, len(csvHeader), len(row))
		}

		lineNo, err := strconv.Atoi(strings.TrimSpace(row[1]))
		if err != nil {
			return nil, fmt.Errorf("row %d: invalid line_no %q: %w", rowNum, row[1], err)
		}
		qty, err := strconv.ParseFloat(strings.TrimSpace(row[4]), 64)
		if err != nil {
			return nil, fmt.Errorf("row %d: invalid qty %q: %w", rowNum, row[4], err)
		}
		unitPriceCents, err := parseDecimalTo2Places(row[5])
		if err != nil {
			return nil, fmt.Errorf("row %d: invalid unit_price %q: %w", rowNum, row[5], err)
		}
		taxRateBps, err := parseDecimalTo2Places(row[6])
		if err != nil {
			return nil, fmt.Errorf("row %d: invalid tax_rate %q: %w", rowNum, row[6], err)
		}

		items = append(items, LineItem{
			InvoiceID:      strings.TrimSpace(row[0]),
			LineNo:         lineNo,
			SKU:            strings.TrimSpace(row[2]),
			Description:    row[3],
			Quantity:       qty,
			UnitPriceCents: unitPriceCents,
			TaxRateBps:     taxRateBps,
			Currency:       strings.ToUpper(strings.TrimSpace(row[7])),
		})
	}
	return items, nil
}

func equalHeader(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// WriteCSV writes items back out in the flat CSV export format.
func WriteCSV(w io.Writer, items []LineItem) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(csvHeader); err != nil {
		return err
	}
	for _, li := range items {
		row := []string{
			li.InvoiceID,
			strconv.Itoa(li.LineNo),
			li.SKU,
			li.Description,
			strconv.FormatFloat(li.Quantity, 'f', -1, 64),
			formatDecimalFrom2Places(li.UnitPriceCents),
			formatDecimalFrom2Places(li.TaxRateBps),
			li.Currency,
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// parseDecimalTo2Places parses a plain decimal string ("19.99", "7.5", "-3")
// into a scaled integer with 2 implied decimal places (1999, 750, -300). It's
// used for both unit_price (dollars -> cents) and tax_rate (percent -> basis
// points), since both are "value * 100" under the hood. Extra digits beyond
// 2 decimal places round half away from zero. An empty string is zero, since
// tax_rate is routinely left blank on untaxed lines.
func parseDecimalTo2Places(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	neg := false
	switch {
	case strings.HasPrefix(s, "-"):
		neg = true
		s = s[1:]
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	}

	intPart, fracPart, _ := strings.Cut(s, ".")
	if intPart == "" {
		intPart = "0"
	}
	if !isDigits(intPart) || !isDigits(fracPart) {
		return 0, fmt.Errorf("not a decimal number: %q", s)
	}

	intVal, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("not a decimal number: %q", s)
	}

	for len(fracPart) < 3 {
		fracPart += "0"
	}
	kept, err := strconv.ParseInt(fracPart[:2], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("not a decimal number: %q", s)
	}

	result := intVal*100 + kept
	if fracPart[2] >= '5' {
		result++
	}
	if neg {
		result = -result
	}
	return result, nil
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func formatDecimalFrom2Places(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := fmt.Sprintf("%d.%02d", v/100, v%100)
	if neg {
		s = "-" + s
	}
	return s
}

// Invoice and JSONLineItem are the grouped JSON representation described in
// the README.
type Invoice struct {
	InvoiceID string         `json:"invoice_id"`
	LineItems []JSONLineItem `json:"line_items"`
}

type JSONLineItem struct {
	LineNo         int     `json:"line_no"`
	SKU            string  `json:"sku"`
	Description    string  `json:"description"`
	Quantity       float64 `json:"quantity"`
	UnitPriceCents int64   `json:"unit_price_cents"`
	TaxRateBps     int64   `json:"tax_rate_bps"`
	Currency       string  `json:"currency"`
	TotalCents     int64   `json:"total_cents"`
}

// GroupByInvoice groups flat line items under their invoice, preserving the
// order invoices and lines first appeared in (map iteration order can't be
// trusted for that).
func GroupByInvoice(items []LineItem) []Invoice {
	order := make([]string, 0)
	byInvoice := make(map[string][]LineItem)
	for _, li := range items {
		if _, ok := byInvoice[li.InvoiceID]; !ok {
			order = append(order, li.InvoiceID)
		}
		byInvoice[li.InvoiceID] = append(byInvoice[li.InvoiceID], li)
	}

	invoices := make([]Invoice, 0, len(order))
	for _, id := range order {
		lines := byInvoice[id]
		jsonLines := make([]JSONLineItem, len(lines))
		for i, li := range lines {
			jsonLines[i] = JSONLineItem{
				LineNo:         li.LineNo,
				SKU:            li.SKU,
				Description:    li.Description,
				Quantity:       li.Quantity,
				UnitPriceCents: li.UnitPriceCents,
				TaxRateBps:     li.TaxRateBps,
				Currency:       li.Currency,
				TotalCents:     li.TotalCents(),
			}
		}
		invoices = append(invoices, Invoice{InvoiceID: id, LineItems: jsonLines})
	}
	return invoices
}

// Flatten is the inverse of GroupByInvoice.
func Flatten(invoices []Invoice) []LineItem {
	items := make([]LineItem, 0)
	for _, inv := range invoices {
		for _, jl := range inv.LineItems {
			items = append(items, LineItem{
				InvoiceID:      inv.InvoiceID,
				LineNo:         jl.LineNo,
				SKU:            jl.SKU,
				Description:    jl.Description,
				Quantity:       jl.Quantity,
				UnitPriceCents: jl.UnitPriceCents,
				TaxRateBps:     jl.TaxRateBps,
				Currency:       jl.Currency,
			})
		}
	}
	return items
}

// CSVToJSON reads CSV from r and writes the grouped JSON format to w.
func CSVToJSON(r io.Reader, w io.Writer) error {
	items, err := ParseCSV(r)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(GroupByInvoice(items))
}

// JSONToCSV reads the grouped JSON format from r and writes CSV to w.
func JSONToCSV(r io.Reader, w io.Writer) error {
	var invoices []Invoice
	if err := json.NewDecoder(r).Decode(&invoices); err != nil {
		return fmt.Errorf("reading json: %w", err)
	}
	return WriteCSV(w, Flatten(invoices))
}
