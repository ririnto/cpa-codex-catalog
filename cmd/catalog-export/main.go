package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ririnto/cpa-codex-catalog/internal/catalog"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("catalog-export", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	basePath := flags.String("base", "", "required base catalog JSON path")
	overridesPath := flags.String("overrides", "", "optional overrides JSON path")
	outPath := flags.String("out", "", "required output JSON path")
	force := flags.Bool("force", false, "replace an existing output file")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintln(stderr, "invalid command line options")
		return 2
	}
	if *basePath == "" || *outPath == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "--base and --out are required")
		return 2
	}
	if err := rejectInputOutputAliases(*outPath, *basePath, *overridesPath); err != nil {
		fmt.Fprintln(stderr, "catalog export failed")
		return 1
	}
	data, err := catalog.Load(*basePath, *overridesPath)
	if err != nil {
		fmt.Fprintln(stderr, "catalog export failed")
		return 1
	}
	if err := writeExport(*outPath, data, *force); err != nil {
		fmt.Fprintln(stderr, "catalog export failed")
		return 1
	}
	fmt.Fprintln(stdout, "Catalog exported.")
	return 0
}

func rejectInputOutputAliases(outPath string, inputPaths ...string) error {
	outputPath, err := cleanAbsolutePath(outPath)
	if err != nil {
		return errors.New("unable to resolve export path")
	}
	outputInfo, outputErr := os.Stat(outputPath)
	if outputErr != nil && !errors.Is(outputErr, os.ErrNotExist) {
		return errors.New("unable to inspect export path")
	}
	for _, inputPath := range inputPaths {
		if inputPath == "" {
			continue
		}
		absoluteInputPath, err := cleanAbsolutePath(inputPath)
		if err != nil {
			return errors.New("unable to resolve input path")
		}
		if outputPath == absoluteInputPath {
			return errors.New("output path aliases an input")
		}
		inputInfo, inputErr := os.Stat(absoluteInputPath)
		if inputErr != nil {
			if errors.Is(inputErr, os.ErrNotExist) {
				continue
			}
			return errors.New("unable to inspect input path")
		}
		if outputErr == nil && os.SameFile(outputInfo, inputInfo) {
			return errors.New("output path aliases an input")
		}
	}
	return nil
}

func cleanAbsolutePath(path string) (string, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolutePath), nil
}

func writeExport(outPath string, data []byte, force bool) error {
	if outPath == "" {
		return errors.New("output path is required")
	}
	directory := filepath.Dir(outPath)
	temporary, err := os.CreateTemp(directory, ".cpa-codex-catalog-")
	if err != nil {
		return errors.New("unable to create temporary export")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return errors.New("unable to secure temporary export")
	}
	written, err := temporary.Write(data)
	if err != nil || written != len(data) {
		temporary.Close()
		return errors.New("unable to write temporary export")
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return errors.New("unable to sync temporary export")
	}
	if err := temporary.Close(); err != nil {
		return errors.New("unable to close temporary export")
	}
	if force {
		if err := os.Rename(temporaryPath, outPath); err != nil {
			return errors.New("unable to replace export")
		}
		return nil
	}
	if err := os.Link(temporaryPath, outPath); err != nil {
		return errors.New("output exists or cannot be created")
	}
	return nil
}
