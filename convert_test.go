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

func TestCSVToJSONToCSV(t *testing.T) {
	input := "invoice_id,line_no,sku,description,qty,unit_price,tax_rate,currency\n" +
		"INV-1,1,SKU-1,Widget,2,19.99,7.25,USD\n"

	var jsonBuf bytes.Buffer
	if err := CSVToJSON(strings.NewReader(input), &jsonBuf); err != nil {
		t.Fatalf("CSVToJSON: %v", err)
	}
	if !strings.Contains(jsonBuf.String(), `"total_cents": 3998`) {
		t.Fatalf("expected total_cents 3998 in json output, got:\n%s", jsonBuf.String())
	}

	var csvBuf bytes.Buffer
	if err := JSONToCSV(&jsonBuf, &csvBuf); err != nil {
		t.Fatalf("JSONToCSV: %v", err)
	}
	if !strings.Contains(csvBuf.String(), "INV-1,1,SKU-1,Widget,2,19.99,7.25,USD") {
		t.Fatalf("expected csv row in output, got:\n%s", csvBuf.String())
	}
}
