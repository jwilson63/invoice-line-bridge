package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseDecimalTo2Places(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    int64
		wantErr bool
	}{
		{"plain cents", "19.99", 1999, false},
		{"one decimal place", "19.9", 1990, false},
		{"whole number", "19", 1900, false},
		{"empty string defaults to zero", "", 0, false},
		{"whitespace only defaults to zero", "  ", 0, false},
		{"rounds half up", "19.995", 2000, false},
		{"rounds down below half", "0.004", 0, false},
		{"rounds a half cent up", "0.005", 1, false},
		{"negative amount", "-5.5", -550, false},
		{"explicit plus sign", "+3.2", 320, false},
		{"leading dot", ".5", 50, false},
		{"not a number", "abc", 0, true},
		{"two decimal points", "1.2.3", 0, true},
		{"trailing garbage", "12.99x", 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseDecimalTo2Places(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("parseDecimalTo2Places(%q) = %d, want error", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDecimalTo2Places(%q) returned unexpected error: %v", c.in, err)
			}
			if got != c.want {
				t.Fatalf("parseDecimalTo2Places(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

func TestLineItemTotalCents(t *testing.T) {
	cases := []struct {
		name           string
		qty            float64
		unitPriceCents int64
		want           int64
	}{
		{"exact multiplication", 3, 335, 1005},
		{"half cent rounds up", 0.5, 1, 1},
		{"one and a half cents rounds up", 1.5, 1, 2},
		{"negative half rounds away from zero", -0.5, 1, -1},
		{"zero quantity is zero total", 0, 999, 0},
		{"fractional quantity", 2.5, 400, 1000},
		{"negative quantity is a credit", -1, 2500, -2500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			li := LineItem{Quantity: c.qty, UnitPriceCents: c.unitPriceCents}
			if got := li.TotalCents(); got != c.want {
				t.Fatalf("TotalCents() = %d, want %d", got, c.want)
			}
		})
	}
}

func TestParseCSVAwkwardRows(t *testing.T) {
	input := `invoice_id,line_no,sku,description,qty,unit_price,tax_rate,currency
INV-1,1,SKU-1,"Widget, ""Deluxe"" Edition",2,19.99,7.25,usd
INV-1,2,SKU-2,Late fee,1,-15,0,usd
INV-1,3,SKU-3,Restocking credit,-1,25,,usd
INV-2,1,SKU-4,Café subscription — Q3,0,49,8.5,USD
`
	items, err := ParseCSV(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseCSV returned error: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("got %d items, want 4", len(items))
	}

	if got, want := items[0].Description, `Widget, "Deluxe" Edition`; got != want {
		t.Errorf("quoted description = %q, want %q", got, want)
	}
	if got := items[1].UnitPriceCents; got != -1500 {
		t.Errorf("negative unit_price (a discount line) = %d, want -1500", got)
	}
	if got := items[2].Quantity; got != -1 {
		t.Errorf("credit line qty = %v, want -1", got)
	}
	if got := items[2].TaxRateBps; got != 0 {
		t.Errorf("blank tax_rate = %d, want 0 (defaulted)", got)
	}
	if got, want := items[3].Currency, "USD"; got != want {
		t.Errorf("currency = %q, want %q (should be uppercased)", got, want)
	}
	if got, want := items[3].Description, "Café subscription — Q3"; got != want {
		t.Errorf("unicode description = %q, want %q", got, want)
	}
	if got := items[3].TotalCents(); got != 0 {
		t.Errorf("zero-quantity line total = %d, want 0", got)
	}
}

func TestParseCSVWithColumnsCustomNames(t *testing.T) {
	colMap, err := ParseColumnMap("invoice_id=Invoice Number,line_no=Line,sku=Item,unit_price=Amount,tax_rate=Tax %")
	if err != nil {
		t.Fatalf("ParseColumnMap: %v", err)
	}

	input := "Invoice Number,Line,Item,description,qty,Amount,Tax %,currency\n" +
		"INV-1,1,SKU-1,Widget,2,19.99,7.25,USD\n"
	items, err := ParseCSVWithColumns(strings.NewReader(input), colMap)
	if err != nil {
		t.Fatalf("ParseCSVWithColumns: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if got, want := items[0].InvoiceID, "INV-1"; got != want {
		t.Errorf("invoice_id = %q, want %q", got, want)
	}
	if got, want := items[0].UnitPriceCents, int64(1999); got != want {
		t.Errorf("unit_price (from Amount column) = %d, want %d", got, want)
	}
}

func TestParseCSVReorderedDefaultColumns(t *testing.T) {
	input := "currency,qty,invoice_id,line_no,sku,description,unit_price,tax_rate\n" +
		"USD,2,INV-1,1,SKU-1,Widget,19.99,7.25\n"
	items, err := ParseCSV(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}
	if len(items) != 1 || items[0].InvoiceID != "INV-1" || items[0].UnitPriceCents != 1999 {
		t.Fatalf("got %+v, want a single INV-1 item with unit_price_cents 1999", items)
	}
}

func TestParseColumnMapErrors(t *testing.T) {
	cases := []string{
		"invoice_id",        // missing '='
		"unknown_field=Foo", // not a recognized field
		"invoice_id=",       // empty column name
		"=Invoice Number",   // empty field
	}
	for _, in := range cases {
		if _, err := ParseColumnMap(in); err == nil {
			t.Errorf("ParseColumnMap(%q): expected error, got nil", in)
		}
	}
}

func TestWriteCSVWithColumnsCustomHeader(t *testing.T) {
	colMap, err := ParseColumnMap("invoice_id=Invoice Number,unit_price=Amount")
	if err != nil {
		t.Fatalf("ParseColumnMap: %v", err)
	}

	items := []LineItem{{InvoiceID: "INV-1", LineNo: 1, SKU: "SKU-1", Description: "Widget", Quantity: 2, UnitPriceCents: 1999, TaxRateBps: 725, Currency: "USD"}}
	var buf bytes.Buffer
	if err := WriteCSVWithColumns(&buf, items, colMap); err != nil {
		t.Fatalf("WriteCSVWithColumns: %v", err)
	}
	if !strings.HasPrefix(buf.String(), "Invoice Number,line_no,sku,description,qty,Amount,tax_rate,currency\n") {
		t.Fatalf("unexpected header, got:\n%s", buf.String())
	}

	roundTripped, err := ParseCSVWithColumns(&buf, colMap)
	if err != nil {
		t.Fatalf("round trip ParseCSVWithColumns: %v", err)
	}
	if len(roundTripped) != 1 || roundTripped[0].InvoiceID != "INV-1" || roundTripped[0].UnitPriceCents != 1999 {
		t.Fatalf("round trip mismatch: got %+v", roundTripped)
	}
}

func TestParseCSVRowLengthMismatch(t *testing.T) {
	input := "invoice_id,line_no,sku,description,qty,unit_price,tax_rate,currency\n" +
		"INV-1,1,SKU-1,Widget,2,19.99\n"
	if _, err := ParseCSV(strings.NewReader(input)); err == nil {
		t.Fatal("expected error for row with missing fields, got nil")
	}
}

func TestParseCSVWrongHeader(t *testing.T) {
	input := "id,line,item\nINV-1,1,SKU-1\n"
	if _, err := ParseCSV(strings.NewReader(input)); err == nil {
		t.Fatal("expected error for unrecognized header, got nil")
	}
}

func TestGroupByInvoicePreservesOrder(t *testing.T) {
	input := `invoice_id,line_no,sku,description,qty,unit_price,tax_rate,currency
INV-100,1,SKU-1,First item,3,19.99,7.25,USD
INV-100,2,SKU-2,Second item,-1,5.5,0,USD
INV-200,1,SKU-3,Only item,1,100,10,EUR
`
	items, err := ParseCSV(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseCSV: %v", err)
	}

	invoices := GroupByInvoice(items)
	if len(invoices) != 2 {
		t.Fatalf("got %d invoices, want 2", len(invoices))
	}
	if invoices[0].InvoiceID != "INV-100" || len(invoices[0].LineItems) != 2 {
		t.Fatalf("first invoice grouping is wrong: %+v", invoices[0])
	}
	if invoices[1].InvoiceID != "INV-200" || len(invoices[1].LineItems) != 1 {
		t.Fatalf("second invoice grouping is wrong: %+v", invoices[1])
	}

	roundTripped := Flatten(invoices)
	if len(roundTripped) != len(items) {
		t.Fatalf("got %d items after round trip, want %d", len(roundTripped), len(items))
	}
	for i := range items {
		if roundTripped[i].InvoiceID != items[i].InvoiceID ||
			roundTripped[i].SKU != items[i].SKU ||
			roundTripped[i].UnitPriceCents != items[i].UnitPriceCents {
			t.Fatalf("item %d changed across round trip: got %+v, want %+v", i, roundTripped[i], items[i])
		}
	}
}

func TestValidateCatchesDuplicateLineNo(t *testing.T) {
	input := "invoice_id,line_no,sku,description,qty,unit_price,tax_rate,currency\n" +
		"INV-1,1,SKU-1,Widget,2,19.99,7.25,USD\n" +
		"INV-1,1,SKU-2,Duplicate line_no,1,5,0,USD\n"
	if err := Validate(strings.NewReader(input), "csv", nil); err == nil {
		t.Fatal("expected error for duplicate line_no within an invoice, got nil")
	}
}

func TestValidateCatchesEmptyInvoiceID(t *testing.T) {
	input := "invoice_id,line_no,sku,description,qty,unit_price,tax_rate,currency\n" +
		",1,SKU-1,Widget,2,19.99,7.25,USD\n"
	if err := Validate(strings.NewReader(input), "csv", nil); err == nil {
		t.Fatal("expected error for empty invoice_id, got nil")
	}
}

func TestValidateAcceptsSameLineNoAcrossDifferentInvoices(t *testing.T) {
	input := "invoice_id,line_no,sku,description,qty,unit_price,tax_rate,currency\n" +
		"INV-1,1,SKU-1,Widget,2,19.99,7.25,USD\n" +
		"INV-2,1,SKU-2,Widget,1,5,0,USD\n"
	if err := Validate(strings.NewReader(input), "csv", nil); err != nil {
		t.Fatalf("Validate: unexpected error: %v", err)
	}
}

func TestValidateJSON(t *testing.T) {
	input := `[{"invoice_id":"INV-1","line_items":[
		{"line_no":1,"sku":"SKU-1","quantity":1,"unit_price_cents":100},
		{"line_no":1,"sku":"SKU-2","quantity":1,"unit_price_cents":200}
	]}]`
	if err := Validate(strings.NewReader(input), "json", nil); err == nil {
		t.Fatal("expected error for duplicate line_no, got nil")
	}
}

func TestValidateUnsupportedFormat(t *testing.T) {
	if err := Validate(strings.NewReader(""), "xml", nil); err == nil {
		t.Fatal("expected error for unsupported format, got nil")
	}
}

func TestCSVToJSONToCSV(t *testing.T) {
	input := "invoice_id,line_no,sku,description,qty,unit_price,tax_rate,currency\n" +
		"INV-1,1,SKU-1,Widget,2,19.99,7.25,USD\n"

	var jsonBuf bytes.Buffer
	if err := CSVToJSON(strings.NewReader(input), &jsonBuf, nil); err != nil {
		t.Fatalf("CSVToJSON: %v", err)
	}
	if !strings.Contains(jsonBuf.String(), `"total_cents": 3998`) {
		t.Fatalf("expected total_cents 3998 in json output, got:\n%s", jsonBuf.String())
	}

	var csvBuf bytes.Buffer
	if err := JSONToCSV(&jsonBuf, &csvBuf, nil); err != nil {
		t.Fatalf("JSONToCSV: %v", err)
	}
	if !strings.Contains(csvBuf.String(), "INV-1,1,SKU-1,Widget,2,19.99,7.25,USD") {
		t.Fatalf("expected csv row in output, got:\n%s", csvBuf.String())
	}
}
