package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/openv/requirements-platform/internal/domain/templates"
)

func main() {
	// Generate the CNC mill example template
	exampleData, err := templates.GenerateCncMillExampleJSON()
	if err != nil {
		log.Fatalf("Failed to generate CNC mill example: %v", err)
	}

	// Get current working directory (should be project root when run from make/scripts)
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get working directory: %v", err)
	}

	outputDir := filepath.Join(cwd, "examples", "cnc-mill")
	outputFile := filepath.Join(outputDir, "project.json")

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	// Write to file
	if err := ioutil.WriteFile(outputFile, exampleData, 0644); err != nil {
		log.Fatalf("Failed to write file: %v", err)
	}

	fmt.Printf("Generated template at: %s\n", outputFile)

	// Pretty print stats
	var export map[string]interface{}
	if err := json.Unmarshal(exampleData, &export); err == nil {
		if artifacts, ok := export["artifacts"].([]interface{}); ok {
			fmt.Printf("Artifacts: %d\n", len(artifacts))
		}
		if links, ok := export["links"].([]interface{}); ok {
			fmt.Printf("Links: %d\n", len(links))
		}
	}
}
