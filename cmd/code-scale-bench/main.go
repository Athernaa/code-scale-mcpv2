package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Athernaa/code-scale-mcpv2/internal/benchmark"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "run" {
		fmt.Fprintln(os.Stderr, "usage: code-scale-bench run [flags]")
		os.Exit(2)
	}
	flags := flag.NewFlagSet("run", flag.ExitOnError)
	corpus := flags.String("corpus", "benchmarks/corpus.json", "benchmark corpus JSON")
	mode := flags.String("mode", "all", "manual, panoramic, scoped_panoramic, primitive, phase7, phase7_no_early_stop, or all")
	task := flags.String("task", "", "run one task ID")
	category := flags.String("category", "", "run one category")
	output := flags.String("output", "benchmarks/reports/latest.json", "JSON report path")
	markdown := flags.String("markdown", "", "Markdown report path; defaults beside JSON output")
	tokenizer := flags.String("tokenizer", "", "tokenizer override")
	budgets := flags.String("budgets", "512,2048,8000,32000", "comma-separated context budgets")
	repeat := flags.Int("repeat", 1, "repetitions per mode/task/budget")
	jsonStdout := flags.Bool("json", false, "also write the JSON report to stdout")
	_ = flags.Parse(os.Args[2:])

	parsedBudgets, err := benchmark.ParseBudgets(*budgets)
	if err != nil {
		fatal(err)
	}
	markdownPath := *markdown
	if markdownPath == "" {
		ext := filepath.Ext(*output)
		markdownPath = (*output)[:len(*output)-len(ext)] + ".md"
	}
	report, err := benchmark.Run(context.Background(), benchmark.Config{CorpusPath: *corpus, OutputPath: *output, MarkdownPath: markdownPath, Mode: *mode, TaskID: *task, Category: *category, Tokenizer: *tokenizer, Budgets: parsedBudgets, Repeat: *repeat})
	if err != nil {
		fatal(err)
	}
	if err := benchmark.WriteReport(report, *output, markdownPath); err != nil {
		fatal(err)
	}
	fmt.Printf("benchmark report: %s\nmarkdown report: %s\nstatus: %s\n", *output, markdownPath, benchmark.Status(report))
	if *jsonStdout {
		data, err := os.ReadFile(*output)
		if err != nil {
			fatal(err)
		}
		fmt.Print(string(data))
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
