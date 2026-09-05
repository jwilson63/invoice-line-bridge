# invoice-line-bridge

Converts invoice line items between two formats I keep having to bridge by hand:

- a flat CSV export, the kind most accounting or billing systems produce, and
- a JSON format grouped by invoice, with money stored as integer cents.

The CSV is easy to open in a spreadsheet but useless for arithmetic: dollar amounts
are decimal strings, there's no grouping by invoice, and every consumer ends up
reinventing its own rounding rules. The JSON groups line items under their invoice
and pre-computes each line's total in cents, so nothing downstream has to trust
float comparisons on money.

## CSV format

```
invoice_id,line_no,sku,description,qty,unit_price,tax_rate,currency
INV-1001,1,SKU-100,Consulting hours,8,150.00,7.25,USD
INV-1001,2,SKU-200,Late fee,1,25.00,0,USD
```

`tax_rate` is a percentage (7.25 means 7.25%) and may be left blank, which is
treated as 0. `qty` can be negative for credits and returns, and can be
fractional (hours, weight, partial units).

Columns can appear in any order, as long as the header names match. If your
CSV export uses different header names, pass `-csv-columns` to map them:

```
./linebridge -in export.csv -out invoices.json \
  -csv-columns "invoice_id=Invoice Number,unit_price=Amount,tax_rate=Tax %"
```

Only the fields you list need mapping; anything left out keeps its default
name (`invoice_id`, `line_no`, `sku`, `description`, `qty`, `unit_price`,
`tax_rate`, `currency`). The same mapping applies to CSV output, so it can
also be used to write a CSV with a different header.

## JSON format

```json
[
  {
    "invoice_id": "INV-1001",
    "line_items": [
      {
        "line_no": 1,
        "sku": "SKU-100",
        "description": "Consulting hours",
        "quantity": 8,
        "unit_price_cents": 15000,
        "tax_rate_bps": 725,
        "currency": "USD",
        "total_cents": 120000
      }
    ]
  }
]
```

Money is stored as integer cents and tax rates as integer basis points (1/100th
of a percent) so nothing downstream has to do float comparisons on currency.

## Usage

```
go build -o linebridge .

./linebridge -in invoices.csv -out invoices.json
./linebridge -in invoices.json -out invoices.csv

# or pipe it, with explicit formats since stdin/stdout have no file extension
cat invoices.csv | ./linebridge -from csv -to json > invoices.json
```

## Rounding

A line's total is `quantity * unit_price`, rounded to the nearest cent, half
away from zero. Decimal amounts in the CSV with more than two decimal places
round the same way when parsed.

## Status

Early. Columns can be reordered or renamed (see `-csv-columns` above), but
every field is still required — there's no support yet for a CSV that's
missing a column entirely (e.g. no `tax_rate` column at all, rather than a
blank one).

## License

MIT
