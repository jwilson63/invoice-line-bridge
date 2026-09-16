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

// csvFields lists the internal field keys in canonical column order. A
// ColumnMap translates these keys to the actual column names in a given CSV
// file; with no mapping, the key doubles as the default column name.
var csvFields = []string{"invoice_id", "line_no", "sku", "description", "qty", "unit_price", "tax_rate", "currency"}

// optionalCSVFields lists fields that can be left out of a CSV header
// entirely (not just left blank per row), because they have a sensible zero
// value: an empty description, an untaxed line, or an unspecified currency.
// invoice_id, line_no, sku, qty, and unit_price have no such default, so a
// source file missing one of those is a real error rather than something to
// paper over.
var optionalCSVFields = map[string]bool{
	"description": true,
	"tax_rate":    true,
	"currency":    true,
}

// ColumnMap overrides the CSV column name used for one or more fields, so a
// source file with different header names (and columns in a different order)
// than the default format in the README can be read or written without a
// preprocessing step. Keys are the field names in csvFields; a field with no
// entry (or a nil map) falls back to using the field name itself as the
// column name.
type ColumnMap map[string]string

func (cm ColumnMap) name(field string) string {
	if cm != nil {
		if n, ok := cm[field]; ok && n != "" {
			return n
		}
	}
	return field
}

// ParseColumnMap parses a "field=Column Name" list, comma-separated, into a
// ColumnMap. Fields left out of s keep their default name. An empty s
// returns a nil map, which is equivalent to the default format.
func ParseColumnMap(s string) (ColumnMap, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	valid := make(map[string]bool, len(csvFields))
	for _, f := range csvFields {
		valid[f] = true
	}

	cm := make(ColumnMap)
	for _, pair := range strings.Split(s, ",") {
		field, name, hasEquals := strings.Cut(pair, "=")
		field = strings.TrimSpace(field)
		name = strings.TrimSpace(name)
		if !hasEquals || field == "" || name == "" {
			return nil, fmt.Errorf("invalid column mapping %q: expected field=Column Name", pair)
		}
		if !valid[field] {
			return nil, fmt.Errorf("invalid column mapping %q: unknown field %q (want one of %v)", pair, field, csvFields)
		}
		cm[field] = name
	}
	return cm, nil
}

// columnIndexes maps each internal field key to its column position in a CSV
// header, using colMap to translate field keys to the actual column names to
// look for. It's what lets input columns be reordered or renamed instead of
// having to appear in the fixed default order. A field in optionalCSVFields
// that has no matching column gets index -1 instead of an error, meaning
// every row should use that field's zero value.
func columnIndexes(header []string, colMap ColumnMap) (map[string]int, error) {
	positions := make(map[string]int, len(header))
	for i, name := range header {
		positions[name] = i
	}

	idx := make(map[string]int, len(csvFields))
	for _, field := range csvFields {
		name := colMap.name(field)
		pos, ok := positions[name]
		if !ok {
			if optionalCSVFields[field] {
				idx[field] = -1
				continue
			}
			return nil, fmt.Errorf("csv header is missing column %q (for field %q)", name, field)
		}
		idx[field] = pos
	}
	return idx, nil
}

// ParseCSV reads the flat CSV export format described in the README.
func ParseCSV(r io.Reader) ([]LineItem, error) {
	return ParseCSVWithColumns(r, nil)
}

