package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	fromFlag := flag.String("from", "", "input format: csv or json (default: inferred from -in's extension)")
	toFlag := flag.String("to", "", "output format: csv or json (default: inferred from -out's extension)")
	inPath := flag.String("in", "", "input file (default: stdin)")
	outPath := flag.String("out", "", "output file (default: stdout)")
	csvColumns := flag.String("csv-columns", "", `override CSV column names, e.g. "invoice_id=Invoice Number,unit_price=Amount" (default: names in README, in any order)`)
	flag.Parse()

	colMap, err := ParseColumnMap(*csvColumns)
	if err != nil {
		fail(err)
	}

	in := os.Stdin
	if *inPath != "" {
		f, err := os.Open(*inPath)
		if err != nil {
			fail(err)
		}
		defer f.Close()
		in = f
	}

	out := os.Stdout
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			fail(err)
		}
		defer f.Close()
		out = f
	}

	from := *fromFlag
	if from == "" {
		from = formatFromPath(*inPath)
	}
	to := *toFlag
	if to == "" {
		to = formatFromPath(*outPath)
	}
	if from == "" || to == "" {
		fail(fmt.Errorf("specify -from/-to, or use -in/-out with .csv/.json extensions"))
	}

	switch {
	case from == "csv" && to == "json":
		err = CSVToJSON(in, out, colMap)
	case from == "json" && to == "csv":
		err = JSONToCSV(in, out, colMap)
	case from == to:
		err = fmt.Errorf("input and output format are both %q, nothing to convert", from)
	default:
		err = fmt.Errorf("unsupported conversion: %s to %s", from, to)
	}
	if err != nil {
		fail(err)
	}
}

func formatFromPath(path string) string {
	switch {
	case strings.HasSuffix(path, ".csv"):
		return "csv"
	case strings.HasSuffix(path, ".json"):
		return "json"
	default:
		return ""
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "linebridge:", err)
	os.Exit(1)
}
