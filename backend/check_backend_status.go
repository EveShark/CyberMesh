package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("CyberMesh Backend - Build Status Check")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println()

	status := make(map[string]bool)
	issues := []string{}

	// Check 1: Binary exists
	fmt.Println("[1/6] Checking if binary exists...")
	if _, err := os.Stat("bin/backend.exe"); err == nil {
		fmt.Println("  [OK] Backend binary exists")
		status["binary"] = true
	} else {
		fmt.Println("  [FAIL] Backend binary missing")
		issues = append(issues, "Need to run: go build -o bin/backend.exe ./cmd/cybermesh/main.go")
		status["binary"] = false
	}

	// Check 2: .env file exists
	fmt.Println("[2/6] Checking configuration...")
	if _, err := os.Stat(".env"); err == nil {
		fmt.Println("  [OK] .env file exists")
		status["config"] = true
	} else {
		fmt.Println("  [FAIL] .env file missing")
		issues = append(issues, "Need .env file with Kafka/DB credentials")
		status["config"] = false
	}

	// Check 3: Source code packages
	fmt.Println("[3/6] Checking source code packages...")
	packages := []string{
		"pkg/state",
		"pkg/consensus",
		"pkg/mempool",
		"pkg/ingest/kafka",
		"pkg/storage",
		"pkg/api",
		"pkg/p2p",
	}
	allPackagesExist := true
	for _, pkg := range packages {
		if _, err := os.Stat(pkg); err == nil {
			fmt.Printf("  [OK] %s\n", pkg)
		} else {
			fmt.Printf("  [MISS] %s\n", pkg)
			allPackagesExist = false
		}
	}
	status["packages"] = allPackagesExist

	// Check 4: Test files
	fmt.Println("[4/6] Checking for test files...")
	testCount := 0
	filepath.Walk("pkg", func(path string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(info.Name(), "_test.go") {
			testCount++
		}
		return nil
	})
	if testCount > 0 {
		fmt.Printf("  [OK] Found %d test files\n", testCount)
		status["tests"] = true
	} else {
		fmt.Println("  [WARN] No test files found")
		issues = append(issues, "No tests - code is untested")
		status["tests"] = false
	}

	// Check 5: Database certs (CockroachDB)
	fmt.Println("[5/6] Checking database certificates...")
	if _, err := os.Stat("certs/root.crt"); err == nil {
		fmt.Println("  [OK] CockroachDB root cert exists")
		status["db_certs"] = true
	} else {
		fmt.Println("  [WARN] CockroachDB cert missing (may use sslmode=require without cert)")
		status["db_certs"] = false
	}

	// Check 6: Code statistics
	fmt.Println("[6/6] Code statistics...")
	goFiles := 0
	filepath.Walk("pkg", func(path string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(info.Name(), ".go") {
			goFiles++
		}
		return nil
	})
	fmt.Printf("  [INFO] %d Go source files\n", goFiles)

	// Summary
	fmt.Println()
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("SUMMARY")
	fmt.Println(strings.Repeat("=", 70))

	allGood := true
	for _, v := range status {
		if !v {
			allGood = false
			break
		}
	}

	if allGood {
		fmt.Println("[OK] Backend is ready to run")
		fmt.Println()
		fmt.Println("To start the backend:")
		fmt.Println("  .\\bin\\backend.exe")
	} else {
		fmt.Println("[WARN] Backend has issues")
		fmt.Println()
		if len(issues) > 0 {
			fmt.Println("Issues found:")
			for _, issue := range issues {
				fmt.Printf("  - %s\n", issue)
			}
		}
	}

	fmt.Println()
	fmt.Println(strings.Repeat("=", 70))
}