// ParseCSVWithColumns is ParseCSV with a custom column mapping. A nil colMap
// is equivalent to ParseCSV.
func ParseCSVWithColumns(r io.Reader, colMap ColumnMap) ([]LineItem, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // checked manually below so the error names the bad row
	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading csv: %w", err)
	}
	if len(records) == 0 {
		return nil, nil
	}

	idx, err := columnIndexes(records[0], colMap)
	if err != nil {
		return nil, err
	}
	width := len(records[0])

	items := make([]LineItem, 0, len(records)-1)
	for i, row := range records[1:] {
		rowNum := i + 2 // 1-indexed, plus the header row
		if len(row) != width {
			return nil, fmt.Errorf("row %d: expected %d fields, got %d", rowNum, width, len(row))
		}

		lineNo, err := strconv.Atoi(strings.TrimSpace(row[idx["line_no"]]))
		if err != nil {
			return nil, fmt.Errorf("row %d: invalid line_no %q: %w", rowNum, row[idx["line_no"]], err)
		}
		qty, err := strconv.ParseFloat(strings.TrimSpace(row[idx["qty"]]), 64)
		if err != nil {
			return nil, fmt.Errorf("row %d: invalid qty %q: %w", rowNum, row[idx["qty"]], err)
		}
		unitPriceCents, err := parseDecimalTo2Places(row[idx["unit_price"]])
		if err != nil {
			return nil, fmt.Errorf("row %d: invalid unit_price %q: %w", rowNum, row[idx["unit_price"]], err)
		}

		var taxRateBps int64
		if i := idx["tax_rate"]; i >= 0 {
			taxRateBps, err = parseDecimalTo2Places(row[i])
			if err != nil {
				return nil, fmt.Errorf("row %d: invalid tax_rate %q: %w", rowNum, row[i], err)
			}
		}

		var description string
		if i := idx["description"]; i >= 0 {
			description = row[i]
		}

		var currency string
		if i := idx["currency"]; i >= 0 {
			currency = strings.ToUpper(strings.TrimSpace(row[i]))
		}

		items = append(items, LineItem{
			InvoiceID:      strings.TrimSpace(row[idx["invoice_id"]]),
			LineNo:         lineNo,
			SKU:            strings.TrimSpace(row[idx["sku"]]),
			Description:    description,
			Quantity:       qty,
			UnitPriceCents: unitPriceCents,
			TaxRateBps:     taxRateBps,
			Currency:       currency,
		})
	}
	return items, nil
}

// WriteCSV writes items back out in the flat CSV export format.
func WriteCSV(w io.Writer, items []LineItem) error {
	return WriteCSVWithColumns(w, items, nil)
}

// WriteCSVWithColumns is WriteCSV with a custom column mapping, used to name
// (and, since csvFields fixes the order, still order) the header. A nil
// colMap is equivalent to WriteCSV.
func WriteCSVWithColumns(w io.Writer, items []LineItem, colMap ColumnMap) error {
	cw := csv.NewWriter(w)
	header := make([]string, len(csvFields))
	for i, field := range csvFields {
		header[i] = colMap.name(field)
	}
	if err := cw.Write(header); err != nil {
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

// CSVToJSON reads CSV from r and writes the grouped JSON format to w. A nil
// colMap uses the default column names described in the README.
func CSVToJSON(r io.Reader, w io.Writer, colMap ColumnMap) error {
	items, err := ParseCSVWithColumns(r, colMap)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(GroupByInvoice(items))
}

// ParseJSON reads the grouped JSON format described in the README.
func ParseJSON(r io.Reader) ([]Invoice, error) {
	var invoices []Invoice
	if err := json.NewDecoder(r).Decode(&invoices); err != nil {
		return nil, fmt.Errorf("reading json: %w", err)
	}
	return invoices, nil
}

// JSONToCSV reads the grouped JSON format from r and writes CSV to w. A nil
// colMap uses the default column names described in the README.
func JSONToCSV(r io.Reader, w io.Writer, colMap ColumnMap) error {
	invoices, err := ParseJSON(r)
	if err != nil {
		return err
	}
	return WriteCSVWithColumns(w, Flatten(invoices), colMap)
}

// ValidateItems checks flat line items for problems that parsing alone won't
// catch: every line needs an invoice to belong to, and line_no is only
// meaningful as an ordering/reference key if it's unique within its invoice.
func ValidateItems(items []LineItem) error {
	seen := make(map[string]map[int]bool)
	for _, li := range items {
		if li.InvoiceID == "" {
			return fmt.Errorf("line %d: empty invoice_id", li.LineNo)
		}
		lines, ok := seen[li.InvoiceID]
		if !ok {
			lines = make(map[int]bool)
			seen[li.InvoiceID] = lines
		}
		if lines[li.LineNo] {
			return fmt.Errorf("invoice %s: duplicate line_no %d", li.InvoiceID, li.LineNo)
		}
		lines[li.LineNo] = true
	}
	return nil
}

// Validate parses r as the given format ("csv" or "json") and checks it with
// ValidateItems, without writing any output.
func Validate(r io.Reader, format string, colMap ColumnMap) error {
	var items []LineItem
	switch format {
	case "csv":
		parsed, err := ParseCSVWithColumns(r, colMap)
		if err != nil {
			return err
		}
		items = parsed
	case "json":
		invoices, err := ParseJSON(r)
		if err != nil {
			return err
		}
		items = Flatten(invoices)
	default:
		return fmt.Errorf("unsupported format for -validate: %s", format)
	}
	return ValidateItems(items)
}
